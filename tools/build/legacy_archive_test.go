package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func TestShippedGuideHasOrderedFirstUseJourney(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "advance.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The reviewed documentation separates daemon lifecycle from one-shot
	// authentication. Check each executable journey in its actual section,
	// rather than requiring the retired mixed sequence in the GUI guide.
	for heading, commands := range map[string][]string{
		"## 独立后台":  {"daemon start", "daemon status", "daemon stop"},
		"## 一次性认证": {"profile list", "auth start", "auth status"},
	} {
		_, section, exists := strings.Cut(strings.ReplaceAll(string(guide), "\r\n", "\n"), heading+"\n")
		if !exists {
			t.Fatalf("missing command journey: %s", heading)
		}
		section, _, _ = strings.Cut(section, "\n## ")
		previous := -1
		for _, token := range commands {
			position := strings.Index(section, `.\sidraviactl.exe `+token)
			if position < 0 || position < previous {
				t.Fatalf("guide order invalid in %s at %q", heading, token)
			}
			previous = position
		}
	}
}

func portableName(version string) string {
	return artifactFileName(version, "release")
}

func fieldValidationName(version string) string {
	return artifactFileName(version, "field-validation")
}

func TestFakeBuilderTwoCallsReuse(t *testing.T) {
	for _, artifact := range []string{"release", "field-validation"} {
		out, fb := buildArtifactOutput(t, "0.1.0-alpha.2", "buildid123", artifact)
		if len(fb.calls) != 2 {
			t.Fatalf("%s: build calls = %d, want 2", artifact, len(fb.calls))
		}
		if fb.calls[0][len(fb.calls[0])-1] != "./cmd/sidraviactl" {
			t.Errorf("%s: call 0 target = %q", artifact, fb.calls[0][len(fb.calls[0])-1])
		}
		if fb.calls[1][len(fb.calls[1])-1] != "./cmd/sidraviad" {
			t.Errorf("%s: call 1 target = %q", artifact, fb.calls[1][len(fb.calls[1])-1])
		}
		contents := readZipContents(t, filepath.Join(out, artifactFileName("0.1.0-alpha.2", artifact)))
		if !bytes.Equal(contents["sidraviactl.exe"], []byte("FAKE-BINARY-./cmd/sidraviactl")) {
			t.Errorf("%s: sidraviactl.exe does not match fake output", artifact)
		}
		if !bytes.Equal(contents["sidraviad.exe"], []byte("FAKE-BINARY-./cmd/sidraviad")) {
			t.Errorf("%s: sidraviad.exe does not match fake output", artifact)
		}
	}
}

