//go:build windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"sidravia/internal/daemon/desktopowner"
	"sidravia/internal/launchcontract"
)

const windowsDesktopOwnerHelperEnv = "SIDRAVIA_TEST_DESKTOP_OWNER_HELPER_7E1A9C2F"

func TestWindowsDesktopOwnerProcessHelper(t *testing.T) {
	if os.Getenv(windowsDesktopOwnerHelperEnv) != "1" {
		return
	}
	var release [1]byte
	if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
		t.Fatalf("helper release read: %v", err)
	}
}

func TestWindowsDesktopOwnerExitUsesRealProcessHandle(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsDesktopOwnerProcessHelper$")
	cmd.Env = append(os.Environ(), windowsDesktopOwnerHelperEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin: %v", err)
	}
	var helperOutput bytes.Buffer
	cmd.Stdout = &helperOutput
	cmd.Stderr = &helperOutput
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper start: %v", err)
	}

	var owner desktopowner.Watcher
	var runtimeResult <-chan error
	helperReaped := false
	runtimeReaped := false
	t.Cleanup(func() {
		if !helperReaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if runtimeResult != nil && !runtimeReaped {
			select {
			case <-runtimeResult:
			case <-time.After(5 * time.Second):
			}
		}
		if owner != nil {
			_ = owner.Close()
		}
	})

	owner, err = desktopowner.Open(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("open desktop owner: %v", err)
	}
	options, err := launchcontract.Desktop(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("desktop options: %v", err)
	}
	rt, observer, hostRunner, sink, shutdown := newCoordinatedLifecycle(t, discardLogger())
	rt.launchOptions = options
	rt.desktopOwner = owner
	runtimeResult = runRuntime(rt, context.Background())
	waitForSignal(t, hostRunner.started, "host start")
	waitForSignal(t, observer.started, "observer start")
	waitForSignal(t, sink.started, "delivery start")

	select {
	case err := <-runtimeResult:
		runtimeReaped = true
		t.Fatalf("runtime returned before owner exit: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release helper: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close helper stdin: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper exit: %v", err)
	}
	helperReaped = true

	if err := waitForRuntimeResult(t, runtimeResult); err != nil {
		runtimeReaped = true
		t.Fatalf("owner exit: %v", err)
	}
	runtimeReaped = true
	assertShutdown(t, shutdown)
	if err := owner.Close(); err != nil {
		t.Fatalf("second owner close: %v", err)
	}
}
