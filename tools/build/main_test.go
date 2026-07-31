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
	writeTestFile(t, filepath.Join(root, "README.md"), "# Sidravia readme\n")
	writeTestFile(t, filepath.Join(root, "LICENSE"), "LICENSE TEXT\n")
	writeTestFile(t, filepath.Join(root, "docs", "getting-started-windows.md"), "# Getting started\n")
	writeTestFile(t, filepath.Join(root, "internal", "daemon", "configuration", "profiles", "jlu.json"), testProfileJSON)
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
	t.Helper()
	root := setupRepoRoot(t)
	fb := &fakeBuilder{}
	out := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: version, buildID: buildID, output: out, goBin: "go"}
	if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out, fb
}

func normalName(version string) string {
	return "sidravia-v" + version + "-windows-amd64.zip"
}

func portableName(version string) string {
	return "sidravia-v" + version + "-windows-amd64-portable.zip"
}

// 1. flag, ProductVersion and BuildID accept/reject matrix.

func TestParseFlags(t *testing.T) {
	t.Run("valid required", func(t *testing.T) {
		c, err := parseFlags([]string{"--version", "1.2.3", "--build-id", "abc", "--output", "out"})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if c.version != "1.2.3" || c.buildID != "abc" || c.output != "out" || c.goBin != "go" || c.help {
			t.Fatalf("bad config: %+v", c)
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

// 5. fake builder receives exactly two build calls; both packages reuse same bytes.

func TestFakeBuilderTwoCallsReuse(t *testing.T) {
	out, fb := buildOutput(t, "0.1.0-alpha.2", "buildid123")
	if len(fb.calls) != 2 {
		t.Fatalf("build calls = %d, want 2", len(fb.calls))
	}
	if fb.calls[0][len(fb.calls[0])-1] != "./cmd/sidravia" {
		t.Errorf("call 0 target = %q", fb.calls[0][len(fb.calls[0])-1])
	}
	if fb.calls[1][len(fb.calls[1])-1] != "./cmd/sidraviad" {
		t.Errorf("call 1 target = %q", fb.calls[1][len(fb.calls[1])-1])
	}
	cliBytes := []byte("FAKE-BINARY-./cmd/sidravia")
	daemonBytes := []byte("FAKE-BINARY-./cmd/sidraviad")
	normal := readZipContents(t, filepath.Join(out, normalName("0.1.0-alpha.2")))
	portable := readZipContents(t, filepath.Join(out, portableName("0.1.0-alpha.2")))
	if !bytes.Equal(normal["sidravia.exe"], cliBytes) || !bytes.Equal(portable["sidravia.exe"], cliBytes) {
		t.Errorf("sidravia.exe bytes differ or do not match fake output")
	}
	if !bytes.Equal(normal["sidraviad.exe"], daemonBytes) || !bytes.Equal(portable["sidraviad.exe"], daemonBytes) {
		t.Errorf("sidraviad.exe bytes differ or do not match fake output")
	}
	if !bytes.Equal(normal["sidravia.exe"], portable["sidravia.exe"]) {
		t.Errorf("sidravia.exe differs between packages")
	}
}

// 6. zip manifest, sort, timestamp, mode, BUILD-INFO, internal sums and marker diff.

func TestZipManifest(t *testing.T) {
	version := "0.1.0-alpha.2"
	buildID := "buildid123"
	out, _ := buildOutput(t, version, buildID)
	cliBytes := []byte("FAKE-BINARY-./cmd/sidravia")
	daemonBytes := []byte("FAKE-BINARY-./cmd/sidraviad")
	baseExpected := []string{
		"BUILD-INFO.txt", "GETTING-STARTED.md", "LICENSE", "README.md",
		"SHA256SUMS", "sidravia.exe", "sidraviad.exe",
	}

	for _, tc := range []struct {
		name string
		mode string
		file string
	}{
		{"normal", "installed", normalName(version)},
		{"portable", "portable", portableName(version)},
	} {
		path := filepath.Join(out, tc.file)
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("%s: open zip: %v", tc.name, err)
		}
		var names []string
		contents := map[string][]byte{}
		for _, f := range zr.File {
			names = append(names, f.Name)
			if f.Name == "sidravia.portable" {
				if tc.mode != "portable" {
					t.Errorf("%s: unexpected marker", tc.name)
				}
			}
			if strings.Contains(f.Name, "\\") || strings.Contains(f.Name, "..") {
				t.Errorf("%s: bad entry name %q", tc.name, f.Name)
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("%s: open entry %q: %v", tc.name, f.Name, err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("%s: read entry %q: %v", tc.name, f.Name, err)
			}
			contents[f.Name] = b
			if !f.Modified.Equal(epoch) {
				t.Errorf("%s: entry %q time = %v, want %v", tc.name, f.Name, f.Modified, epoch)
			}
			if f.Comment != "" {
				t.Errorf("%s: entry %q has comment", tc.name, f.Name)
			}
			if f.Method != zip.Deflate {
				t.Errorf("%s: entry %q method = %d, want Deflate", tc.name, f.Name, f.Method)
			}
			mode := (f.ExternalAttrs >> 16) & 0o777
			wantMode := uint32(0o644)
			if strings.HasSuffix(f.Name, ".exe") {
				wantMode = 0o755
			}
			if mode != wantMode {
				t.Errorf("%s: entry %q mode = %o, want %o", tc.name, f.Name, mode, wantMode)
			}
		}
		if zr.Comment != "" {
			t.Errorf("%s: archive has comment", tc.name)
		}
		zr.Close()

		expected := append([]string{}, baseExpected...)
		expected = append(expected, "institution-profiles/jlu.json")
		if tc.mode == "portable" {
			expected = append(expected, "sidravia.portable")
		}
		if !sortedEqual(names, expected) {
			t.Errorf("%s: names = %v, want sorted %v", tc.name, names, expected)
		}
		wantInfo := fmt.Sprintf("ProductVersion: %s\nBuildID: %s\nTarget: windows/amd64\nLayoutMode: %s\n", version, buildID, tc.mode)
		if string(contents["BUILD-INFO.txt"]) != wantInfo {
			t.Errorf("%s: BUILD-INFO = %q, want %q", tc.name, contents["BUILD-INFO.txt"], wantInfo)
		}
		if string(contents["README.md"]) != "# Sidravia readme\n" {
			t.Errorf("%s: README.md wrong", tc.name)
		}
		if string(contents["LICENSE"]) != "LICENSE TEXT\n" {
			t.Errorf("%s: LICENSE wrong", tc.name)
		}
		if string(contents["GETTING-STARTED.md"]) != "# Getting started\n" {
			t.Errorf("%s: GETTING-STARTED.md wrong", tc.name)
		}
		if string(contents["institution-profiles/jlu.json"]) != testProfileJSON {
			t.Errorf("%s: profile wrong", tc.name)
		}
		wantInternal := sha256Hex(cliBytes) + "  sidravia.exe\n" + sha256Hex(daemonBytes) + "  sidraviad.exe\n"
		if string(contents["SHA256SUMS"]) != wantInternal {
			t.Errorf("%s: internal sums = %q, want %q", tc.name, contents["SHA256SUMS"], wantInternal)
		}
		if tc.mode == "portable" {
			if len(contents["sidravia.portable"]) != 0 {
				t.Errorf("%s: marker not empty", tc.name)
			}
		} else {
			if _, ok := contents["sidravia.portable"]; ok {
				t.Errorf("%s: marker present in normal package", tc.name)
			}
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
	out1, _ := buildOutput(t, version, buildID)
	out2, _ := buildOutput(t, version, buildID)
	for _, name := range []string{normalName(version), portableName(version), "SHA256SUMS.txt"} {
		b1, err := os.ReadFile(filepath.Join(out1, name))
		if err != nil {
			t.Fatalf("read out1/%s: %v", name, err)
		}
		b2, err := os.ReadFile(filepath.Join(out2, name))
		if err != nil {
			t.Fatalf("read out2/%s: %v", name, err)
		}
		if !bytes.Equal(b1, b2) {
			t.Errorf("%s differs between repeated builds", name)
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
	tt.packageArtifacts = func(string, string, string, string, []byte, []byte) error { return errZip }
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

// 9. external sums contain exactly the two zips, correct content, stable order.

func TestExternalSums(t *testing.T) {
	version := "0.1.0-alpha.2"
	out, _ := buildOutput(t, version, "buildid123")
	sumsBytes, err := os.ReadFile(filepath.Join(out, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatalf("read sums: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(sumsBytes), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("sums lines = %d, want 2", len(lines))
	}
	// Lexicographic order: portable name before normal name.
	portableBytes, _ := os.ReadFile(filepath.Join(out, portableName(version)))
	normalBytes, _ := os.ReadFile(filepath.Join(out, normalName(version)))
	wantLines := []string{
		sha256Hex(portableBytes) + "  " + portableName(version),
		sha256Hex(normalBytes) + "  " + normalName(version),
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("sums line %d = %q, want %q", i, lines[i], want)
		}
	}
	// Output directory has exactly the three files.
	listing, _ := os.ReadDir(out)
	if len(listing) != 3 {
		t.Errorf("output entries = %d, want 3", len(listing))
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
