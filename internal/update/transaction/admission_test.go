package transaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sidravia/internal/release"
)

func TestStartupAdmissionSurvivesCoordinatorExit(t *testing.T) {
	s, r, roots, root, target := fixture(t)
	ctx := context.Background()
	start, err := s.AcquireStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start()
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Phase{Verified, Preparing, Quiescent} {
		next, _, err := s.Advance(ctx, r.ID, r.Phase, p, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = next
	}
	execution, err := s.AcquireExecution(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(root, target, roots)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AcquireStart(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("active execution admitted startup: %v", err)
	}
	if _, _, err := s.Advance(ctx, r.ID, Quiescent, Applying, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	execution() // simulate coordinator exit: kernel lock gone, durable state remains
	if _, err := other.AcquireStart(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("partial update admitted startup: %v", err)
	}
	if _, err := other.AcquireExecution(ctx, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("replayed apply admitted", err)
	}
	if _, _, err := s.Advance(ctx, r.ID, Applying, RepairRequired, "apply_failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := other.AcquireStart(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("repair state admitted: %v", err)
	}
}
func TestAdmissionRejectsCorruptRecordAndAllowsCompleted(t *testing.T) {
	s, r, _, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Phase{Verified, Preparing, Quiescent, Applying, Applied, Completed} {
		next, _, err := s.Advance(ctx, r.ID, r.Phase, p, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = next
	}
	release, err := s.AcquireStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.WriteFile(filepath.Join(s.directory, "current.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireStart(ctx); err == nil {
		t.Fatal("corrupt state admitted")
	}
}
func TestShortStartupExcludesExecution(t *testing.T) {
	s, r, roots, root, target := fixture(t)
	ctx := context.Background()
	startup, err := s.AcquireStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []Phase{Verified, Preparing, Quiescent} {
		next, _, err := s.Advance(ctx, r.ID, r.Phase, phase, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = next
	}
	other, err := Open(root, target, roots)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AcquireExecution(ctx, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("execution raced startup: %v", err)
	}
	startup()
	execution, err := other.AcquireExecution(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer execution()
	for _, phase := range []Phase{Applying, Applied, Completed} {
		next, _, err := s.Advance(ctx, r.ID, r.Phase, phase, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = next
	}
	if _, err := s.AcquireStart(ctx); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed but still executing admitted startup: %v", err)
	}
}

func TestMovedOrCacheClearedTargetKeepsCrashSeal(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, StartupSeal), []byte("interrupted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	moved := target + "-moved"
	if err := os.Rename(target, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(moved) })
	// A completely fresh external root represents a different XDG cache or a
	// deleted cache. The moved directory still refuses admission via its seal.
	s, err := Open(t.TempDir(), moved, release.PublisherTrust())
	if err != nil {
		t.Fatal(err)
	}
	if release, err := s.AcquireStart(context.Background()); err == nil {
		release()
		t.Fatal("moving a partial installation bypassed protection")
	}
}
