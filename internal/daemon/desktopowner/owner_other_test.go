//go:build !windows

package desktopowner

import (
	"errors"
	"testing"
)

func TestOpenIsExplicitlyUnsupportedOffWindows(t *testing.T) {
	got, err := Open(42)
	if got != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open = %v,%v", got, err)
	}
}
