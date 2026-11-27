package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
	testGettingStarted  = "# Sidravia package guide\n[More commands](advance.md)\n"
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
	writeTestFile(t, filepath.Join(root, "THIRD-PARTY-NOTICES.txt"), "third-party notices\n")
	writeTestFile(t, filepath.Join(root, "docs", "guide.md"), testGettingStarted)
	writeTestFile(t, filepath.Join(root, "internal", "connection", "profile", "data", "jlu.json"), testProfileJSON)
	writeTestFile(t, filepath.Join(root, "scripts", "field-test.ps1"), testFieldTestScript)
	writeTestFile(t, filepath.Join(root, "scripts", "install.ps1"), testInstallScript)
	writeTestFile(t, filepath.Join(root, "scripts", "uninstall.ps1"), testUninstallScript)
	writeTestFile(t, filepath.Join(root, "tools", "cli_smoke.ps1"), testCLISmokeScript)
	if err := os.MkdirAll(filepath.Join(root, "cmd", "sidraviactl"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/sidraviactl: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "sidraviad"), 0o755); err != nil {
		t.Fatalf("mkdir cmd/sidraviad: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "sidraviaupdater"), 0o755); err != nil {
		t.Fatal(err)
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
		publish:          publishDirectory,
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

func readZipContents(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		if _, exists := out[f.Name]; exists {
			t.Fatalf("duplicate entry %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %q: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read entry %q: %v (close: %v)", f.Name, err, closeErr)
		}
		out[f.Name] = b
	}
	return out
}

func assertOutputAbsentAndParentEmpty(t *testing.T, output string) {
	t.Helper()
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Errorf("expected output %q to be absent, stat error: %v", output, err)
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("output parent not clean: %d entries", len(entries))
	}
}
