//go:build windows

package installationmanager

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDataDeletionRejectsJunctionAncestry(t *testing.T) {
	for _, redirected := range []string{"config", "cache", "logs"} {
		t.Run(redirected, func(t *testing.T) {
			root := t.TempDir()
			config, cache := filepath.Join(root, "config"), filepath.Join(root, "cache")
			outside := filepath.Join(root, "outside")
			junction := map[string]string{"config": config, "cache": cache, "logs": filepath.Join(cache, "logs")}[redirected]
			if err := os.MkdirAll(outside, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(junction), 0700); err != nil {
				t.Fatal(err)
			}
			winDir, err := windows.GetWindowsDirectory()
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(filepath.Join(winDir, "System32", "cmd.exe"), "/d", "/c", "mklink", "/J", junction, outside).CombinedOutput()
			if err != nil {
				t.Fatalf("create owned junction: %v: %s", err, output)
			}
			defer os.Remove(junction)
			files := []string{filepath.Join(config, "configurations.json"), filepath.Join(config, "gui-settings.json"), filepath.Join(cache, "runtime.json"), filepath.Join(cache, "logs", "sidraviad-2026-10-12-000001.log")}
			for _, path := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("owned fixture must survive"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := removeData(config, cache)
			if err == nil || changed {
				t.Fatalf("junction accepted or data changed: changed=%t err=%v", changed, err)
			}
			for _, path := range files {
				if data, err := os.ReadFile(path); err != nil || string(data) != "owned fixture must survive" {
					t.Fatalf("file changed before refusal: %s: %v", path, err)
				}
			}
		})
	}
}
