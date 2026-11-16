//go:build linux

package portable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sidravia/internal/update/linuxplatform"
	"strings"
	"testing"
	"time"

	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

func verifiedOperation(t *testing.T, target string, old, next testImage) (*operations.Store, string) {
	t.Helper()
	store, err := operations.Open(t.TempDir(), target, next.keys)
	if err != nil {
		t.Fatal(err)
	}
	r, err := operations.New(strings.Repeat("d", 32), update.Target{ID: target, Version: old.inventory.manifest.Version, BuildID: old.inventory.manifest.BuildID, Layout: "portable"}, next.inventory.candidate, next.verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(context.Background(), r.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return store, r.ID
}
func TestExecuteStoppedApplicationTransfersLeaseAndNeverStartsIt(t *testing.T) {
	target := t.TempDir()
	old := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
	next := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
	install(t, target, old)
	store, id := verifiedOperation(t, target, old, next)
	payload := extract(t, target, next)
	release, err := store.AcquireStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := Execute(context.Background(), Execution{Store: store, OperationID: id, Target: target, Old: old.inventory, Payload: payload, ReleaseRuntime: release}); err != nil {
		t.Fatal(err)
	}
	r, _, err := store.Read(context.Background())
	if err != nil || r.Phase != operations.Completed || r.Recovery == nil || r.Recovery.Intent.Mode != "none" {
		t.Fatal(r.Phase, err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "program")); string(data) != "new" {
		t.Fatal("file application missing")
	}
	if err := Execute(context.Background(), Execution{Store: store, OperationID: id, Target: target, Old: old.inventory, Payload: payload}); err == nil {
		t.Fatal("replayed operation admitted")
	}
}

func TestAbandonedRecoveryCanBeReconciledWithoutLaunchingAgain(t *testing.T) {
	target := t.TempDir()
	old := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
	next := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
	install(t, target, next)
	store, id := verifiedOperation(t, target, old, next)
	child := exec.Command("/usr/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	owner, err := linuxplatform.Observe(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	record, _, _ := store.Read(context.Background())
	recovery := operations.Recovery{SourceFilesSHA256: digest(old.inventory.data), Intent: update.RestoreIntent{Mode: "headless"}, CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: strings.Repeat("a", 32), ExpiresAt: record.CreatedAt.Add(20 * time.Minute), LogLevel: "info"}
	if err := store.SetRecovery(context.Background(), id, recovery, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase := operations.Verified
	for _, nextPhase := range []operations.Phase{operations.Preparing, operations.Quiescent, operations.Applying, operations.Applied, operations.Resuming} {
		if _, _, err := store.Advance(context.Background(), id, phase, nextPhase, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		phase = nextPhase
	}
	if err := ReconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
		t.Fatal("live owner reconciled")
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	if err := ReconcileInterrupted(context.Background(), store, id, target, next.inventory); err != nil {
		t.Fatal(err)
	}
	record, _, err = store.Read(context.Background())
	if err != nil || record.Phase != operations.ResumeFailed {
		t.Fatal(record.Phase, err)
	}
	if err := ReconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
		t.Fatal("orphan recovery replayed")
	}
}

func TestWaitingVerifiedRecordGetsFreshFiniteRecoveryWindow(t *testing.T) {
	for _, age := range []time.Duration{2 * time.Hour, 29*time.Minute + 50*time.Second} {
		t.Run(age.String(), func(t *testing.T) {
			target := t.TempDir()
			old := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
			next := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
			install(t, target, old)
			store, err := operations.Open(t.TempDir(), target, next.keys)
			if err != nil {
				t.Fatal(err)
			}
			r, err := operations.New(strings.Repeat("c", 32), update.Target{ID: target, Version: "1.0.0", BuildID: "old", Layout: "portable"}, next.inventory.candidate, next.verified, time.Now().Add(-age))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Advance(context.Background(), r.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			p := extract(t, target, next)
			if err := Execute(context.Background(), Execution{Store: store, OperationID: r.ID, Target: target, Old: old.inventory, Payload: p}); err != nil {
				t.Fatal(err)
			}
			current, _, err := store.Read(context.Background())
			if err != nil || current.Recovery == nil || current.Recovery.IssuedAt.Before(before) || current.Recovery.ExpiresAt.Before(before.Add(29*time.Minute)) {
				t.Fatal("stale record shortened recovery window", err)
			}
		})
	}
}

func TestReconcileCannotUseCoreInventoryToUnlockGUIOperation(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("GUI payload is AMD64")
	}
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint("resuming=", resume), func(t *testing.T) {
			target := t.TempDir()
			old := guiFixture(t, "1.0.0", "old")
			next := guiFixture(t, "1.1.0", "new")
			core := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
			if resume {
				core = image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
			}
			install(t, target, core)
			store, id := verifiedOperation(t, target, old, next)
			owner := exec.Command("/usr/bin/sleep", "30")
			if err := owner.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { owner.Process.Kill(); owner.Wait() }()
			peer, err := linuxplatform.Observe(owner.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			r, _, _ := store.Read(context.Background())
			rec := operations.Recovery{SourceFilesSHA256: digest(old.inventory.data), Intent: update.RestoreIntent{Mode: "desktop"}, CoordinatorPID: peer.PID, CoordinatorStart: peer.Start, Nonce: strings.Repeat("a", 32), ExpiresAt: r.CreatedAt.Add(20 * time.Minute), LogLevel: "info"}
			if err := store.SetRecovery(context.Background(), id, rec, time.Now()); err != nil {
				t.Fatal(err)
			}
			phase := operations.Verified
			phases := []operations.Phase{operations.Preparing}
			if resume {
				phases = append(phases, operations.Quiescent, operations.Applying, operations.Applied, operations.Resuming)
			}
			for _, nextPhase := range phases {
				if _, _, err := store.Advance(context.Background(), id, phase, nextPhase, "", time.Now()); err != nil {
					t.Fatal(err)
				}
				phase = nextPhase
			}
			owner.Process.Signal(os.Interrupt)
			owner.Wait()
			if err := ReconcileInterrupted(context.Background(), store, id, target, core.inventory); err == nil {
				t.Fatal("core inventory unlocked a GUI tree")
			}
			r, _, err = store.Read(context.Background())
			if err != nil || r.Phase != phase {
				t.Fatal("mismatch changed phase", r.Phase, err)
			}
		})
	}
}
