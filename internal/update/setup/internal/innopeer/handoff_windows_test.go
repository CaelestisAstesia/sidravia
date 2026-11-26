//go:build windows

package innopeer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"sidravia/internal/process/winproc"

	"golang.org/x/sys/windows"
)

func TestOfferCannotBeReplacedAndCancelsWithoutClient(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := winproc.FileHash(exe)
	if err != nil {
		t.Fatal(err)
	}
	id := "456789abcdef0123456789abcdef0123"
	first, err := NewOffer(id, hash)
	if windows.GetCurrentProcessToken().IsElevated() {
		if first != nil {
			first.Close()
		}
		if !errors.Is(err, winproc.ErrPeer) {
			t.Fatalf("elevated offer must be rejected: %v", err)
		}
		t.Skip("elevated refusal verified; handoff success requires medium integrity")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := NewOffer(id, hash); err == nil {
		second.Close()
		t.Fatal("pipe replaced")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err = first.Await(ctx, first.owner); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestObserveRejectsNonexistentProcess(t *testing.T) {
	if p, err := winproc.Observe(0); err == nil {
		p.Close()
		t.Fatal("invalid process accepted")
	}
}

func TestHandoffRejectsUnlaunchedCoordinator(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := winproc.FileHash(exe)
	if err != nil {
		t.Fatal(err)
	}
	id := "56789abcdef0123456789abcdef01234"
	offer, err := NewOffer(id, hash)
	if windows.GetCurrentProcessToken().IsElevated() {
		if offer != nil {
			offer.Close()
		}
		if !errors.Is(err, winproc.ErrPeer) {
			t.Fatalf("elevated offer must be rejected: %v", err)
		}
		t.Skip("elevated refusal verified; coordinator test requires medium integrity")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer offer.Close()
	cmd := exec.Command(exe, "-test.run=^TestHandoffProbe$")
	cmd.Env = append(os.Environ(), "SIDRAVIA_TEST_PIPE="+id, "SIDRAVIA_TEST_SERVER="+strconv.Itoa(os.Getpid()))
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	peer, err := winproc.Observe(uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(err)
	}
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// The helper is the updater's child, but it reports updater as coordinator.
	// A trusted helper hash alone must not authorize this unrelated parent.
	_, err = offer.Await(ctx, peer)
	if !errors.Is(err, winproc.ErrPeer) {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("foreign coordinator: %v", err)
	}
	offer.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestHandoffProbe(t *testing.T) {
	id := os.Getenv("SIDRAVIA_TEST_PIPE")
	if id == "" {
		t.Skip("subprocess helper")
	}
	value, _ := strconv.ParseUint(os.Getenv("SIDRAVIA_TEST_SERVER"), 10, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Acknowledge(ctx, id, uint32(value), uint32(value)); err == nil {
		t.Fatal("unlaunched coordinator accepted")
	}
}
