//go:build linux

package portable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sidravia/internal/update/linuxplatform"
	"strconv"
	"syscall"
)

type applicationProcess = linuxplatform.Process

func observeProcess(pid int) (*applicationProcess, error) { return linuxplatform.Observe(pid) }
func daemonName() string                                  { return "sidraviad" }
func guiRuntime(target string) string                     { return filepath.Join(target, "gui-runtime", "sidravia_gui") }
func guiEntry(target string) string                       { return filepath.Join(target, "sidravia_gui") }
func finishApplication(string, string) error              { return nil }
func observeExecutable(pid int, path string) (*applicationProcess, error) {
	process, err := linuxplatform.Observe(pid)
	if err != nil {
		return nil, err
	}
	expected, err := os.Stat(path)
	if err != nil {
		process.Close()
		return nil, err
	}
	actual, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil || process.Executable != path || !os.SameFile(expected, actual) {
		process.Close()
		return nil, errors.Join(ErrChanged, err)
	}
	return process, nil
}

func startApplication(path string, args, env []string) (int, error) {
	command := exec.Command(path, args...)
	command.Env = env
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	command.Stdout = null
	command.Stderr = null
	err = command.Start()
	null.Close()
	if err != nil {
		return 0, err
	}
	go func() { _ = command.Wait() }()
	return command.Process.Pid, nil
}
