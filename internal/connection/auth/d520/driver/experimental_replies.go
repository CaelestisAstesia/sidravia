package driver

import (
	"bytes"
	"encoding/binary"
	"time"

	"sidravia/internal/connection/auth/protocol"
)

// Three slots bound correlation memory independently of traffic volume. A new
// send replaces the same slot. Deadlines are fixed at send time, not refreshed
// by unrelated traffic, duplicate packets, or later interval negotiation.
type pendingPing struct {
	expires        time.Time
	late, received bool
}

type pingChallengeValue struct {
	salt     [4]byte
	extended bool
}

type pendingPingChallenge struct {
	serial   byte
	expires  time.Time
	late     bool
	reply    *pingChallengeValue
	consumed bool
}

type moduleReply struct {
	typ         byte
	version     uint16
	challenge   [8]byte
	frameLength uint32
}

type pendingModule struct {
	serial, typ byte
	nonce       [4]byte
	expires     time.Time
	late        bool
	reply       *moduleReply
}

func (exec *execution) replyExpiry() time.Time {
	now := time.Now()
	expiry := now.Add(5 * exec.experimental.interval)
	if !exec.experimental.lastEvidence.IsZero() {
		expiry = minTime(expiry, exec.experimental.lastEvidence.Add(5*exec.experimental.interval))
	}
	return expiry
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func parseExperimentalModule(p []byte, sent *pendingModule) (*moduleReply, exchangeResponse, error) {
	if len(p) < 5 || p[0] != 7 || p[4] != 0x0b {
		return nil, responseIgnore, nil
	}
	if len(p) < 40 || len(p) > maxResponseDatagramLength || int(binary.LittleEndian.Uint16(p[2:4])) != len(p) {
		return nil, 0, invalidResponse(protocol.ExchangeInvalidLength, "module frame length mismatch")
	}
	if p[1] != sent.serial || !bytes.Equal(p[8:12], sent.nonce[:]) {
		return nil, responseIgnore, nil
	}
	expected := byte(2)
	if sent.typ == 3 {
		expected = 4
	}
	if p[5] != expected && !(sent.typ == 1 && p[5] == 6) {
		return nil, 0, invalidResponse(protocol.ExchangeInvalidType, "module response stage mismatch")
	}
	result := &moduleReply{typ: p[5], version: binary.LittleEndian.Uint16(p[6:8]), frameLength: uint32(len(p))}
	copy(result.challenge[:], p[16:24])
	if p[5] == 6 {
		size := binary.LittleEndian.Uint32(p[16:20])
		if size == 0 || size > 262144 || result.version == 0 {
			return nil, 0, invalidResponse(protocol.ExchangeInvalidStructure, "module metadata out of bounds")
		}
	}
	return result, responseAccept, nil
}

func parseExperimentalPingChallenge(p []byte, serial byte) (*pingChallengeValue, exchangeResponse, error) {
	if len(p) < 5 || p[0] != 7 || p[4] != 2 {
		return nil, responseIgnore, nil
	}
	if len(p) < 16 || len(p) > maxResponseDatagramLength || int(binary.LittleEndian.Uint16(p[2:4])) != len(p) {
		return nil, 0, invalidResponse(protocol.ExchangeInvalidLength, "ping challenge frame length mismatch")
	}
	if p[1] != serial {
		return nil, responseIgnore, nil
	}
	value := &pingChallengeValue{extended: p[5]&0x80 != 0}
	copy(value.salt[:], p[8:12])
	return value, responseAccept, nil
}

// Only timed-out online exchanges are eligible here. These results never
// satisfy another current request. Returning true wakes the receive-only
// scheduler so the original stage can consume its own result. Uncorrelated
// ping replies have only a single outstanding send/window, not a fictitious
// wire transaction ID; delayed duplicates after a new send remain ambiguous.
func (exec *execution) receiveLateReply(p []byte) bool {
	state := &exec.experimental
	now := time.Now()
	if flight := state.ping; flight != nil {
		if !now.Before(flight.expires) {
			state.ping = nil
		} else if flight.late && !flight.received && isExperimentalPingReply(p) {
			flight.received = true
			state.lastEvidence = now
			exec.learnInterval(p)
			recordDecision(exec.diagnostics, "late_ping_reply", 0)
			return true
		}
	}
	if flight := state.pingChallenge; flight != nil {
		if !now.Before(flight.expires) {
			state.pingChallenge = nil
		} else if flight.late && flight.reply == nil {
			reply, class, err := parseExperimentalPingChallenge(p, flight.serial)
			if err == nil && class == responseAccept {
				flight.reply = reply
				recordDecision(exec.diagnostics, "late_ping_challenge", 0)
				return true
			}
		}
	}
	if flight := state.module; flight != nil {
		if !now.Before(flight.expires) {
			state.module = nil
		} else if flight.late && flight.reply == nil {
			reply, class, err := parseExperimentalModule(p, flight)
			if err == nil && class == responseAccept {
				flight.reply = reply
				if reply.typ == 4 {
					state.lastEvidence = now
				}
				recordDecision(exec.diagnostics, "late_module_reply", int(reply.typ))
				return true
			}
		}
	}
	return false
}

func (exec *execution) consumePingReply() bool {
	flight := exec.experimental.ping
	if flight != nil && flight.received && time.Now().Before(flight.expires) {
		exec.experimental.ping = nil
		exec.noteConsumedReply()
		return true
	}
	return false
}

// Receipt may cache a late result, but only consumption is actual progress.
// Reusing a challenge for a retry and ignored duplicates never call this.
func (exec *execution) noteConsumedReply() {
	exec.experimental.consumedReplies++
}
