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
