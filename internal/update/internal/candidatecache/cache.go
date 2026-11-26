package candidatecache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/release"
	"sidravia/internal/update/internal/github"
)

// Cache records only authorize a single leaf in a private package directory.
// Unexpected paths, links or changed bytes require explicit cache repair.
func Inspect(root, path string, candidate *release.Candidate) (bool, error) {
	if candidate == nil || path == "" {
		return false, release.ErrMetadata
	}
	directory := filepath.Dir(path)
	if filepath.Clean(path) != path || filepath.Dir(directory) != root || !strings.HasPrefix(filepath.Base(directory), "package-") || filepath.Base(path) != candidate.Name {
		return false, release.ErrMetadata
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, release.ErrMetadata
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
func removeCached(root, path string, candidate *release.Candidate) error {
	exists, err := Inspect(root, path, candidate)
	if err != nil || !exists {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	// Never remove unrecognized files inside this directory.
	return os.Remove(filepath.Dir(path))
}
func SamePackage(a, b *release.Candidate) bool {
	return a != nil && b != nil && a.ID == b.ID && a.Version == b.Version && a.Name == b.Name && a.Size == b.Size && a.SHA256 == b.SHA256 && a.DownloadURL == b.DownloadURL
}

type backoff struct {
	SchemaVersion int       `json:"schemaVersion"`
	Until         time.Time `json:"until"`
}

type State struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Selection     release.Selection   `json:"selection"`
	CheckedAt     time.Time           `json:"checkedAt"`
	Check         release.CheckResult `json:"check"`
	Downloaded    string              `json:"downloaded,omitempty"`
	Retired       *retiredPackage     `json:"retired,omitempty"`
}

type retiredPackage struct {
	Path      string             `json:"path"`
	Candidate *release.Candidate `json:"candidate"`
}
