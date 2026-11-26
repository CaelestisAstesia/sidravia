package candidatecache

import (
	"context"
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
