package fixture

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
	"testing"
)

type Windows struct {
	Candidate release.Candidate
	Index     []byte
	Verified  release.VerifiedRelease
	Archive   []byte
	Keys      *release.PublisherKeys
	Files     map[string][]byte
}

func WindowsImage(t *testing.T, version, build string, files map[string][]byte) Windows {
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
	return Windows{c, raw, v, append([]byte(nil), buf.Bytes()...), keys, contents}
}
func WindowsFiles(value string) map[string][]byte {
	m := map[string][]byte{}
	for _, name := range []string{"sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe", "sidraviaupdater.exe", "institution-profiles/test.json"} {
		m[name] = []byte(value)
	}
	return m
}
func InstallWindows(t *testing.T, target string, i Windows) {
	t.Helper()
	for name, data := range i.Files {
		path := filepath.Join(target, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, data, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
