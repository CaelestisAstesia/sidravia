//go:build linux

package host

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
)

func validLinuxHostConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		ProductVersion:  "0.1.0-test",
		BuildID:         "test-build",
		Token:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RuntimeInfoPath: filepath.Join(t.TempDir(), "runtime", "runtime.json"),
		Handler:         http.NewServeMux(),
	}
}

func waitForRuntimeInfo(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runtime info %q never appeared", path)
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	return info.Mode().Perm()
}

// TestLinuxHostRejectsInvalidConfig proves input rejection happens before any
// filesystem or listener mutation: no lock file is created for any rejection.
func TestLinuxHostRejectsInvalidConfig(t *testing.T) {
	base := validLinuxHostConfig(t)

	cases := []struct {
		name      string
		mutate    func(Config) Config
		nilCtx    bool
		wantLabel string
	}{
		{"nil context", func(c Config) Config { return c }, true, "host: nil context"},
		{"empty runtime path", func(c Config) Config { c.RuntimeInfoPath = ""; return c }, false, "runtime info path"},
		{"relative runtime path", func(c Config) Config { c.RuntimeInfoPath = "runtime/runtime.json"; return c }, false, "runtime info path"},
		{"unclean runtime path", func(c Config) Config { c.RuntimeInfoPath = c.RuntimeInfoPath + "/."; return c }, false, "runtime info path"},
		{"nil handler", func(c Config) Config { c.Handler = nil; return c }, false, "nil handler"},
		{"empty token", func(c Config) Config { c.Token = ""; return c }, false, "empty token"},
		{"empty product version", func(c Config) Config { c.ProductVersion = ""; return c }, false, "empty product version"},
		{"empty build id", func(c Config) Config { c.BuildID = ""; return c }, false, "empty build ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.mutate(base)
			var err error
			if tc.nilCtx {
				err = Run(nil, cfg)
			} else {
				err = Run(context.Background(), cfg)
			}
			if err == nil {
				t.Fatal("Run = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tc.wantLabel) {
				t.Errorf("err = %v, want label %q", err, tc.wantLabel)
			}
			if cfg.RuntimeInfoPath != "" {
				if _, statErr := os.Stat(cfg.RuntimeInfoPath + ".lock"); statErr == nil {
					t.Error("lock file created before mutation was rejected")
				}
			}
		})
	}
}

