package driver

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"time"

	"golang.org/x/crypto/md4"

	"sidravia/internal/connection/auth/d520/internal/settings"
	"sidravia/internal/connection/auth/protocol"
)

// All mutable experimental wire state belongs to one execution. The socket is
// read by that execution both during requests and between heartbeat sends.
type experimentalState struct {
	interval         time.Duration
	lastEvidence     time.Time
	lastConfirmation time.Time
	challenge        [8]byte
	moduleVersion    uint16
	moduleOffset     uint32
	completions      int
	challengePing    bool
	pingSerial       byte
	loginFlags       uint16
	ping             *pendingPing
	pingChallenge    *pendingPingChallenge
	module           *pendingModule
	moduleUpdates    int
	// Local consumption revision, not a wire identity or online proof.
	consumedReplies uint64
}

func (exec *execution) roundTrip(ctx context.Context, timeout time.Duration, request []byte, classify responseClassifier, phase string) ([]byte, *runError) {
	if !exec.definition.cfg.JluExperimental {
		return exec.exchange.roundTrip(ctx, timeout, request, classify, phase, exec.diagnostics)
	}
	wrapped := func(packet []byte) (exchangeResponse, error) {
		category := "rx_unknown"
		if len(packet) > 0 {
			switch packet[0] {
			case 2:
				category = "rx_challenge"
			case 4:
				category = "rx_success"
			case 5:
				category = "rx_rejection"
			case 0x4d:
				category = "rx_notification"
			case 7:
				if len(packet) > 4 {
					switch packet[4] {
					case 0x0b:
						category = "rx_module"
					case 6:
						category = "rx_ping_reply"
					case 2:
						category = "rx_ping_challenge"
					case 4:
						category = "rx_server_table"
					}
				}
			}
		}
		recordExchange(exec.diagnostics, category, protocol.ExchangeObserved, len(packet))
		exec.observeBusinessPacket(packet)
		if failure := exec.handleControl(ctx, packet); failure != nil {
			return 0, failure
		}
		if len(packet) == 0 || len(packet) > maxResponseDatagramLength {
			return 0, invalidResponse(protocol.ExchangeInvalidLength, "invalid datagram length")
		}
		if packet[0] == 0x4d {
			return responseIgnore, nil
		}
		if (phase == "online_receive" || phase == phaseKeepaliveKA1 || phase == phaseKeepaliveKA2) && exec.receiveLateReply(packet) {
			if request == nil {
				return responseAccept, nil
			}
			return responseIgnore, nil
		}
		class, err := classify(packet)
		if err == nil && class == responseAccept && exec.hasAuthInfo &&
			(isExperimentalPingReply(packet) || (len(packet) >= 40 && packet[0] == 7 && packet[4] == 0x0b && packet[5] == 4)) {
			exec.experimental.lastEvidence = time.Now()
		}
		return class, err
	}
	return exec.exchange.roundTrip(ctx, timeout, request, wrapped, phase, exec.diagnostics)
}

// Only a framed ping reply (subtype 6) can satisfy KA1. In particular, module,
// statistics, unknown 07 messages and challenge replies cannot satisfy it.
func isExperimentalPingReply(p []byte) bool {
	if len(p) < 6 || len(p) > maxResponseDatagramLength || p[0] != 7 || p[4] != 6 {
		return false
	}
	declaredLength := int(binary.LittleEndian.Uint16(p[2:4]))
	// The campus reply declares 16 bytes even when the datagram carries
	// online statistics starting at offset 32.
	return declaredLength == len(p) || (declaredLength == 16 && len(p) == 72)
}

func (exec *execution) learnInterval(p []byte) {
	seconds := p[5]
	if seconds > 15 && seconds <= 60 {
		exec.experimental.interval = time.Duration(seconds) * time.Second
		recordDecision(exec.diagnostics, "heartbeat_interval_seconds", int(seconds))
	}
}

