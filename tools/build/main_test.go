package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

var errBoom = errors.New("simulated build failure")
var errZip = errors.New("simulated zip failure")
var errPublish = errors.New("simulated publish failure")

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// These representative scripts prove Release/validation content separation.
// Release scripts carry a UTF-8 BOM; field-test/cli-smoke stay plain ASCII.
const (
	testFieldTestScript = "# field-test.ps1\n"
	testCLISmokeScript  = "# cli-smoke.ps1\n"
	testInstallScript   = "\xEF\xBB\xBF# install.ps1\n"
	testUninstallScript = "\xEF\xBB\xBF# uninstall.ps1\n"
	testGettingStarted  = "# Sidravia package guide\n"
)

// testProfileJSON is a representative non-secret institution Profile the fake
// repo ships. The build tool embeds it byte-for-byte; TestZipManifest asserts
// both zips carry it at their documented paths.
const testProfileJSON = `{
  "schemaVersion": 1,
  "institutionProfileId": "jlu",
  "displayName": "吉林大学",
  "authenticationProtocolId": "drcom-5.2.0-d",
  "institutionProtocolConfiguration": {
    "serverAddress": "10.100.61.3",
    "serverPort": 61440,
    "localPort": {"mode": "fixed", "value": 61440},
    "authVersionHex": "2c00",
    "keepAliveVersionHex": "dc02",
    "controlCheckStatusHex": "20",
    "ipdogHex": "01",
    "adapterNumberHex": "01",
    "osInfoHex": "940000000600000000000000280a000002000000",
    "challengePaddingHex": "000000000000000000000000000000",
    "loginIPDogPaddingHex": "00000000",
    "loginDHCPPaddingHex": "0000000000000000",
    "loginAuthExtensionPaddingHex": "0000",
    "challengeTimeout": "3s",
    "loginTimeout": "5s",
    "keepaliveTimeout": "3s",
    "logoutTimeout": "1s",
    "heartbeatInterval": "20s",
    "busyMaxAttempts": 3,
    "busyBackoffMin": "1s",
    "busyBackoffMax": "2s"
  }
}
`

func setupRepoRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module sidravia\n\ngo 1.26.4\n")
	// README.md exists in the repo but must not be packaged by the builder.
	writeTestFile(t, filepath.Join(root, "README.md"), "# Sidravia readme\n")
	writeTestFile(t, filepath.Join(root, "LICENSE"), "LICENSE TEXT\n")
	writeTestFile(t, filepath.Join(root, "tools", "build", "assets", "GETTING-STARTED.md"), testGettingStarted)
	writeTestFile(t, filepath.Join(root, "internal", "daemon", "configuration", "profiles", "jlu.json"), testProfileJSON)
	writeTestFile(t, filepath.Join(root, "scripts", "field-test.ps1"), testFieldTestScript)
	writeTestFile(t, filepath.Join(root, "scripts", "install.ps1"), testInstallScript)
	writeTestFile(t, filepath.Join(root, "scripts", "uninstall.ps1"), testUninstallScript)
	writeTestFile(t, filepath.Join(root, "tools", "cli_smoke.ps1"), testCLISmokeScript)
	if err := os.MkdirAll(filepath.Join(root, "cmd", "sidravia"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/sidravia: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "sidraviad"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/sidraviad: %v", err)
	}
	return root
}

// fakeBuilder records every build call and returns deterministic bytes derived
// from the package path so the two binaries differ.
type fakeBuilder struct {
	calls [][]string
	envs  [][]string
}

func (f *fakeBuilder) build(env []string, argv []string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	f.envs = append(f.envs, append([]string(nil), env...))
	pkg := argv[len(argv)-1]
	return []byte("FAKE-BINARY-" + pkg), nil
}

func newFakeTool(cfg config, repoRoot string, fb *fakeBuilder) *tool {
	if cfg.artifact == "" {
		cfg.artifact = "release"
	}
	return &tool{
		cfg:              cfg,
		repoRoot:         repoRoot,
		build:            fb.build,
		packageArtifacts: writeArtifacts,
		publish:          os.Rename,
		runtimeVersion:   func() string { return requiredGoVersion },
		probeGoVersion:   func(string) (string, error) { return requiredGoVersion, nil },
	}
}