// TestLinuxHostPublishesLoopbackRuntimeAndModes proves the endpoint is
// loopback-only, the runtime record carries the right PID/version/build ID, and
// the runtime file, lock file and runtime directory use owner-only modes.
func TestLinuxHostPublishesLoopbackRuntimeAndModes(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()

	waitForRuntimeInfo(t, cfg.RuntimeInfoPath)

	data, err := os.ReadFile(cfg.RuntimeInfoPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := contract.DecodeRuntimeInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Endpoint, "ws://127.0.0.1:") || !strings.HasSuffix(info.Endpoint, "/ipc") {
		t.Errorf("Endpoint = %q, want 127.0.0.1 loopback ipc", info.Endpoint)
	}
	if info.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if info.Token != cfg.Token {
		t.Errorf("Token = %q, want %q", info.Token, cfg.Token)
	}
	if info.ProductVersion != cfg.ProductVersion {
		t.Errorf("ProductVersion = %q, want %q", info.ProductVersion, cfg.ProductVersion)
	}
	if info.BuildID != cfg.BuildID {
		t.Errorf("BuildID = %q, want %q", info.BuildID, cfg.BuildID)
	}

	if mode := filePerm(t, cfg.RuntimeInfoPath); mode != 0o600 {
		t.Errorf("runtime info mode = %o, want 600", mode)
	}
	if mode := filePerm(t, cfg.RuntimeInfoPath+".lock"); mode != 0o600 {
		t.Errorf("lock mode = %o, want 600", mode)
	}
	if mode := filePerm(t, filepath.Dir(cfg.RuntimeInfoPath)); mode != 0o700 {
		t.Errorf("runtime dir mode = %o, want 700", mode)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// TestLinuxHostRejectsConcurrentRun proves a second Run against the same
// runtime path is rejected while the first remains reachable.
func TestLinuxHostRejectsConcurrentRun(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()
	waitForRuntimeInfo(t, cfg.RuntimeInfoPath)

	second := Run(context.Background(), cfg)
	if second == nil || !strings.Contains(second.Error(), "already running") {
		t.Errorf("second Run = %v, want already running", second)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("first Run = %v, want nil", err)
	}
}

// TestLinuxHostCancellationCleansRuntime proves cancellation performs shutdown
// and removes runtime info when the stored PID and token still match, while the
// lock file is retained.
func TestLinuxHostCancellationCleansRuntime(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()
	waitForRuntimeInfo(t, cfg.RuntimeInfoPath)

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}

	if _, err := os.Stat(cfg.RuntimeInfoPath); !os.IsNotExist(err) {
		t.Errorf("runtime info still exists after cancellation: %v", err)
	}
	if _, err := os.Stat(cfg.RuntimeInfoPath + ".lock"); err != nil {
		t.Errorf("lock file removed after exit: %v", err)
	}
}

// TestLinuxHostRetainsReplacementRuntimeInfo proves runtime info replaced with a
// different token and PID is retained when the original host exits.
func TestLinuxHostRetainsReplacementRuntimeInfo(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()
	waitForRuntimeInfo(t, cfg.RuntimeInfoPath)

	replacement := contract.RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:9/ipc",
		PID:            os.Getpid() + 1,
		Token:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ProductVersion: cfg.ProductVersion,
		BuildID:        cfg.BuildID,
	}
	data, err := contract.EncodeRuntimeInfo(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.RuntimeInfoPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}

	stored, err := os.ReadFile(cfg.RuntimeInfoPath)
	if err != nil {
		t.Fatalf("runtime info removed despite token mismatch: %v", err)
	}
	kept, err := contract.DecodeRuntimeInfo(stored)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Token != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("retained token = %q, want replacement token", kept.Token)
	}
}

func TestLinuxHostLockCauseReachable(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	cause := errors.New("injected lock failure")
	deps := defaultLinuxHostDeps()
	deps.acquireLock = func(string) (int, error) { return -1, cause }
	err := runLinuxHost(context.Background(), cfg, deps)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want lock cause", err)
	}
}

func TestLinuxHostStorageCauseReachable(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	cause := errors.New("injected store failure")
	deps := defaultLinuxHostDeps()
	deps.newStore = func(jsonfile.SecureStoreOptions) (*jsonfile.SecureStore, error) { return nil, cause }
	err := runLinuxHost(context.Background(), cfg, deps)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want store cause", err)
	}
	if !strings.Contains(err.Error(), "host: secure store") {
		t.Errorf("error = %v, want secure store label", err)
	}
}

func TestLinuxHostListenerCauseReachable(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	cause := errors.New("injected listen failure")
	deps := defaultLinuxHostDeps()
	deps.listen = func(string, string) (net.Listener, error) { return nil, cause }
	err := runLinuxHost(context.Background(), cfg, deps)
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want listen cause", err)
	}
	if !strings.Contains(err.Error(), "host: listen") {
		t.Errorf("error = %v, want listen label", err)
	}
}

func TestLinuxHostShutdownCauseReachable(t *testing.T) {
	cfg := validLinuxHostConfig(t)
	cause := errors.New("injected shutdown failure")
	deps := defaultLinuxHostDeps()
	deps.shutdown = func(*http.Server) error { return cause }
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runLinuxHost(ctx, cfg, deps) }()
	waitForRuntimeInfo(t, cfg.RuntimeInfoPath)

	cancel()
	err := <-errCh
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want shutdown cause", err)
	}
}
