package d520

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

// d520Run is the immutable, cancellable blocking D520 Run. Each Execute
// creates fresh per-call execution state and owns one UDP socket for the
// duration of that call.
type d520Run struct {
	definition runDefinition
}

// ka2Exchange identifies a sent KA2 request. The first-bootstrap distinction
// is retained so response type 6 cannot be accepted for later Type 1 packets.
type ka2Exchange struct {
	requestType         byte
	firstBootstrapType1 bool
}

// execution holds the per-call transient state owned by one Execute: the login
// salt, Auth Info, KA2 Tail and serial, the set of already-sent KA exchanges,
// and the current socket. Nothing here is shared across calls or stored on the
// immutable run definition.
type execution struct {
	definition runDefinition
	observer   protocol.AuthenticationProtocolRunObserver
	exchange   *udpExchange

	salt        [4]byte
	authInfo    [16]byte
	hasAuthInfo bool

	tail   [4]byte
	serial byte

	ka1Sent bool
	ka2Sent map[byte]ka2Exchange
}

func newExecution(definition runDefinition, observer protocol.AuthenticationProtocolRunObserver) *execution {
	return &execution{
		definition: definition,
		observer:   observer,
		ka2Sent:    make(map[byte]ka2Exchange),
	}
}

// Execute runs the blocking D520 lifecycle: Challenge, Login (with bounded
// server-busy retry), KA1 + KA2 1-1-3 bootstrap, one
// AuthenticationEstablished notification, then periodic KA1 + KA2 1-3
// heartbeats. It returns nil on cancellation after the requested cleanup, or a
// stable public failure on any protocol failure.
func (r *d520Run) Execute(ctx context.Context, observer protocol.AuthenticationProtocolRunObserver) *protocol.AuthenticationProtocolRunFailure {
	exec := newExecution(r.definition, observer)
	ex, err := openUDPExchange(r.definition.login.clientIPv4, r.definition.cfg.serverAddress, r.definition.cfg.serverPort)
	if err != nil {
		return networkIOError("open socket", err).toFailure()
	}
	exec.exchange = ex
	defer exec.exchange.close()

	var failure *runError
	if failure = exec.challenge(ctx); failure == nil {
		if failure = exec.login(ctx); failure == nil {
			if failure = exec.bootstrap(ctx); failure == nil {
				failure = exec.heartbeatLoop(ctx)
			}
		}
	}
	return exec.finalize(ctx, failure)
}

// challenge requests the initial salt used by Login.
func (exec *execution) challenge(ctx context.Context) *runError {
	salt, failure := exec.requestChallenge(ctx, exec.definition.cfg.challengeTimeout)
	if failure != nil {
		return failure
	}
	exec.salt = salt
	return nil
}

