package installationmanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDeletionWhitelist(t *testing.T) {
	config, cache := filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "cache")
	remove := []string{filepath.Join(config, "configurations.json"), filepath.Join(config, "gui-settings.json"), filepath.Join(cache, "runtime.json"), filepath.Join(cache, "logs", "sidraviad.log"), filepath.Join(cache, "logs", "sidraviad-2026-10-12-000001.log")}
	keep := []string{filepath.Join(config, "namespaces", "test", "configurations.json"), filepath.Join(config, "other.json"), filepath.Join(cache, "logs", "unrelated.log"), filepath.Join(cache, "logs", "sidraviad-unknown.log")}
	for _, p := range append(remove, keep...) {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := removeData(config, cache)
	if err != nil || !changed {
		t.Fatalf("%t %v", changed, err)
	}
	for _, p := range remove {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("not removed: %s", p)
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("unrelated removed: %s", p)
		}
	}
	changed, err = removeData(config, cache)
	if err != nil || changed {
		t.Fatalf("repeat changed=%t %v", changed, err)
	}
}
func TestDataDeletionRejectsLinksBeforeMutation(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "configurations.json")
	if err := os.WriteFile(first, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "gui-settings.json")); err != nil {
		t.Skip("symlink unavailable")
	}
	changed, err := removeData(root, filepath.Join(t.TempDir(), "cache"))
	if err == nil || changed {
		t.Fatalf("unsafe deletion: %t %v", changed, err)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("changed before complete validation")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("followed link")
	}
}
