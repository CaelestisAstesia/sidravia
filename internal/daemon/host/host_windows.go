//go:build windows

package host

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/ipc/contract"
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

	// Create named mutex for single-instance enforcement.
	mutexName := "Local\\Sidravia-" + sid
	mutexNameUTF16, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return fmt.Errorf("host: mutex name: %w", err)
	}
	mutex, err := windows.CreateMutex(nil, false, mutexNameUTF16)
	if err != nil {
		return fmt.Errorf("host: create mutex: %w", err)
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(mutex)
		return fmt.Errorf("sidraviad is already running in this user session")
	}
	defer windows.CloseHandle(mutex)

	// Write runtime info.
	store, err := jsonfile.NewSecureStore(jsonfile.SecureStoreOptions{
		IntendedOwnerSID: sid,
	})
	if err != nil {
		return fmt.Errorf("host: secure store: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
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

	// Ensure runtime info cleanup on all exit paths.
	defer cleanupRuntimeInfo(cfg.RuntimeInfoPath, info, store, os.Remove)

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
