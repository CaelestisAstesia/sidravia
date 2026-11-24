// Package testfixture supports source overlays for native update tests.
package testfixture

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sourceOverlay struct {
	t            testing.TB
	repo         string
	owned        string
	replacements map[string]string
}

// WritePublisherOverlay writes a fixture-only Go build overlay for the
// synthetic publisher. The caller retains ownership of the fixture directory.
func WritePublisherOverlay(t testing.TB, repo, owned string, pub ed25519.PublicKey) string {
	t.Helper()
	return newPublisherOverlay(t, repo, owned, pub).Write()
}

func newPublisherOverlay(t testing.TB, repo, owned string, pub ed25519.PublicKey) *sourceOverlay {
	t.Helper()
	overlay := &sourceOverlay{t: t, repo: repo, owned: owned, replacements: map[string]string{}}
	overlay.Replace("internal/update/releases/trust.go", func(source string) string {
		parts := make([]string, 0, len(pub))
		for _, b := range pub {
			parts = append(parts, fmt.Sprint(b))
		}
		return strings.Replace(source, "NewPublisherKeys([]ed25519.PublicKey{ed25519.PublicKey(publisherPublicKey)})",
			"NewPublisherKeys([]ed25519.PublicKey{{"+strings.Join(parts, ",")+"}})", 1)
	})
	return overlay
}

// Replace records a fixture-specific patch without editing the repository.
func (o *sourceOverlay) Replace(relative string, edit func(string) string) {
	o.t.Helper()
	original := filepath.Join(o.repo, filepath.FromSlash(relative))
	data, err := os.ReadFile(original)
	if err != nil {
		o.t.Fatal(err)
	}
	updated := edit(string(data))
	if updated == string(data) {
		o.t.Fatal("overlay source changed", relative)
	}
	destination := filepath.Join(o.owned, strings.ReplaceAll(relative, "/", "-"))
	if err := os.WriteFile(destination, []byte(updated), 0o600); err != nil {
		o.t.Fatal(err)
	}
	o.replacements[original] = destination
}

// Write saves the manifest used by go build.
func (o *sourceOverlay) Write() string {
	o.t.Helper()
	path := filepath.Join(o.owned, "overlay.json")
	data, err := json.Marshal(map[string]map[string]string{"Replace": o.replacements})
	if err != nil {
		o.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		o.t.Fatal(err)
	}
	return path
}
