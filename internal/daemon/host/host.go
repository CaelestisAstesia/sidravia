package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// Config holds the parameters needed to start the daemon host.
type Config struct {
	ProductVersion  string
	BuildID         string
	Token           string
	RuntimeInfoPath string
	Handler         http.Handler
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
