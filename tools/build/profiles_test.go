package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/daemon/persistence/jsonfile"
)

func syntheticProfile(id string) string {
	return strings.Replace(testProfileJSON, `"jlu"`, `"`+id+`"`, 1)
}

func buildWithProfileOrder(t *testing.T, artifact string, order []string) []byte {
	t.Helper()
	root := setupRepoRoot(t)
	profileDir := filepath.Join(root, "internal", "daemon", "configuration", "profiles")
	if err := os.Remove(filepath.Join(profileDir, "jlu.json")); err != nil {
		t.Fatal(err)
	}
	for _, id := range order {
		writeTestFile(t, filepath.Join(profileDir, id+".json"), syntheticProfile(id))
	}
	output := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: "0.1.0", buildID: "abc", output: output, goBin: "go", artifact: artifact}
	if err := newFakeTool(cfg, root, &fakeBuilder{}).execute(io.Discard, io.Discard); err != nil {
		t.Fatalf("execute: %v", err)
	}
	archive, err := os.ReadFile(filepath.Join(output, artifactFileName("0.1.0", artifact)))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	return archive
}

func TestAdditionalOfficialProfileIsValidatedAndPackagedDeterministically(t *testing.T) {
	for _, artifact := range []string{"release", "field-validation"} {
		t.Run(artifact, func(t *testing.T) {
			first := buildWithProfileOrder(t, artifact, []string{"alpha-campus", "zeta-campus"})
			reversed := buildWithProfileOrder(t, artifact, []string{"zeta-campus", "alpha-campus"})
			if !bytes.Equal(first, reversed) {
				t.Fatal("archive differs when distinct source files are created in reverse order")
			}
			archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			var names []string
			for _, file := range archive.File {
				if !strings.HasPrefix(file.Name, "institution-profiles/") {
					continue
				}
				rc, err := file.Open()
				if err != nil {
					t.Fatalf("open %s: %v", file.Name, err)
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					t.Fatalf("read %s: %v", file.Name, err)
				}
				id := strings.TrimSuffix(strings.TrimPrefix(file.Name, "institution-profiles/"), ".json")
				if !bytes.Equal(data, []byte(syntheticProfile(id))) {
					t.Errorf("%s Profile bytes changed", file.Name)
				}
				names = append(names, file.Name)
			}
			if got, want := strings.Join(names, ","), "institution-profiles/alpha-campus.json,institution-profiles/zeta-campus.json"; got != want {
				t.Errorf("Profile entries = %q, want %q", got, want)
			}
		})
	}
}

func TestOfficialProfilesUseValidatedSnapshot(t *testing.T) {
	for _, artifact := range []string{"release", "field-validation"} {
		t.Run(artifact, func(t *testing.T) {
			root := setupRepoRoot(t)
			output := filepath.Join(t.TempDir(), "dist")
			fb := &fakeBuilder{}
			cfg := config{version: "0.1.0", buildID: "abc", output: output, goBin: "go", artifact: artifact}
			tool := newFakeTool(cfg, root, fb)
			tool.build = func(env, argv []string) ([]byte, error) {
				writeTestFile(t, filepath.Join(root, "internal", "daemon", "configuration", "profiles", "jlu.json"), "invalid after snapshot")
				return fb.build(env, argv)
			}
			if err := tool.execute(io.Discard, io.Discard); err != nil {
				t.Fatalf("execute: %v", err)
			}
			contents := readZipContents(t, filepath.Join(output, artifactFileName("0.1.0", artifact)))
			if !bytes.Equal(contents["institution-profiles/jlu.json"], []byte(testProfileJSON)) {
				t.Fatal("archive did not retain the validated input bytes")
			}
		})
	}
}

