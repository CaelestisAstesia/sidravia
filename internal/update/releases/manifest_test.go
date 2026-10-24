package releases

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestManifestSignatureAndCandidateBinding(t *testing.T) {
	// Synthetic deterministic key is confined to this test, not a publisher key.
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public := key.Public().(ed25519.PublicKey)
	asset := ManifestAsset{OS: "windows", Arch: "amd64", Kind: "setup", Name: "sidravia-v1.0.0-windows-amd64-setup.exe", Size: 3, SHA256: strings.Repeat("a", 64)}
	data, _ := json.Marshal(Manifest{SchemaVersion: 1, Version: "1.0.0", MinimumProtocol: 1, Assets: []ManifestAsset{asset}})
	signature := ed25519.Sign(key, data)
	manifest, err := VerifyManifest(data, signature, public, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{Version: "1.0.0", Name: asset.Name, Size: 3, SHA256: asset.SHA256}
	if !manifest.Authenticates(candidate) {
		t.Fatal("signed candidate rejected")
	}
	candidate.SHA256 = strings.Repeat("b", 64)
	if manifest.Authenticates(candidate) {
		t.Fatal("replaced package accepted")
	}
	for _, bad := range [][]byte{append([]byte(" "), data...), append(data, 0)} {
		if _, err := VerifyManifest(bad, signature, public, "1.0.0"); !errors.Is(err, ErrUntrusted) {
			t.Fatal(err)
		}
	}
	if _, err := VerifyManifest(data, signature, nil, "1.0.0"); !errors.Is(err, ErrUntrusted) {
		t.Fatal(err)
	}
	if _, err := VerifyManifest(data, signature, public, "2.0.0"); !errors.Is(err, ErrMetadata) {
		t.Fatal(err)
	}
}
