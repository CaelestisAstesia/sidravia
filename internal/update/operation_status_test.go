package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"sidravia/internal/installation/change"
	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
)

func TestExecutionStatusProjectsVerifiedRecordWithoutProofOrPath(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := key.Public().(ed25519.PublicKey)
	keyID, _ := release.PublisherKeyID(public)
	asset := release.ReleaseAsset{ManifestAsset: release.ManifestAsset{OS: "linux", Arch: "amd64", Kind: "core-portable", Name: "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz", Size: 3, SHA256: strings.Repeat("a", 64)}, FilesSHA256: strings.Repeat("b", 64), PayloadKind: "core"}
	m := release.ReleaseManifest{SchemaVersion: 2, KeyID: keyID, Version: "1.0.0", BuildID: "new", MinimumProtocol: 1, Assets: []release.ReleaseAsset{asset}}
	data, _ := json.Marshal(m)
	keys, _ := release.NewPublisherKeys([]ed25519.PublicKey{public})
	verified, err := keys.Verify(data, ed25519.Sign(key, data), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	store, err := transaction.Open(t.TempDir(), target, keys)
	if err != nil {
		t.Fatal(err)
	}
	if view, err := readExecutionStatus(context.Background(), store); err != nil || view != nil {
		t.Fatal("new store falsely reports an update", err)
	}
	candidate := release.Candidate{ID: "1:2", Version: m.Version, Name: asset.Name, Size: asset.Size, SHA256: asset.SHA256}
	r, err := transaction.New(strings.Repeat("a", 32), change.Target{ID: target, Version: "0.9.0", BuildID: "old", Layout: "portable"}, candidate, verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	view, err := readExecutionStatus(context.Background(), store)
	if err != nil || view.ID != r.ID || view.Phase != transaction.Created {
		t.Fatal("execution status lost its state", err)
	}
	status := composeStatus(cached{}, false, view)
	if status.SchemaVersion != 1 || status.Check.State != "not_checked" {
		t.Fatal("absent check cache was mistaken for completed checking")
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte(target)) || bytes.Contains(raw, []byte("signature")) || bytes.Contains(raw, []byte("manifest")) {
		t.Fatal("execution status exposed private storage/proof data")
	}
}
