package userservice

import (
	"context"
	"errors"

	"sidravia/internal/productlayout"
)

var ErrLeaseClosed = errors.New("user service lease is closed")

type serviceReadiness = func(context.Context, productlayout.Layout) error

// RegistrationLease keeps one registration stable while its daemon is
// inspected, stopped or relaunched. It has no registration or journal writes.
type RegistrationLease struct{ core *serviceLease }

// SwitchLease can change the registration and its switch journal. The
// acquiring scope owns closure. Copies share the same validity and OS lock.
type SwitchLease struct{ core *serviceLease }

func (lease *RegistrationLease) Service(ctx context.Context, action string, layout productlayout.Layout, ready serviceReadiness) (ServiceState, error) {
	if lease == nil {
		return ServiceState{}, ErrLeaseClosed
	}
	switch action {
	case "status", "start", "stop", "recover-start":
		return lease.core.service(ctx, action, layout, ready, nil, false)
	default:
		return ServiceState{}, ErrServiceConflict
	}
}

func (lease *SwitchLease) Service(ctx context.Context, action string, layout productlayout.Layout, ready serviceReadiness) (ServiceState, error) {
	if lease == nil {
		return ServiceState{}, ErrLeaseClosed
	}
	return lease.core.service(ctx, action, layout, ready, nil, true)
}

// Replace verifies the previously observed source exited before changing its
// registration. waitStopped runs after the manager stops the source and before
// the first write. The lease stays held for the complete operation.
func (lease *SwitchLease) Replace(ctx context.Context, layout productlayout.Layout, waitStopped func(context.Context) error, ready serviceReadiness) (ServiceState, error) {
	if lease == nil {
		return ServiceState{}, ErrLeaseClosed
	}
	return lease.core.service(ctx, "replace", layout, ready, waitStopped, true)
}

func (lease *SwitchLease) Save(ctx context.Context, state ServiceSwitch) error {
	if lease == nil {
		return ErrLeaseClosed
	}
	return lease.core.switchJournal(ctx, &state)
}

func (lease *SwitchLease) Clear(ctx context.Context) error {
	if lease == nil {
		return ErrLeaseClosed
	}
	return lease.core.switchJournal(ctx, nil)
}

func (lease *SwitchLease) Reload(ctx context.Context) error {
	if lease == nil {
		return ErrLeaseClosed
	}
	return lease.core.reload(ctx)
}
