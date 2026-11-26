//go:build linux

package userservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"sidravia/internal/installation/internal/integrationfile"
	"sidravia/internal/installation/userservice/registration"
	"sidravia/internal/productlayout"
)

type serviceLease struct {
	unit    string
	fd      int
	closing atomic.Bool
	gate    sync.Mutex
}

// WithServiceRegistration excludes competing registration changes for an
// operation that keeps the registration unchanged.
func WithServiceRegistration(ctx context.Context, use func(*RegistrationLease) error) error {
	path, err := registration.UnitPath()
	if err != nil {
		return err
	}
	return withServiceLease(ctx, path, func(core *serviceLease) error {
		return use(&RegistrationLease{core: core})
	})
}

// WithServiceSwitch grants switch write access until use returns. Recovery can
// use a separately bounded context within the same scope after ctx is canceled.
func WithServiceSwitch(ctx context.Context, use func(*SwitchLease) error) error {
	path, err := registration.UnitPath()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return withServiceLease(ctx, path, func(core *serviceLease) error {
		return use(&SwitchLease{core: core})
	})
}

func withServiceLease(ctx context.Context, path string, use func(*serviceLease) error) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	core, err := openServiceLease(path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, core.close()) }()
	return use(core)
}

func openServiceLease(path string) (*serviceLease, error) {
	fd, err := unix.Open(filepath.Dir(path), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(ErrServiceConflict, err, unix.Close(fd))
	}
	for _, scratch := range []string{path + ".sidravia-tmp", switchPath(path) + ".sidravia-tmp"} {
		if err := integrationfile.ClearScratch(scratch); err != nil {
			return nil, errors.Join(registrationFileError(err), unix.Close(fd))
		}
	}
	return &serviceLease{unit: path, fd: fd}, nil
}

func (core *serviceLease) close() error {
	core.closing.Store(true)
	core.gate.Lock()
	defer core.gate.Unlock()
	return unix.Close(core.fd)
}

func (core *serviceLease) run(ctx context.Context, operation func(string) error) error {
	if core == nil || core.closing.Load() {
		return ErrLeaseClosed
	}
	core.gate.Lock()
	defer core.gate.Unlock()
	// Closing rejects queued writes. A write admitted under the gate retains
	// the directory's flock for its entire effect, including compensation.
	if core.closing.Load() {
		return ErrLeaseClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return operation(core.unit)
}

func (core *serviceLease) service(ctx context.Context, action string, layout productlayout.Layout, ready serviceReadiness, waitStopped func(context.Context) error, switching bool) (ServiceState, error) {
	var state ServiceState
	err := core.run(ctx, func(path string) error {
		if !layout.Namespace.IsProduction() {
			return ErrServiceUnavailable
		}
		target, err := serviceTarget(layout, action)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
		defer cancel()
		state, err = applyService(ctx, path, action, target, ready, waitStopped, switching)
		return err
	})
	return state, err
}

func (core *serviceLease) switchJournal(ctx context.Context, state *ServiceSwitch) error {
	return core.run(ctx, func(path string) error {
		return saveServiceSwitchAt(path, state)
	})
}

func (core *serviceLease) reload(ctx context.Context) error {
	return core.run(ctx, func(path string) error {
		return reloadServiceSwitchAt(ctx, path)
	})
}
