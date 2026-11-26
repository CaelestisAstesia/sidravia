//go:build windows

package installationgate

import (
	"context"
	"os"
	"path/filepath"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/launchcontract"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/setup"
)

func AcquireRun() (func(), error) { return AcquireRunIdentity("", "") }
func AcquireRunIdentity(version, build string) (func(), error) {
	return AcquireRunIdentityContext(context.Background(), version, build)
}
func AcquireRunIdentityContext(ctx context.Context, version, build string) (func(), error) {
	release, e := AcquireStart()
	if e != nil {
		return nil, e
	}
	defer release()
	layout, e := productlayout.Resolve()
	if e != nil {
		return nil, e
	}
	_, sealErr := os.Lstat(filepath.Join(layout.ExecutableDirectory, operations.StartupSeal))
	sealed := !os.IsNotExist(sealErr)
	root, e := productlayout.UpdateOperationRoot()
	if e != nil {
		return nil, e
	}
	store, e := operations.Open(root, layout.ExecutableDirectory, releases.PublisherTrust())
	if e != nil {
		return nil, e
	}
	r, exists, e := store.Read(ctx)
	if e != nil {
		return nil, e
	}
	if sealed || exists && r.RequiresStartupBlock() {
		var admissionErr error
		if exists && r.Source.Layout == "portable" {
			_, admissionErr = store.AcquireRecoveryStart(ctx, os.Getenv(launchcontract.EnvRecoveryOperation), os.Getenv(launchcontract.EnvRecoveryToken), version, build, os.Getenv(launchcontract.EnvNamespace), os.Getenv(launchcontract.EnvRecoveryConfiguration))
		} else {
			admissionErr = setup.AuthorizeRecovery(ctx, layout.ExecutableDirectory, version, build)
		}
		if admissionErr != nil {
			return nil, ErrBusy
		}
	}

	return func() {}, nil
}
