package installationmanager

import (
	"os"
	"path/filepath"
	"testing"
)

// Some hosts put their temporary directory below a system link (for example
// /var on macOS). Success fixtures must not exercise link refusal by accident.
func dataTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDataDeletionWhitelist(t *testing.T) {
	root := dataTempDir(t)
	config, cache := filepath.Join(root, "config"), filepath.Join(root, "cache")
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
	root := dataTempDir(t)
	first := filepath.Join(root, "configurations.json")
	if err := os.WriteFile(first, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dataTempDir(t), "outside.json")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "gui-settings.json")); err != nil {
		t.Skip("symlink unavailable")
	}
	changed, err := removeData(root, filepath.Join(dataTempDir(t), "cache"))
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

func TestDataDeletionRejectsLinkedAncestorBeforeMutation(t *testing.T) {
	root := dataTempDir(t)
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, redirected := range []string{"config", "cache"} {
		t.Run(redirected, func(t *testing.T) {
			config, cache := filepath.Join(real, "config"), filepath.Join(real, "cache")
			files := []string{filepath.Join(config, "configurations.json"), filepath.Join(cache, "runtime.json")}
			for _, path := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if redirected == "config" {
				config = filepath.Join(alias, "config")
			} else {
				cache = filepath.Join(alias, "cache")
			}
			changed, err := removeData(config, cache)
			if err == nil || changed {
				t.Fatalf("linked ancestor accepted: changed=%t err=%v", changed, err)
			}
			for _, path := range files {
				if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
					t.Fatalf("changed before link refusal: %s: %v", path, err)
				}
			}
		})
	}
}
