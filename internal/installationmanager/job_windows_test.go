//go:build windows

package installationmanager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestManagerJobChild(t *testing.T) {
	pidFile := os.Getenv("SIDRAVIA_JOB_TEST_PID_FILE")
	if pidFile == "" {
		t.Skip("helper subprocess only")
	}
	if err := ensureManagerJob(); err != nil {
		t.Fatal(err)
	}
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(filepath.Join(dir, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		child.Process.Kill()
		child.Wait()
		t.Fatal(err)
	}
	_ = child.Wait()
}

func TestManagerJobLifetime(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	helper := exec.Command(exe, "-test.run=^TestManagerJobChild$")
	helper.Env = append(os.Environ(), "SIDRAVIA_JOB_TEST_PID_FILE="+pidFile)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = helper.Process.Kill(); _ = helper.Wait() }()
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("fixture child did not start")
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// Killing the helper is expected; verify its child actually terminates.
	_ = helper.Wait()
	result, err := windows.WaitForSingleObject(process, 5000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("orphaned adapter child: wait=%d error=%v", result, err)
	}
}
