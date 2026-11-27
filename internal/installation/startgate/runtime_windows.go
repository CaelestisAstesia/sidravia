//go:build windows

package startgate

import (
	"context"
	"errors"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/update/setup/receipt"
	"sidravia/internal/update/transaction/admission"
)

func AcquireRun() (func(), error) { return AcquireRunIdentity("", "") }
func AcquireRunIdentity(version, build string) (func(), error) {
	return AcquireRunIdentityContext(context.Background(), version, build)
}
func AcquireRunIdentityContext(ctx context.Context, version, build string) (func(), error) {
	release, err := AcquireStart()
	if err != nil {
		return nil, err
	}
	defer release()
	layout, err := productlayout.Resolve()
	if err != nil {
		return nil, err
	}
	needsReceipt, err := admission.Check(ctx, layout.ExecutableDirectory)
	if errors.Is(err, admission.ErrRecoveryDenied) {
		return nil, ErrBusy
	}
	if err != nil {
		return nil, err
	}
	if needsReceipt {
		if err := receipt.AuthorizeRecovery(ctx, layout.ExecutableDirectory, version, build); err != nil {
			return nil, ErrBusy
		}
	}
	return func() {}, nil
}
