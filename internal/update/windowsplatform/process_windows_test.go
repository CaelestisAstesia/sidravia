//go:build windows

package windowsplatform

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestGenerationRunningWithRetainedExitedProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestGenerationProcessHelper$")
	child.Env = append(os.Environ(), "SIDRAVIA_TEST_GENERATION_PROCESS=1")
	child.Stdout, child.Stderr = io.Discard, io.Discard
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		stdin.Close()
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	pid := uint32(child.Process.Pid)
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	var start, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(handle, &start, &exit, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	created := uint64(start.HighDateTime)<<32 | uint64(start.LowDateTime)
	if running, e := GenerationRunning(pid, created); e != nil || !running {
		t.Fatalf("live generation: running=%v, err=%v", running, e)
	}
	if running, e := GenerationRunning(pid, created+1); e != nil || running {
		t.Fatalf("different generation: running=%v, err=%v", running, e)
	}
	if _, e := GenerationRunning(pid, 0); e == nil {
		t.Fatal("missing creation identity accepted")
	}
	if _, e := GenerationRunning(0, created); e == nil {
		t.Fatal("missing process identity accepted")
	}
	stdin.Close()
	if err = child.Wait(); err != nil {
		t.Fatal(err)
	}
	if running, e := GenerationRunning(pid, created); e != nil || running {
		t.Fatalf("retained exited generation: running=%v, err=%v", running, e)
	}
}

func TestGenerationProcessHelper(t *testing.T) {
	if os.Getenv("SIDRAVIA_TEST_GENERATION_PROCESS") != "1" {
		t.Skip("subprocess helper")
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}
