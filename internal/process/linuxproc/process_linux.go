//go:build linux

// Package linuxproc pins same-user local processes by pidfd. It never signals by
// numeric PID, chooses user intent, or owns product execution state.
package linuxproc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrPeer = errors.New("Linux update process identity unavailable or changed")

type Process struct {
	PID        int
	Start      string
	Executable string
	UID        uint32
	fd         int
	owned      bool
}

func Observe(pid int) (*Process, error) {
	if pid <= 0 {
		return nil, ErrPeer
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Process, error) { unix.Close(fd); return nil, err }
	directory := filepath.Join("/proc", strconv.Itoa(pid))
	info, err := os.Stat(directory)
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fail(ErrPeer)
	}
	start, _, err := statFields(pid)
	if err != nil {
		return fail(err)
	}
	exe, err := os.Readlink(filepath.Join(directory, "exe"))
	if err != nil {
		return fail(err)
	}
	p := &Process{PID: pid, Start: start, Executable: exe, UID: stat.Uid, fd: fd, owned: true}
	alive, err := p.Running()
	if err != nil || !alive {
		return fail(errors.Join(ErrPeer, err))
	}
	return p, nil
}
func (p *Process) Close() error {
	if p == nil || !p.owned || p.fd < 0 {
		return nil
	}
	err := unix.Close(p.fd)
	p.fd = -1
	p.owned = false
	return err
}
func (p *Process) Running() (bool, error) {
	if p == nil || !p.owned || p.fd < 0 {
		return false, ErrPeer
	}
	poll := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
	_, err := unix.Poll(poll, 0)
	if err != nil {
		return false, err
	}
	if poll[0].Revents&(unix.POLLNVAL|unix.POLLERR) != 0 {
		return false, ErrPeer
	}
	return poll[0].Revents == 0, nil
}
func (p *Process) Wait(ctx context.Context) error {
	if p == nil || !p.owned || p.fd < 0 {
		return ErrPeer
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		poll := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
		_, err := unix.Poll(poll, 50)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if poll[0].Revents&(unix.POLLNVAL|unix.POLLERR) != 0 {
			return ErrPeer
		}
		if poll[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			return nil
		}
	}
}
func statFields(pid int) (string, int, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", 0, err
	}
	at := strings.LastIndexByte(string(data), ')')
	if at < 0 {
		return "", 0, ErrPeer
	}
	fields := strings.Fields(string(data)[at+1:])
	if len(fields) <= 19 {
		return "", 0, ErrPeer
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, ErrPeer
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", 0, ErrPeer
	}
	return fields[19], parent, nil
}

// DescendsFrom proves the live coordinator generation is an ancestor. Process
// reparenting, PID reuse, unreadable peers and overly deep chains fail closed.
func DescendsFrom(pid, coordinator int, start string) error {
	owner, err := Observe(coordinator)
	if err != nil {
		return err
	}
	defer owner.Close()
	if owner.Start != start {
		return ErrPeer
	}
	for depth := 0; depth < 64 && pid > 1; depth++ {
		observed, err := Observe(pid)
		if err != nil {
			return err
		}
		ownStart, parent, err := statFields(pid)
		observed.Close()
		if err != nil || ownStart != observed.Start {
			return ErrPeer
		}
		if pid == coordinator {
			if ownStart == start {
				return nil
			}
			return ErrPeer
		}
		pid = parent
	}
	return ErrPeer
}

// GenerationRunning observes only liveness/start ticks; restricted executable
// links do not turn an unknown peer into a claimed-dead coordinator.
func GenerationRunning(pid int, start string) (bool, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	actual, _, err := statFields(pid)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if actual != start {
		return false, nil
	}
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err = unix.Poll(poll, 0)
	if err != nil {
		return false, err
	}
	if poll[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
		return false, ErrPeer
	}
	return poll[0].Revents == 0, nil
}
