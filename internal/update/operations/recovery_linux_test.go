//go:build linux

package operations

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"sidravia/internal/process/linuxproc"
	"sidravia/internal/update"
	"sidravia/internal/update/releases"
)

func TestRecoveryAdmissionChild(t *testing.T) {
	if os.Getenv("SIDRAVIA_RECOVERY_FIXTURE") != "child" {
		return
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	keys, _ := releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	store, err := Open(os.Getenv("SIDRAVIA_FIXTURE_ROOT"), os.Getenv("SIDRAVIA_FIXTURE_TARGET"), keys)
	if err != nil {
		t.Fatal(err)
	}
	token := os.Getenv("SIDRAVIA_FIXTURE_NONCE")
	release, err := store.AcquireRecoveryStart(context.Background(), strings.Repeat("a", 32), token, "1.0.0", "new", "fixture", "")
	want := os.Getenv("SIDRAVIA_FIXTURE_ALLOWED") == "yes"
	if (err == nil) != want {
		t.Fatalf("unexpected admission: %v", err)
	}
	if release != nil {
		release()
	}
	fmt.Println("checked")
}
func TestRecoveryBoundToPhaseIdentityNonceAndCoordinatorTree(t *testing.T) {
	s, r, _, root, target := fixture(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Advance(ctx, r.ID, Created, Verified, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	owner, err := linuxproc.Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	recovery := Recovery{SourceFilesSHA256: strings.Repeat("a", 64), Intent: update.RestoreIntent{Mode: "headless"}, Namespace: "fixture", CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: strings.Repeat("b", 32), ExpiresAt: r.CreatedAt.Add(20 * time.Minute), LogLevel: "info"}
	if err := s.SetRecovery(ctx, r.ID, recovery, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecovery(ctx, r.ID, recovery, time.Now()); err == nil {
		t.Fatal("rewrote frozen intent")
	}
	phase := Verified
	for _, next := range []Phase{Preparing, Quiescent, Applying, Applied, Resuming} {
		if _, _, err := s.Advance(ctx, r.ID, phase, next, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		phase = next
	}
	if release, err := s.AcquireStart(ctx); err == nil {
		release()
		t.Fatal("ordinary startup bypassed Resuming")
	}
	// The coordinator itself is not a descendant launch. Knowing its nonce is
	// not a blanket bypass for another process tree or the wrong new build.
	if release, err := s.AcquireRecoveryStart(ctx, r.ID, recovery.Nonce, "1.0.0", "new", "fixture", ""); err == nil {
		release()
		t.Fatal("coordinator self-admitted")
	}
	run := func(token string, allowed bool) {
		command := exec.Command(os.Args[0], "-test.run=^TestRecoveryAdmissionChild$")
		yes := "no"
		if allowed {
			yes = "yes"
		}
		command.Env = append(os.Environ(), "SIDRAVIA_RECOVERY_FIXTURE=child", "SIDRAVIA_FIXTURE_ROOT="+root, "SIDRAVIA_FIXTURE_TARGET="+target, "SIDRAVIA_FIXTURE_NONCE="+token, "SIDRAVIA_FIXTURE_ALLOWED="+yes)
		command.Stdin = strings.NewReader("")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %s %v", out, err)
		}
	}
	run(recovery.Nonce, true)
	run(strings.Repeat("c", 32), false)
	if _, _, err := s.Advance(ctx, r.ID, Resuming, Completed, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	run(recovery.Nonce, false)
}
