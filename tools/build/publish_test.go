package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errBoom = errors.New("simulated build failure")
var errZip = errors.New("simulated zip failure")
var errPublish = errors.New("simulated publish failure")

func TestExistingOutputFails(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	if err := os.WriteFile(out, []byte("preexisting"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	err := tt.execute(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "preexisting" {
		t.Errorf("existing output overwritten: %q (%v)", b, err)
	}
	if len(fb.calls) != 0 {
		t.Errorf("build called")
	}
}

func TestNonDirParentFails(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	parentFile := filepath.Join(outParent, "afile")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := filepath.Join(parentFile, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	err := tt.execute(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
	if len(fb.calls) != 0 {
		t.Errorf("build called")
	}
}

func TestBuildFailureCleans(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.build = func([]string, []string) ([]byte, error) { return nil, errBoom }
	err := tt.execute(io.Discard, io.Discard)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
	assertOutputAbsentAndParentEmpty(t, out)
}

func TestZipFailureCleans(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.packageArtifacts = func(string, string, string, string, string, []zipEntry, []byte, []byte) error { return errZip }
	err := tt.execute(io.Discard, io.Discard)
	if !errors.Is(err, errZip) {
		t.Fatalf("expected errZip, got %v", err)
	}
	if len(fb.calls) != 2 {
		t.Errorf("build calls = %d, want 2", len(fb.calls))
	}
	assertOutputAbsentAndParentEmpty(t, out)
}

func TestPublishFailureCleans(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.publish = func(string, string) error { return errPublish }
	err := tt.execute(io.Discard, io.Discard)
	if !errors.Is(err, errPublish) {
		t.Fatalf("expected errPublish, got %v", err)
	}
	assertOutputAbsentAndParentEmpty(t, out)
}

func TestErrorCauseFilesystem(t *testing.T) {
	root := setupRepoRoot(t)
	// Output parent does not exist.
	out := filepath.Join(t.TempDir(), "missing-sub", "dist")
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, &fakeBuilder{})
	err := tt.execute(io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("expected *fs.PathError cause, got %T: %v", err, err)
	}
}