func TestInvalidOfficialProfileSetFailsWithoutPublishing(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		data string
	}{
		{name: "malformed JSON", file: "jlu.json", data: `{"schemaVersion":`},
		{name: "unknown protocol", file: "jlu.json", data: strings.Replace(testProfileJSON, `"drcom-5.2.0-d"`, `"unknown-protocol"`, 1)},
		{name: "filename mismatch duplicate ID", file: "test-campus.json", data: testProfileJSON},
		{name: "invalid protocol configuration", file: "jlu.json", data: strings.Replace(testProfileJSON, `"serverPort": 61440`, `"serverPort": 0`, 1)},
		{name: "duplicate JSON field", file: "jlu.json", data: strings.Replace(testProfileJSON, `"schemaVersion": 1,`, `"schemaVersion": 1, "schemaVersion": 1,`, 1)},
		{name: "unknown JSON field", file: "jlu.json", data: strings.Replace(testProfileJSON, `"schemaVersion": 1,`, `"schemaVersion": 1, "unknown": true,`, 1)},
		{name: "trailing JSON", file: "jlu.json", data: testProfileJSON + `{}`},
		{name: "oversized input", file: "jlu.json", data: strings.Repeat(" ", int(jsonfile.ProfileFileSizeLimit)+1)},
	} {
		for _, artifact := range []string{"release", "field-validation"} {
			for _, existing := range []bool{false, true} {
				state := "absent output"
				if existing {
					state = "existing output"
				}
				t.Run(tc.name+"/"+artifact+"/"+state, func(t *testing.T) {
					root := setupRepoRoot(t)
					writeTestFile(t, filepath.Join(root, "internal", "daemon", "configuration", "profiles", tc.file), tc.data)
					parent := t.TempDir()
					output := filepath.Join(parent, "dist")
					marker := filepath.Join(output, "keep.txt")
					if existing {
						writeTestFile(t, marker, "keep")
					}
					fb := &fakeBuilder{}
					cfg := config{version: "0.1.0", buildID: "abc", output: output, goBin: "go", artifact: artifact}
					err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard)
					if err == nil || !strings.Contains(err.Error(), "load official institution profiles") {
						t.Fatalf("expected Profile input failure before output checks, got %v", err)
					}
					if len(fb.calls) != 0 {
						t.Fatalf("binary builder called %d times on invalid inputs", len(fb.calls))
					}
					if existing {
						data, err := os.ReadFile(marker)
						if err != nil || string(data) != "keep" {
							t.Fatalf("existing output changed: %q (%v)", data, err)
						}
						entries, err := os.ReadDir(output)
						if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
							t.Fatalf("existing output contents changed: %v (%v)", entries, err)
						}
					} else if _, err := os.Lstat(output); !os.IsNotExist(err) {
						t.Fatalf("output published or stat failed: %v", err)
					}
					entries, err := os.ReadDir(parent)
					want := 0
					if existing {
						want = 1
					}
					if err != nil || len(entries) != want {
						t.Fatalf("output parent changed: %v (%v)", entries, err)
					}
				})
			}
		}
	}
}

func TestOfficialProfileSourceRejectsAbsentEmptyAndNonregularInputs(t *testing.T) {
	for _, kind := range []string{"absent directory", "empty directory", "directory with JSON suffix", "symlink JSON"} {
		t.Run(kind, func(t *testing.T) {
			root := setupRepoRoot(t)
			profileDir := filepath.Join(root, "internal", "daemon", "configuration", "profiles")
			switch kind {
			case "absent directory":
				if err := os.RemoveAll(profileDir); err != nil {
					t.Fatal(err)
				}
			case "empty directory":
				if err := os.Remove(filepath.Join(profileDir, "jlu.json")); err != nil {
					t.Fatal(err)
				}
			case "directory with JSON suffix":
				if err := os.Mkdir(filepath.Join(profileDir, "folder.json"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink JSON":
				if err := os.Symlink(filepath.Join(profileDir, "jlu.json"), filepath.Join(profileDir, "link.json")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			output := filepath.Join(t.TempDir(), "dist")
			fb := &fakeBuilder{}
			cfg := config{version: "0.1.0", buildID: "abc", output: output, goBin: "go", artifact: "release"}
			err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "load official institution profiles") {
				t.Fatalf("invalid source accepted or wrong error: %v", err)
			}
			if len(fb.calls) != 0 {
				t.Fatalf("binary builder called %d times", len(fb.calls))
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("output published or stat failed: %v", err)
			}
		})
	}
}
