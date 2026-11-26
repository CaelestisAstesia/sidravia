//go:build !linux

package userservice

import (
	"context"

	productlayout "sidravia/internal/installation/layout"
)

type serviceLease struct{}

func WithServiceRegistration(context.Context, func(*RegistrationLease) error) error {
	return ErrServiceUnavailable
}

func WithServiceSwitch(context.Context, func(*SwitchLease) error) error {
	return ErrServiceUnavailable
}

func (*serviceLease) service(context.Context, string, productlayout.Layout, serviceReadiness, bool) (ServiceState, error) {
	return ServiceState{}, ErrLeaseClosed
}

func (*serviceLease) switchJournal(context.Context, *ServiceSwitch) error { return ErrLeaseClosed }
func (*serviceLease) reload(context.Context) error                        { return ErrLeaseClosed }
