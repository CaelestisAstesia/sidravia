//go:build linux

package linuxproc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPidfdIdentityExitAndAncestry(t *testing.T) {
	self, err := Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	child := exec.Command("/usr/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	peer, err := Observe(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := DescendsFrom(peer.PID, self.PID, self.Start); err != nil {
		t.Fatal(err)
	}
	if err := DescendsFrom(peer.PID, self.PID, "1"); err == nil {
		t.Fatal("wrong coordinator generation accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := peer.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	wait, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := peer.Wait(wait); err != nil {
		t.Fatal(err)
	}
	running, err := peer.Running()
	if err != nil || running {
		t.Fatal("exited generation alive", err)
	}
}

func TestZeroAndClosedProcessDoNotObserveUnownedDescriptors(t *testing.T) {
	var zero Process
	if err := zero.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := zero.Running(); !errors.Is(err, ErrPeer) {
		t.Fatal(err)
	}
	if err := zero.Wait(context.Background()); !errors.Is(err, ErrPeer) {
		t.Fatal(err)
	}
	p, err := Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(context.Background()); !errors.Is(err, ErrPeer) {
		t.Fatal(err)
	}
}
