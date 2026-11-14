//go:build linux

package desktopowner

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestExactOwnerExitAndCancellation(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	start, err := processStart(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIDRAVIA_DESKTOP_OWNER_START", start)
	w, err := Open(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	done := make(chan error, 1)
	go func() { done <- w.Wait(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("live owner completed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner exit not observed")
	}
	// A second wait observes the same exited identity, without reopening its PID.
	if err := w.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	start, err = processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIDRAVIA_DESKTOP_OWNER_START", start)
	self, err := Open(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- self.Wait(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel not bounded")
	}
}
func TestRejectInvalidAndMissingOwner(t *testing.T) {
	for _, pid := range []int{0, -1, 1 << 30} {
		if w, err := Open(pid); err == nil {
			w.Close()
			t.Fatalf("accepted pid %d", pid)
		}
	}
}

func TestRejectChangedOwnerIdentity(t *testing.T) {
	t.Setenv("SIDRAVIA_DESKTOP_OWNER_START", "0")
	if w, err := Open(os.Getpid()); err == nil {
		w.Close()
		t.Fatal("accepted changed identity")
	}
	t.Setenv("SIDRAVIA_DESKTOP_OWNER_START", "")
	if w, err := Open(os.Getpid()); err == nil {
		w.Close()
		t.Fatal("accepted missing identity")
	}
}