func (exec *execution) handleControl(ctx context.Context, p []byte) *runError {
	if len(p) < 4 || len(p) > 200 || p[0] != 0x4d || !exec.hasAuthInfo {
		return nil
	}
	switch p[1] {
	case 0x15:
		if bytes.HasPrefix(p[4:], []byte("AdminReset")) {
			recordDecision(exec.diagnostics, "server_reset", 0)
			return &runError{operation: "server control", kind: kindServerReset}
		}
		recordDecision(exec.diagnostics, "server_offline", 0)
		return &runError{operation: "server control", kind: kindServerOffline}
	case 0x26:
		if time.Since(exec.experimental.lastConfirmation) < 5*time.Second {
			return nil
		}
		// The login option selects the session token derivation. The 9-byte proof
		// includes that token and the reported address count, never remote text.
		password, err := settings.EncodeText(exec.definition.login.password)
		if err != nil {
			return contractViolationError("online confirmation", err)
		}
		digestA := md5A(exec.salt[:], password)
		token := binary.LittleEndian.Uint16(digestA)
		if exec.experimental.loginFlags&1 == 0 {
			input := append(append([]byte(nil), digestA...), password...)
			digest := md5.Sum(input)
			token = 0
			for i := 0; i < len(digest); i += 2 {
				token += binary.LittleEndian.Uint16(digest[i : i+2])
			}
		}
		proof := []byte{0xfe, byte(token) + byte(token>>8), 1, 0x14, 0, 7, 0x0b, byte(token), byte(token >> 8)}
		digest := md5.Sum(proof)
		reply := make([]byte, 36)
		copy(reply, proof[:3])
		copy(reply[3:19], digest[:])
		reply[19] = 0xff
		copy(reply[20:], exec.authInfo[:])
		deadline := time.Now().Add(exec.definition.cfg.KeepaliveTimeout)
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			deadline = limit
		}
		if ctx.Err() != nil {
			return networkTimeoutError("online confirmation", ctx.Err())
		}
		if err := exec.exchange.conn.SetWriteDeadline(deadline); err != nil {
			return networkIOError("online confirmation", err)
		}
		exec.diagnostics.DatagramEvent("online_confirmation", protocol.DatagramDirectionTx, reply)
		if _, err := exec.exchange.conn.Write(reply); err != nil {
			return udpIOFailure(ctx, "online confirmation", err)
		}
		exec.experimental.lastConfirmation = time.Now()
		recordExchange(exec.diagnostics, "online_confirmation", protocol.ExchangeAccepted, len(p))
	}
	return nil
}

// challengeDigest selects a wire-defined eight-byte projection. These legacy
// hashes are interoperability fields, not password storage or security APIs.
func challengeDigest(salt [4]byte, extended bool) [8]byte {
	input := append([]byte(nil), salt[:]...)
	if extended {
		input = append(input, 0x6a, 0xa2, 0x33, 1)
	}
	var digest []byte
	var indices [8]int
	switch salt[0] & 3 {
	case 1:
		sum := md5.Sum(input)
		digest = sum[:]
		indices = [8]int{2, 3, 8, 9, 5, 6, 13, 14}
	case 2:
		hash := md4.New()
		hash.Write(input)
		digest = hash.Sum(nil)
		indices = [8]int{1, 2, 8, 9, 4, 5, 11, 12}
	case 3:
		sum := sha1.Sum(input)
		digest = sum[:]
		indices = [8]int{2, 3, 9, 10, 5, 6, 15, 16}
	default:
		return [8]byte{0xc7, 0x2f, 0x31, 1, 0x7e, 0, 0, 0}
	}
	var result [8]byte
	for i, index := range indices {
		result[i] = digest[index]
	}
	return result
}

func (exec *execution) sendChallengePing(ctx context.Context, phase string) *runError {
	if exec.consumePingReply() {
		exec.experimental.pingChallenge = nil
		return nil
	}
	state := &exec.experimental
	flight := state.pingChallenge
	var value *pingChallengeValue
	if flight != nil && flight.reply != nil && time.Now().Before(flight.expires) {
		value = flight.reply
	} else {
		serial := state.pingSerial
		state.pingSerial++
		flight = &pendingPingChallenge{serial: serial, expires: exec.replyExpiry()}
		state.pingChallenge = flight
		request := []byte{7, serial, 8, 0, 1, 0, 0, 0}
		response, failure := exec.roundTrip(ctx, exec.definition.cfg.KeepaliveTimeout, request, func(p []byte) (exchangeResponse, error) {
			_, class, err := parseExperimentalPingChallenge(p, serial)
			return class, err
		}, phase)
		if failure != nil {
			flight.late = failure.kind == kindNetworkTimeout
			return failure
		}
		value, _, _ = parseExperimentalPingChallenge(response, serial)
		flight.reply = value
	}
	if !flight.consumed {
		flight.consumed = true
		exec.noteConsumedReply()
	}
	password, err := settings.EncodeText(exec.definition.login.password)
	if err != nil {
		return contractViolationError("ping build", err)
	}
	ping, err := buildChallengePingRequest(md5A(exec.salt[:], password), exec.authInfo, value.salt, value.extended, uint16(time.Now().Unix()))
	if err != nil {
		return contractViolationError("challenge ping build", err)
	}
	failure := exec.exchangeExperimentalPing(ctx, ping, phase)
	if failure == nil {
		state.pingChallenge = nil
	}
	return failure
}

