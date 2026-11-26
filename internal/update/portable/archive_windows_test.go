//go:build windows

package portable

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveSnapshotUsesOnlyTheAuthenticatedBytes(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	image := windowsImage(t, "1.1.0", "new", winFiles("new"))
	candidate := image.inventory.candidate
	archive := filepath.Join(directory, candidate.Name)
	if err := os.WriteFile(archive, image.archive, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, size, err := openArchive(ctx, directory, archive, candidate.Size)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	if err := verifyArchive(ctx, snapshot, candidate); err != nil {
		t.Fatal(err)
	}

	altered := bytes.Repeat([]byte{'x'}, len(image.archive))
	if err := os.WriteFile(archive, altered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(archive, archive+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, altered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.WriteAt([]byte{'x'}, 0); err == nil {
		t.Fatal("read handle retained write access")
	}
	if err := os.WriteFile(snapshot.Name(), altered, 0600); err == nil {
		t.Fatal("snapshot allowed a second writer")
	}
	if err := os.Rename(archive, snapshot.Name()); err == nil {
		t.Fatal("snapshot allowed replacement")
	}
	asset, err := image.verified.Asset(candidate)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := extractRelease(ctx, target, snapshot, size, image.verified, candidate, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := payload.Close(); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshot.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot close did not remove the file: %v", err)
	}
	if _, err := ExtractRelease(ctx, target, archive, image.verified, candidate); !errors.Is(err, ErrChanged) {
		t.Fatalf("fresh extraction accepted altered candidate: %v", err)
	}
}
