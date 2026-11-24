package d520

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	protocol "sidravia/internal/daemon/authentication/protocol"
)

// d520Run is the immutable, cancellable blocking D520 Run. Each Execute
// creates fresh per-call execution state and owns one UDP socket for the
// duration of that call.
type d520Run struct {
	definition  runDefinition
	diagnostics protocol.AuthenticationProtocolDiagnostics
}

// execution holds the per-call transient state owned by one Execute: the login
// salt, Auth Info, KA2 Tail and serial, the set of already-sent KA exchanges,
// and the current socket. Nothing here is shared across calls or stored on the
// immutable run definition.
type execution struct {
	definition  runDefinition
	observer    protocol.AuthenticationProtocolRunObserver
	exchange    *udpExchange
	diagnostics protocol.AuthenticationProtocolDiagnostics

	salt        [4]byte
	authInfo    [16]byte
	hasAuthInfo bool

	tail   [4]byte
	serial byte

	ka1Sent      bool
	ka2Sent      map[byte]ka2Exchange
	experimental experimentalState
}

func newExecution(definition runDefinition, observer protocol.AuthenticationProtocolRunObserver, diagnostics protocol.AuthenticationProtocolDiagnostics) *execution {
	return &execution{
		definition:  definition,
		observer:    observer,
		diagnostics: diagnostics,
		ka2Sent:     make(map[byte]ka2Exchange),
	}
}

