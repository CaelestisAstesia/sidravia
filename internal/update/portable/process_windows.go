//go:build windows

package portable

import (
	"context"
	"golang.org/x/sys/windows"
	"path/filepath"
	"sidravia/internal/update/windowsplatform"
	"strconv"
	"strings"
)

// Common coordination uses a stable PID/creation generation on both platforms.
type applicationProcess struct {
	native *windowsplatform.Process
	PID    int
	Start  string
}

func observeProcess(pid int) (*applicationProcess, error) {
	if pid <= 0 || uint64(pid) > 1<<32-1 {
		return nil, ErrInvalid
	}
	p, err := windowsplatform.Observe(uint32(pid))
	if err != nil {
		return nil, err
	}
	return &applicationProcess{p, int(p.PID), strconv.FormatUint(p.Created, 10)}, nil
}
func (p *applicationProcess) Close() error                   { return p.native.Close() }
func (p *applicationProcess) Running() (bool, error)         { return p.native.Running(), nil }
func (p *applicationProcess) Wait(ctx context.Context) error { return p.native.Wait(ctx) }
func observeExecutable(pid int, path string) (*applicationProcess, error) {
	p, err := observeProcess(pid)
	if err != nil {
		return nil, err
	}
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		p.Close()
		return nil, e
	}
	if p.native.SID != user.User.Sid.String() || !strings.EqualFold(p.native.Path, path) || !p.native.Running() {
		p.Close()
		return nil, ErrChanged
	}
	return p, nil
}
func daemonName() string              { return "sidraviad.exe" }
func guiRuntime(target string) string { return filepath.Join(target, "Sidravia.exe") }
func guiEntry(target string) string   { return guiRuntime(target) }
func startApplication(path string, args, env []string) (int, error) {
	cmd, err := windowsplatform.StartApplicationEnvironment(path, args, env)
	if err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}
func observeManagedGUI(string) (*applicationProcess, error) { return nil, ErrUnsupported }
