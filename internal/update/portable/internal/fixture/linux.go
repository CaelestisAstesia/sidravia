package fixture

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
	"sort"
	"strings"
	"testing"
)

type Linux struct {
	Manifest  packagefiles.Manifest
	Candidate release.Candidate
	Index     []byte
	Verified  release.VerifiedRelease
	Archive   []byte
	Keys      *release.PublisherKeys
	Contents  map[string]string
}

func LinuxImage(t *testing.T, version, build string, contents map[string]string, alter func(*tar.Header), kind string) Linux {
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
	return SignLinuxImage(t, m, data, archive.Bytes(), contents)
}
func SignLinuxImage(t *testing.T, m packagefiles.Manifest, data, archive []byte, contents map[string]string) Linux {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	pub := key.Public().(ed25519.PublicKey)
	id, _ := release.PublisherKeyID(pub)
	name := "sidravia-v" + m.Version + "-" + m.OS + "-" + m.Arch + "-" + m.PayloadKind + "-portable.tar.gz"
	if m.OS == "windows" {
		name = strings.TrimSuffix(name, ".tar.gz") + ".zip"
	}
	c := release.Candidate{ID: "1:2", Version: m.Version, Name: name, Size: int64(len(archive)), SHA256: digest(archive)}
	manifest := release.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: m.Version, BuildID: m.BuildID, MinimumProtocol: 1, Assets: []release.ReleaseAsset{{ManifestAsset: release.ManifestAsset{OS: m.OS, Arch: m.Arch, Kind: m.PayloadKind + "-portable", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: digest(data), PayloadKind: m.PayloadKind}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := release.NewPublisherKeys([]ed25519.PublicKey{pub})
	v, err := keys.Verify(raw, ed25519.Sign(key, raw), m.Version)
	if err != nil {
		t.Fatal(err)
	}
	return Linux{m, c, data, v, append([]byte(nil), archive...), keys, contents}
}
func InstallLinux(t *testing.T, target string, i Linux) {
	t.Helper()
	for name, data := range i.Contents {
		path := filepath.Join(target, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), func() os.FileMode {
			for _, f := range i.Manifest.Files {
				if f.Path == name {
					return os.FileMode(f.Mode)
				}
			}
			return 0644
		}()); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, packagefiles.Name), i.Index, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sidravia.portable"), nil, 0644); err != nil {
		t.Fatal(err)
	}
}