// Execute runs the blocking D520 lifecycle: Challenge, Login (with bounded
// server-busy retry), KA1 + KA2 1-1-3 bootstrap, one
// AuthenticationEstablished notification, then periodic KA1 + KA2 1-3
// heartbeats. It returns nil on cancellation after the requested cleanup, or a
// stable public failure on any protocol failure.
func (r *d520Run) Execute(ctx context.Context, observer protocol.AuthenticationProtocolRunObserver) (result *protocol.AuthenticationProtocolRunFailure) {
	exec := newExecution(r.definition, observer, r.diagnostics)
	ex, err := openUDPExchange(r.definition.socketSourceIPv4, r.definition.cfg.localPort, r.definition.cfg.serverAddress, r.definition.cfg.serverPort)
	if err != nil {
		recordNetworkFailure(r.diagnostics, "open_socket", err)
		return networkIOError("open socket", err).toFailure()
	}
	networkObserver, observesNetwork := observer.(protocol.AuthenticationProtocolRunNetworkObserver)
	openingAccepted := false
	defer func() {
		closeErr := ex.close()
		if closeErr != nil {
			recordNetworkFailure(r.diagnostics, "close_socket", closeErr)
		}
		if openingAccepted {
			networkObserver.ProtocolSocketClosed(closeErr == nil)
		}
		if closeErr != nil {
			if result == nil {
				result = networkIOError("close socket", closeErr).toFailure()
			} else {
				result.DiagnosticCause = errors.Join(result.DiagnosticCause, closeErr)
			}
		}
	}()
	exec.exchange = ex
	if observesNetwork {
		local := ex.conn.LocalAddr().(*net.UDPAddr).AddrPort()
		remote := ex.conn.RemoteAddr().(*net.UDPAddr).AddrPort()
		local = netip.AddrPortFrom(local.Addr().Unmap(), local.Port())
		remote = netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())
		if err := networkObserver.ProtocolSocketOpened(local, remote); err != nil {
			return contractViolationError("socket observation", err).toFailure()
		}
		openingAccepted = true
	}

	var failure *runError
	loginCtx := ctx
	if exec.definition.cfg.jluExperimental {
		cfg := exec.definition.cfg
		budget := cfg.challengeTimeout + time.Duration(cfg.busyMaxAttempts)*cfg.loginTimeout + time.Duration(cfg.busyMaxAttempts-1)*cfg.busyBackoffMax + 5*cfg.keepaliveTimeout
		var cancel context.CancelFunc
		loginCtx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
		exec.experimental.interval = cfg.heartbeatInterval
	}
	if failure = exec.challenge(loginCtx); failure == nil {
		if failure = exec.login(loginCtx); failure == nil {
			if failure = exec.bootstrap(loginCtx); failure == nil {
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
	if exec.definition.cfg.jluExperimental {
		var correlation [3]byte
		if _, err := rand.Read(correlation[:]); err != nil {
			return [4]byte{}, contractViolationError("challenge randomness", err)
		}
		req[1] = correlation[0]
		seed = binary.LittleEndian.Uint16(correlation[1:])
		binary.LittleEndian.PutUint16(req[2:4], seed)
	}
	classify := func(datagram []byte) (exchangeResponse, error) {
		if exec.definition.cfg.jluExperimental {
			if len(datagram) == 0 || datagram[0] != 2 {
				return responseIgnore, nil
			}
			if len(datagram) < 24 {
				return 0, invalidResponse(protocol.ExchangeInvalidLength, "challenge negotiation truncated")
			}
			if datagram[1] != req[1] || binary.LittleEndian.Uint16(datagram[2:4]) != seed {
				return responseIgnore, nil
			}
			// A nonzero password-extension indicator at [8:10] does not prove
			// that the extension is required. Keep the existing login format;
			// the server's login response determines whether it is accepted.
			if len(datagram) >= 44 {
				exec.experimental.challengePing = binary.LittleEndian.Uint16(datagram[42:44])&0x8000 != 0
			}
		}
		if _, err := parseChallengeResponse(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}
	resp, failure := exec.roundTrip(ctx, timeout, req, classify, phaseChallenge)
	if failure != nil {
		return [4]byte{}, failure
	}
	parsed, _ := parseChallengeResponse(resp)
	if exec.definition.cfg.jluExperimental {
		exec.experimental.pingSerial = resp[1] + 1
	}
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
		if exec.definition.cfg.jluExperimental && (len(datagram) == 0 || (datagram[0] != 4 && datagram[0] != 5)) {
			return responseIgnore, nil
		}
		if _, err := parseLoginResponse(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}

	maxAttempts := exec.definition.cfg.busyMaxAttempts
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, failure := exec.roundTrip(ctx, exec.definition.cfg.loginTimeout, loginReq, classify, phaseLogin)
		if failure != nil {
			return failure
		}
		parsed, _ := parseLoginResponse(resp)
		switch parsed.kind {
		case loginResponseSuccess:
			exec.authInfo = parsed.authInfo
			exec.hasAuthInfo = true
			if exec.definition.cfg.jluExperimental {
				if len(resp) < 43 {
					return responseInvalidError("login negotiation", errors.New("login options truncated"))
				}
				offset := 43 + 12*int(resp[41])
				if offset+2 > len(resp) {
					return responseInvalidError("login negotiation", errors.New("login options truncated"))
				}
				exec.experimental.loginFlags = binary.LittleEndian.Uint16(resp[offset : offset+2])
				if sink, ok := exec.observer.(protocol.BusinessObserver); ok {
					if stats, valid := parseLoginStatistics(resp); valid {
						sink.ObserveAccountStatistics(stats)
					}
				}

			}
			return nil
		case loginResponseRejection:
			if exec.definition.cfg.jluExperimental {
				recordDecision(exec.diagnostics, "login_rejection", int(parsed.code))
				recordDecision(exec.diagnostics, "login_rejection_detail_length", len(resp)-5)
			}
			if parsed.code != 0x02 {
				if exec.definition.cfg.jluExperimental {
					failure := experimentalRejectionError(resp)
					recordDecision(exec.diagnostics, "login_rejection_class", int(failure.reason))
					return failure
				}
				return rejectionError("login", parsed.code, nil)
			}
			if attempt+1 >= maxAttempts {
				return serverBusyExhaustedError("login", nil)
			}
			backoff := randomBackoff(exec.definition.cfg.busyBackoffMin, exec.definition.cfg.busyBackoffMax)
			if failure := exec.waitBusyBackoff(ctx, backoff); failure != nil {
				return failure
			}
		}
	}
	return serverBusyExhaustedError("login", nil)
}

// bootstrap runs the initial KA1 + KA2 1-1-3 sequence and notifies the
// observer exactly once that authentication is established.
func (exec *execution) bootstrap(ctx context.Context) *runError {
	if failure := exec.sendKA1(ctx, phaseBootstrapKA1); failure != nil {
		return failure
	}
	if failure := exec.sendKA2(ctx, ka2Type1, true, phaseBootstrapKA2); failure != nil {
		return failure
	}
	if !exec.definition.cfg.jluExperimental {
		if failure := exec.sendKA2(ctx, ka2Type1, false, phaseBootstrapKA2); failure != nil {
			return failure
		}
	}
	if failure := exec.sendKA2(ctx, ka2Type3, false, phaseBootstrapKA2); failure != nil {
		return failure
	}
	exec.observer.AuthenticationEstablished()
	return nil
}

// finalize maps the outcome to a public failure. On cancellation it performs
// the requested cleanup. Successful cleanup returns nil; failed cleanup
// returns a stable internal failure for Session diagnostics, while Session
// still treats the authoritative cancellation as a successful stop. On a
// protocol failure after Auth Info was supplied it first attempts bounded
// best-effort Logout, retaining any cleanup failure only in the diagnostic
// cause without changing the original code, description or handling
// recommendation.
func (exec *execution) finalize(ctx context.Context, failure *runError) *protocol.AuthenticationProtocolRunFailure {
	if cause := context.Cause(ctx); cause != nil {
		var cancellation protocol.AuthenticationProtocolRunCancellationCause
		if errors.As(cause, &cancellation) &&
			cancellation.CleanupRequirement == protocol.TerminateWithBestEffortLogout &&
			exec.hasAuthInfo {
			if cleanupFailure := exec.bestEffortLogout(); cleanupFailure != nil {
				return cleanupError("logout", cleanupFailure).toFailure()
			}
		}
		return nil
	}
	if failure == nil {
		return contractViolationError("execute", errors.New("run ended without cancellation or failure")).toFailure()
	}
	if exec.hasAuthInfo && failure.kind != kindServerReset && failure.kind != kindServerOffline {
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
	if exec.definition.cfg.jluExperimental {
		var cancel context.CancelFunc
		logoutCtx, cancel = context.WithTimeout(logoutCtx, 2*exec.definition.cfg.logoutTimeout)
		defer cancel()
	}
	salt, challengeFailure := exec.requestChallenge(logoutCtx, exec.definition.cfg.logoutTimeout)
	if challengeFailure != nil {
		salt = exec.salt
	}
	logoutReq, err := buildLogoutRequest(exec.definition.login, salt, exec.authInfo)
	if err != nil {
		return contractViolationError("logout build", err)
	}
	classify := func(datagram []byte) (exchangeResponse, error) {
		if exec.definition.cfg.jluExperimental && (len(datagram) == 0 || datagram[0] != 4) {
			return responseIgnore, nil
		}
		if err := parseLogoutACK(datagram); err != nil {
			return 0, err
		}
		return responseAccept, nil
	}
	_, failure := exec.roundTrip(logoutCtx, exec.definition.cfg.logoutTimeout, logoutReq, classify, phaseLogout)
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

// challengeSeedOffsetMin and challengeSeedOffsetMax are the inclusive bounds of
// the random offset added to the timestamp seconds when forming the Challenge
// seed, matching the audited Drcom-CLI behavior. The offset is sampled from
// [challengeSeedOffsetMin, challengeSeedOffsetMax].
const (
	challengeSeedOffsetMin = 0x0f
	challengeSeedOffsetMax = 0xff
)

// projectChallengeSeed returns the little-endian Challenge seed
// (timestamp_seconds + randomOffset) % 0xFFFF. It is a private pure projection
// with no randomness source of its own; randomChallengeSeed supplies the offset
// sampled from [challengeSeedOffsetMin, challengeSeedOffsetMax]. Tests exercise
// both inclusive endpoints and timestamp wraparound directly.
func projectChallengeSeed(timestamp int64, randomOffset int) uint16 {
	return uint16((timestamp + int64(randomOffset)) % 0xFFFF)
}

// randomChallengeSeedOffset samples an offset from the inclusive range
// [challengeSeedOffsetMin, challengeSeedOffsetMax] using standard-library
// randomness. The entropy fallback stays inside the same range.
func randomChallengeSeedOffset() int {
	var buf [1]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return challengeSeedOffsetMin
	}
	span := challengeSeedOffsetMax - challengeSeedOffsetMin + 1
	return challengeSeedOffsetMin + int(buf[0])%span
}

// randomChallengeSeed returns the LE u16 Challenge seed
// (timestamp_seconds + random_offset) % 0xFFFF, with random_offset sampled
// from [challengeSeedOffsetMin, challengeSeedOffsetMax].
func randomChallengeSeed() uint16 {
	return projectChallengeSeed(time.Now().Unix(), randomChallengeSeedOffset())
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