func TestZipManifest(t *testing.T) {
	version := "0.1.0-alpha.2"
	buildID := "buildid123"
	cliBytes := []byte("FAKE-BINARY-./cmd/sidraviactl")
	daemonBytes := []byte("FAKE-BINARY-./cmd/sidraviad")
	baseExpected := []string{
		"BUILD-INFO.txt", "GETTING-STARTED.md", "LICENSE",
		"SHA256SUMS", "institution-profiles/jlu.json", "scripts/install.ps1",
		"scripts/uninstall.ps1", "sidraviactl.exe", "sidravia.portable", "sidraviad.exe",
	}

	for _, tc := range []struct {
		artifact string
		file     string
		field    bool
	}{
		{"release", portableName(version), false},
		{"field-validation", fieldValidationName(version), true},
	} {
		out, _ := buildArtifactOutput(t, version, buildID, tc.artifact)
		path := filepath.Join(out, tc.file)
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("%s: open zip: %v", tc.artifact, err)
		}
		var names []string
		contents := map[string][]byte{}
		for _, f := range zr.File {
			names = append(names, f.Name)
			if f.Name == "sidravia.portable" {
				// Every currently emitted artifact is explicitly portable.
			}
			if strings.Contains(f.Name, "\\") || strings.Contains(f.Name, "..") {
				t.Errorf("%s: bad entry name %q", tc.artifact, f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("%s: open entry %q: %v", tc.artifact, f.Name, err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("%s: read entry %q: %v", tc.artifact, f.Name, err)
			}
			contents[f.Name] = b
			if !f.Modified.Equal(epoch) {
				t.Errorf("%s: entry %q time = %v, want %v", tc.artifact, f.Name, f.Modified, epoch)
			}
			if f.Comment != "" {
				t.Errorf("%s: entry %q has comment", tc.artifact, f.Name)
			}
			if f.Method != zip.Deflate {
				t.Errorf("%s: entry %q method = %d, want Deflate", tc.artifact, f.Name, f.Method)
			}
			mode := (f.ExternalAttrs >> 16) & 0o777
			wantMode := uint32(0o644)
			if strings.HasSuffix(f.Name, ".exe") {
				wantMode = 0o755
			}
			if mode != wantMode {
				t.Errorf("%s: entry %q mode = %o, want %o", tc.artifact, f.Name, mode, wantMode)
			}
		}
		if zr.Comment != "" {
			t.Errorf("%s: archive has comment", tc.artifact)
		}
		zr.Close()

		expected := append([]string{}, baseExpected...)
		if tc.field {
			expected = append(expected, "scripts/cli-smoke.ps1", "scripts/field-test.ps1")
		}
		if !sortedEqual(names, expected) {
			t.Errorf("%s: names = %v, want sorted %v", tc.artifact, names, expected)
		}
		wantInfo := fmt.Sprintf("ProductVersion: %s\nBuildID: %s\nTarget: windows/amd64\nLayoutMode: portable\nArtifactKind: %s\n", version, buildID, tc.artifact)
		if string(contents["BUILD-INFO.txt"]) != wantInfo {
			t.Errorf("%s: BUILD-INFO = %q, want %q", tc.artifact, contents["BUILD-INFO.txt"], wantInfo)
		}
		if _, ok := contents["README.md"]; ok {
			t.Errorf("%s: archive must not contain README.md", tc.artifact)
		}
		if string(contents["LICENSE"]) != "LICENSE TEXT\n" {
			t.Errorf("%s: LICENSE wrong", tc.artifact)
		}
		if string(contents["GETTING-STARTED.md"]) != testGettingStarted {
			t.Errorf("%s: GETTING-STARTED.md must equal docs/guide.md", tc.artifact)
		}
		if string(contents["institution-profiles/jlu.json"]) != testProfileJSON {
			t.Errorf("%s: profile wrong", tc.artifact)
		}
		if tc.field {
			if string(contents["scripts/field-test.ps1"]) != testFieldTestScript {
				t.Errorf("%s: field test script wrong", tc.artifact)
			}
			if string(contents["scripts/cli-smoke.ps1"]) != testCLISmokeScript {
				t.Errorf("%s: CLI smoke script wrong", tc.artifact)
			}
		} else if _, ok := contents["scripts/field-test.ps1"]; ok {
			t.Errorf("release contains field-test.ps1")
		} else if _, ok := contents["scripts/cli-smoke.ps1"]; ok {
			t.Errorf("release contains cli-smoke.ps1")
		}
		if string(contents["scripts/install.ps1"]) != testInstallScript {
			t.Errorf("%s: install script wrong", tc.artifact)
		}
		if string(contents["scripts/uninstall.ps1"]) != testUninstallScript {
			t.Errorf("%s: uninstall script wrong", tc.artifact)
		}
		wantInternal := sha256Hex(cliBytes) + "  sidraviactl.exe\n" + sha256Hex(daemonBytes) + "  sidraviad.exe\n"
		if string(contents["SHA256SUMS"]) != wantInternal {
			t.Errorf("%s: internal sums = %q, want %q", tc.artifact, contents["SHA256SUMS"], wantInternal)
		}
		if len(contents["sidravia.portable"]) != 0 {
			t.Errorf("%s: marker not empty", tc.artifact)
		}
	}
}

func sortedEqual(got, want []string) bool {
	gotSorted := slices.Clone(got)
	wantSorted := slices.Clone(want)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)
	return slices.Equal(gotSorted, wantSorted)
}

func TestReproduciblePackaging(t *testing.T) {
	version := "0.1.0-alpha.2"
	buildID := "buildid123"
	for _, artifact := range []string{"release", "field-validation"} {
		out1, _ := buildArtifactOutput(t, version, buildID, artifact)
		out2, _ := buildArtifactOutput(t, version, buildID, artifact)
		for _, name := range []string{artifactFileName(version, artifact), "SHA256SUMS.txt"} {
			b1, err := os.ReadFile(filepath.Join(out1, name))
			if err != nil {
				t.Fatalf("%s: read out1/%s: %v", artifact, name, err)
			}
			b2, err := os.ReadFile(filepath.Join(out2, name))
			if err != nil {
				t.Fatalf("%s: read out2/%s: %v", artifact, name, err)
			}
			if !bytes.Equal(b1, b2) {
				t.Errorf("%s: %s differs between repeated builds", artifact, name)
			}
		}
	}
}

func TestMissingOfficialProfileFailsInputs(t *testing.T) {
	root := setupRepoRoot(t)
	profilePath := filepath.Join(root, "internal", "daemon", "configuration", "profiles", "jlu.json")
	if err := os.Remove(profilePath); err != nil {
		t.Fatalf("remove profile: %v", err)
	}
	out := filepath.Join(t.TempDir(), "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	err := tt.execute(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no lowercase .json Profiles") {
		t.Fatalf("expected missing-profile input error, got %v", err)
	}
	if len(fb.calls) != 0 {
		t.Errorf("build called %d times", len(fb.calls))
	}
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output created")
	}
}

