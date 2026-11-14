package session

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"sidravia/internal/daemon/authentication/protocol"
)

const businessMessageLimit = 16
const businessTextLimit = 512

type BusinessMessage struct {
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"receivedAt"`
	Text       string    `json:"text"`
}
type BusinessStatistics struct {
	ReceivedAt   time.Time `json:"receivedAt"`
	Source       string    `json:"source"`
	Scope        string    `json:"scope"`
	TrafficBytes *uint64   `json:"trafficBytes"`
	BalanceCents *uint64   `json:"balanceCents"`
}
type BusinessSnapshot struct {
	SessionID           AuthenticationSessionID `json:"sessionId"`
	RunGeneration       uint64                  `json:"runGeneration"`
	Revision            uint64                  `json:"revision"`
	Current             bool                    `json:"current"`
	StatisticsSupported bool                    `json:"statisticsSupported"`
	TextSupported       bool                    `json:"textSupported"`
	Statistics          *BusinessStatistics     `json:"statistics"`
	Messages            []BusinessMessage       `json:"messages"`
	UpdatedAt           time.Time               `json:"updatedAt"`
}

func (value BusinessSnapshot) clone() BusinessSnapshot {
	value.Messages = append([]BusinessMessage{}, value.Messages...)
	if value.Statistics != nil {
		stats := *value.Statistics
		if stats.TrafficBytes != nil {
			n := *stats.TrafficBytes
			stats.TrafficBytes = &n
		}
		if stats.BalanceCents != nil {
			n := *stats.BalanceCents
			stats.BalanceCents = &n
		}
		value.Statistics = &stats
	}
	return value
}

type businessMailbox struct {
	mu         sync.Mutex
	statistics *protocol.AccountStatistics
	texts      []string
}

// Transport slots only. Business ownership and de-duplication stay in the actor.
func (m *businessMailbox) offerStatistics(value protocol.AccountStatistics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statistics = &value
}
func (m *businessMailbox) offerText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.texts) == businessMessageLimit {
		copy(m.texts, m.texts[1:])
		m.texts = m.texts[:businessMessageLimit-1]
	}
	m.texts = append(m.texts, text)
}
func (m *businessMailbox) take() (*protocol.AccountStatistics, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats, texts := m.statistics, m.texts
	m.statistics = nil
	m.texts = nil
	return stats, texts
}

type businessQuery struct{ reply chan BusinessSnapshot }

func (businessQuery) isSessionMessage() {}
func (s *AuthenticationSession) Business(ctx context.Context) (BusinessSnapshot, error) {
	reply := make(chan BusinessSnapshot, 1)
	if err := s.send(ctx, businessQuery{reply: reply}); err != nil {
		return BusinessSnapshot{}, err
	}
	select {
	case value := <-reply:
		return value, nil
	case <-ctx.Done():
		return BusinessSnapshot{}, ctx.Err()
	case <-s.done:
		return BusinessSnapshot{}, ErrAuthenticationSessionClosed
	}
}
func (s *AuthenticationSession) businessChanged() {
	s.business.Revision++
	s.business.UpdatedAt = s.now()
}
func (s *AuthenticationSession) beginBusinessRun(generation uint64) {
	s.business.RunGeneration = generation
	s.business.Statistics = nil
	s.business.Messages = []BusinessMessage{}
	s.businessChanged()
}
func normalizeBusinessText(text string) (string, bool) {
	if !utf8.ValidString(text) || len(text) > 4096 {
		return "", false
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	return text, text != "" && len(text) <= businessTextLimit
}
func (o protocolRunObserver) ObserveAccountStatistics(value protocol.AccountStatistics) {
	if o.context == nil || o.business == nil || o.context.Err() != nil {
		return
	}
	o.business.offerStatistics(value)
	o.wakeBusiness()
}
func (o protocolRunObserver) ObserveTextNotification(text string) {
	if o.context == nil || o.business == nil || o.context.Err() != nil {
		return
	}
	normalized, ok := normalizeBusinessText(text)
	if !ok {
		return
	}
	o.business.offerText(normalized)
	o.wakeBusiness()
}
func (o protocolRunObserver) wakeBusiness() {
	select {
	case o.session.businessWake <- struct{}{}:
	default:
	}
}
func (s *AuthenticationSession) consumeBusiness() {
	run := s.active
	if run == nil || context.Cause(run.context) != nil || run.generation != s.business.RunGeneration {
		return
	}
	statistics, texts := run.business.take()
	now := s.now()
	if statistics != nil && s.business.StatisticsSupported {
		value := &BusinessStatistics{ReceivedAt: now, Source: "server_login", Scope: "server_billing_history"}
		if statistics.HasTraffic && statistics.TrafficBytes <= 4398046510080 {
			n := statistics.TrafficBytes
			value.TrafficBytes = &n
		}
		if statistics.HasBalance && statistics.BalanceCents <= 2147483647 {
			n := statistics.BalanceCents
			value.BalanceCents = &n
		}
		s.business.Statistics = value
		s.businessChanged()
	}
	if !s.business.TextSupported {
		return
	}
	for _, text := range texts {
		duplicate := false
		for _, previous := range s.business.Messages {
			if previous.Text == text && now.Sub(previous.ReceivedAt) < 10*time.Second {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		s.businessMessageID++
		item := BusinessMessage{ID: strconv.FormatUint(s.businessMessageID, 10), ReceivedAt: now, Text: text}
		if len(s.business.Messages) == businessMessageLimit {
			copy(s.business.Messages, s.business.Messages[1:])
			s.business.Messages = s.business.Messages[:businessMessageLimit-1]
		}
		s.business.Messages = append(s.business.Messages, item)
		s.businessChanged()
	}
}
func (s *AuthenticationSession) businessSnapshot() BusinessSnapshot {
	value := s.business.clone()
	value.Current = s.active != nil && s.active.established && context.Cause(s.active.context) == nil
	return value
}
