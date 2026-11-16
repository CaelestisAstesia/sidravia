package operations

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sidravia/internal/update"
	"sidravia/internal/update/releases"
)

func fixture(t *testing.T) (*Store, Record, *releases.PublisherKeys, string, string) {
	t.Helper()
	target := t.TempDir()
	root := t.TempDir()
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := key.Public().(ed25519.PublicKey)
	id, _ := releases.PublisherKeyID(public)
	m := releases.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: "1.0.0", BuildID: "new", MinimumProtocol: 1, Assets: []releases.ReleaseAsset{{ManifestAsset: releases.ManifestAsset{OS: "linux", Arch: "amd64", Kind: "core-portable", Name: "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz", Size: 3, SHA256: strings.Repeat("a", 64)}, FilesSHA256: strings.Repeat("b", 64), PayloadKind: "core"}}}
	data, _ := json.Marshal(m)
	data = append([]byte(" \n"), data...) // signed whitespace must survive persistence
	roots, _ := releases.NewPublisherKeys([]ed25519.PublicKey{public})
	verified, err := roots.Verify(data, ed25519.Sign(key, data), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	c := releases.Candidate{ID: "1:2", Version: m.Version, Name: m.Assets[0].Name, Size: 3, SHA256: m.Assets[0].SHA256}
	record, err := New(strings.Repeat("a", 32), update.Target{ID: target, Version: "0.9.0", BuildID: "old", Layout: "portable"}, c, verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, target, roots)
	if err != nil {
		t.Fatal(err)
	}
	return store, record, roots, root, target
}
func TestPersistedApplyingAndRepairPreventRestartAfterCoordinatorExit(t *testing.T) {
	store, r, roots, root, target := fixture(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, next := range []Phase{Verified, Preparing, Quiescent, Applying} {
		got, changed, err := store.Advance(ctx, r.ID, r.Phase, next, "", time.Now())
		if err != nil || !changed {
			t.Fatal("transition did not persist", err)
		}
		r = got
	}
	reopened, err := Open(root, target, roots)
	if err != nil {
		t.Fatal(err)
	}
	got, exists, err := reopened.Read(ctx)
	if err != nil || !exists || !got.RequiresStartupBlock() || got.Phase != Applying {
		t.Fatalf("crash marker lost %+v %v", got, err)
	}
	if !bytes.HasPrefix(got.Manifest, []byte(" \n")) {
		t.Fatal("signed bytes reformatted")
	}
	got, _, err = reopened.Advance(ctx, r.ID, Applying, RepairRequired, "apply_failed", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequiresStartupBlock() {
		t.Fatal("failed write unblocked startup")
	}
	if _, _, err = reopened.Advance(ctx, r.ID, RepairRequired, Aborted, "cancelled", time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatal("repair marker blindly aborted", err)
	}
	r.ID = strings.Repeat("b", 32)
	r.Phase = Created
	if _, err = reopened.Create(ctx, r); !errors.Is(err, ErrConflict) {
		t.Fatal("unfinished operation overwritten", err)
	}
}
func TestTransitionsAreBoundAndReplayWithoutSideEffects(t *testing.T) {
	store, r, _, _, _ := fixture(t)
	ctx := context.Background()
	created, err := store.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(ctx, r.ID, Created, Applying, "", time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatal("skipped verification and quiescence")
	}
	if _, _, err := store.Advance(ctx, strings.Repeat("b", 32), Created, Verified, "", time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatal("different operation changed stage")
	}
	for _, next := range []Phase{Verified, Preparing, Quiescent, Applying, Applied, Resuming, Completed} {
		before := r.Phase
		r, _, err = store.Advance(ctx, r.ID, before, next, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		replay, changed, err := store.Advance(ctx, r.ID, before, next, "", time.Now())
		if err != nil || changed || !replay.UpdatedAt.Equal(r.UpdatedAt) {
			t.Fatal("stage replay changed state", err)
		}
	}
	if r.RequiresStartupBlock() {
		t.Fatal("completed operation still blocks")
	}
	replay, err := store.Create(ctx, created)
	if err != nil || replay.Phase != Completed {
		t.Fatal("create replay reset operation", err)
	}
}
func TestRecordsRejectChangedProofAndRevokedRoots(t *testing.T) {
	store, r, _, root, target := fixture(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Signature[0] ^= 1
	if _, err := store.Create(ctx, r); err == nil {
		t.Fatal("tampered proof accepted")
	}
	empty, _ := releases.NewPublisherKeys(nil)
	reader, err := Open(root, target, empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.Read(ctx); !errors.Is(err, releases.ErrUntrusted) {
		t.Fatal("cached trust survived root removal", err)
	}
	if err := os.WriteFile(filepath.Join(store.directory, "current.json"), []byte(`{"schemaVersion":1`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read(ctx); err == nil {
		t.Fatal("partial record treated as idle")
	}
}
func TestTargetCacheOutsideProgramAndConcurrentStores(t *testing.T) {
	store, r, roots, root, target := fixture(t)
	if _, err := Open(filepath.Join(target, "updates"), target, roots); err == nil {
		t.Fatal("marker inside replacement target")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err == nil {
		if _, err := Open(filepath.Join(alias, "missing-cache"), target, roots); err == nil {
			t.Fatal("cache alias entered program tree")
		}
	}
	second, err := Open(root, target, roots)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i, s := range []*Store{store, second} {
		workers.Add(1)
		go func(i int, s *Store) {
			defer workers.Done()
			<-start
			copy := r
			copy.ID = strings.Repeat(string(rune('a'+i)), 32)
			_, err := s.Create(context.Background(), copy)
			results <- err
		}(i, s)
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent store owners: %d", successes)
	}
}
func TestCancellationAndIdentityCannotCreateUnsafeRecord(t *testing.T) {
	store, r, roots, _, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Create(ctx, r); err == nil {
		t.Fatal("canceled operation persisted")
	}
	for _, mutate := range []func(*Record){func(r *Record) { r.Target.ID = filepath.Join(r.Source.ID, "other") }, func(r *Record) { r.Target.Version = "0.8.0" }, func(r *Record) { r.Target.BuildID = "wrong" }, func(r *Record) { r.Phase = Phase("unknown") }, func(r *Record) { r.FailureCode = "raw private error" }} {
		copy := r
		mutate(&copy)
		if copy.Validate(roots) == nil {
			t.Fatal("invalid identity/state accepted")
		}
	}
	data, _, err := store.Read(context.Background())
	if err != nil || data.ID != "" {
		t.Fatal("canceled record left state", err)
	}
	// The operation directory name is a target hash, not caller-supplied IDs.
	raw, err := hex.DecodeString(filepath.Base(store.directory))
	if err != nil || len(raw) != 32 {
		t.Fatal("target storage identity is not a bounded hash")
	}
}

func TestFinalOperationDirectoryAliasCannotEnterProgramTree(t *testing.T) {
	opened, record, roots, root, target := fixture(t)
	resolved, err := resolveExistingPrefix(target)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Separator == '\\' {
		resolved = strings.ToLower(resolved)
	}
	sum := sha256.Sum256([]byte(resolved))
	leaf := filepath.Join(root, hex.EncodeToString(sum[:]))
	if err := os.Symlink(target, leaf); err != nil {
		t.Skip("symlink fixture unavailable")
	}
	if _, err := Open(root, target, roots); err == nil {
		t.Fatal("final operation directory alias accepted")
	}
	if _, err := opened.Create(context.Background(), record); err == nil {
		t.Fatal("directory alias introduced after Open accepted")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("program tree was modified", err)
	}
}
func TestOldOperationIDCannotReplayAfterAnotherTerminalRecord(t *testing.T) {
	store, a, _, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(ctx, a.ID, Created, Aborted, "cancelled", time.Now()); err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID = strings.Repeat("b", 32)
	if _, err := store.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Advance(ctx, b.ID, Created, Aborted, "cancelled", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, a); !errors.Is(err, ErrConflict) {
		t.Fatal("old operation ID received a new admission", err)
	}
}