func buildOutput(t *testing.T, version, buildID string) (string, *fakeBuilder) {
	return buildArtifactOutput(t, version, buildID, "release")
}

func buildArtifactOutput(t *testing.T, version, buildID, artifact string) (string, *fakeBuilder) {
	t.Helper()
	root := setupRepoRoot(t)
	fb := &fakeBuilder{}
	out := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: version, buildID: buildID, output: out, goBin: "go", artifact: artifact}
	if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out, fb
}

func portableName(version string) string {
	return artifactFileName(version, "release")
}

func fieldValidationName(version string) string {
	return artifactFileName(version, "field-validation")
}

// 1. flag, ProductVersion and BuildID accept/reject matrix.

func TestParseFlags(t *testing.T) {
	t.Run("valid required", func(t *testing.T) {
		c, err := parseFlags([]string{"--version", "1.2.3", "--build-id", "abc", "--output", "out"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if c.version != "1.2.3" || c.buildID != "abc" || c.output != "out" || c.goBin != "go" || c.artifact != "release" || c.help {
			t.Fatalf("bad config: %+v", c)
		}
	})
	t.Run("field validation artifact", func(t *testing.T) {
		c, err := parseFlags([]string{"--version=1.0.0", "--build-id=x", "--output=o", "--artifact=field-validation"})
		if err != nil || c.artifact != "field-validation" {
			t.Fatalf("artifact=%q err=%v", c.artifact, err)
		}
	})
	t.Run("equal form and custom go", func(t *testing.T) {
		c, err := parseFlags([]string{"--version=1.0.0", "--build-id=x", "--output=o", "--go=/p/go"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if c.goBin != "/p/go" || c.version != "1.0.0" {
			t.Fatalf("bad config: %+v", c)
		}
	})
	t.Run("help", func(t *testing.T) {
		for _, args := range [][]string{{"--help"}, {"-h"}} {
			c, err := parseFlags(args)
			if err != nil || !c.help {
				t.Fatalf("args=%v: help=%v err=%v", args, c.help, err)
			}
		}
	})
	t.Run("help must be sole argument", func(t *testing.T) {
		for _, args := range [][]string{
			{"--help", "unexpected"},
			{"unexpected", "--help"},
			{"--version", "1.0.0", "--help"},
			{"--help", "--version", "1.0.0"},
			{"-h", "--output", "out"},
			{"--help", "--help"},
		} {
			if _, err := parseFlags(args); err == nil {
				t.Fatalf("args=%v: expected error, got nil", args)
			}
		}
	})
	for name, args := range map[string][]string{
		"missing version":  {"--build-id", "x", "--output", "o"},
		"missing build-id": {"--version", "1.0.0", "--output", "o"},
		"missing output":   {"--version", "1.0.0", "--build-id", "x"},
		"empty value":      {"--version", "", "--build-id", "x", "--output", "o"},
		"repeated":         {"--version", "1.0.0", "--version", "2.0.0", "--build-id", "x", "--output", "o"},
		"unknown flag":     {"--foo", "1"},
		"positional":       {"--version", "1.0.0", "--build-id", "x", "--output", "o", "extra"},
		"missing value":    {"--version"},
		"invalid artifact": {"--version", "1.0.0", "--build-id", "x", "--output", "o", "--artifact", "installed"},
		"empty artifact":   {"--version", "1.0.0", "--build-id", "x", "--output", "o", "--artifact="},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(args); err == nil {
				t.Fatalf("expected error for %v", args)
			}
		})
	}
}

func TestValidateProductVersion(t *testing.T) {
	for _, v := range []string{
		"0.1.0", "0.1.0-alpha.2", "1.2.3", "0.0.0", "10.20.30",
		"0.1.0-alpha", "0.1.0-alpha-rc.1", "0.1.0-0", "0.1.0-1.2",
	} {
		if err := validateProductVersion(v); err != nil {
			t.Errorf("accept %q: %v", v, err)
		}
	}
	for _, v := range []string{
		"", "v0.1.0", "V0.1.0", "01.0.0", "0.01.0", "0.1.0+local", "0.1.0+build",
		"0.1.0-", "0.1.0-alpha.", "0.1", "0.1.0.0", "0.1.0-alpha.02",
		"0.1.0 alpha", "0.1.0/alpha", "0.1.0\\alpha", "0.1.0\talpha",
		"0.1.0-α",
	} {
		if err := validateProductVersion(v); err == nil {
			t.Errorf("reject %q: expected error", v)
		}
	}
}

func TestValidateBuildID(t *testing.T) {
	for _, v := range []string{
		"a", "abc", "0", "605fbcc2841525949f748fb90771887e9c0f74db",
		"A.B-C_D", strings.Repeat("a", 64),
	} {
		if err := validateBuildID(v); err != nil {
			t.Errorf("accept %q: %v", v, err)
		}
	}
	for _, v := range []string{
		"", ".abc", "-abc", "_abc", "/abc", "a b", "a$b", "a/b",
		"abc:def", strings.Repeat("a", 65), "a-α",
	} {
		if err := validateBuildID(v); err == nil {
			t.Errorf("reject %q: expected error", v)
		}
	}
}

// 2. runtime/subprocess Go version mismatch fails before build/output mutation.

func TestRuntimeVersionMismatchNoMutation(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.runtimeVersion = func() string { return "go1.25.0" }
	err := tt.execute(io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), requiredGoVersion) {
		t.Fatalf("expected version error, got %v", err)
	}
	if len(fb.calls) != 0 {
		t.Errorf("build called %d times", len(fb.calls))
	}
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output created")
	}
	entries, _ := os.ReadDir(outParent)
	if len(entries) != 0 {
		t.Errorf("parent not clean: %d entries", len(entries))
	}
}

