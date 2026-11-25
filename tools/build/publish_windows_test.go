package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishDirectoryWithLongWindowsPaths(t *testing.T) {
	parent := filepath.Join(t.TempDir(), strings.Repeat("segment\\", 40), "parent")
	if len(parent) <= 260 {
		t.Fatalf("fixture path is too short: %d characters", len(parent))
	}
	source, destination := filepath.Join(parent, "pending"), filepath.Join(parent, "candidate")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := publishDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	unc := "\\\\server\\share\\" + strings.Repeat("segment\\", 40) + "parent"
	extended := "\\\\?\\UNC\\" + unc[2:]
	if publicationPath(unc) != extended || publicationPath(extended) != extended {
		t.Fatal("UNC long paths must preserve their share and extended prefix")
	}
}
