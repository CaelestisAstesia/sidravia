package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/release"
)

func fixture(t *testing.T) (string, string, ed25519.PublicKey, []byte, []byte) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	id, _ := release.PublisherKeyID(public)
	m := release.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: "1.0.0", BuildID: "build", MinimumProtocol: 1, Assets: []release.ReleaseAsset{{ManifestAsset: release.ManifestAsset{OS: "linux", Arch: "amd64", Kind: "core-portable", Name: "sidravia-v1.0.0-linux-amd64-core-portable.tar.gz", Size: 3, SHA256: strings.Repeat("a", 64)}, PayloadKind: "core", FilesSHA256: strings.Repeat("b", 64)}}}
	data, _ := json.Marshal(m)
	data = append([]byte(" \n"), data...)
	root := t.TempDir()
	manifest := filepath.Join(root, release.ReleaseManifestName)
	output := filepath.Join(root, release.ReleaseSignatureName)
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	return manifest, output, public, data, []byte(base64.StdEncoding.EncodeToString(seed))
}
func TestOfflineSigningBindsExactBytesAndCannotOverwrite(t *testing.T) {
	manifest, output, public, data, seed := fixture(t)
	var out, stderr bytes.Buffer
	if code := run([]string{"--manifest", manifest, "--output", output}, bytes.NewReader(seed), &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	signature, err := os.ReadFile(output)
	if err != nil || !ed25519.Verify(public, data, signature) {
		t.Fatal("exact input bytes not signed", err)
	}
	original, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("signer rewrote manifest", err)
	}
	if code := run([]string{"--manifest", manifest, "--output", output}, bytes.NewReader(seed), &out, &stderr); code == 0 {
		t.Fatal("existing signature overwritten")
	}
	again, _ := os.ReadFile(output)
	if !bytes.Equal(signature, again) {
		t.Fatal("signature changed on rejected overwrite")
	}
}
func TestSigningRejectsBadSeedOrAmbiguousManifestWithoutOutput(t *testing.T) {
	for _, mode := range []string{"seed", "root", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			manifest, output, _, data, seed := fixture(t)
			if mode == "seed" {
				seed = []byte("not a key")
			}
			if mode == "root" {
				seed = []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
			}
			if mode == "duplicate" {
				data = bytes.Replace(data, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":2,"schemaVersion":2`), 1)
				if err := os.WriteFile(manifest, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, stderr bytes.Buffer
			if code := run([]string{"--manifest", manifest, "--output", output}, bytes.NewReader(seed), &out, &stderr); code == 0 {
				t.Fatal("invalid signing request accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("rejected request left a signature")
			}
			if bytes.Contains(stderr.Bytes(), seed) {
				t.Fatal("signing seed leaked to diagnostic")
			}
		})
	}
}

type forbiddenInput struct{}

func (forbiddenInput) Read([]byte) (int, error) { panic("help read private input") }
func TestHelpNeverReadsPrivateInput(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run([]string{"--help"}, forbiddenInput{}, &out, &stderr); code != 0 || out.Len() == 0 {
		t.Fatal("help failed", code)
	}
}

func TestExtendedSigningRejectsLegacyAndPreservesExtendedBytes(t *testing.T) {
	legacy, legacySignature, public, raw, seed := fixture(t)
	var m release.ReleaseManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	old := m.Assets[0]
	m.Assets[0].Kind, m.Assets[0].PayloadKind, m.Assets[0].Arch = "gui-portable", "gui", "arm64"
	m.Assets[0].Name = "sidravia-v1.0.0-linux-arm64-gui-portable.tar.gz"
	xz := m.Assets[0]
	xz.Name = strings.TrimSuffix(xz.Name, ".gz") + ".xz"
	m.Assets = append(m.Assets, xz)
	extended := filepath.Join(filepath.Dir(legacy), release.ExtendedReleaseManifestName)
	signature := filepath.Join(filepath.Dir(legacy), release.ExtendedReleaseSignatureName)
	for _, tc := range []struct {
		manifest, output string
		assets           []release.ReleaseAsset
		wantSuccess      bool
	}{
		{legacy, legacySignature, m.Assets, false},
		{extended, signature, append([]release.ReleaseAsset{old}, m.Assets...), false},
		{extended, legacySignature, m.Assets, false},
		{extended, signature, m.Assets, true},
	} {
		m.Assets = tc.assets
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tc.manifest, data, 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		code := run([]string{"--manifest", tc.manifest, "--output", tc.output}, bytes.NewReader(seed), &out, &stderr)
		sig, err := os.ReadFile(tc.output)
		if tc.wantSuccess {
			if code != 0 || err != nil || !ed25519.Verify(public, data, sig) {
				t.Fatalf("exact extended bytes not signed: %d %s %v", code, stderr.String(), err)
			}
		} else if code == 0 || !os.IsNotExist(err) {
			t.Fatalf("wrong metadata location was signed: %s", tc.manifest)
		}
	}
}

func TestPublicVerificationOfPublishedMetadata(t *testing.T) {
	for _, tc := range []struct {
		manifest, signature, arch, kind, payload, name string
	}{
		{release.ReleaseManifestName, release.ReleaseSignatureName, "amd64", "core-portable", "core",
			"sidravia-v1.0.0-linux-amd64-core-portable.tar.gz"},
		{release.XZReleaseManifestName, release.XZReleaseSignatureName, "amd64", "core-portable", "core",
			"sidravia-v1.0.0-linux-amd64-core-portable.tar.xz"},
		{release.ExtendedReleaseManifestName, release.ExtendedReleaseSignatureName, "arm64", "gui-portable", "gui",
			"sidravia-v1.0.0-linux-arm64-gui-portable.tar.gz"},
		{"manifest.json", "signature.bin", "arm64", "gui-portable", "gui",
			"sidravia-v1.0.0-linux-arm64-gui-portable.tar.gz"},
	} {
		t.Run(tc.manifest, func(t *testing.T) {
			path, _, public, raw, encodedSeed := fixture(t)
			var m release.ReleaseManifest
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			asset := &m.Assets[0]
			asset.Arch, asset.Kind, asset.PayloadKind, asset.Name = tc.arch, tc.kind, tc.payload, tc.name
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			seed, err := base64.StdEncoding.DecodeString(string(encodedSeed))
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(path)
			manifest := filepath.Join(dir, tc.manifest)
			signature := filepath.Join(dir, tc.signature)
			key := filepath.Join(dir, "public.txt")
			for path, content := range map[string][]byte{
				manifest: raw, signature: ed25519.Sign(ed25519.NewKeyFromSeed(seed), raw),
				key: []byte(base64.StdEncoding.EncodeToString(public)),
			} {
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"verify", "--manifest", manifest, "--signature", signature, "--public-key", key}
			if tc.manifest != "manifest.json" {
				args = append(args, "--published")
			}
			var out, stderr bytes.Buffer
			if code := run(args, forbiddenInput{}, &out, &stderr); code != 0 {
				t.Fatal(code, stderr.String())
			}
		})
	}
}
