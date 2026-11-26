//go:build windows

package portable

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"sidravia/internal/installation/change"
	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
	"sidravia/internal/update/transaction"
)

type winImage struct {
	inventory Inventory
	verified  release.VerifiedRelease
	archive   []byte
	keys      *release.PublisherKeys
	files     map[string][]byte
}

func windowsImage(t *testing.T, version, build string, files map[string][]byte) winImage {
	t.Helper()
	kind := "core"
	if _, ok := files["Sidravia.exe"]; ok {
		kind = "gui"
	}
	m := packagefiles.Manifest{SchemaVersion: 1, Version: version, BuildID: build, OS: "windows", Arch: "amd64", PayloadKind: kind}
	for name, data := range files {
		m.Files = append(m.Files, packagefiles.File{Path: name, Size: int64(len(data)), SHA256: digest(data), Mode: 0644})
	}
	raw, err := packagefiles.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{packagefiles.Name: raw, "sidravia.portable": {}}
	for k, v := range files {
		contents[k] = v
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range contents {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0644)
		f, e := w.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	c := release.Candidate{ID: "2:22", Version: version, Name: "sidravia-v" + version + "-windows-amd64-" + kind + "-portable.zip", Size: int64(buf.Len()), SHA256: digest(buf.Bytes())}
	c.DownloadURL = "https://github.com/CaelestisAstesia/sidravia/releases/download/v" + version + "/" + c.Name
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	pub := key.Public().(ed25519.PublicKey)
	id, _ := release.PublisherKeyID(pub)
	manifest := release.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: version, BuildID: build, MinimumProtocol: 1, Assets: []release.ReleaseAsset{{ManifestAsset: release.ManifestAsset{OS: "windows", Arch: "amd64", Kind: kind + "-portable", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: digest(raw), PayloadKind: kind}}}
	b, _ := json.Marshal(manifest)
	keys, _ := release.NewPublisherKeys([]ed25519.PublicKey{pub})
	v, e := keys.Verify(b, ed25519.Sign(key, b), version)
	if e != nil {
		t.Fatal(e)
	}
	i, e := Authenticate(v, c, raw)
	if e != nil {
		t.Fatal(e)
	}
	return winImage{i, v, append([]byte(nil), buf.Bytes()...), keys, contents}
}
func winFiles(value string) map[string][]byte {
	m := map[string][]byte{}
	for _, name := range []string{"sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe", "sidraviaupdater.exe", "institution-profiles/test.json"} {
		m[name] = []byte(value)
	}
	return m
}
func winInstall(t *testing.T, target string, i winImage) {
	t.Helper()
	for name, data := range i.files {
		path := filepath.Join(target, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, data, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func TestWindowsSharedPublication(t *testing.T) {
	for _, scenario := range []string{"complete", "occupied", "old-modified", "unknown-dll", "hard-link", "index-link", "new-user-collision"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(base, "portable with spaces")
			oldFiles := winFiles("old")
			oldFiles["retired.dll"] = []byte("retired")
			old := windowsImage(t, "1.0.0", "old", oldFiles)
			files := winFiles("new")
			files["new.txt"] = []byte("new")
			next := windowsImage(t, "1.1.0", "new", files)
			winInstall(t, target, old)
			user := filepath.Join(target, "user-notes.txt")
			os.WriteFile(user, []byte("keep"), 0644)
			archive := filepath.Join(base, "next.zip")
			os.WriteFile(archive, next.archive, 0644)
			payload, e := ExtractRelease(ctx, target, archive, next.verified, next.inventory.candidate)
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				if e := payload.Close(); e != nil {
					t.Error(e)
				}
			}()
			store, e := transaction.Open(filepath.Join(base, "cache"), target, next.keys)
			if e != nil {
				t.Fatal(e)
			}
			record, e := transaction.New(strings.Repeat("a", 32), change.Target{ID: target, Layout: "portable", Version: "1.0.0", BuildID: "old"}, next.inventory.candidate, next.verified, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			if _, e = store.Create(ctx, record); e != nil {
				t.Fatal(e)
			}
			for _, step := range [][2]transaction.Phase{{transaction.Created, transaction.Verified}, {transaction.Verified, transaction.Preparing}, {transaction.Preparing, transaction.Quiescent}} {
				if _, _, e = store.Advance(ctx, record.ID, step[0], step[1], "", time.Now()); e != nil {
					t.Fatal(e)
				}
			}
			if e = Preflight(ctx, target, old.inventory, payload); e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "old-modified":
				os.WriteFile(filepath.Join(target, "sidraviad.exe"), []byte("bad"), 0644)
			case "unknown-dll":
				os.WriteFile(filepath.Join(target, "unknown.dll"), []byte("unknown"), 0644)
			case "hard-link", "index-link":
				name := "sidraviad.exe"
				if scenario == "index-link" {
					name = packagefiles.Name
				}
				if e = os.Link(filepath.Join(target, name), filepath.Join(base, "alias")); e != nil {
					t.Fatal(e)
				}
			case "new-user-collision":
				os.WriteFile(filepath.Join(target, "new.txt"), []byte("user"), 0644)
			case "occupied":
				p, _ := windows.UTF16PtrFromString(filepath.Join(target, "sidraviactl.exe"))
				h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
				if e != nil {
					t.Fatal(e)
				}
				defer windows.CloseHandle(h)
			}
			err = Apply(ctx, store, record.ID, target, old.inventory, payload)
			if scenario != "complete" {
				if err == nil {
					t.Fatal("unsafe publication accepted")
				}
				r, _, e := store.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if scenario == "occupied" {
					if r.Phase != transaction.RepairRequired {
						t.Fatalf("occupied phase %s %v", r.Phase, err)
					}
					if _, e = os.Stat(filepath.Join(target, transaction.StartupSeal)); e != nil {
						t.Fatal("missing repair seal", e)
					}
				} else if r.Phase != transaction.Quiescent {
					t.Fatalf("prewrite refusal changed phase: %s", r.Phase)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			root, e := os.OpenRoot(target)
			if e != nil {
				t.Fatal(e)
			}
			e = verifyInventory(ctx, root, next.inventory)
			root.Close()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(filepath.Join(target, "retired.dll")); !os.IsNotExist(e) {
				t.Fatal("retired file retained", e)
			}
			if b, e := os.ReadFile(user); e != nil || string(b) != "keep" {
				t.Fatal("user file changed", e)
			}
			if _, e = os.Stat(filepath.Join(target, transaction.StartupSeal)); e != nil {
				t.Fatal("recovery seal absent", e)
			}
			if _, _, e = store.Advance(ctx, record.ID, transaction.Applied, transaction.Completed, "", time.Now()); e != nil {
				t.Fatal(e)
			}
			if e = finishApplication(target, record.ID); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestWindowsPublicationDoesNotReplaceNewUserFile(t *testing.T) {
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	os.WriteFile(filepath.Join(root.Name(), "stage"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(root.Name(), "user"), []byte("keep"), 0644)
	if e = publish(root, "stage", "user", false); e == nil {
		t.Fatal("no-replace replaced a user file")
	}
	b, e := root.ReadFile("user")
	if e != nil || string(b) != "keep" {
		t.Fatal(e)
	}
	if e = publish(root, "stage", "user", true); e != nil {
		t.Fatal(e)
	}
	b, e = root.ReadFile("user")
	if e != nil || string(b) != "new" {
		t.Fatal(e)
	}
}