func TestFieldToolsAreOnlyValidationInputs(t *testing.T) {
	for _, missing := range []string{"scripts/field-test.ps1", "tools/cli_smoke.ps1"} {
		t.Run(missing, func(t *testing.T) {
			root := setupRepoRoot(t)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(missing))); err != nil {
				t.Fatalf("remove: %v", err)
			}

			releaseOut := filepath.Join(t.TempDir(), "release")
			releaseBuilder := &fakeBuilder{}
			release := newFakeTool(config{version: "0.1.0", buildID: "abc", output: releaseOut, goBin: "go", artifact: "release"}, root, releaseBuilder)
			if err := release.execute(io.Discard, io.Discard); err != nil {
				t.Fatalf("release unexpectedly depends on %s: %v", missing, err)
			}

			validationOut := filepath.Join(t.TempDir(), "validation")
			validationBuilder := &fakeBuilder{}
			validation := newFakeTool(config{version: "0.1.0", buildID: "abc", output: validationOut, goBin: "go", artifact: "field-validation"}, root, validationBuilder)
			err := validation.execute(io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), filepath.Base(missing)) {
				t.Fatalf("validation missing-input error = %v", err)
			}
			if len(validationBuilder.calls) != 0 {
				t.Errorf("validation built before input failure")
			}
			if _, statErr := os.Lstat(validationOut); !os.IsNotExist(statErr) {
				t.Errorf("validation output created")
			}
		})
	}
}

func TestReleaseScriptsRequireBOM(t *testing.T) {
	for _, script := range []string{"scripts/install.ps1", "scripts/uninstall.ps1"} {
		t.Run(script, func(t *testing.T) {
			root := setupRepoRoot(t)
			// Rewrite without the UTF-8 BOM.
			raw := []byte("# " + filepath.Base(script) + "\n")
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(script)), raw, 0o644); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			out := filepath.Join(t.TempDir(), "dist")
			fb := &fakeBuilder{}
			tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
			err := tt.execute(io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "BOM") {
				t.Fatalf("expected BOM error, got %v", err)
			}
			if len(fb.calls) != 0 {
				t.Errorf("build called %d times", len(fb.calls))
			}
			if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
				t.Errorf("output created")
			}
		})
	}
}

func TestZipScriptBytesMatchSource(t *testing.T) {
	version := "0.1.0-alpha.2"
	root := setupRepoRoot(t)
	out := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: version, buildID: "buildid123", output: out, goBin: "go", artifact: "field-validation"}
	if err := newFakeTool(cfg, root, &fakeBuilder{}).execute(io.Discard, io.Discard); err != nil {
		t.Fatalf("execute: %v", err)
	}
	contents := readZipContents(t, filepath.Join(out, fieldValidationName(version)))
	sourceMap := map[string]string{
		"scripts/install.ps1":    "scripts/install.ps1",
		"scripts/uninstall.ps1":  "scripts/uninstall.ps1",
		"scripts/field-test.ps1": "scripts/field-test.ps1",
		"scripts/cli-smoke.ps1":  "tools/cli_smoke.ps1",
	}
	for zipName, source := range sourceMap {
		onDisk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
		if err != nil {
			t.Fatalf("read source %s: %v", source, err)
		}
		if !bytes.Equal(contents[zipName], onDisk) {
			t.Errorf("zip entry %s differs from source %s bytes", zipName, source)
		}
	}
}

func TestExternalSums(t *testing.T) {
	version := "0.1.0-alpha.2"
	out, _ := buildOutput(t, version, "buildid123")
	sumsBytes, err := os.ReadFile(filepath.Join(out, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatalf("read sums: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(sumsBytes), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("sums lines = %d, want 1", len(lines))
	}
	portableBytes, err := os.ReadFile(filepath.Join(out, portableName(version)))
	if err != nil {
		t.Fatalf("read portable archive: %v", err)
	}
	wantLines := []string{
		sha256Hex(portableBytes) + "  " + portableName(version),
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("sums line %d = %q, want %q", i, lines[i], want)
		}
	}
	// Output directory has exactly the selected zip and sums file.
	listing, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	if len(listing) != 2 {
		t.Errorf("output entries = %d, want 2", len(listing))
	}
	wantNames := map[string]bool{portableName(version): true, "SHA256SUMS.txt": true}
	for _, entry := range listing {
		if entry.IsDir() || !wantNames[entry.Name()] {
			t.Errorf("unexpected output entry: %s", entry.Name())
		}
	}
}
