package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"sidravia/internal/update/releases"
)

var ErrIntegrity = errors.New("update package integrity check failed")

// Download stores a verified artifact in a new private child of an already
// secured cache root. It never executes a file. Publisher trust is independent.
func Download(ctx context.Context, client *http.Client, c releases.Candidate, cacheRoot string) (string, error) {
	if err := releases.ValidateCandidate(c); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Lstat(cacheRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", releases.ErrMetadata
	}
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 10 * time.Minute
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !releases.DownloadHost(req.URL) {
			return releases.ErrMetadata
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Sidravia-Updater")
	response, err := copyClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("package source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != c.Size {
		return "", ErrIntegrity
	}
	directory, err := os.MkdirTemp(cacheRoot, "package-")
	if err != nil {
		return "", err
	}
	// These are the only leaves this function creates; never recursively delete
	// unrecognized content, even in the event of a concurrent local modification.
	partial := filepath.Join(directory, c.Name+".partial")
	final := filepath.Join(directory, c.Name)
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(partial)
			_ = os.Remove(directory)
		}
	}()
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(response.Body, c.Size+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(copyErr, syncErr, closeErr); err != nil {
		return "", err
	}
	if size != c.Size || hex.EncodeToString(hash.Sum(nil)) != c.SHA256 {
		return "", ErrIntegrity
	}
	// A fully buffered response can reach EOF despite caller cancellation.
	// Publication is the commit point; before it, cancellation removes the
	// task-owned partial bytes rather than reporting a completed download.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(partial, final); err != nil {
		return "", err
	}
	complete = true
	return final, nil
}
