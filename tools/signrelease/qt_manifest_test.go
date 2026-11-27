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

func TestQtManifestRejectsMixedBuildsAndMaintenanceImages(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "publisher")
	if err := os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(pub)), 0600); err != nil {
		t.Fatal(err)
	}
	digest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	for _, tt := range []struct {
		name, guiBuild, guiImage string
		accepted                 bool
	}{
		{"single build and image", "fixture", "maint", true},
		{"mixed builds", "other-fixture", "maint", false},
		{"mixed maintenance images", "fixture", "other-maint", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			artifacts := t.TempDir()
			args := []string{"-version", "1.2.3", "-build-id", "fixture", "-public-key", key}
			for _, profile := range []string{"core", "gui"} {
				index := release.QtRepositoryIndex{
					SchemaVersion: 1, Product: "Sidravia", Target: "windows-amd64", Profile: profile,
					Version: "1.2.3", BuildID: "fixture", MaintenanceSHA256: digest("maint"),
					Files: map[string]string{"Updates.xml": digest("<Updates/>")},
				}
				if profile == "gui" {
					index.BuildID, index.MaintenanceSHA256 = tt.guiBuild, digest(tt.guiImage)
				}
				body, err := json.Marshal(index)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(artifacts, "sidravia-v1.2.3-windows-amd64-qt-"+profile+"-repository.zip")
				out, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				archive := zip.NewWriter(out)
				for name, data := range map[string][]byte{
					qtrepository.Inventory: body, qtrepository.Signature: ed25519.Sign(priv, body),
					"Updates.xml": []byte("<Updates/>"),
				} {
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
				if err := out.Close(); err != nil {
					t.Fatal(err)
				}
				args = append(args, path)
			}
			var manifest bytes.Buffer
			if code := prepareQtManifest(args, &manifest, io.Discard); (code == 0) != tt.accepted || (manifest.Len() != 0) != tt.accepted {
				t.Fatalf("status %d, %d output bytes, accepted = %t", code, manifest.Len(), tt.accepted)
			}
			if !tt.accepted {
				return
			}
			manifestPath := filepath.Join(artifacts, release.QtReleaseManifestName)
			signaturePath := filepath.Join(artifacts, release.QtReleaseSignatureName)
			if err := os.WriteFile(manifestPath, manifest.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(signaturePath, ed25519.Sign(priv, manifest.Bytes()), 0600); err != nil {
				t.Fatal(err)
			}
			verify := []string{"verify", "-manifest", manifestPath, "-signature", signaturePath,
				"-public-key", key, "-published", "-qt-artifacts", artifacts, "-json"}
			var output, stderr bytes.Buffer
			if run(verify, forbiddenInput{}, &output, &stderr) != 0 || !bytes.Contains(output.Bytes(), []byte(`"artifactBytes":"passed"`)) {
				t.Fatalf("published Qt verification failed: %s %s", &output, &stderr)
			}
			archive := args[len(args)-1]
			original, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			// A trailing byte leaves the ZIP inventory and its inner signature intact.
			if err := os.WriteFile(archive, append(original, 0), 0600); err != nil {
				t.Fatal(err)
			}
			if run(verify, forbiddenInput{}, io.Discard, io.Discard) == 0 {
				t.Fatal("verification accepted different repository bytes")
			}
		})
	}
}