func TestSubprocessVersionMismatchNoMutation(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.probeGoVersion = func(string) (string, error) { return "go1.25.0", nil }
	if err := tt.execute(io.Discard, io.Discard); err == nil {
		t.Fatal("expected error")
	}
	if len(fb.calls) != 0 {
		t.Errorf("build called %d times", len(fb.calls))
	}
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output created")
	}
	entries, _ := os.ReadDir(outParent)
	if len(entries) != 0 {
		t.Errorf("parent not clean: %d entries", len(entries))
	}
}

// 3. buildEnv removes conflicts case-insensitively, adds fixed vars, preserves input.

func TestBuildEnv(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "HOME=/u", "GOPATH=/g", "GOPROXY=off",
		"goos=linux", "GoArch=arm64", "Cgo_Enabled=1", "GOAMD64=v3",
		"GOEXPERIMENT=rangefunc", "GOFIPS140=on", "GOFLAGS=-x",
		"GOWORK=on", "GOENV=/e", "GOTOOLCHAIN=auto",
	}
	snapshot := strings.Join(parent, "\x00")
	result := buildEnv(parent)
	if strings.Join(parent, "\x00") != snapshot {
		t.Errorf("parent slice was modified")
	}
	// The parent's conflicting values must be gone and replaced by the fixed ones.
	for _, bad := range []string{
		"goos=linux", "GoArch=arm64", "Cgo_Enabled=1", "GOAMD64=v3",
		"GOEXPERIMENT=rangefunc", "GOFIPS140=on", "GOFLAGS=-x",
		"GOWORK=on", "GOENV=/e", "GOTOOLCHAIN=auto",
	} {
		if containsExact(result, bad) {
			t.Errorf("conflict entry %q still present", bad)
		}
	}
	fixed := map[string]string{
		"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOAMD64": "v1",
		"GOEXPERIMENT": "", "GOFIPS140": "off", "GOFLAGS": "", "GOWORK": "off",
		"GOENV": "off", "GOTOOLCHAIN": "local",
	}
	for k, v := range fixed {
		got, ok := envGet(result, k)
		if !ok || got != v {
			t.Errorf("var %s = %q (ok=%v), want %q", k, got, ok, v)
		}
	}
	for _, preserved := range []string{"PATH=/usr/bin", "HOME=/u", "GOPATH=/g", "GOPROXY=off"} {
		if !containsExact(result, preserved) {
			t.Errorf("preserved var %q missing from result", preserved)
		}
	}
}