// Retain a timed-out ping until this send is replaced or its fixed evidence
// window expires. The wire ACK contains no nonce; this bound is not strong
// cross-Run replay protection.
func (exec *execution) exchangeExperimentalPing(ctx context.Context, request []byte, phase string) *runError {
	flight := &pendingPing{expires: exec.replyExpiry()}
	exec.experimental.ping = flight
	response, failure := exec.roundTrip(ctx, exec.definition.cfg.KeepaliveTimeout, request, func(p []byte) (exchangeResponse, error) {
		if !isExperimentalPingReply(p) {
			return responseIgnore, nil
		}
		return responseAccept, nil
	}, phase)
	if failure != nil {
		flight.late = failure.kind == kindNetworkTimeout
		return failure
	}
	exec.learnInterval(response)
	exec.noteConsumedReply()
	exec.experimental.ping = nil
	return nil
}

func (exec *execution) experimentalHeartbeat(ctx context.Context) *runError {
	state := &exec.experimental
	if state.lastEvidence.IsZero() {
		state.lastEvidence = time.Now()
	}
	next := time.Now().Add(state.interval)
	// Count timed-out sends since the last validated result consumed by
	// its own stage, including progress inside a multi-exchange stage call.
	consecutiveTimeouts := 0
	step := 0
	recovering := false
	for {
		expiry := state.lastEvidence.Add(5 * state.interval)
		if !time.Now().Before(expiry) {
			return networkTimeoutError("online evidence expired", nil)
		}
		if time.Now().Before(next) {
			until := next
			if expiry.Before(until) {
				until = expiry
			}
			reply, failure := exec.roundTrip(ctx, time.Until(until), nil, func([]byte) (exchangeResponse, error) { return responseIgnore, nil }, "online_receive")
			if ctx.Err() != nil {
				return networkTimeoutError("online wait", ctx.Err())
			}
			if failure != nil && failure.kind != kindNetworkTimeout {
				return failure
			}
			if reply != nil && recovering {
				next = time.Now()
			}
			continue
		}
		cycleCtx, cancel := context.WithDeadline(ctx, expiry)
		progressBefore := state.consumedReplies
		var failure *runError
		switch step {
		case 0:
			failure = exec.sendKA1(cycleCtx, phaseKeepaliveKA1)
		case 1:
			failure = exec.sendKA2(cycleCtx, ka2Type1, false, phaseKeepaliveKA2)
		case 2:
			failure = exec.sendKA2(cycleCtx, ka2Type3, false, phaseKeepaliveKA2)
		}
		cancel()
		if ctx.Err() != nil {
			return networkTimeoutError("online cycle", ctx.Err())
		}
		if failure != nil && failure.kind != kindNetworkTimeout {
			return failure
		}
		if state.consumedReplies != progressBefore {
			consecutiveTimeouts = 0
			recordDecision(exec.diagnostics, "online_recovery_consecutive_timeouts", 0)
		}
		if failure == nil {
			recovering = false
			step++
			if step < 3 {
				next = time.Now()
				continue
			}
			step = 0
		}
		delay := state.interval
		if failure != nil {
			consecutiveTimeouts++
			recovering = true
			recordDecision(exec.diagnostics, "online_recovery_consecutive_timeouts", consecutiveTimeouts)
			if consecutiveTimeouts >= 5 {
				return networkTimeoutError("online consecutive timeouts exhausted", nil)
			}
			delay = min(delay, 15*time.Second)
			recordExchange(exec.diagnostics, "online_recovery", protocol.ExchangeNetworkTimeout, 0)
		}
		next = time.Now().Add(delay)
	}
}

// A busy response still permits asynchronous traffic without moving the
// already chosen retry time. The legacy path retains its original sleep.
func (exec *execution) waitBusyBackoff(ctx context.Context, duration time.Duration) *runError {
	if !exec.definition.cfg.JluExperimental {
		if !sleepCancellable(ctx, duration) {
			return networkTimeoutError("login busy backoff", ctx.Err())
		}
		return nil
	}
	_, failure := exec.roundTrip(ctx, duration, nil, func([]byte) (exchangeResponse, error) { return responseIgnore, nil }, "login_backoff")
	if ctx.Err() == nil && (failure == nil || failure.kind == kindNetworkTimeout) {
		return nil
	}
	return failure
}

func buildChallengePingRequest(digestA []byte, authInfo [16]byte, salt [4]byte, extended bool, timestamp uint16) ([]byte, error) {
	ping, err := buildExperimentalPingRequest(digestA, authInfo, int64(timestamp))
	if err != nil {
		return nil, err
	}
	if salt[0]&3 != 0 {
		clear(ping[1:8])
		copy(ping[8:12], salt[:])
		digest := challengeDigest(salt, extended)
		copy(ping[12:20], digest[:])
	}
	return ping, nil
}

func buildExperimentalPingRequest(digestA []byte, authInfo [16]byte, seconds int64) ([]byte, error) {
	ping, err := buildKA1Request(digestA, authInfo, uint16(seconds))
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint16(ping[36:38], uint16(seconds))
	return ping, nil
}
