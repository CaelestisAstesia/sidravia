package contract

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MethodSessionBusinessGet = "session.business.get"
const BusinessResultLimit = 60 * 1024

type BusinessStatistics struct {
	ReceivedAt   string  `json:"receivedAt"`
	Source       string  `json:"source"`
	Scope        string  `json:"scope"`
	TrafficBytes *uint64 `json:"trafficBytes"`
	BalanceCents *uint64 `json:"balanceCents"`
}
type BusinessMessage struct {
	ID         string `json:"id"`
	ReceivedAt string `json:"receivedAt"`
	Text       string `json:"text"`
}
type BusinessResult struct {
	SessionID           string              `json:"sessionId"`
	RunGeneration       uint64              `json:"runGeneration"`
	Revision            uint64              `json:"revision"`
	Current             bool                `json:"current"`
	StatisticsSupported bool                `json:"statisticsSupported"`
	TextSupported       bool                `json:"textSupported"`
	Statistics          *BusinessStatistics `json:"statistics"`
	Messages            []BusinessMessage   `json:"messages"`
	UpdatedAt           string              `json:"updatedAt"`
}

func (v *BusinessStatistics) UnmarshalJSON(data []byte) error {
	if err := businessObject(data, []string{"receivedAt", "source", "scope", "trafficBytes", "balanceCents"}, "trafficBytes", "balanceCents"); err != nil {
		return err
	}
	type plain BusinessStatistics
	var p plain
	if err := decodeStrict(data, &p); err != nil {
		return err
	}
	if !networkTime(p.ReceivedAt) || p.Source != "server_login" || p.Scope != "server_billing_history" || p.TrafficBytes != nil && *p.TrafficBytes > 4398046510080 || p.BalanceCents != nil && *p.BalanceCents > 2147483647 {
		return errors.New("invalid business statistics")
	}
	*v = BusinessStatistics(p)
	return nil
}
func (v *BusinessMessage) UnmarshalJSON(data []byte) error {
	if err := networkObject(data, []string{"id", "receivedAt", "text"}); err != nil {
		return err
	}
	type plain BusinessMessage
	var p plain
	if err := decodeStrict(data, &p); err != nil {
		return err
	}
	id, err := strconv.ParseUint(p.ID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != p.ID || !networkTime(p.ReceivedAt) || !validBusinessText(p.Text) {
		return errors.New("invalid business message")
	}
	*v = BusinessMessage(p)
	return nil
}
func validBusinessText(text string) bool {
	if text == "" || len(text) > 512 || !utf8.ValidString(text) || strings.TrimSpace(text) != text {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func (v *BusinessResult) UnmarshalJSON(data []byte) error {
	if len(data) > BusinessResultLimit {
		return errors.New("business result too large")
	}
	if err := businessObject(data, []string{"sessionId", "runGeneration", "revision", "current", "statisticsSupported", "textSupported", "statistics", "messages", "updatedAt"}, "statistics"); err != nil {
		return err
	}
	type plain BusinessResult
	var p plain
	if err := decodeStrict(data, &p); err != nil {
		return err
	}
	if p.SessionID == "" || p.Revision == 0 || !networkTime(p.UpdatedAt) || p.Messages == nil || len(p.Messages) > 16 || !p.StatisticsSupported && p.Statistics != nil || !p.TextSupported && len(p.Messages) > 0 || p.RunGeneration == 0 && (p.Current || p.Statistics != nil || len(p.Messages) > 0) {
		return errors.New("invalid business result")
	}
	var last uint64
	for _, m := range p.Messages {
		id, _ := strconv.ParseUint(m.ID, 10, 64)
		if id <= last {
			return errors.New("unordered business messages")
		}
		last = id
	}
	*v = BusinessResult(p)
	return nil
}
func DecodeBusinessResult(data []byte) (BusinessResult, error) {
	var v BusinessResult
	err := decodeStrict(data, &v)
	return v, err
}
func MarshalBusinessResult(v BusinessResult) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	_, err = DecodeBusinessResult(data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Nullable fields are explicit values, never omitted or interchangeable with zero.
func businessObject(data []byte, required []string, nullable ...string) error {
	if err := validateNetworkUnicode(data); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict(data, &fields); err != nil {
		return err
	}
	if len(fields) != len(required) {
		return errors.New("invalid business fields")
	}
	nulls := map[string]bool{}
	for _, key := range nullable {
		nulls[key] = true
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || string(v) == "null" && !nulls[key] {
			return errors.New("missing business field")
		}
	}
	return nil
}
