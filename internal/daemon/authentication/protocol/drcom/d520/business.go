package d520

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"strings"

	"sidravia/internal/daemon/authentication/protocol"
)

func (factory) BusinessCapabilities(raw protocol.InstitutionProtocolConfiguration) (protocol.BusinessCapabilities, error) {
	cfg, err := decodeInstitutionProtocolConfiguration(raw)
	return protocol.BusinessCapabilities{Statistics: cfg.jluExperimental, TextNotifications: cfg.jluExperimental}, err
}

func parseLoginStatistics(p []byte) (protocol.AccountStatistics, bool) {
	if len(p) < 39 || len(p) > maxResponseDatagramLength || p[0] != 4 {
		return protocol.AccountStatistics{}, false
	}
	stats := protocol.AccountStatistics{HasTraffic: true, TrafficBytes: uint64(binary.LittleEndian.Uint32(p[9:13])) * 1024}
	balance := binary.LittleEndian.Uint32(p[13:17])
	if balance < 0x80000000 {
		stats.HasBalance = true
		stats.BalanceCents = uint64(balance)
	}
	return stats, true
}

func (exec *execution) messageToken() (uint16, error) {
	password, err := encodeProtocolText(exec.definition.login.password)
	if err != nil {
		return 0, err
	}
	digest := md5A(exec.salt[:], password)
	if exec.experimental.loginFlags&1 != 0 {
		return binary.LittleEndian.Uint16(digest), nil
	}
	input := append(append([]byte(nil), digest...), password...)
	sum := md5.Sum(input)
	var token uint16
	for i := 0; i < len(sum); i += 2 {
		token += binary.LittleEndian.Uint16(sum[i : i+2])
	}
	return token, nil
}

// The implemented wire subset has no encoding tag. Accept the unambiguous
// ASCII subset only; do not guess a locale or silently replace undecodable text.
func parseTextNotification(p []byte, token uint16) (string, bool) {
	if len(p) < 7 || len(p) > 200 || p[0] != 0x4d || p[1] != 0x38 {
		return "", false
	}
	body := p[4:]
	end := bytes.IndexByte(body, 0)
	if end < 2 {
		return "", false
	}
	for _, value := range body[end:] {
		if value != 0 {
			return "", false
		}
	}
	var sum uint16
	for i := 4; i < len(p); i += 2 {
		word := uint16(p[i])
		if i+1 < len(p) {
			word |= uint16(p[i+1]) << 8
		}
		sum += word
	}
	if binary.LittleEndian.Uint16(p[2:4]) != sum*token {
		return "", false
	}
	for _, value := range body[:end] {
		if value < 32 && value != '\r' && value != '\n' && value != '\t' || value >= 127 {
			return "", false
		}
	}
	text := string(body[:end])
	for _, prefix := range []string{"DRCOMWEBMSG", "#POPUP#", "POPUP", "DWN ", "TCD ", "TCT "} {
		if strings.HasPrefix(text, prefix) {
			return "", false
		}
	}
	return text, true
}

func (exec *execution) observeBusinessPacket(p []byte) {
	if !exec.definition.cfg.jluExperimental || !exec.hasAuthInfo {
		return
	}
	sink, ok := exec.observer.(protocol.BusinessObserver)
	if !ok {
		return
	}
	if len(p) < 2 || p[0] != 0x4d || p[1] != 0x38 {
		return
	}
	token, err := exec.messageToken()
	if err != nil {
		return
	}
	if text, valid := parseTextNotification(p, token); valid {
		sink.ObserveTextNotification(text)
	}
}
