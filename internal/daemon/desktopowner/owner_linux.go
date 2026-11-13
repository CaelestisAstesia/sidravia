//go:build linux

package desktopowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sidravia/internal/launchcontract"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// pidfd pins the process identity in the kernel, even after its numeric PID is
// reused. Unsupported kernels fail closed; kill(pid, 0) is not a substitute.
type watcher struct {
	fd       int
	once     sync.Once
	closeErr error
}

func Open(pid int) (Watcher, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("desktop owner pid: %w", ErrUnsupported)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, fmt.Errorf("open desktop owner: %w", err)
	}
	// GUI captures its own start time before spawning CTL. Comparing after
	// pidfd_open prevents a delayed bootstrap from attaching to a reused PID.
	expected := os.Getenv(launchcontract.EnvDesktopOwnerStart)
	actual, err := processStart(pid)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, pollErr := unix.Poll(fds, 0)
	if err != nil || expected == "" || actual != expected || pollErr != nil || fds[0].Revents != 0 {
		_ = unix.Close(fd)
		return nil, errors.New("desktop owner identity unavailable or changed")
	}
	return &watcher{fd: fd}, nil
}

// The lifecycle owner cancels and joins Wait before Close, as on Windows.
func (w *watcher) Wait(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	fds := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		_, err := unix.Poll(fds, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("wait desktop owner: %w", err)
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return errors.New("desktop owner descriptor failed")
		}
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			return nil
		}
	}
	return nil
}
func (w *watcher) Close() error {
	w.once.Do(func() { w.closeErr = unix.Close(w.fd) })
	return w.closeErr
}

func processStart(pid int) (string, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(stat)[end+1:])
	if len(fields) <= 19 {
		return "", errors.New("missing process start time")
	}
	return fields[19], nil
}
