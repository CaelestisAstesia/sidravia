//go:build linux

package portable

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
	"sidravia/internal/process/linuxproc"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
)

func verifiedOperation(t *testing.T, target string, old, next testImage) (*transaction.Store, string) {
	t.Helper()
	store, err := transaction.Open(t.TempDir(), target, next.keys)
	if err != nil {
		t.Fatal(err)
	}
	r, err := transaction.New(strings.Repeat("d", 32), change.Target{ID: target, Version: old.Manifest.Version, BuildID: old.Manifest.BuildID, Layout: "portable"}, next.Candidate, next.verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(context.Background(), r.ID, transaction.Created, transaction.Verified, "", time.Now()); err != nil {
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
	if err := execute(context.Background(), execution{Store: store, OperationID: id, Target: target, Old: old.inventory, Payload: payload, ReleaseRuntime: release}); err != nil {
		t.Fatal(err)
	}
	r, _, err := store.Read(context.Background())
	if err != nil || r.Phase != transaction.Completed || r.Recovery == nil || r.Recovery.Intent.Mode != "none" {
		t.Fatal(r.Phase, err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "program")); string(data) != "new" {
		t.Fatal("file application missing")
	}
	if err := execute(context.Background(), execution{Store: store, OperationID: id, Target: target, Old: old.inventory, Payload: payload}); err == nil {
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
	owner, err := linuxproc.Observe(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	record, _, _ := store.Read(context.Background())
	recovery := transaction.Recovery{SourceFilesSHA256: digest(old.Index), Intent: maintenance.Intent{Mode: "headless"}, CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: strings.Repeat("a", 32), ExpiresAt: record.CreatedAt.Add(20 * time.Minute), LogLevel: "info"}
	if err := store.SetRecovery(context.Background(), id, recovery, time.Now()); err != nil {
		t.Fatal(err)
	}
	phase := transaction.Verified
	for _, nextPhase := range []transaction.Phase{transaction.Preparing, transaction.Quiescent, transaction.Applying, transaction.Applied, transaction.Resuming} {
		if _, _, err := store.Advance(context.Background(), id, phase, nextPhase, "", time.Now()); err != nil {
			t.Fatal(err)
		}
		phase = nextPhase
	}
	if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
		t.Fatal("live owner reconciled")
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err != nil {
		t.Fatal(err)
	}
	record, _, err = store.Read(context.Background())
	if err != nil || record.Phase != transaction.ResumeFailed {
		t.Fatal(record.Phase, err)
	}
	if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
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
			store, err := transaction.Open(t.TempDir(), target, next.keys)
			if err != nil {
				t.Fatal(err)
			}
			r, err := transaction.New(strings.Repeat("c", 32), change.Target{ID: target, Version: "1.0.0", BuildID: "old", Layout: "portable"}, next.Candidate, next.verified, time.Now().Add(-age))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Advance(context.Background(), r.ID, transaction.Created, transaction.Verified, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			p := extract(t, target, next)
			if err := execute(context.Background(), execution{Store: store, OperationID: r.ID, Target: target, Old: old.inventory, Payload: p}); err != nil {
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
			peer, err := linuxproc.Observe(owner.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			r, _, _ := store.Read(context.Background())
			rec := transaction.Recovery{SourceFilesSHA256: digest(old.Index), Intent: maintenance.Intent{Mode: "desktop"}, CoordinatorPID: peer.PID, CoordinatorStart: peer.Start, Nonce: strings.Repeat("a", 32), ExpiresAt: r.CreatedAt.Add(20 * time.Minute), LogLevel: "info"}
			if err := store.SetRecovery(context.Background(), id, rec, time.Now()); err != nil {
				t.Fatal(err)
			}
			phase := transaction.Verified
			phases := []transaction.Phase{transaction.Preparing}
			if resume {
				phases = append(phases, transaction.Quiescent, transaction.Applying, transaction.Applied, transaction.Resuming)
			}
			for _, nextPhase := range phases {
				if _, _, err := store.Advance(context.Background(), id, phase, nextPhase, "", time.Now()); err != nil {
					t.Fatal(err)
				}
				phase = nextPhase
			}
			owner.Process.Signal(os.Interrupt)
			owner.Wait()
			if err := reconcileInterrupted(context.Background(), store, id, target, core.inventory); err == nil {
				t.Fatal("core inventory unlocked a GUI tree")
			}
			r, _, err = store.Read(context.Background())
			if err != nil || r.Phase != phase {
				t.Fatal("mismatch changed phase", r.Phase, err)
			}
		})
	}
}

func TestAppliedInterruptionRequiresDeadOwnerAndExactTargetBytes(t *testing.T) {
	for _, phase := range []transaction.Phase{transaction.Applied, transaction.Resuming} {
		t.Run(string(phase), func(t *testing.T) {
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
			peer, err := linuxproc.Observe(child.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			recovery := transaction.Recovery{SourceFilesSHA256: digest(old.Index), Intent: maintenance.Intent{Mode: "headless"}, CoordinatorPID: peer.PID, CoordinatorStart: peer.Start, Nonce: strings.Repeat("a", 32), ExpiresAt: time.Now().Add(20 * time.Minute), LogLevel: "info"}
			if err := store.SetRecovery(context.Background(), id, recovery, time.Now()); err != nil {
				t.Fatal(err)
			}
			previous := transaction.Verified
			for _, p := range []transaction.Phase{transaction.Preparing, transaction.Quiescent, transaction.Applying, transaction.Applied, transaction.Resuming} {
				if _, _, err := store.Advance(context.Background(), id, previous, p, "", time.Now()); err != nil {
					t.Fatal(err)
				}
				previous = p
				if p == phase {
					break
				}
			}
			if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
				t.Fatal("live coordinator reconciled")
			}
			child.Process.Signal(os.Interrupt)
			child.Wait()
			if err := os.WriteFile(filepath.Join(target, "program"), []byte("bad"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
				t.Fatal("changed target reconciled")
			}
			if err := os.WriteFile(filepath.Join(target, "program"), []byte("new"), 0755); err != nil {
				t.Fatal(err)
			}
			wrongCandidate := next.Candidate
			wrongCandidate.ID = "99:99"
			wrong, err := filetxn.Authenticate(next.verified, wrongCandidate, next.Index)
			if err != nil {
				t.Fatal(err)
			}
			if err := reconcileInterrupted(context.Background(), store, id, target, wrong); err == nil {
				t.Fatal("wrong candidate reconciled")
			}
			if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err != nil {
				t.Fatal(err)
			}
			r, _, err := store.Read(context.Background())
			if err != nil || r.Phase != transaction.ResumeFailed || r.RequiresStartupBlock() {
				t.Fatalf("%+v %v", r, err)
			}
			if err := reconcileInterrupted(context.Background(), store, id, target, next.inventory); err == nil {
				t.Fatal("terminal recovery replayed")
			}
		})
	}
}

func TestUnstartedOperationReconciliationHonorsCoordinatorLease(t *testing.T) {
	for _, phase := range []transaction.Phase{transaction.Created, transaction.Verified} {
		t.Run(string(phase), func(t *testing.T) {
			target := t.TempDir()
			old := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
			next := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
			install(t, target, old)
			store, err := transaction.Open(t.TempDir(), target, next.keys)
			if err != nil {
				t.Fatal(err)
			}
			r, err := transaction.New(strings.Repeat("a", 32), change.Target{ID: target, Version: "1.0.0", BuildID: "old", Layout: "portable"}, next.Candidate, next.verified, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if phase == transaction.Verified {
				if _, _, err := store.Advance(context.Background(), r.ID, transaction.Created, phase, "", time.Now()); err != nil {
					t.Fatal(err)
				}
				release, err := store.AcquireCoordinator(context.Background(), r.ID)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				if err := reconcileInterrupted(ctx, store, r.ID, target, old.inventory); err == nil {
					t.Fatal("active unstarted coordinator reconciled")
				}
				cancel()
				release()
			}
			if err := reconcileInterrupted(context.Background(), store, r.ID, target, old.inventory); err != nil {
				t.Fatal(err)
			}
			current, _, err := store.Read(context.Background())
			if err != nil || current.Phase != transaction.Aborted {
				t.Fatal(current.Phase, err)
			}
		})
	}
}
