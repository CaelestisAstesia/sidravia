package qtrepository

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/release"
)

func TestStageRejectsChangedArchiveAndMismatchedInventory(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		badMember   bool
		badZIP      bool
		wrongTarget bool
		wrongImage  bool
	}{
		{"exact repository", false, false, false, false},
		{"payload differs from index", true, false, false, false},
		{"archive differs from published bytes", false, true, false, false},
		{"signed index for wrong architecture", false, false, true, false},
		{"signed index for replacement maintenance image", false, false, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			archivePath := filepath.Join(base, "repo.zip")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			public, private, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			exact := []byte("<Updates/>")
			target := "windows-amd64"
			if scenario.wrongTarget {
				target = "windows-arm64"
			}
			runtime := digest([]byte("fixture maintenance image"))
			if scenario.wrongImage {
				runtime = digest([]byte("different maintenance image"))
			}
			index, err := json.Marshal(release.QtRepositoryIndex{SchemaVersion: 1, Product: "Sidravia", Target: target, Profile: "core", Version: "1.0.0", BuildID: "fixture", MaintenanceSHA256: runtime, Files: map[string]string{"Updates.xml": digest(exact)}})
			if err != nil {
				t.Fatal(err)
			}
			member := exact
			if scenario.badMember {
				member = []byte("<Changed/>")
			}
			repo := zip.NewWriter(file)
			for name, data := range map[string][]byte{
				"Updates.xml": member, Inventory: index, Signature: ed25519.Sign(private, index),
			} {
				entry, err := repo.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := errors.Join(repo.Close(), file.Close()); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			candidate := release.Candidate{
				Version: "1.0.0", Name: "sidravia-v1.0.0-windows-amd64-qt-core-repository.zip",
				SHA256: digest(body), Size: int64(len(body)),
			}
			keyID, err := release.PublisherKeyID(public)
			if err != nil {
				t.Fatal(err)
			}
			keys, err := release.NewPublisherKeys([]ed25519.PublicKey{public})
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := json.Marshal(release.ReleaseManifest{
				SchemaVersion: 2, MinimumProtocol: 1, KeyID: keyID, Version: candidate.Version, BuildID: "fixture",
				Assets: []release.ReleaseAsset{{
					ManifestAsset: release.ManifestAsset{
						OS: "windows", Arch: "amd64", Kind: release.QtCoreRepository,
						Name: candidate.Name, Size: candidate.Size, SHA256: candidate.SHA256,
					},
					PayloadKind: "core", FilesSHA256: digest(index),
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			proof, err := keys.Verify(metadata, ed25519.Sign(private, metadata), candidate.Version)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.badZIP {
				body[0] ^= 1
				if err := os.WriteFile(archivePath, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			destination, err := Stage(context.Background(), archivePath, base, digest([]byte("fixture maintenance image")), proof, candidate)
			if scenario.badZIP || scenario.badMember || scenario.wrongTarget || scenario.wrongImage {
				want := release.ErrUntrusted
				if scenario.wrongTarget {
					want = release.ErrMetadata
				}
				if scenario.wrongImage {
					want = ErrMaintenanceImage
				}
				if !errors.Is(err, want) || destination != "" {
					t.Fatalf("expected changed bytes to be rejected, got %v", err)
				}
				if entries, listErr := os.ReadDir(base); listErr != nil || len(entries) != 1 {
					t.Fatalf("failed stage left temporary files: %v, %v", entries, listErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(destination, "Updates.xml"))
			if err != nil || string(got) != string(exact) {
				t.Fatalf("staged bytes differ: %q, %v", got, err)
			}
		})
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
