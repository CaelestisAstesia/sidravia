//go:build linux

package portable

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"sidravia/internal/update"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
)

type testImage struct {
	inventory Inventory
	verified  releases.VerifiedRelease
	archive   []byte
	keys      *releases.PublisherKeys
	contents  map[string]string
}

func image(t *testing.T, version, build string, contents map[string]string, alter func(*tar.Header)) testImage {
	return imageKind(t, version, build, contents, alter, "core")
}
func imageKind(t *testing.T, version, build string, contents map[string]string, alter func(*tar.Header), kind string) testImage {
	t.Helper()
	m := packagefiles.Manifest{SchemaVersion: 1, Version: version, BuildID: build, OS: "linux", Arch: runtime.GOARCH, PayloadKind: kind}
	for name, data := range contents {
		m.Files = append(m.Files, packagefiles.File{Path: name, Size: int64(len(data)), SHA256: digest([]byte(data)), Mode: func() uint32 {
			if kind == "gui" && (name == "sidravia_gui" || name == "gui-runtime/sidravia_gui") {
				return 0755
			}
			return 0644
		}()})
	}
	data, err := packagefiles.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for k, v := range contents {
		files[k] = v
	}
	files[packagefiles.Name] = string(data)
	files["sidravia.portable"] = ""
	names := []string{}
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		h := &tar.Header{Name: "sidravia-v" + version + "-linux-" + runtime.GOARCH + "/" + name, Size: int64(len(files[name])), Mode: 0644, Typeflag: tar.TypeReg}
		if kind == "gui" && (name == "sidravia_gui" || name == "gui-runtime/sidravia_gui") {
			h.Mode = 0755
		}
		if alter != nil {
			alter(h)
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(files[name])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return signImage(t, m, data, archive.Bytes(), contents)
}
func signImage(t *testing.T, m packagefiles.Manifest, data, archive []byte, contents map[string]string) testImage {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	pub := key.Public().(ed25519.PublicKey)
	id, _ := releases.PublisherKeyID(pub)
	name := "sidravia-v" + m.Version + "-" + m.OS + "-" + m.Arch + "-" + m.PayloadKind + "-portable.tar.gz"
	if m.OS == "windows" {
		name = strings.TrimSuffix(name, ".tar.gz") + ".zip"
	}
	c := releases.Candidate{ID: "1:2", Version: m.Version, Name: name, Size: int64(len(archive)), SHA256: digest(archive)}
	release := releases.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: m.Version, BuildID: m.BuildID, MinimumProtocol: 1, Assets: []releases.ReleaseAsset{{ManifestAsset: releases.ManifestAsset{OS: m.OS, Arch: m.Arch, Kind: m.PayloadKind + "-portable", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: digest(data), PayloadKind: m.PayloadKind}}}
	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := releases.NewPublisherKeys([]ed25519.PublicKey{pub})
	v, err := keys.Verify(raw, ed25519.Sign(key, raw), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Authenticate(v, c, data)
	if err != nil {
		t.Fatal(err)
	}
	return testImage{i, v, append([]byte(nil), archive...), keys, contents}
}
func install(t *testing.T, target string, i testImage) {
	t.Helper()
	for name, data := range i.contents {
		path := filepath.Join(target, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), func() os.FileMode {
			for _, f := range i.inventory.manifest.Files {
				if f.Path == name {
					return os.FileMode(f.Mode)
				}
			}
			return 0644
		}()); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, packagefiles.Name), i.inventory.data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sidravia.portable"), nil, 0644); err != nil {
		t.Fatal(err)
	}
}
func extract(t *testing.T, target string, i testImage) *Payload {
	t.Helper()
	archive := filepath.Join(t.TempDir(), i.inventory.candidate.Name)
	if err := os.WriteFile(archive, i.archive, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Extract(context.Background(), target, archive, i.inventory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}
func quiescent(t *testing.T, target string, old, next testImage) (*operations.Store, string) {
	t.Helper()
	s, err := operations.Open(t.TempDir(), target, next.keys)
	if err != nil {
		t.Fatal(err)
	}
	r, err := operations.New(strings.Repeat("a", 32), update.Target{ID: target, Version: old.inventory.manifest.Version, BuildID: old.inventory.manifest.BuildID, Layout: "portable"}, next.inventory.candidate, next.verified, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []operations.Phase{operations.Verified, operations.Preparing, operations.Quiescent} {
		n, _, err := s.Advance(context.Background(), r.ID, r.Phase, phase, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		r = n
	}
	return s, r.ID
}
func TestApplyPreservesDataAndUnknownFiles(t *testing.T) {
	target := t.TempDir()
	old := image(t, "1.0.0", "old", map[string]string{"program": "old", "obsolete": "stale", "data/shared": "same"}, nil)
	next := image(t, "1.1.0", "new", map[string]string{"program": "new", "new-dir/new-file": "added", "data/shared": "same"}, nil)
	install(t, target, old)
	protected := map[string]string{"config/credentials.json": "private", "logs/run.log": "log", "runtime/state": "runtime", "namespaces/test/file": "ns", "unknown": "keep", "data/user": "keep shared"}
	for name, data := range protected {
		path := filepath.Join(target, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := extract(t, target, next)
	s, id := quiescent(t, target, old, next)
	if err := Apply(context.Background(), s, id, target, old.inventory, p); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := verifyInventory(context.Background(), root, next.inventory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "obsolete")); !os.IsNotExist(err) {
		t.Fatal("stale owned file not removed", err)
	}
	for name, want := range protected {
		got, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(got) != want {
			t.Fatalf("data changed %s: %v", name, err)
		}
	}
	r, _, err := s.Read(context.Background())
	if err != nil || r.Phase != operations.Applied {
		t.Fatal(r.Phase, err)
	}
	if _, err := s.AcquireStart(context.Background()); err == nil {
		t.Fatal("restart before lifecycle completion")
	}
	if err := Apply(context.Background(), s, id, target, old.inventory, p); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("replay admitted", err)
	}
	if _, _, err := s.Advance(context.Background(), id, operations.Applied, operations.Completed, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	start, err := s.AcquireStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start()
}
func TestPreflightRefusesChangedFilesAndCollisionsWithoutWriting(t *testing.T) {
	for _, kind := range []string{"modified", "hardlink", "symlink", "parent-link", "unknown", "case-alias", "cancelled", "active-process"} {
		t.Run(kind, func(t *testing.T) {
			target := t.TempDir()
			old := image(t, "1.0.0", "old", map[string]string{"program": "old", "nested/owned": "old nested"}, nil)
			next := image(t, "1.1.0", "new", map[string]string{"program": "new", "nested/owned": "new nested", "added": "new"}, nil)
			install(t, target, old)
			switch kind {
			case "modified":
				os.WriteFile(filepath.Join(target, "program"), []byte("user modified"), 0644)
			case "hardlink":
				if err := os.Link(filepath.Join(target, "program"), filepath.Join(target, "unknown")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				os.Remove(filepath.Join(target, "program"))
				os.Symlink("nested/owned", filepath.Join(target, "program"))
			case "parent-link":
				os.Rename(filepath.Join(target, "nested"), filepath.Join(target, "other"))
				os.Symlink("other", filepath.Join(target, "nested"))
			case "unknown":
				os.WriteFile(filepath.Join(target, "added"), []byte("user"), 0644)
			case "case-alias":
				os.WriteFile(filepath.Join(target, "ADDED"), []byte("user"), 0644)
			case "active-process":
				executable := filepath.Join(target, "unowned-live-process")
				src, err := os.Open("/usr/bin/sleep")
				if err != nil {
					t.Fatal(err)
				}
				dest, err := os.OpenFile(executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(dest, src)
				src.Close()
				dest.Close()
				if err != nil {
					t.Fatal(err)
				}
				proc := exec.Command(executable, "30")
				if err := proc.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { proc.Process.Kill(); proc.Wait() }()
			}
			original, err := os.ReadFile(filepath.Join(target, packagefiles.Name))
			if err != nil {
				t.Fatal(err)
			}
			p := extract(t, target, next)
			s, id := quiescent(t, target, old, next)
			ctx := context.Background()
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := Apply(ctx, s, id, target, old.inventory, p); err == nil {
				t.Fatal("unsafe preflight accepted")
			}
			got, _ := os.ReadFile(filepath.Join(target, packagefiles.Name))
			if !bytes.Equal(got, original) {
				t.Fatal("preflight changed baseline")
			}
			r, _, err := s.Read(context.Background())
			if err != nil || r.Phase != operations.Quiescent {
				t.Fatal("preflight changed phase", r.Phase, err)
			}
			if data, _ := os.ReadFile(filepath.Join(target, "nested/owned")); string(data) != "old nested" {
				t.Fatal("preflight wrote files")
			}
		})
	}
}
func TestExtractionRejectsSignedButUnsafeArchives(t *testing.T) {
	cases := map[string]func(*tar.Header){
		"traversal": func(h *tar.Header) {
			if strings.HasSuffix(h.Name, "/program") {
				h.Name = "../../outside"
			}
		},
		"symlink": func(h *tar.Header) {
			if strings.HasSuffix(h.Name, "/program") {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/tmp/outside"
				h.Size = 0
			}
		},
		"hardlink": func(h *tar.Header) {
			if strings.HasSuffix(h.Name, "/program") {
				h.Typeflag = tar.TypeLink
				h.Linkname = "outside"
				h.Size = 0
			}
		},
		"mode": func(h *tar.Header) { h.Mode = 04755 },
		"duplicate": func(h *tar.Header) {
			if strings.HasSuffix(h.Name, "/sidravia.portable") {
				h.Name = strings.TrimSuffix(h.Name, "sidravia.portable") + "program"
			}
		},
		"unknown": func(h *tar.Header) {
			if strings.HasSuffix(h.Name, "/program") {
				h.Name = strings.TrimSuffix(h.Name, "program") + "unknown"
			}
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			target := t.TempDir()
			i := image(t, "1.1.0", "new", map[string]string{"program": "safe"}, alter)
			archive := filepath.Join(t.TempDir(), i.inventory.candidate.Name)
			os.WriteFile(archive, i.archive, 0600)
			if p, err := Extract(context.Background(), target, archive, i.inventory); err == nil {
				p.Close()
				t.Fatal("unsafe archive accepted")
			}
			entries, _ := os.ReadDir(filepath.Dir(target))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".sidravia-stage-") {
					t.Fatal("failed extraction leaked staging")
				}
			}
		})
	}
}
func TestZIPUsesSameAuthenticatedExtraction(t *testing.T) {
	m := packagefiles.Manifest{SchemaVersion: 1, Version: "1.1.0", BuildID: "new", OS: "windows", Arch: "amd64", PayloadKind: "core", Files: []packagefiles.File{{Path: "program.exe", Size: 4, SHA256: digest([]byte("safe")), Mode: 0644}}}
	data, err := packagefiles.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, contents := range map[string]string{"program.exe": "safe", packagefiles.Name: string(data), "sidravia.portable": ""} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0644)
		if name == "program.exe" {
			h.SetMode(0755)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	i := signImage(t, m, data, archive.Bytes(), map[string]string{"program.exe": "safe"})
	target := t.TempDir()
	p := extract(t, target, i)
	if err := applicationPlatform(p.inventory); !errors.Is(err, ErrUnsupported) {
		t.Fatal("foreign application admitted", err)
	}
}

func TestWriteFailureKeepsDurableRepairState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary-account permission enforcement")
	}
	target := t.TempDir()
	old := image(t, "1.0.0", "old", map[string]string{"a": "old a", "z/b": "old b"}, nil)
	next := image(t, "1.1.0", "new", map[string]string{"a": "new a", "z/b": "new b"}, nil)
	install(t, target, old)
	p := extract(t, target, next)
	s, id := quiescent(t, target, old, next)
	// A real source-directory rename failure after the first product publication.
	// No fake filesystem or widened production test hook is involved.
	if err := p.root.Chmod("z", 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if p.root != nil {
			p.root.Chmod("z", 0700)
		} else {
			os.Chmod(filepath.Join(p.directory, "z"), 0700)
		}
	})
	if err := Apply(context.Background(), s, id, target, old.inventory, p); err == nil {
		t.Fatal("write failure ignored")
	}
	a, _ := os.ReadFile(filepath.Join(target, "a"))
	b, _ := os.ReadFile(filepath.Join(target, "z/b"))
	if string(a) != "new a" || string(b) != "old b" {
		t.Fatalf("fault did not cross publication boundary: %q %q", a, b)
	}
	r, exists, err := s.Read(context.Background())
	if err != nil || !exists || r.Phase != operations.RepairRequired {
		t.Fatal("missing durable repair", r.Phase, err)
	}
	if _, err := os.Stat(filepath.Join(target, operations.StartupSeal)); err != nil {
		t.Fatal("partial write lost relocation seal", err)
	}
	if _, err := s.AcquireStart(context.Background()); err == nil {
		t.Fatal("mixed program admitted")
	}
	raw, _ := os.ReadFile(filepath.Join(target, packagefiles.Name))
	if !bytes.Equal(raw, old.inventory.data) {
		t.Fatal("partial update published new baseline")
	}
}
func TestCancelledAndCorruptArchiveCannotPublishPayload(t *testing.T) {
	target := t.TempDir()
	i := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
	path := filepath.Join(t.TempDir(), i.inventory.candidate.Name)
	if err := os.WriteFile(path, i.archive, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, err := Extract(ctx, target, path, i.inventory); !errors.Is(err, context.Canceled) {
		if p != nil {
			p.Close()
		}
		t.Fatal(err)
	}
	i.archive[len(i.archive)/2] ^= 1
	if err := os.WriteFile(path, i.archive, 0600); err != nil {
		t.Fatal(err)
	}
	if p, err := Extract(context.Background(), target, path, i.inventory); err == nil {
		p.Close()
		t.Fatal("changed signed archive accepted")
	}
}

func TestPublicationDoesNotOverwriteNewUnknownFile(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile("staged", []byte("product"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("destination", []byte("user data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publish(root, "staged", "destination", false); err == nil {
		t.Fatal("no-replace overwrote user data")
	}
	data, err := root.ReadFile("destination")
	if err != nil || string(data) != "user data" {
		t.Fatal("unknown bytes changed", err)
	}
}
func TestCleanupPreservesSubstitutedRootAndUnknownContents(t *testing.T) {
	for _, mode := range []string{"root", "unknown", "leaf"} {
		t.Run(mode, func(t *testing.T) {
			i := image(t, "1.1.0", "new", map[string]string{"program": "safe"}, nil)
			p := extract(t, t.TempDir(), i)
			switch mode {
			case "root":
				moved := p.directory + "-moved"
				if err := os.Rename(p.directory, moved); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(moved) })
				if err := os.Mkdir(p.directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.directory, "unknown"), []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := p.root.WriteFile("unknown", []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			case "leaf":
				if err := p.root.Rename("program", "original"); err != nil {
					t.Fatal(err)
				}
				if err := p.root.WriteFile("program", []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.Close(); err == nil {
				t.Fatal("unknown stage changes silently removed")
			}
			name := "unknown"
			if mode == "leaf" {
				name = "program"
			}
			data, err := os.ReadFile(filepath.Join(p.directory, name))
			if err != nil || string(data) != "user" {
				t.Fatal("unknown staging data deleted", err)
			}
		})
	}
}

func TestDurableSealIsCancellationCommitPoint(t *testing.T) {
	target := t.TempDir()
	old := image(t, "1.0.0", "old", map[string]string{"program": "old"}, nil)
	next := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
	install(t, target, old)
	s, id := quiescent(t, target, old, next)
	root, err := os.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	work, _, err := sealApplication(ctx, root, id)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // exactly after the durable seal and before the Applying transition
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("fixture did not cancel")
	}
	if _, changed, err := s.Advance(work, id, operations.Quiescent, operations.Applying, "", time.Now()); err != nil || !changed {
		t.Fatal("cancellation stranded the sealed pre-write phase", err)
	}
}
