//go:build !windows && !linux

package environment

import (
	"context"
	"errors"
	"testing"
)

func TestReadSystemHostInformationUnsupportedOffWindows(t *testing.T) {
	_, err := ReadSystemHostInformation()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestSystemObserverObserveUnsupportedOffWindows(t *testing.T) {
	observer := NewSystemObserver()
	output := make(chan Snapshot, 1)
	err := observer.Observe(context.Background(), output)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
	select {
	case snapshot := <-output:
		t.Fatalf("unsupported observe published a snapshot: %+v", snapshot)
	default:
	}
}
