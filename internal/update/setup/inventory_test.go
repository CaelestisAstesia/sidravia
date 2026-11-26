package setup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
)

func signedInventory(t *testing.T) (release.VerifiedRelease, release.Candidate, []byte, map[string][]byte) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	id, _ := release.PublisherKeyID(key.Public().(ed25519.PublicKey))
	files := map[string][]byte{"sidraviactl.exe": []byte("ctl"), "sidraviad.exe": []byte("daemon"), "sidraviaupdater.exe": []byte("updater"), "sidraviamgr.exe": []byte("manager"), "Sidravia.exe": []byte("gui"), "flutter_windows.dll": []byte("flutter")}
	m := packagefiles.Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "test", OS: "windows", Arch: "amd64", PayloadKind: "gui"}
	for path, data := range files {
		m.Files = append(m.Files, packagefiles.File{Path: path, Size: int64(len(data)), SHA256: sum(data), Mode: 0644})
	}
	index, e := packagefiles.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	c := release.Candidate{ID: "1:2", Version: m.Version, Name: "sidravia-v1.0.0-windows-amd64-setup.exe", Size: 1, SHA256: sum([]byte("x"))}
	manifest, _ := json.Marshal(release.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: m.Version, BuildID: m.BuildID, MinimumProtocol: 1, Assets: []release.ReleaseAsset{{ManifestAsset: release.ManifestAsset{OS: "windows", Arch: "amd64", Kind: "setup", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: sum(index), PayloadKind: "gui"}}})
	roots, _ := release.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	proof, e := roots.Verify(manifest, ed25519.Sign(key, manifest), m.Version)
	if e != nil {
		t.Fatal(e)
	}
	return proof, c, index, files
}
func TestSelectedComponentInventoryAndUnknownExecutableBoundary(t *testing.T) {
	for _, scenario := range []string{"gui", "core", "missing", "modified", "unknown-dll", "gui-remnant", "hard-link", "index-hard-link", "index-changed", "uninstaller-hard-link"} {
		t.Run(scenario, func(t *testing.T) {
			v, c, index, files := signedInventory(t)
			target := t.TempDir()
			hasGUI := scenario != "core" && scenario != "gui-remnant"
			for path, data := range files {
				if !hasGUI && !CoreFile(path) {
					continue
				}
				if e := os.WriteFile(filepath.Join(target, path), data, 0644); e != nil {
					t.Fatal(e)
				}
			}
			if e := os.WriteFile(filepath.Join(target, packagefiles.Name), index, 0644); e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "missing":
				os.Remove(filepath.Join(target, "sidraviad.exe"))
			case "modified":
				os.WriteFile(filepath.Join(target, "sidraviactl.exe"), []byte("bad"), 0644)
			case "unknown-dll", "gui-remnant":
				os.WriteFile(filepath.Join(target, "unowned.dll"), []byte("dll"), 0644)
			case "index-changed":
				os.WriteFile(filepath.Join(target, packagefiles.Name), bytes.Repeat([]byte{'x'}, len(index)), 0644)
			case "index-hard-link", "uninstaller-hard-link":
				external := filepath.Join(t.TempDir(), "external-index")
				if e := os.WriteFile(external, index, 0644); e != nil {
					t.Fatal(e)
				}
				leaf := packagefiles.Name
				if scenario == "uninstaller-hard-link" {
					leaf = "unins000.dat"
				} else {
					os.Remove(filepath.Join(target, leaf))
				}
				if e := os.Link(external, filepath.Join(target, leaf)); e != nil {
					t.Fatal(e)
				}
			case "hard-link":
				external := filepath.Join(t.TempDir(), "other")
				os.WriteFile(external, files["sidraviactl.exe"], 0644)
				os.Remove(filepath.Join(target, "sidraviactl.exe"))
				if e := os.Link(external, filepath.Join(target, "sidraviactl.exe")); e != nil {
					t.Fatal(e)
				}
			}
			_, e := VerifyInstalled(context.Background(), target, v, c, index, hasGUI)
			if scenario == "gui" || scenario == "core" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("unverified installation accepted")
			}
		})
	}
}
