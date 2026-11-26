//go:build windows

package desktopowner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeHandle struct {
	mu          sync.Mutex
	waits       []uint32
	closeCount  int
	waitErr     error
	statuses    []uint32
	waitStarted chan struct{}
	startOnce   sync.Once
}

func (h *fakeHandle) wait(v uint32) (uint32, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.waits = append(h.waits, v)
	h.startOnce.Do(func() {
		if h.waitStarted != nil {
			close(h.waitStarted)
		}
	})
	if h.waitErr != nil {
		return 0, h.waitErr
	}
	if len(h.statuses) > 0 {
		status := h.statuses[0]
		h.statuses = h.statuses[1:]
		return status, nil
	}
	return 0, nil
}

func TestWatcherCancellationAfterTimeoutClosesOnlyOnce(t *testing.T) {
	h := &fakeHandle{statuses: []uint32{waitTimeout, waitTimeout}, waitStarted: make(chan struct{})}
	w, err := openWith(42, func(uint32) (processHandle, error) { return h, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Wait(ctx) }()
	select {
	case <-h.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("watcher did not wait")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("wait=%v", err)
	}
	_ = w.Close()
	_ = w.Close()
	h.mu.Lock()
	closes := h.closeCount
	h.mu.Unlock()
	if closes != 1 {
		t.Fatalf("closes=%d", closes)
	}
}

func TestOpenFailurePreservesCause(t *testing.T) {
	cause := errors.New("open")
	if _, err := openWith(42, func(uint32) (processHandle, error) { return nil, cause }); !errors.Is(err, cause) {
		t.Fatalf("err=%v", err)
	}
}
func (h *fakeHandle) close() error { h.mu.Lock(); defer h.mu.Unlock(); h.closeCount++; return nil }

func TestWatcherOwnsOneHandleAndClosesOnce(t *testing.T) {
	h := &fakeHandle{}
	w, err := openWith(42, func(uint32) (processHandle, error) { return h, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	waitCount := len(h.waits)
	h.mu.Unlock()
	if waitCount != 1 {
		t.Fatalf("waits=%v", h.waits)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	closes := h.closeCount
	h.mu.Unlock()
	if closes != 1 {
		t.Fatalf("closes=%d", closes)
	}
}

func TestWatcherReportsWaitFailure(t *testing.T) {
	cause := errors.New("wait")
	w, err := openWith(42, func(uint32) (processHandle, error) { return &fakeHandle{waitErr: cause}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(w.Wait(context.Background()), cause) {
		t.Fatal("wait cause lost")
	}
}
