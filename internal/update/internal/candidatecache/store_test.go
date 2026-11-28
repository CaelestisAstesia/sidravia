package candidatecache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"
)

func TestMutationReopensTheCacheAndKeepsTheLockWhileConsuming(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("HOME", home)
	ctx := context.Background()
	selection := release.Selection{Kind: "core-portable", Channel: "stable", CurrentVersion: "1.0.0"}
	directory := t.TempDir()
	first, failure := Open(directory, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	second, failure := Open(directory, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	if _, exists, failure := second.Read(ctx); exists || failure != nil {
		t.Fatalf("expected no initial state: exists=%v failure=%+v", exists, failure)
	}
	firstState := State{SchemaVersion: 1, Check: release.CheckResult{State: "available"}}
	if out := first.WithMutation(ctx, func(m Mutation) request.Outcome { return m.Commit(ctx, firstState, "") }); !out.OK {
		t.Fatalf("first writer did not commit: %+v", out)
	}
	out := second.WithMutation(ctx, func(m Mutation) request.Outcome {
		if m.previous.Check.State != firstState.Check.State {
			t.Errorf("second writer used stale state: %+v", m.previous)
		}
		competed := first.WithMutation(ctx, func(Mutation) request.Outcome {
			t.Error("competing writer entered while selection was in use")
			return request.Success(nil)
		})
		if competed.OK || competed.Code != "update_busy" {
			t.Errorf("competing result: %+v", competed)
		}
		return request.Success(nil)
	})
	if !out.OK {
		t.Fatalf("second writer could not consume the new state: %+v", out)
	}
}

func TestMutationPreservesVerifiedDownloadAndRetiresSupersededPackage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("HOME", home)
	ctx := context.Background()
	directory := t.TempDir()
	selection := release.Selection{Kind: release.QtCoreRepository, Channel: "stable", CurrentVersion: "1.0.0"}
	store, failure := Open(directory, selection)
	if failure != nil {
		t.Fatal(failure)
	}

	writePackage := func(root, version string) (string, *release.Candidate) {
		t.Helper()
		packageDir, err := os.MkdirTemp(root, "package-")
		if err != nil {
			t.Fatal(err)
		}
		name := "qt-core-repository.zip"
		content := []byte("repository fixture for " + version)
		path := filepath.Join(packageDir, name)
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		return path, &release.Candidate{
			ID: version, Version: version, Name: name, Size: int64(len(content)),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(content)),
		}
	}
	stateFor := func(candidate *release.Candidate) State {
		return State{
			SchemaVersion: 1,
			Selection:     selection,
			Check:         release.CheckResult{State: "available", Candidate: candidate},
		}
	}
	mutate := func(run func(Mutation) request.Outcome) {
		t.Helper()
		if out := store.WithMutation(ctx, run); !out.OK {
			t.Fatalf("cache mutation failed: %+v", out)
		}
	}

	var firstPath string
	var firstCandidate *release.Candidate
	mutate(func(m Mutation) request.Outcome {
		firstPath, firstCandidate = writePackage(m.Root(), "1.1.0")
		state := stateFor(firstCandidate)
		state.Downloaded = firstPath
		return m.Commit(ctx, state, firstPath)
	})

	reopened, failure := Open(directory, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	store = reopened
	mutate(func(m Mutation) request.Outcome {
		state := stateFor(firstCandidate)
		if failure := m.PreserveDownload(&state); failure != nil || state.Downloaded != firstPath {
			t.Fatalf("same candidate did not retain its verified download: %+v, %+v", state, failure)
		}
		return m.Commit(ctx, state, "")
	})
	if state, exists, failure := store.Read(ctx); failure != nil || !exists || state.Downloaded != firstPath {
		t.Fatalf("reading cache lost its verified download: %+v, %v, %+v", state, exists, failure)
	}

	var nextPath string
	var nextCandidate *release.Candidate
	mutate(func(m Mutation) request.Outcome {
		nextPath, nextCandidate = writePackage(m.Root(), "1.2.0")
		state := stateFor(nextCandidate)
		if failure := m.PreserveDownload(&state); failure != nil || state.Downloaded != "" {
			t.Fatalf("a different candidate inherited the old download: %+v, %+v", state, failure)
		}
		state.Downloaded = nextPath
		return m.Commit(ctx, state, nextPath)
	})
	if _, err := os.Stat(filepath.Dir(firstPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded package directory was retained: %v", err)
	}

	if err := os.WriteFile(nextPath, []byte("repository fixture for 1.2.x"), 0600); err != nil {
		t.Fatal(err)
	}
	out := store.WithMutation(ctx, func(m Mutation) request.Outcome {
		state := stateFor(nextCandidate)
		if failure := m.PreserveDownload(&state); failure != nil {
			return request.Fail(failure.Code, failure.Message, failure.Result)
		}
		return request.Success(state)
	})
	if out.OK || out.Code != "cache_invalid" {
		t.Fatalf("cache accepted a same-size replacement with different bytes: %+v", out)
	}
}
