package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

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
	assertOutputAbsentAndParentEmpty(t, out)
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
	assertOutputAbsentAndParentEmpty(t, out)
}

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

func TestBuildArgvInjectsSharedProductIdentity(t *testing.T) {
	cli := buildArgv("go", "/p/sidraviactl.exe", "./cmd/sidraviactl", "0.1.0-alpha.2", "abc")
	wantCLI := []string{
		"go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w -buildid= -X main.ProductVersion=0.1.0-alpha.2 -X main.BuildID=abc", "-o", "/p/sidraviactl.exe", "./cmd/sidraviactl",
	}
	if !slices.Equal(cli, wantCLI) {
		t.Errorf("cli argv = %v, want %v", cli, wantCLI)
	}

	daemon := buildArgv("/x/go", "/p/sidraviad.exe", "./cmd/sidraviad", "0.1.0-alpha.2", "abc")
	wantDaemonLdflags := "-s -w -buildid= -X main.ProductVersion=0.1.0-alpha.2 -X main.BuildID=abc"
	if daemon[0] != "/x/go" || daemon[1] != "build" {
		t.Errorf("daemon head = %v", daemon[:2])
	}
	if !slices.Equal(daemon[2:6], []string{"-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags"}) {
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
