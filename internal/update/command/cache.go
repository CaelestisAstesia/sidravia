package command

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/update/github"
	"sidravia/internal/update/releases"
)

// Cache records only authorize a single leaf in a private package directory.
// Unexpected paths, links or changed bytes require explicit cache repair.
func inspectCached(root, path string, candidate *releases.Candidate) (bool, error) {
	if candidate == nil || path == "" {
		return false, releases.ErrMetadata
	}
	directory := filepath.Dir(path)
	if filepath.Clean(path) != path || filepath.Dir(directory) != root || !strings.HasPrefix(filepath.Base(directory), "package-") || filepath.Base(path) != candidate.Name {
		return false, releases.ErrMetadata
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, releases.ErrMetadata
	}
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != candidate.Size {
		return false, github.ErrIntegrity
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, io.LimitReader(f, candidate.Size+1))
	closeErr := f.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return false, err
	}
	if size != candidate.Size || hex.EncodeToString(hash.Sum(nil)) != candidate.SHA256 {
		return false, github.ErrIntegrity
	}
	return true, nil
}
func removeCached(root, path string, candidate *releases.Candidate) error {
	exists, err := inspectCached(root, path, candidate)
	if err != nil || !exists {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	// Never remove unrecognized files inside this directory.
	return os.Remove(filepath.Dir(path))
}
func samePackage(a, b *releases.Candidate) bool {
	return a != nil && b != nil && a.ID == b.ID && a.Version == b.Version && a.Name == b.Name && a.Size == b.Size && a.SHA256 == b.SHA256 && a.DownloadURL == b.DownloadURL
}

type backoff struct {
	SchemaVersion int       `json:"schemaVersion"`
	Until         time.Time `json:"until"`
}

type cached struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Selection     releases.Selection   `json:"selection"`
	CheckedAt     time.Time            `json:"checkedAt"`
	Check         releases.CheckResult `json:"check"`
	Downloaded    string               `json:"downloaded,omitempty"`
}
