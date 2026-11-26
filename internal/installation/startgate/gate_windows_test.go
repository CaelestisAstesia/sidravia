//go:build windows

package startgate

import (
	"errors"
	"testing"
)

func TestInstallerExclusionAcrossThreads(t *testing.T) {
	release, err := AcquireStart()
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() {
		unlock, e := AcquireStart()
		if e == nil {
			unlock()
		}
		blocked <- e
	}()
	if e := <-blocked; !errors.Is(e, ErrBusy) {
		release()
		t.Fatalf("parallel start admitted: %v", e)
	}
	release()
	again, err := AcquireStart()
	if err != nil {
		t.Fatal(err)
	}
	again()
}
