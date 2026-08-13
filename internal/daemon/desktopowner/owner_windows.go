//go:build windows

package desktopowner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

const waitSlice = 100 * time.Millisecond

const (
	waitObject0 = uint32(windows.WAIT_OBJECT_0)
	waitTimeout = uint32(windows.WAIT_TIMEOUT)
)

type processHandle interface {
	wait(uint32) (uint32, error)
	close() error
}

type windowsHandle windows.Handle

func (h windowsHandle) wait(milliseconds uint32) (uint32, error) {
	return windows.WaitForSingleObject(windows.Handle(h), milliseconds)
}
func (h windowsHandle) close() error { return windows.CloseHandle(windows.Handle(h)) }

type watcher struct {
	handle   processHandle
	once     sync.Once
	closeErr error
}

func Open(pid int) (Watcher, error) {
	return openWith(pid, func(pid uint32) (processHandle, error) {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
		if err != nil {
			return nil, err
		}
		return windowsHandle(h), nil
	})
}

func openWith(pid int, open func(uint32) (processHandle, error)) (Watcher, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("desktop owner pid: %w", ErrUnsupported)
	}
	h, err := open(uint32(pid))
	if err != nil {
		return nil, fmt.Errorf("open desktop owner: %w", err)
	}
	return &watcher{handle: h}, nil
}

func (w *watcher) Wait(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		status, err := w.handle.wait(uint32(waitSlice / time.Millisecond))
		if err != nil {
			return fmt.Errorf("wait desktop owner: %w", err)
		}
		switch status {
		case waitObject0:
			return nil
		case waitTimeout:
			continue
		default:
			return fmt.Errorf("wait desktop owner returned %d", status)
		}
	}
}

func (w *watcher) Close() error {
	w.once.Do(func() { w.closeErr = w.handle.close() })
	return w.closeErr
}
