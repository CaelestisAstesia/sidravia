//go:build linux

package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
)

// Run starts the daemon host on Linux. It validates its inputs, acquires a
// same-user single-instance flock for the whole host lifetime, listens only on
// 127.0.0.1, publishes the runtime record through the secure store, serves the
// supplied handler, and on stop performs the bounded graceful HTTP shutdown and
// removes runtime info only when the stored PID and token still match.
func Run(ctx context.Context, cfg Config) error {
	return runLinuxHost(ctx, cfg, defaultLinuxHostDeps())
}

// linuxHostDeps bundles the platform operations runLinuxHost depends on, so
// tests can inject private replacements for listener, store, signal or lock
// behavior. It is not an exported seam.
type linuxHostDeps struct {
	acquireLock func(runtimeInfoPath string) (int, error)
	releaseLock func(int) error
	newStore    func(jsonfile.SecureStoreOptions) (*jsonfile.SecureStore, error)
	listen      func(network, address string) (net.Listener, error)
	shutdown    func(server *http.Server) error
}

func defaultLinuxHostDeps() linuxHostDeps {
	return linuxHostDeps{
		acquireLock: acquireLinuxLock,
		releaseLock: func(fd int) error { return unix.Close(fd) },
		newStore:    jsonfile.NewSecureStore,
		listen:      net.Listen,
		shutdown: func(server *http.Server) error {
			return shutdownHTTPServer(server.Shutdown)
		},
	}
}

func runLinuxHost(ctx context.Context, cfg Config, deps linuxHostDeps) error {
	if err := validateLinuxConfig(ctx, cfg); err != nil {
		return err
	}

	lockFD, err := deps.acquireLock(cfg.RuntimeInfoPath)
	if err != nil {
		return err
	}
	defer deps.releaseLock(lockFD)

	store, err := deps.newStore(jsonfile.SecureStoreOptions{
		IntendedOwnerSID:                   "",
		AllowUnsupportedProtectionFallback: cfg.AllowUnsupportedProtectionFallback,
		OnUnprotected:                      cfg.OnUnprotected,
	})
	if err != nil {
		return fmt.Errorf("host: secure store: %w", err)
	}

	listener, err := deps.listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("host: listen: %w", err)
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return fmt.Errorf("host: parse listener address: %w", err)
	}

	info := contract.RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:" + portStr + "/ipc",
		PID:            os.Getpid(),
		Token:          cfg.Token,
		ProductVersion: cfg.ProductVersion,
		BuildID:        cfg.BuildID,
	}
	infoData, err := contract.EncodeRuntimeInfo(info)
	if err != nil {
		return fmt.Errorf("host: encode runtime info: %w", err)
	}
	if err := store.Replace(ctx, cfg.RuntimeInfoPath, infoData); err != nil {
		return fmt.Errorf("host: write runtime info: %w", err)
	}

	defer cleanupRuntimeInfo(cfg.RuntimeInfoPath, info, store, os.Remove)

	server := &http.Server{Handler: cfg.Handler}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-sigCtx.Done():
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("host: server: %w", err)
		}
	}

	if err := deps.shutdown(server); err != nil {
		return err
	}
	return nil
}

func validateLinuxConfig(ctx context.Context, cfg Config) error {
	if ctx == nil {
		return errors.New("host: nil context")
	}
	if cfg.RuntimeInfoPath == "" || !filepath.IsAbs(cfg.RuntimeInfoPath) || filepath.Clean(cfg.RuntimeInfoPath) != cfg.RuntimeInfoPath {
		return errors.New("host: runtime info path must be absolute and clean")
	}
	if cfg.Handler == nil {
		return errors.New("host: nil handler")
	}
	if cfg.Token == "" {
		return errors.New("host: empty token")
	}
	if cfg.ProductVersion == "" {
		return errors.New("host: empty product version")
	}
	if cfg.BuildID == "" {
		return errors.New("host: empty build ID")
	}
	return nil
}

// acquireLinuxLock creates the runtime directory with mode 0700, opens the lock
// file with O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW and mode 0600, hardens it with
// Fchmod, and holds a nonblocking exclusive flock for the whole host lifetime.
// An already held lock returns a fixed "already running" error that does not
// include a path; other errors wrap and preserve their cause without printing
// token or handler data. The lock file remains as a harmless zero-length
// owner-only coordination inode after exit; it is never unlinked, so no inode
// replacement race is introduced.
func acquireLinuxLock(runtimeInfoPath string) (int, error) {
	lockPath := runtimeInfoPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return -1, fmt.Errorf("host: create runtime directory: %w", err)
	}
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return -1, fmt.Errorf("host: open lock: %w", err)
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("host: harden lock: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return -1, errors.New("sidraviad is already running in this user session")
		}
		return -1, fmt.Errorf("host: acquire lock: %w", err)
	}
	return fd, nil
}
