package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sidravia/internal/release"
)

func TestQtDistributionRequiresCombinedWindowsPayload(t *testing.T) {
	base := []string{"--version", "0.6.0-alpha.1", "--build-id", "test", "--output", "out", "--artifact", "distribution"}
	tools := t.TempDir()
	for _, tc := range []struct {
		name, target string
		args         []string
		valid        bool
	}{
		{"combined", "windows-amd64", []string{"--gui-directory", "gui", "--ifw", tools}, true},
		{"gui absent", "windows-amd64", []string{"--ifw", tools}, false},
		{"two installer builders", "windows-amd64", []string{"--gui-directory", "gui", "--ifw", tools, "--iscc", "iscc"}, false},
		{"other platform", "linux-amd64", []string{"--gui-directory", "gui", "--ifw", tools}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string(nil), base...), "--target", tc.target)
			_, err := parseFlags(append(args, tc.args...))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}

func TestQtRepositoryInventoryAuthenticatesGeneratedFiles(t *testing.T) {
	root := t.TempDir()
	payload := map[string][]byte{"Updates.xml": []byte("<Updates/>"), "archive.7z": []byte("archive")}
	for name, content := range payload {
		if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "repo.zip")
	if err := packageQtRepository(root, archive, "windows-amd64", "core", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	gotHashes := map[string]string{}
	var inventory release.QtRepositoryIndex
	for _, entry := range reader.File {
		f, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == "sidravia-ifw-inventory.json" {
			if err := json.Unmarshal(data, &inventory); err != nil {
				t.Fatal(err)
			}
		} else {
			gotHashes[entry.Name] = sha256Hex(data)
		}
	}
	if !reflect.DeepEqual(gotHashes, inventory.Files) || len(gotHashes) != len(payload) {
		t.Fatalf("inventory does not match archived files: %v %v", inventory, gotHashes)
	}
}

func TestQtCandidateVersionRestrictsUnsupportedComparisonShapes(t *testing.T) {
	for _, value := range []string{"1.0.0", "1.0.0-rc.1", "1.0.0-dev.qt.3"} {
		if !validQtCandidateVersion(value) {
			t.Errorf("expected a Qt candidate version, got %q", value)
		}
	}
	for _, value := range []string{"1.0.0-1", "1.0.0-alpha", "1.0.0-alpha.beta", "1.0.0-alpha.01", "1000000000000000000.0.0"} {
		if validQtCandidateVersion(value) {
			t.Errorf("accepted a version outside the Qt candidate subset, %q", value)
		}
	}
}