func envGet(env []string, key string) (string, bool) {
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func containsExact(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// 4. go build argv has correct target, readonly module, trimpath, VCS, ldflags, output.

func TestBuildArgv(t *testing.T) {
	cli := buildArgv("go", "/p/sidravia.exe", "./cmd/sidravia", "0.1.0-alpha.2", "abc", false)
	wantCLI := []string{
		"go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w -buildid=", "-o", "/p/sidravia.exe", "./cmd/sidravia",
	}
	if !sliceEqual(cli, wantCLI) {
		t.Errorf("cli argv = %v, want %v", cli, wantCLI)
	}

	daemon := buildArgv("/x/go", "/p/sidraviad.exe", "./cmd/sidraviad", "0.1.0-alpha.2", "abc", true)
	wantDaemonLdflags := "-s -w -buildid= -X main.ProductVersion=0.1.0-alpha.2 -X main.BuildID=abc"
	if daemon[0] != "/x/go" || daemon[1] != "build" {
		t.Errorf("daemon head = %v", daemon[:2])
	}
	if !sliceEqual(daemon[2:6], []string{"-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags"}) {
		t.Errorf("daemon flags = %v", daemon[2:6])
	}
	if daemon[6] != wantDaemonLdflags {
		t.Errorf("daemon ldflags = %q, want %q", daemon[6], wantDaemonLdflags)
	}
	if daemon[7] != "-o" || daemon[8] != "/p/sidraviad.exe" {
		t.Errorf("daemon output = %v", daemon[7:9])
	}
	if daemon[9] != "./cmd/sidraviad" {
		t.Errorf("daemon target = %q", daemon[9])
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 5. each artifact builds exactly one shared pair of product binaries.

func TestFakeBuilderTwoCallsReuse(t *testing.T) {
	for _, artifact := range []string{"release", "field-validation"} {
		out, fb := buildArtifactOutput(t, "0.1.0-alpha.2", "buildid123", artifact)
		if len(fb.calls) != 2 {
			t.Fatalf("%s: build calls = %d, want 2", artifact, len(fb.calls))
		}
		if fb.calls[0][len(fb.calls[0])-1] != "./cmd/sidravia" {
			t.Errorf("%s: call 0 target = %q", artifact, fb.calls[0][len(fb.calls[0])-1])
		}
		if fb.calls[1][len(fb.calls[1])-1] != "./cmd/sidraviad" {
			t.Errorf("%s: call 1 target = %q", artifact, fb.calls[1][len(fb.calls[1])-1])
		}
		contents := readZipContents(t, filepath.Join(out, artifactFileName("0.1.0-alpha.2", artifact)))
		if !bytes.Equal(contents["sidravia.exe"], []byte("FAKE-BINARY-./cmd/sidravia")) {
			t.Errorf("%s: sidravia.exe does not match fake output", artifact)
		}
		if !bytes.Equal(contents["sidraviad.exe"], []byte("FAKE-BINARY-./cmd/sidraviad")) {
			t.Errorf("%s: sidraviad.exe does not match fake output", artifact)
		}
	}
}

// 6. exact Release/validation manifests, metadata and content separation.

func TestZipManifest(t *testing.T) {
	version := "0.1.0-alpha.2"
	buildID := "buildid123"
	cliBytes := []byte("FAKE-BINARY-./cmd/sidravia")
	daemonBytes := []byte("FAKE-BINARY-./cmd/sidraviad")
	baseExpected := []string{
		"BUILD-INFO.txt", "GETTING-STARTED.md", "LICENSE",
		"SHA256SUMS", "institution-profiles/jlu.json", "scripts/install.ps1",
		"scripts/uninstall.ps1", "sidravia.exe", "sidravia.portable", "sidraviad.exe",
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
			t.Errorf("%s: GETTING-STARTED.md must come from the dedicated package asset", tc.artifact)
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
		wantInternal := sha256Hex(cliBytes) + "  sidravia.exe\n" + sha256Hex(daemonBytes) + "  sidraviad.exe\n"
		if string(contents["SHA256SUMS"]) != wantInternal {
			t.Errorf("%s: internal sums = %q, want %q", tc.artifact, contents["SHA256SUMS"], wantInternal)
		}
		if len(contents["sidravia.portable"]) != 0 {
			t.Errorf("%s: marker not empty", tc.artifact)
		}
	}
}

func readZipContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %q: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read entry %q: %v", f.Name, err)
		}
		out[f.Name] = b
	}
	return out
}

func sortedEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSorted := append([]string{}, got...)
	wantSorted := append([]string{}, want...)
	sortStrings(gotSorted)
	sortStrings(wantSorted)
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			return false
		}
	}
	return true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// 7. same fixture repeated packaging is byte-identical.

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

