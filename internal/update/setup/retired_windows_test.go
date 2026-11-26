//go:build windows

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	packagefiles "sidravia/internal/release/files"
)

func TestRetiredCodeDeletionKeepsModifiedLinkedAndExternalFiles(t *testing.T) {
	for _, scenario := range []string{"matching", "modified", "linked", "alias", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			target, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(target, "retired.dll")
			data := []byte("old")
			if e := os.WriteFile(path, data, 0644); e != nil {
				t.Fatal(e)
			}
			external := filepath.Join(t.TempDir(), "external.dll")
			if e := os.WriteFile(external, data, 0644); e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "modified":
				if e := os.WriteFile(path, []byte("bad"), 0644); e != nil {
					t.Fatal(e)
				}
			case "linked":
				os.Remove(path)
				if e := os.Link(external, path); e != nil {
					t.Fatal(e)
				}
			case "alias":
				os.Remove(path)
				if e := os.Symlink(external, path); e != nil {
					t.Skip("native symbolic link not available", e)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			e := removeRetiredFile(ctx, target, packagefiles.File{Path: "retired.dll", Size: 3, SHA256: sum(data), Mode: 0644})
			if scenario == "matching" {
				if e != nil {
					t.Fatal(e)
				}
				if _, e = os.Lstat(path); !os.IsNotExist(e) {
					t.Fatal("retired code still present", e)
				}
			} else {
				if e == nil {
					t.Fatal("unverified code removed")
				}
				if _, e = os.Lstat(path); e != nil {
					t.Fatal("changed/linked/cancelled file removed", e)
				}
			}
			if b, e := os.ReadFile(external); e != nil || string(b) != "old" {
				t.Fatal("external data changed", e)
			}
		})
	}
}
