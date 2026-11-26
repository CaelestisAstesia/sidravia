//go:build linux

package portable

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/release"
)

func TestExtractReleaseAuthenticatesDiscoveredIndex(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed-index", true: "index-proof-mismatch"}[mismatch], func(t *testing.T) {
			i := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
			proof := i.verified
			if mismatch {
				data, _ := proof.Proof()
				var m release.ReleaseManifest
				if err := json.Unmarshal(data, &m); err != nil {
					t.Fatal(err)
				}
				m.Assets[0].FilesSHA256 = strings.Repeat("a", 64)
				data, _ = json.Marshal(m)
				key := ed25519.NewKeyFromSeed(make([]byte, 32))
				var err error
				proof, err = i.keys.Verify(data, ed25519.Sign(key, data), m.Version)
				if err != nil {
					t.Fatal(err)
				}
			}
			archive := filepath.Join(t.TempDir(), i.inventory.candidate.Name)
			if err := os.WriteFile(archive, i.archive, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := ExtractRelease(context.Background(), t.TempDir(), archive, proof, i.inventory.candidate)
			if mismatch {
				if err == nil {
					p.Close()
					t.Fatal("discovered index bypassed publisher proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestXZReleaseUsesAuthenticatedIndexAndFullArchiveValidation(t *testing.T) {
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("XZ fixture encoder unavailable")
	}
	original := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
	gz, err := gzip.NewReader(bytes.NewReader(original.archive))
	if err != nil {
		t.Fatal(err)
	}
	rawTar, err := io.ReadAll(gz)
	gz.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("xz", "--threads=1", "--lzma2=dict=8MiB", "--stdout")
	cmd.Stdin = bytes.NewReader(rawTar)
	archive, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, badProof := range []bool{false, true} {
		raw, _ := original.verified.Proof()
		var m release.ReleaseManifest
		json.Unmarshal(raw, &m)
		a := &m.Assets[0]
		a.Name = strings.TrimSuffix(a.Name, ".gz") + ".xz"
		a.Size = int64(len(archive))
		a.SHA256 = digest(archive)
		if badProof {
			a.FilesSHA256 = strings.Repeat("b", 64)
		}
		raw, _ = json.Marshal(m)
		key := ed25519.NewKeyFromSeed(make([]byte, 32))
		proof, err := original.keys.Verify(raw, ed25519.Sign(key, raw), m.Version)
		if err != nil {
			t.Fatal(err)
		}
		c := original.inventory.candidate
		c.Name = a.Name
		c.Size = a.Size
		c.SHA256 = a.SHA256
		path := filepath.Join(t.TempDir(), c.Name)
		os.WriteFile(path, archive, 0600)
		payload, err := ExtractRelease(context.Background(), t.TempDir(), path, proof, c)
		if badProof {
			if err == nil {
				payload.Close()
				t.Fatal("XZ index mismatch accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = payload.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
