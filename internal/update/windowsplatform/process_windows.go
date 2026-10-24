//go:build windows

// Package windowsplatform implements Windows mechanisms, not update policy.
package windowsplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrPeer = errors.New("update peer identity could not be confirmed")

type Process struct {
	handle    windows.Handle
	PID       uint32
	Created   uint64
	Path      string
	SID       string
	Integrity uint32
}

func Observe(pid uint32) (*Process, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	p := &Process{handle: h, PID: pid}
	fail := func(err error) (*Process, error) { p.Close(); return nil, err }
	var created, exited, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return fail(err)
	}
	p.Created = uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	name := make([]uint16, 32768)
	n := uint32(len(name))
	if err = windows.QueryFullProcessImageName(h, 0, &name[0], &n); err != nil {
		return fail(err)
	}
	p.Path = windows.UTF16ToString(name[:n])
	var token windows.Token
	if err = windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err != nil {
		return fail(err)
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return fail(err)
	}
	p.SID = u.User.Sid.String()
	var needed uint32
	err = windows.GetTokenInformation(token, windows.TokenIntegrityLevel, nil, 0, &needed)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || needed < uint32(unsafe.Sizeof(windows.Tokenmandatorylabel{})) {
		return fail(ErrPeer)
	}
	buf := make([]byte, needed)
	if err = windows.GetTokenInformation(token, windows.TokenIntegrityLevel, &buf[0], needed, &needed); err != nil {
		return fail(err)
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
	sid := label.Label.Sid
	if sid.SubAuthorityCount() == 0 {
		return fail(ErrPeer)
	}
	p.Integrity = sid.SubAuthority(uint32(sid.SubAuthorityCount() - 1))
	if token.IsElevated() || p.Integrity != 0x2000 /* SECURITY_MANDATORY_MEDIUM_RID */ {
		return fail(ErrPeer)
	}
	return p, nil
}
func (p *Process) Close() error { return windows.CloseHandle(p.handle) }
func (p *Process) Wait(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := windows.WaitForSingleObject(p.handle, 0)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func FileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func parentPID(pid uint32) (uint32, error) {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(h, &entry); err == nil; err = windows.Process32Next(h, &entry) {
		if entry.ProcessID == pid {
			return entry.ParentProcessID, nil
		}
	}
	return 0, ErrPeer
}

// StartIndependent requires the caller to have verified the installer artifact.
// If a job does not allow breakaway, it fails before any application is stopped.
func StartIndependent(path string, args ...string) (*exec.Cmd, error) {
	var inJob int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	ok, _, err := proc.Call(uintptr(windows.CurrentProcess()), 0, uintptr(unsafe.Pointer(&inJob)))
	if ok == 0 {
		return nil, fmt.Errorf("observe process job: %w", err)
	}
	flags := uint32(0)
	if inJob != 0 {
		flags = windows.CREATE_BREAKAWAY_FROM_JOB
	}
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// ExitCode is meaningful only after Wait has confirmed process termination.
func (p *Process) ExitCode() (uint32, error) {
	var code uint32
	err := windows.GetExitCodeProcess(p.handle, &code)
	return code, err
}

// Running observes the retained process identity, never a recycled PID.
func (p *Process) Running() bool {
	result, err := windows.WaitForSingleObject(p.handle, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}

// ObserveParent retains the exact process that launched the short host.
func ObserveParent() (*Process, error) {
	pid, err := parentPID(uint32(os.Getpid()))
	if err != nil {
		return nil, err
	}
	return Observe(pid)
}
