package session

import (
	"sync"
	"time"

	"sidravia/internal/daemon/authentication/protocol"
)

type RetryPolicy interface {
	Delay(protocol.AuthenticationProtocolFailureHandlingRecommendation, uint32) (time.Duration, bool)
}

type RetryCancellation interface {
	Cancel()
}

type RetryScheduler interface {
	Schedule(time.Duration, func()) RetryCancellation
}

type defaultRetryPolicy struct{}

func NewDefaultRetryPolicy() RetryPolicy {
	return defaultRetryPolicy{}
}

func (defaultRetryPolicy) Delay(
	recommendation protocol.AuthenticationProtocolFailureHandlingRecommendation,
	_ uint32,
) (time.Duration, bool) {
	switch recommendation {
	case protocol.RetryAfterStandardDelay:
		return 5 * time.Second, true
	case protocol.RetryAfterExtendedDelay:
		return 30 * time.Second, true
	default:
		return 0, false
	}
}

type timerRetryScheduler struct{}

func NewTimerRetryScheduler() RetryScheduler {
	return timerRetryScheduler{}
}

type timerRetryCancellation struct {
	once   sync.Once
	cancel func()
}

func (c *timerRetryCancellation) Cancel() {
	c.once.Do(c.cancel)
}

func (timerRetryScheduler) Schedule(delay time.Duration, callback func()) RetryCancellation {
	timer := time.AfterFunc(delay, callback)
	cancellation := &timerRetryCancellation{
		cancel: func() {
			timer.Stop()
		},
	}
	return cancellation
}
