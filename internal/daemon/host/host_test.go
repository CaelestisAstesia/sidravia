package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestShutdownHTTPServerInvokesShutdownOnce(t *testing.T) {
	t.Parallel()

	count := 0
	err := shutdownHTTPServer(func(context.Context) error {
		count++
		return nil
	})

	if err != nil {
		t.Fatalf("shutdownHTTPServer() error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("shutdown function called %d times, want 1", count)
	}
}

func TestShutdownHTTPServerUsesFiveSecondContextBoundedByClock(t *testing.T) {
	t.Parallel()

	var elapsed time.Duration
	err := shutdownHTTPServer(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("shutdown context has no deadline")
		}
		elapsed = time.Until(deadline)
		if elapsed <= 0 {
			t.Fatalf("shutdown context deadline is not in the future: %v", elapsed)
		}
		if elapsed > 5*time.Second+250*time.Millisecond {
			t.Fatalf("shutdown timeout %v is longer than expected five-second bound", elapsed)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("shutdownHTTPServer() error = %v, want nil", err)
	}
}

func TestShutdownHTTPServerNilShutdownReturnsNil(t *testing.T) {
	t.Parallel()

	err := shutdownHTTPServer(func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("shutdownHTTPServer() error = %v, want nil", err)
	}
}

func TestShutdownHTTPServerWrapsFailureWithStaticLabel(t *testing.T) {
	t.Parallel()

	cause := errors.New("shutdown failed sentinel")
	err := shutdownHTTPServer(func(context.Context) error {
		return cause
	})

	if err == nil {
		t.Fatal("shutdownHTTPServer() error = nil, want wrapped sentinel")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, %v) = false, want true", err, cause)
	}
	if !strings.Contains(err.Error(), "host: shutdown") {
		t.Fatalf("shutdown error = %v, want static host-shutdown label", err)
	}
}
