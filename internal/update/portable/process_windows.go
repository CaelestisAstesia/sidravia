//go:build windows

package portable

import (
	"context"
	"errors"
	"os"
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
	if !strings.EqualFold(p.native.Path, path) || !p.native.Running() {
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
func finishApplication(target, id string) error {
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := openRegular(root, "sidravia.update")
	if err != nil {
		return err
	}
	data := make([]byte, 34)
	n, readErr := f.Read(data)
	closeErr := f.Close()
	if readErr != nil || string(data[:n]) != id+"\n" {
		return errors.Join(ErrChanged, readErr, closeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Remove("sidravia.update")
}
