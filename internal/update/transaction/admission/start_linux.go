package admission

import (
	"context"
	"errors"
	"os"
)

func Acquire(ctx context.Context, target string) (func(), error) {
	store, err := open(target)
	if err != nil {
		return nil, err
	}
	return store.AcquireStart(ctx)
}

func AcquireRecovery(ctx context.Context, target string, denied error, r Recovery) (func(), error) {
	store, err := open(target)
	if err != nil {
		return nil, errors.Join(denied, err)
	}
	return store.AcquireRecoveryStart(ctx, r.operation, r.token, r.version, r.build, r.namespace, r.configuration)
}

// AcquireForExec returns only the shared runtime lease and whether recovery
// admitted this process. The caller controls inheritance and closes on error.
func AcquireForExec(ctx context.Context, target string, r Recovery) (_ *os.File, recovery bool, _ error) {
	store, err := open(target)
	if err != nil {
		return nil, false, err
	}
	lease, err := store.AcquireInheritedStart(ctx)
	if err == nil {
		return lease, false, nil
	}
	lease, err = store.AcquireInheritedRecoveryStart(ctx, r.operation, r.token, r.version, r.build, r.namespace, r.configuration)
	return lease, err == nil, err
}
