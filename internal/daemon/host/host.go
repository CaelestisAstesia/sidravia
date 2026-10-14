package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sidravia/internal/productlayout"
)

// ErrAlreadyRunning reports singleton contention, never typed readiness.
var ErrAlreadyRunning = errors.New("sidraviad is already running in this user session")

// Config holds the parameters needed to start the daemon host.
type Config struct {
	ProductVersion                     string
	BuildID                            string
	Token                              string
	Namespace                          productlayout.Namespace
	RuntimeInfoPath                    string
	Handler                            http.Handler
	AllowUnsupportedProtectionFallback bool
	OnUnprotected                      func()
	// OnPostCommitDiagnostic follows the synchronous SecureStore observer contract.
	OnPostCommitDiagnostic func(error)
	// OnReady signals owned singleton admission and successful runtime publication.
	OnReady func()
}

// daemonMutexName returns the Windows singleton name for one user and one
// validated runtime namespace. The production namespace deliberately keeps
// the historical byte-exact name; isolated namespaces use a case-folded,
// deterministic suffix so Windows aliases share ownership.
func daemonMutexName(sid string, namespace productlayout.Namespace) string {
	name := "Local\\Sidravia-" + sid
	if value := namespace.String(); value != "" {
		name += "-ns-" + strings.ToLower(value)
	}
	return name
}

// GenerateToken creates a random 64-character lowercase hex token.
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func shutdownHTTPServer(shutdown func(context.Context) error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("host: shutdown: %w", err)
	}
	return nil
}