// 8. existing output, non-dir parent, build/zip/publish failure never overwrite or leave target.

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
	if err == nil || !strings.Contains(err.Error(), "jlu.json") {
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

// 8b. release scripts without a UTF-8 BOM are rejected before any build.
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

// 8c. shipped script bytes in the zip equal the on-disk source bytes exactly.
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
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output left behind")
	}
	entries, _ := os.ReadDir(outParent)
	if len(entries) != 0 {
		t.Errorf("parent not clean: %d entries", len(entries))
	}
}

func TestZipFailureCleans(t *testing.T) {
	root := setupRepoRoot(t)
	outParent := t.TempDir()
	out := filepath.Join(outParent, "dist")
	fb := &fakeBuilder{}
	tt := newFakeTool(config{version: "0.1.0", buildID: "abc", output: out, goBin: "go"}, root, fb)
	tt.packageArtifacts = func(string, string, string, string, string, []byte, []byte) error { return errZip }
	err := tt.execute(io.Discard, io.Discard)
	if !errors.Is(err, errZip) {
		t.Fatalf("expected errZip, got %v", err)
	}
	if len(fb.calls) != 2 {
		t.Errorf("build calls = %d, want 2", len(fb.calls))
	}
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output left behind")
	}
	entries, _ := os.ReadDir(outParent)
	if len(entries) != 0 {
		t.Errorf("parent not clean: %d entries", len(entries))
	}
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
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Errorf("output left behind")
	}
	entries, _ := os.ReadDir(outParent)
	if len(entries) != 0 {
		t.Errorf("parent not clean: %d entries", len(entries))
	}
}

// 9. external sums contain exactly the selected zip and output has two files.

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
	portableBytes, _ := os.ReadFile(filepath.Join(out, portableName(version)))
	wantLines := []string{
		sha256Hex(portableBytes) + "  " + portableName(version),
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("sums line %d = %q, want %q", i, lines[i], want)
		}
	}
	// Output directory has exactly the selected zip and sums file.
	listing, _ := os.ReadDir(out)
	if len(listing) != 2 {
		t.Errorf("output entries = %d, want 2", len(listing))
	}
}

// 10. errors preserve observable filesystem or command cause.

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

func TestErrorCauseCommand(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skipf("go binary not found at %s: %v", goBin, err)
	}
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "out.exe")
	argv := []string{goBin, "build", "-o", outFile, "./nonexistent-sidravia-pkg"}
	_, err := realBuildFunc(tmp)(nil, argv)
	if err == nil {
		t.Fatal("expected build error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("expected *exec.ExitError cause, got %T: %v", err, err)
	}
}

func TestHelpPrintsAndExits(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"--help"}, &stdout, io.Discard)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "--version") || !strings.Contains(stdout.String(), "go1.26.4") {
		t.Errorf("help text missing content: %q", stdout.String())
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	code := run([]string{"--version", "v0.1.0", "--build-id", "x", "--output", "o"}, io.Discard, io.Discard)
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid version")
	}
}

func TestRunRejectsMixedHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"--help", "unexpected"}, &stdout, io.Discard)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}
