//go:build windows

package portable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRunnerReadyUsesWaitCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.json")
	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, err := readRunnerReady(expired, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired readiness wait returned %v", err)
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if _, err := readRunnerReady(stopped, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled readiness wait returned %v", err)
	}
	if _, err := readRunnerReady(context.Background(), path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing receipt returned %v", err)
	}
	want := runnerID{PID: 42, Created: 12}
	if err := savePrivate(context.Background(), path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := readRunnerReady(context.Background(), path); err != nil || got != want {
		t.Fatalf("valid receipt returned %+v, %v", got, err)
	}
	if got, err := readRunnerReady(expired, path); !errors.Is(err, context.DeadlineExceeded) || got != (runnerID{}) {
		t.Fatalf("ready receipt after deadline returned %+v, %v", got, err)
	}
}
