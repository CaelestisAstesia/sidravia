//go:build windows

package transaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPortableSealOwnership(t *testing.T) {
	for _, scenario := range []string{"own", "other-operation", "trailing-data", "large", "hard-link", "directory"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			id := strings.Repeat("a", 32)
			path := filepath.Join(root, StartupSeal)
			data := id + "\n"
			switch scenario {
			case "other-operation":
				data = strings.Repeat("b", 32) + "\n"
			case "trailing-data":
				data += "more"
			case "large":
				data = strings.Repeat("x", 1<<20)
			}
			if scenario == "directory" {
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(path, []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "hard-link" {
				if e := os.Link(path, filepath.Join(root, "alias")); e != nil {
					t.Fatal(e)
				}
			}
			err := VerifyPortableSeal(root, id)
			if (err == nil) != (scenario == "own") {
				t.Fatalf("seal %s: %v", scenario, err)
			}
		})
	}
}
