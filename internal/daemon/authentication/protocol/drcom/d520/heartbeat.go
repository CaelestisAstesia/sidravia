package d520

import (
	"context"
	"fmt"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

// ka2Exchange identifies a sent KA2 request. The first-bootstrap distinction
// is retained so response type 6 cannot be accepted for later Type 1 packets.
type ka2Exchange struct {
	requestType         byte
	firstBootstrapType1 bool
}

// heartbeatLoop waits the Profile heartbeat interval and then runs KA1 + KA2
// 1-3 until a failure or cancellation occurs.
func (exec *execution) heartbeatLoop(ctx context.Context) *runError {
	if exec.definition.cfg.jluExperimental {
		return exec.experimentalHeartbeat(ctx)
	}
	for {
		if !sleepCancellable(ctx, exec.definition.cfg.heartbeatInterval) {
			return networkTimeoutError("heartbeat wait", ctx.Err())
		}
		if failure := exec.sendKA1(ctx, phaseKeepaliveKA1); failure != nil {
			return failure
		}
		if failure := exec.sendKA2(ctx, ka2Type1, false, phaseKeepaliveKA2); failure != nil {
			return failure
		}
		if failure := exec.sendKA2(ctx, ka2Type3, false, phaseKeepaliveKA2); failure != nil {
			return failure
		}
	}
}

// sendKA1 builds and sends one KA1 request using the login salt and current
// Auth Info, and accepts a structurally valid KA1 response.
func (exec *execution) sendKA1(ctx context.Context, phase string) *runError {
	if exec.definition.cfg.jluExperimental && exec.consumePingReply() {
		exec.experimental.pingChallenge = nil
		return nil
	}
	if exec.definition.cfg.jluExperimental && exec.experimental.challengePing {
		return exec.sendChallengePing(ctx, phase)
	}
	passwordBytes, err := encodeProtocolText(exec.definition.login.password)
	if err != nil {
		return contractViolationError("ka1 build", err)
	}
	digestA := md5A(exec.salt[:], passwordBytes)
	timestamp := uint16(time.Now().Unix() % 0xFFFF)
	req, err := buildKA1Request(digestA, exec.authInfo, timestamp)
	if exec.definition.cfg.jluExperimental {
		req, err = buildExperimentalPingRequest(digestA, exec.authInfo, time.Now().Unix())
	}
	if err != nil {
		return contractViolationError("ka1 build", err)
	}

	if exec.definition.cfg.jluExperimental {
		return exec.exchangeExperimentalPing(ctx, req, phase)
	}
	classify := exec.classifyKA1Response()
	exec.ka1Sent = true
	_, failure := exec.roundTrip(ctx, exec.definition.cfg.keepaliveTimeout, req, classify, phase)
	return failure
}

// sendKA2 builds and sends one KA2 request with the current serial and tail.
// firstType1 selects the init version for the first Type1 packet. On an
// accepted normal response it replaces Tail; a bootstrap Type 6 response
// leaves Tail unchanged. Every accepted response increments the serial
// (wrapping at 255).
func (exec *execution) sendKA2(ctx context.Context, typ byte, firstType1 bool, phase string) *runError {
	if exec.definition.cfg.jluExperimental {
		return exec.sendExperimentalModule(ctx, typ, phase)
	}
	serial := exec.serial
	req, err := buildKA2Request(serial, typ, firstType1, exec.definition.cfg.keepAliveVersion, exec.tail, exec.definition.login.clientIPv4)
	if err != nil {
		return contractViolationError("ka2 build", err)
	}
	exec.ka2Sent[serial] = ka2Exchange{requestType: typ, firstBootstrapType1: firstType1}
	classify := exec.classifyKA2Response(serial, typ, firstType1)
	resp, failure := exec.roundTrip(ctx, exec.definition.cfg.keepaliveTimeout, req, classify, phase)
	if failure != nil {
		return failure
	}
	parsed, err := parseKA2Response(resp, serial, typ, firstType1)
	if err != nil {
		return responseInvalidError("ka2 response", err)
	}
	if parsed.tailRefill != nil {
		exec.tail = *parsed.tailRefill
	}
	exec.serial++
	return nil
}

// classifyKA1Response accepts the expected KA1 response and ignores a
// complete KA2 response provably belonging to an already-sent KA2 exchange.
func (exec *execution) classifyKA1Response() responseClassifier {
	return func(datagram []byte) (exchangeResponse, error) {
		if serial, typ, frame, ok := inspectKA2Response(datagram); ok {
			if sent, found := exec.ka2Sent[serial]; found && ka2ResponseCompatible(sent.requestType, sent.firstBootstrapType1, typ, frame) {
				return responseIgnore, nil
			}
			return 0, invalidResponse(protocol.ExchangeInvalidSequence, "unexpected ka2 response serial %d type %d while waiting for ka1", serial, typ)
		}
		if err := parseKA1Response(datagram); err == nil {
			return responseAccept, nil
		} else {
			return 0, fmt.Errorf("unexpected response while waiting for ka1: %w", err)
		}
	}
}

// classifyKA2Response accepts the expected KA2 response (matching serial and
// type) and ignores a complete prior KA1 response or a complete KA2 response
// for an already-sent serial/type. The expected response is checked before the
// stale-response rules.
func (exec *execution) classifyKA2Response(expectedSerial, expectedType byte, firstBootstrapType1 bool) responseClassifier {
	return func(datagram []byte) (exchangeResponse, error) {
		_, parseErr := parseKA2Response(datagram, expectedSerial, expectedType, firstBootstrapType1)
		if parseErr == nil {
			return responseAccept, nil
		}
		if serial, typ, frame, ok := inspectKA2Response(datagram); ok {
			if sent, found := exec.ka2Sent[serial]; found && ka2ResponseCompatible(sent.requestType, sent.firstBootstrapType1, typ, frame) {
				return responseIgnore, nil
			}
			return 0, fmt.Errorf("unexpected ka2 response serial %d type %d while waiting for serial %d type %d: %w", serial, typ, expectedSerial, expectedType, parseErr)
		}
		if isCompleteKA1Response(datagram) && exec.ka1Sent {
			return responseIgnore, nil
		}
		return 0, fmt.Errorf("unexpected response while waiting for ka2: %w", parseErr)
	}
}
