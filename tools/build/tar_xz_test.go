package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xi2/xz"
)

func TestXZIsAnExplicitLinuxDistributionEncoding(t *testing.T) {
	for _, target := range []string{"linux-amd64", "linux-arm64", "windows-amd64"} {
		_, err := parseFlags([]string{"--artifact", "distribution", "--version", "1.0.0", "--build-id", "test", "--target", target, "--compression", "xz", "--output", "out"})
		if (err == nil) != strings.HasPrefix(target, "linux-") {
			t.Fatal(target, err)
		}
	}
}
func TestXZProducerUsesDeterministicTarAndBoundedDictionary(t *testing.T) {
	version, err := xzCommand("--version").Output()
	if err != nil || !strings.HasPrefix(string(version), "xz (XZ Utils) "+xzVersion+"\n") {
		t.Skip("fixed XZ encoder unavailable")
	}
	entries := []archiveEntry{{name: "LICENSE", data: []byte("license"), mode: 0644}}
	var previous []byte
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(t.TempDir(), name+".tar.xz")
		if err := writeTarXZ(path, "root", entries); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if previous != nil && string(data) != string(previous) {
			t.Fatal("XZ output is nondeterministic")
		}
		previous = data
		f, _ := os.Open(path)
		r, err := xz.NewReader(f, 8<<20)
		if err != nil {
			t.Fatal(err)
		}
		tr := tar.NewReader(r)
		h, err := tr.Next()
		if err != nil || h.Name != "root/LICENSE" {
			t.Fatal(h, err)
		}
		body, err := io.ReadAll(tr)
		if err != nil || string(body) != "license" {
			t.Fatal(string(body), err)
		}
		if _, err = io.Copy(io.Discard, r); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
