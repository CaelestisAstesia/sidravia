package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/release"
	"sidravia/internal/release/qtrepository"
)

func TestQtRepositorySignerPreservesExactInventoryAndRejectsChangedFiles(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := base64.StdEncoding.EncodeToString(private.Seed())
	exact := []byte("<Updates/>")
	hash := sha256.Sum256(exact)
	index, err := json.Marshal(release.QtRepositoryIndex{
		SchemaVersion: 1, Product: "Sidravia", Target: "windows-amd64", Profile: "core", Version: "1.0.0", BuildID: "fixture",
		MaintenanceSHA256: hex.EncodeToString(hash[:]), Files: map[string]string{"Updates.xml": hex.EncodeToString(hash[:])},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, withChanges := range []bool{false, true} {
		dir := t.TempDir()
		in, out := filepath.Join(dir, "unsigned.zip"), filepath.Join(dir, "signed.zip")
		source, err := os.Create(in)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(source)
		updates := exact
		if withChanges {
			updates = []byte("<Altered/>")
		}
		for name, data := range map[string][]byte{"Updates.xml": updates, qtrepository.Inventory: index} {
			entry, err := archive.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := source.Close(); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		result := run([]string{"qt-repository", "--input", in, "--output", out}, bytes.NewBufferString(seed), io.Discard, &stderr)
		if withChanges {
			if _, statErr := os.Stat(out); result == 0 || !os.IsNotExist(statErr) {
				t.Fatalf("signer produced output for altered payload: %v, %v", result, statErr)
			}
			continue
		}
		if result != 0 {
			t.Fatalf("signing failed: %s", &stderr)
		}
		signed, err := zip.OpenReader(out)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := signed.Open(qtrepository.Signature)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := io.ReadAll(raw)
		raw.Close()
		signed.Close()
		if err != nil || !ed25519.Verify(public, index, signature) {
			t.Fatalf("unable to verify exact signed inventory: %v", err)
		}
	}
}