// requestChallenge sends one Challenge request and returns the response salt.
// The classifier accepts only a valid Challenge response; any other datagram
// fails the exchange.
func (exec *execution) requestChallenge(ctx context.Context, timeout time.Duration) ([4]byte, *runError) {
	seed := randomChallengeSeed()
	req := buildChallengeRequest(seed, exec.definition.cfg.challengePadding)
	classify := func(datagram []byte) (exchangeResponse, error) {
		if _, err := parseChallengeResponse(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}
	resp, failure := exec.exchange.roundTrip(ctx, timeout, req, classify)
	if failure != nil {
		return [4]byte{}, failure
	}
	parsed, _ := parseChallengeResponse(resp)
	return parsed.salt, nil
}

// login sends the Login request and handles server-busy retry. busyMaxAttempts
// counts the initial request; only rejection code 0x02 sleeps a cancellable
// random backoff in the configured inclusive range and retries with the same
// request. Other rejection codes return immediately.
func (exec *execution) login(ctx context.Context) *runError {
	in := exec.definition.login
	in.authExtTail = randomAuthExtTail()
	loginReq, err := buildLoginRequest(in, exec.salt)
	if err != nil {
		return contractViolationError("login build", err)
	}
	classify := func(datagram []byte) (exchangeResponse, error) {
		if _, err := parseLoginResponse(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}

	maxAttempts := exec.definition.cfg.busyMaxAttempts
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, failure := exec.exchange.roundTrip(ctx, exec.definition.cfg.loginTimeout, loginReq, classify)
		if failure != nil {
			return failure
		}
		parsed, _ := parseLoginResponse(resp)
		switch parsed.kind {
		case loginResponseSuccess:
			exec.authInfo = parsed.authInfo
			exec.hasAuthInfo = true
			return nil
		case loginResponseRejection:
			if parsed.code != 0x02 {
				return rejectionError("login", parsed.code, nil)
			}
			if attempt+1 >= maxAttempts {
				return serverBusyExhaustedError("login", nil)
			}
			backoff := randomBackoff(exec.definition.cfg.busyBackoffMin, exec.definition.cfg.busyBackoffMax)
			if !sleepCancellable(ctx, backoff) {
				return networkTimeoutError("login busy backoff", ctx.Err())
			}
		}
	}
	return serverBusyExhaustedError("login", nil)
}

// bootstrap runs the initial KA1 + KA2 1-1-3 sequence and notifies the
// observer exactly once that authentication is established.
func (exec *execution) bootstrap(ctx context.Context) *runError {
	if failure := exec.sendKA1(ctx); failure != nil {
		return failure
	}
	if failure := exec.sendKA2(ctx, ka2Type1, true); failure != nil {
		return failure
	}
	if failure := exec.sendKA2(ctx, ka2Type1, false); failure != nil {
		return failure
	}
	if failure := exec.sendKA2(ctx, ka2Type3, false); failure != nil {
		return failure
	}
	exec.observer.AuthenticationEstablished()
	return nil
}

// heartbeatLoop waits the Profile heartbeat interval and then runs KA1 + KA2
// 1-3 until a failure or cancellation occurs.
func (exec *execution) heartbeatLoop(ctx context.Context) *runError {
	for {
		if !sleepCancellable(ctx, exec.definition.cfg.heartbeatInterval) {
			return networkTimeoutError("heartbeat wait", ctx.Err())
		}
		if failure := exec.sendKA1(ctx); failure != nil {
			return failure
		}
		if failure := exec.sendKA2(ctx, ka2Type1, false); failure != nil {
			return failure
		}
		if failure := exec.sendKA2(ctx, ka2Type3, false); failure != nil {
			return failure
		}
	}
}

// sendKA1 builds and sends one KA1 request using the login salt and current
// Auth Info, and accepts a structurally valid KA1 response.
func (exec *execution) sendKA1(ctx context.Context) *runError {
	passwordBytes, err := encodeProtocolText(exec.definition.login.password)
	if err != nil {
		return contractViolationError("ka1 build", err)
	}
	digestA := md5A(exec.salt[:], passwordBytes)
	timestamp := uint16(time.Now().Unix() % 0xFFFF)
	req, err := buildKA1Request(digestA, exec.authInfo, timestamp)
	if err != nil {
		return contractViolationError("ka1 build", err)
	}
	classify := exec.classifyKA1Response()
	exec.ka1Sent = true
	_, failure := exec.exchange.roundTrip(ctx, exec.definition.cfg.keepaliveTimeout, req, classify)
	return failure
}

// sendKA2 builds and sends one KA2 request with the current serial and tail.
// firstType1 selects the init version for the first Type1 packet. On an
// accepted response it replaces Tail and increments the serial (wrapping at
// 255).
func (exec *execution) sendKA2(ctx context.Context, typ byte, firstType1 bool) *runError {
	serial := exec.serial
	req, err := buildKA2Request(serial, typ, firstType1, exec.definition.cfg.keepAliveVersion, exec.tail, exec.definition.login.clientIPv4)
	if err != nil {
		return contractViolationError("ka2 build", err)
	}
	exec.ka2Sent[serial] = ka2Exchange{requestType: typ, firstBootstrapType1: firstType1}
	classify := exec.classifyKA2Response(serial, typ, firstType1)
	resp, failure := exec.exchange.roundTrip(ctx, exec.definition.cfg.keepaliveTimeout, req, classify)
	if failure != nil {
		return failure
	}
	parsed, err := parseKA2Response(resp, serial, typ, firstType1)
	if err != nil {
		return responseInvalidError("ka2 response", err)
	}
	exec.tail = parsed.tail
	exec.serial++
	return nil
}

// classifyKA1Response accepts the expected KA1 response and ignores a
// complete KA2 response provably belonging to an already-sent KA2 exchange.
func (exec *execution) classifyKA1Response() responseClassifier {
	return func(datagram []byte) (exchangeResponse, error) {
		if serial, typ, ok := inspectKA2Response(datagram); ok {
			if sent, found := exec.ka2Sent[serial]; found && ka2ResponseTypeCompatible(sent.requestType, sent.firstBootstrapType1, typ) {
				return responseIgnore, nil
			}
			return 0, fmt.Errorf("unexpected ka2 response serial %d type %d while waiting for ka1", serial, typ)
		}
		if parseKA1Response(datagram) == nil {
			return responseAccept, nil
		}
		return 0, fmt.Errorf("unexpected response while waiting for ka1")
	}
}

// classifyKA2Response accepts the expected KA2 response (matching serial and
// type) and ignores a complete prior KA1 response or a complete KA2 response
// for an already-sent serial/type. The expected response is checked before the
// stale-response rules.
func (exec *execution) classifyKA2Response(expectedSerial, expectedType byte, firstBootstrapType1 bool) responseClassifier {
	return func(datagram []byte) (exchangeResponse, error) {
		if _, err := parseKA2Response(datagram, expectedSerial, expectedType, firstBootstrapType1); err == nil {
			return responseAccept, nil
		}
		if serial, typ, ok := inspectKA2Response(datagram); ok {
			if sent, found := exec.ka2Sent[serial]; found && ka2ResponseTypeCompatible(sent.requestType, sent.firstBootstrapType1, typ) {
				return responseIgnore, nil
			}
			return 0, fmt.Errorf("unexpected ka2 response serial %d type %d while waiting for serial %d type %d", serial, typ, expectedSerial, expectedType)
		}
		if isCompleteKA1Response(datagram) && exec.ka1Sent {
			return responseIgnore, nil
		}
		return 0, fmt.Errorf("unexpected response while waiting for ka2")
	}
}

// finalize maps the outcome to a public failure. On cancellation it performs
// the requested cleanup and returns nil. On a protocol failure after Auth Info
// was supplied it first attempts bounded best-effort Logout, retaining any
// cleanup failure only in the diagnostic cause without changing the original
// code, description or handling recommendation.
func (exec *execution) finalize(ctx context.Context, failure *runError) *protocol.AuthenticationProtocolRunFailure {
	if cause := context.Cause(ctx); cause != nil {
		var cancellation protocol.AuthenticationProtocolRunCancellationCause
		if errors.As(cause, &cancellation) &&
			cancellation.CleanupRequirement == protocol.TerminateWithBestEffortLogout &&
			exec.hasAuthInfo {
			_ = exec.bestEffortLogout()
		}
		return nil
	}
	if failure == nil {
		return contractViolationError("execute", errors.New("run ended without cancellation or failure")).toFailure()
	}
	if exec.hasAuthInfo {
		if cleanupFailure := exec.bestEffortLogout(); cleanupFailure != nil {
			if failure.cause != nil {
				failure.cause = fmt.Errorf("%w; best-effort logout also failed: %v", failure.cause, cleanupFailure)
			} else {
				failure.cause = fmt.Errorf("best-effort logout also failed: %v", cleanupFailure)
			}
		}
	}
	return failure.toFailure()
}

// bestEffortLogout performs bounded best-effort cleanup using an independent
// context (never the already cancelled execution context). It requests a fresh
// Challenge for a fresh salt, falls back to the saved login salt if that
// fails, then sends one Logout and optionally accepts its ACK. It never blocks
// beyond these two bounded exchanges.
func (exec *execution) bestEffortLogout() *runError {
	logoutCtx := context.Background()
	salt, challengeFailure := exec.requestChallenge(logoutCtx, exec.definition.cfg.logoutTimeout)
	if challengeFailure != nil {
		salt = exec.salt
	}
	logoutReq, err := buildLogoutRequest(exec.definition.login, salt, exec.authInfo)
	if err != nil {
		return contractViolationError("logout build", err)
	}
	classify := func(datagram []byte) (exchangeResponse, error) {
		if err := parseLogoutACK(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}
	_, failure := exec.exchange.roundTrip(logoutCtx, exec.definition.cfg.logoutTimeout, logoutReq, classify)
	return failure
}

// sleepCancellable sleeps for d but returns false immediately if ctx is
// cancelled.
func sleepCancellable(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// randomChallengeSeed returns the LE u16 Challenge seed
// (timestamp_seconds + random) % 0xFFFF.
func randomChallengeSeed() uint16 {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return uint16(time.Now().Unix() % 0xFFFF)
	}
	random := int64(binary.BigEndian.Uint16(buf[:]))
	timestamp := time.Now().Unix()
	return uint16((timestamp + random) % 0xFFFF)
}

// randomAuthExtTail returns the two-byte Login auth-extension tail.
func randomAuthExtTail() [2]byte {
	var tail [2]byte
	if _, err := rand.Read(tail[:]); err != nil {
		return [2]byte{0x12, 0x34}
	}
	return tail
}

// randomBackoff returns a random duration in the inclusive range [min, max].
func randomBackoff(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return min
	}
	return projectBackoff(binary.BigEndian.Uint64(buf[:]), min, max)
}

// projectBackoff maps a supplied uint64 sample into the closed duration
// interval [min, max], so every integer nanosecond in that interval is
// reachable. It is deterministic and has no randomness source of its own;
// randomBackoff supplies the sample. When max <= min it returns min.
func projectBackoff(sample uint64, min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	span := uint64(max-min) + 1
	return min + time.Duration(sample%span)
}
