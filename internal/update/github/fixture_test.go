package github

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
)

func signedReleaseFixture(t *testing.T) (ed25519.PrivateKey, releases.ReleaseManifest, releases.Candidate, []byte) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	id, _ := releases.PublisherKeyID(key.Public().(ed25519.PublicKey))
	files, err := packagefiles.Encode(packagefiles.Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "build", OS: "linux", Arch: "amd64", PayloadKind: "core", Files: []packagefiles.File{{Path: "sidraviactl", Size: 3, SHA256: strings.Repeat("a", 64), Mode: 0755}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(files)
	asset := releases.ReleaseAsset{ManifestAsset: releases.ManifestAsset{OS: "linux", Arch: "amd64", Kind: "core-portable", Name: "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz", Size: 3, SHA256: strings.Repeat("b", 64)}, FilesSHA256: hex.EncodeToString(sum[:]), PayloadKind: "core"}
	m := releases.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: "1.0.0", BuildID: "build", MinimumProtocol: 1, Assets: []releases.ReleaseAsset{asset}}
	c := releases.Candidate{Version: m.Version, Name: asset.Name, Size: asset.Size, SHA256: asset.SHA256}
	return key, m, c, files
}
