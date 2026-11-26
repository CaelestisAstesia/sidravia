//go:build linux

package userservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"sidravia/internal/installation/userservice/registration"
	"sidravia/internal/productlayout"
)

func switchRecordForLease(root string) ServiceSwitch {
	return ServiceSwitch{
		ID: "11111111111111111111111111111111",
		Layout: productlayout.Layout{
			ExecutableDirectory: filepath.Join(root, "product"),
			ConfigurationsPath:  filepath.Join(root, "configurations.json"),
			RuntimeInfoPath:     filepath.Join(root, "runtime.json"),
		},
		Mode: "headless", RuntimePID: 1, Expires: time.Now().UTC().Add(time.Minute),
	}
}

func TestServiceSwitchLeaseRejectsEscapedCopies(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	path := filepath.Join(root, registration.UnitName)
	var escaped *SwitchLease
	var copyOfLease SwitchLease
	if err := withServiceLease(ctx, path, func(core *serviceLease) error {
		escaped = &SwitchLease{core: core}
		copyOfLease = *escaped
		return escaped.Save(ctx, switchRecordForLease(root))
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(switchPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := escaped.Clear(ctx); !errors.Is(err, ErrLeaseClosed) {
		t.Fatalf("escaped clear: %v", err)
	}
	if err := copyOfLease.Save(ctx, switchRecordForLease(root)); !errors.Is(err, ErrLeaseClosed) {
		t.Fatalf("copied save: %v", err)
	}
	if _, err := copyOfLease.Service(ctx, "unregister", productlayout.Layout{}, nil); !errors.Is(err, ErrLeaseClosed) {
		t.Fatalf("copied registration change: %v", err)
	}
	after, err := os.ReadFile(switchPath(path))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("escaped lease changed the journal: %v", err)
	}
}

func TestServiceLeaseClosureWaitsForAdmittedWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	root := t.TempDir()
	path := filepath.Join(root, registration.UnitName)
	entered := make(chan *serviceLease, 1)
	writeFinished := make(chan error, 1)
	scopeFinished := make(chan error, 1)
	proceed := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(proceed) })
	go func() {
		scopeFinished <- withServiceLease(ctx, path, func(core *serviceLease) error {
			admitted := make(chan struct{})
			go func() {
				writeFinished <- core.run(ctx, func(unit string) error {
					close(admitted)
					<-proceed
					state := switchRecordForLease(root)
					return saveServiceSwitchAt(unit, &state)
				})
			}()
			select {
			case <-admitted:
				entered <- core
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var core *serviceLease
	select {
	case core = <-entered:
	case <-ctx.Done():
		t.Fatal("write never entered admission")
	}
	for !core.closing.Load() && ctx.Err() == nil {
		runtime.Gosched()
	}
	if ctx.Err() != nil {
		t.Fatal("scope did not begin closure")
	}
	select {
	case err := <-scopeFinished:
		t.Fatalf("scope closed before its published write: %v", err)
	default:
	}
	if contender, err := openServiceLease(path); !errors.Is(err, ErrServiceConflict) {
		if contender != nil {
			_ = contender.close()
		}
		t.Fatalf("directory lock escaped while a write was admitted: %v", err)
	}
	alias := SwitchLease{core: core}
	if err := alias.Clear(ctx); !errors.Is(err, ErrLeaseClosed) {
		t.Fatalf("late write was admitted: %v", err)
	}
	release.Do(func() { close(proceed) })
	for _, done := range []<-chan error{writeFinished, scopeFinished} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("write or lease failed to drain")
		}
	}
	if _, exists, err := loadServiceSwitch(switchPath(path)); !exists || err != nil {
		t.Fatalf("admitted write did not publish its journal: %t %v", exists, err)
	}
	if err := withServiceLease(ctx, path, func(*serviceLease) error { return nil }); err != nil {
		t.Fatalf("released lock could not be reacquired: %v", err)
	}
}

func TestCanceledOperationCanCompensateBeforeLeaseCloses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, registration.UnitName)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	compensationFailed := errors.New("could not restore original generation")
	if err := withServiceLease(ctx, path, func(core *serviceLease) error {
		lease := SwitchLease{core: core}
		if err := lease.Save(ctx, switchRecordForLease(root)); err != nil {
			return err
		}
		cancel()
		if err := lease.Clear(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled write: %v", err)
		}
		compensate, finish := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer finish()
		return core.run(compensate, func(string) error {
			if contender, err := openServiceLease(path); !errors.Is(err, ErrServiceConflict) {
				if contender != nil {
					_ = contender.close()
				}
				t.Fatalf("compensation did not retain flock: %v", err)
			}
			return compensationFailed
		})
	}); !errors.Is(err, compensationFailed) {
		t.Fatalf("compensation result: %v", err)
	}
	if err := withServiceLease(t.Context(), path, func(core *serviceLease) error {
		current, exists, err := loadServiceSwitch(switchPath(path))
		if err != nil || !exists || current.ID != switchRecordForLease(root).ID {
			t.Fatalf("journal unavailable to the next lock holder: %t %v", exists, err)
		}
		stable := RegistrationLease{core: core}
		if _, err := stable.Service(t.Context(), "register", current.Layout, nil); !errors.Is(err, ErrServiceConflict) {
			t.Fatalf("stable lease can change registration: %v", err)
		}
		switching := SwitchLease{core: core}
		return switching.Clear(t.Context())
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationReplacementDoesNotWriteWithoutObservedExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), registration.UnitName)
	initial := []byte("existing unit")
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	observationFailed := errors.New("process wait unavailable")
	err := replaceRegistration(t.Context(), path, ServiceState{}, productlayout.Layout{}, initial, []byte("replacement"), productlayout.Layout{}, nil, func(context.Context) error {
		return observationFailed
	})
	if !errors.Is(err, ErrServiceStop) || !errors.Is(err, observationFailed) {
		t.Fatalf("exit-observation failure: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, initial) {
		t.Fatalf("registration changed without exit evidence: %q %v", after, err)
	}
}
