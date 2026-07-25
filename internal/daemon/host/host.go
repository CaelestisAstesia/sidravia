package host

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
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

// DefaultRuntimeInfoPath returns the default path for the runtime info file.
func DefaultRuntimeInfoPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Sidravia", "runtime.json"), nil
}
