//go:build linux

package transaction

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"sidravia/internal/release"
)

func TestRuntimeAdmissionChild(t *testing.T) {
	if os.Getenv("SIDRAVIA_ADMISSION_FIXTURE") != "child" {
		return
	}
	s, err := Open(os.Getenv("SIDRAVIA_ADMISSION_ROOT"), os.Getenv("SIDRAVIA_ADMISSION_TARGET"), release.PublisherTrust())
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.AcquireStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Println("ready")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeLeaseAcrossProcesses(t *testing.T) {
	s, r, _, root, target := fixture(t)
	child := exec.Command(os.Args[0], "-test.run=^TestRuntimeAdmissionChild$")
	child.Env = append(os.Environ(), "SIDRAVIA_ADMISSION_FIXTURE=child", "SIDRAVIA_ADMISSION_ROOT="+root, "SIDRAVIA_ADMISSION_TARGET="+target)
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	ready := make(chan error, 1)
	go func() {
		line, e := bufio.NewReader(stdout).ReadString('\n')
		if e == nil && line != "ready\n" {
			e = ErrInvalid
		}
		ready <- e
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child admission timed out")
	}
	// Ordinary runtime leases coexist, even across process boundaries.
	own, err := s.AcquireStart(context.Background())
	if err != nil {
		t.Fatal("runtime leases are not shared", err)
	}
	own()
	if _, err := s.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []Phase{Verified, Preparing, Quiescent} {
		next, _, err := s.Advance(context.Background(), r.ID, r.Phase, phase, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = next
	}
	if release, err := s.AcquireExecution(context.Background(), r.ID); err == nil {
		release()
		t.Fatal("live child allowed replacement")
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	execution, err := s.AcquireExecution(context.Background(), r.ID)
	if err != nil {
		t.Fatal("exited child retained lease", err)
	}
	execution()
	if release, err := s.AcquireStart(context.Background()); err == nil {
		release()
		t.Fatal("quiescent durable gate lost with child exit")
	}
}
