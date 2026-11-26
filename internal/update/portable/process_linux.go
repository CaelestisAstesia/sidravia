//go:build linux

package portable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sidravia/internal/process/linuxproc"
	"strconv"
	"strings"
	"syscall"
)

type applicationProcess = linuxproc.Process

func observeProcess(pid int) (*applicationProcess, error) { return linuxproc.Observe(pid) }
func daemonName() string                                  { return "sidraviad" }
func guiRuntime(target string) string                     { return filepath.Join(target, "gui-runtime", "sidravia_gui") }
func guiEntry(target string) string                       { return filepath.Join(target, "sidravia_gui") }
func observeExecutable(pid int, path string) (*applicationProcess, error) {
	process, err := linuxproc.Observe(pid)
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

// Track the graphical process by executable and pidfd. The service IPC no
// longer reports a desktop owner, so that field cannot locate an open GUI.
func observeManagedGUI(target string) (*applicationProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var gui *applicationProcess
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil || strings.TrimSuffix(exe, " (deleted)") != guiRuntime(target) {
			continue
		}
		current, err := observeExecutable(pid, guiRuntime(target))
		if err != nil || gui != nil {
			if current != nil {
				current.Close()
			}
			if gui != nil {
				gui.Close()
			}
			return nil, errors.Join(ErrPreparation, err)
		}
		gui = current
	}
	return gui, nil
}
