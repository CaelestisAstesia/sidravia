package d520

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

// moduleChecksum folds little-endian words, then multiplies in 32 bits. Login
// uses the low word; the module Type3 carries the complete result.
func moduleChecksum(data []byte) uint32 {
	var folded uint16
	for i := 0; i+1 < len(data); i += 2 {
		folded ^= binary.LittleEndian.Uint16(data[i : i+2])
	}
	return uint32(folded) * 711
}

func buildExperimentalModule(serial, typ byte, version uint16, nonce [4]byte, challenge [8]byte, ip [4]byte) []byte {
	p := make([]byte, 40)
	p[0] = 7
	p[1] = serial
	p[2] = 40
	p[4] = 0x0b
	p[5] = typ
	binary.LittleEndian.PutUint16(p[6:8], version)
	copy(p[8:12], nonce[:])
	copy(p[16:24], challenge[:])
	if typ == 3 {
		copy(p[28:32], ip[:])
		binary.LittleEndian.PutUint32(p[24:28], moduleChecksum(p))
	}
	return p
}

func (exec *execution) sendExperimentalModule(ctx context.Context, typ byte, phase string) *runError {
	state := &exec.experimental
	for {
		var reply *moduleReply
		flight := state.module
		if flight != nil && flight.typ == typ && flight.reply != nil && time.Now().Before(flight.expires) {
			reply = flight.reply
		} else {
			var nonce [4]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return contractViolationError("module randomness", err)
			}
			version := state.moduleVersion
			if version == 0 {
				version = 9999
			}
			if state.completions == 10 {
				version = state.moduleVersion - 1
				state.completions = 0
			}
			request := buildExperimentalModule(exec.serial, typ, version, nonce, state.challenge, exec.definition.login.clientIPv4)
			binary.LittleEndian.PutUint32(request[12:16], state.moduleOffset)
			if typ == 3 {
				clear(request[24:28])
				binary.LittleEndian.PutUint32(request[24:28], moduleChecksum(request))
			}
			flight = &pendingModule{serial: exec.serial, typ: typ, nonce: nonce, expires: exec.replyExpiry()}
			state.module = flight
			response, failure := exec.roundTrip(ctx, exec.definition.cfg.keepaliveTimeout, request, func(p []byte) (exchangeResponse, error) {
				_, class, err := parseExperimentalModule(p, flight)
				return class, err
			}, phase)
			if failure != nil {
				flight.late = failure.kind == kindNetworkTimeout
				return failure
			}
			reply, _, _ = parseExperimentalModule(response, flight)
		}
		state.module = nil
		exec.serial++
		switch reply.typ {
		case 2:
			state.moduleUpdates = 0
			if reply.version == 0 {
				return &runError{operation: "module negotiation", kind: kindNegotiationUnsupported}
			}
			state.challenge = reply.challenge
			exec.noteConsumedReply()
			return nil
		case 4:
			state.moduleUpdates = 0
			state.completions++
			exec.noteConsumedReply()
			return nil
		case 6:
			state.moduleUpdates++
			if state.moduleUpdates > 1 {
				return responseInvalidError("module negotiation", errors.New("repeated module metadata without challenge"))
			}
			exec.noteConsumedReply()
			state.moduleVersion = reply.version
			state.moduleOffset = reply.frameLength
			recordDecision(exec.diagnostics, "module_version", int(state.moduleVersion))
			recordExchange(exec.diagnostics, "module_metadata", protocol.ExchangeAccepted, int(reply.frameLength))
		}
	}
}
