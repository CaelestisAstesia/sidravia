//go:build windows

package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows"

	"sidravia/internal/document/securefile"
)

// Run starts the daemon host on Windows.
func Run(ctx context.Context, cfg Config) error {
	token := windows.GetCurrentProcessToken()
	defer token.Close()

	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("host: get token user: %w", err)
	}
	sid := tokenUser.User.Sid.String()

	// Create a namespace-owned mutex for single-instance enforcement.
	mutexName := daemonMutexName(sid, cfg.Namespace)
	mutexNameUTF16, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return fmt.Errorf("host: mutex name: %w", err)
	}
	mutex, err := windows.CreateMutex(nil, false, mutexNameUTF16)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return ErrAlreadyRunning
	}
	if err != nil {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return fmt.Errorf("host: create mutex: %w", err)
	}
	defer windows.CloseHandle(mutex)

	// Write runtime info.
	store, err := securefile.NewSecureStore(securefile.SecureStoreOptions{
		IntendedOwnerSID:                   sid,
		AllowUnsupportedProtectionFallback: cfg.AllowUnsupportedProtectionFallback,
		OnUnprotected:                      cfg.OnUnprotected,
		OnPostCommitDiagnostic:             cfg.OnPostCommitDiagnostic,
	})
	if err != nil {
		return fmt.Errorf("host: secure store: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("host: listen: %w", err)
	}
	defer listener.Close()

	info, err := publishRuntimeInfo(ctx, cfg, listener, store)
	if err != nil {
		return err
	}

	// Ensure runtime info cleanup on all exit paths.
	defer cleanupRuntimeInfo(cfg.RuntimeInfoPath, info, store, os.Remove)

	if cfg.OnReady != nil {
		cfg.OnReady()
	}

	// Start HTTP server.
	server := &http.Server{
		Handler: cfg.Handler,
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	// Wait for shutdown signal.
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-sigCtx.Done():
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("host: server: %w", err)
		}
	}

	// Graceful shutdown.
	if err := shutdownHTTPServer(server.Shutdown); err != nil {
		return err
	}

	return nil
}
