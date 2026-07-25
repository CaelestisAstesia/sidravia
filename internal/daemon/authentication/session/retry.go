package session

import (
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
