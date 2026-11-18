package releases

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sidravia/internal/update/packagefiles"
)

func TestPublisherTrustPinsReviewedPublicKey(t *testing.T) {
	const expectedID = "c68f12717c77cdf1fc732185e2a5977340970e69da491da1847b5faf19ddeb9a"
	public, err := base64.StdEncoding.DecodeString("gZswhkys+jYbMtgpUxgKT7iDyhV6E2XH1zbdZHcPWP4=")
	if err != nil {
		t.Fatal(err)
	}
	id, err := PublisherKeyID(public)
	if err != nil || id != expectedID {
		t.Fatal("reviewed publisher identity changed", err)
	}
	roots := PublisherTrust()
	if !roots.Configured() || len(roots.keys) != 1 || string(roots.keys[id]) != string(public) {
		t.Fatal("production must trust only the reviewed publisher")
	}
	roots.keys[id][0] ^= 1
	if string(PublisherTrust().keys[id]) != string(public) {
		t.Fatal("caller changed the build-pinned root")
	}
}

func signedReleaseFixture(t *testing.T) (ed25519.PrivateKey, ReleaseManifest, Candidate, []byte) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	id, _ := PublisherKeyID(key.Public().(ed25519.PublicKey))
	files, err := packagefiles.Encode(packagefiles.Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "build", OS: "linux", Arch: "amd64", PayloadKind: "core", Files: []packagefiles.File{{Path: "sidraviactl", Size: 3, SHA256: strings.Repeat("a", 64), Mode: 0755}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(files)
	asset := ReleaseAsset{ManifestAsset: ManifestAsset{OS: "linux", Arch: "amd64", Kind: "core-portable", Name: "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz", Size: 3, SHA256: strings.Repeat("b", 64)}, FilesSHA256: hex.EncodeToString(sum[:]), PayloadKind: "core"}
	m := ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: "1.0.0", BuildID: "build", MinimumProtocol: 1, Assets: []ReleaseAsset{asset}}
	c := Candidate{Version: m.Version, Name: asset.Name, Size: asset.Size, SHA256: asset.SHA256}
	return key, m, c, files
}
func TestPublisherTrustBindsCandidateAndOwnership(t *testing.T) {
	key, m, c, files := signedReleaseFixture(t)
	public := key.Public().(ed25519.PublicKey)
	roots, err := NewPublisherKeys([]ed25519.PublicKey{public})
	if err != nil {
		t.Fatal(err)
	}
	public[0] ^= 1 // The caller no longer owns the pinned copy.
	data, _ := json.Marshal(m)
	verified, err := roots.Verify(data, ed25519.Sign(key, data), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if verified.BuildID() != "build" {
		t.Fatal("build identity lost")
	}
	got, err := verified.VerifyFiles(c, files)
	if err != nil || got.PayloadKind != "core" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := verified.VerifyFiles(c, append(files, ' ')); !errors.Is(err, ErrUntrusted) {
		t.Fatal("ownership bytes were not bound", err)
	}
	c.SHA256 = strings.Repeat("c", 64)
	if _, err := verified.Asset(c); !errors.Is(err, ErrUntrusted) {
		t.Fatal("package bytes were not bound", err)
	}
	if _, err := PublisherTrust().Verify(data, ed25519.Sign(key, data), m.Version); !errors.Is(err, ErrUntrusted) {
		t.Fatal("synthetic key trusted by production", err)
	}
}
func TestPublisherTrustRejectsUnknownRootsAndMalformedSignedMetadata(t *testing.T) {
	key, m, _, _ := signedReleaseFixture(t)
	roots, _ := NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	data, _ := json.Marshal(m)
	if _, err := roots.Verify(append(data, ' '), ed25519.Sign(key, data), m.Version); !errors.Is(err, ErrUntrusted) {
		t.Fatal("modified signed bytes accepted")
	}
	for _, mutate := range []func(*ReleaseManifest){
		func(m *ReleaseManifest) { m.KeyID = strings.Repeat("a", 64) },
		func(m *ReleaseManifest) { m.SchemaVersion = 1 },
		func(m *ReleaseManifest) { m.MinimumProtocol = 2 },
		func(m *ReleaseManifest) { m.BuildID = "../other" },
		func(m *ReleaseManifest) { m.Assets[0].FilesSHA256 = "" },
		func(m *ReleaseManifest) { m.Assets[0].Name = "sidravia-v1.0.0-linux-amd64-gui-portable.tar.gz" },
		func(m *ReleaseManifest) { m.Assets = append(m.Assets, m.Assets[0]) },
	} {
		_, copy, _, _ := signedReleaseFixture(t)
		mutate(&copy)
		raw, _ := json.Marshal(copy)
		if _, err := roots.Verify(raw, ed25519.Sign(key, raw), m.Version); err == nil {
			t.Fatal("malformed signed metadata accepted")
		}
	}
	for _, bad := range []string{strings.Replace(string(data), `"buildId":"build"`, `"buildId":"build","BuildID":"other"`, 1), strings.Replace(string(data), `"size":3`, `"size":null`, 1), strings.Replace(string(data), `"schemaVersion":2`, `"schemaVersion":2,"schemaVersion":2`, 1), string(data) + `{}`} {
		raw := []byte(bad)
		if _, err := roots.Verify(raw, ed25519.Sign(key, raw), m.Version); err == nil {
			t.Fatal("ambiguous signed JSON accepted")
		}
	}
	if _, err := NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey), key.Public().(ed25519.PublicKey)}); err == nil {
		t.Fatal("duplicate root accepted")
	}
}
func TestVerifiedOwnershipCannotChangeTargetOrBuild(t *testing.T) {
	key, m, c, files := signedReleaseFixture(t)
	var payload packagefiles.Manifest
	if err := json.Unmarshal(files, &payload); err != nil {
		t.Fatal(err)
	}
	payload.BuildID = "other"
	raw, _ := packagefiles.Encode(payload)
	sum := sha256.Sum256(raw)
	m.Assets[0].FilesSHA256 = hex.EncodeToString(sum[:])
	data, _ := json.Marshal(m)
	roots, _ := NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	verified, err := roots.Verify(data, ed25519.Sign(key, data), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verified.VerifyFiles(c, raw); !errors.Is(err, ErrMetadata) {
		t.Fatal("ownership build mismatch accepted", err)
	}
}

func TestSetupAssetBindsItsCoreOrGUIPayloadExplicitly(t *testing.T) {
	for _, kind := range []string{"core", "gui"} {
		t.Run(kind, func(t *testing.T) {
			key, m, c, files := signedReleaseFixture(t)
			var payload packagefiles.Manifest
			if err := json.Unmarshal(files, &payload); err != nil {
				t.Fatal(err)
			}
			payload.OS = "windows"
			payload.PayloadKind = kind
			files, err := packagefiles.Encode(payload)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(files)
			a := &m.Assets[0]
			a.OS = "windows"
			a.Kind = "setup"
			a.Name = "sidravia-v1.0.0-windows-amd64-setup.exe"
			a.PayloadKind = kind
			a.FilesSHA256 = hex.EncodeToString(sum[:])
			c.Name = a.Name
			data, _ := json.Marshal(m)
			roots, _ := NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
			verified, err := roots.Verify(data, ed25519.Sign(key, data), m.Version)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verified.VerifyFiles(c, files); err != nil {
				t.Fatal("setup payload was not consumable", err)
			}
			m.Assets[0].Kind = "core-portable"
			m.Assets[0].Name = "sidravia-v1.0.0-windows-amd64-core-portable.zip"
			if kind == "gui" {
				raw, _ := json.Marshal(m)
				if _, err := roots.Verify(raw, ed25519.Sign(key, raw), m.Version); err == nil {
					t.Fatal("core archive claimed GUI payload")
				}
			}
		})
	}
}
