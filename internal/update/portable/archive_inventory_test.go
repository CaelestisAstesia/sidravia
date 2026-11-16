//go:build linux

package portable

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/update/releases"
)

func TestExtractReleaseAuthenticatesDiscoveredIndex(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed-index", true: "index-proof-mismatch"}[mismatch], func(t *testing.T) {
			i := image(t, "1.1.0", "new", map[string]string{"program": "new"}, nil)
			proof := i.verified
			if mismatch {
				data, _ := proof.Proof()
				var m releases.ReleaseManifest
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
