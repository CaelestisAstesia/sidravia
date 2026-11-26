package installationmanager

import (
	"context"
	"errors"

	productlayout "sidravia/internal/installation/layout"
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
		return lease.core.service(ctx, action, layout, ready, false)
	default:
		return ServiceState{}, ErrServiceConflict
	}
}

func (lease *SwitchLease) Service(ctx context.Context, action string, layout productlayout.Layout, ready serviceReadiness) (ServiceState, error) {
	if lease == nil {
		return ServiceState{}, ErrLeaseClosed
	}
	return lease.core.service(ctx, action, layout, ready, true)
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
