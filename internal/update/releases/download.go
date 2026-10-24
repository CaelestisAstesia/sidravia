package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrIntegrity = errors.New("update package integrity check failed")

// Download stores a verified artifact in a new private child of an already
// secured cache root. It never executes a file. Publisher trust is independent.
func Download(ctx context.Context, client *http.Client, c Candidate, cacheRoot string) (string, error) {
	if err := validateCandidate(c); err != nil {
		return "", err
	}
	info, err := os.Lstat(cacheRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrMetadata
	}
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.Timeout = 10 * time.Minute
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !downloadHost(req.URL) {
			return ErrMetadata
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
	if err = os.Rename(partial, final); err != nil {
		return "", err
	}
	complete = true
	return final, nil
}
func downloadHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	switch u.Host {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return false
}
func validateCandidate(c Candidate) error {
	v, err := ParseVersion(c.Version)
	if err != nil {
		return ErrMetadata
	}
	if c.Name == "" || len(c.Name) > 200 || !strings.HasPrefix(c.Name, "sidravia-v"+v.String()+"-") {
		return ErrMetadata
	}
	for _, ch := range c.Name {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("._+-", ch)) {
			return ErrMetadata
		}
	}
	if c.Size <= 0 || c.Size > 2<<30 {
		return ErrMetadata
	}
	hash, err := hex.DecodeString(c.SHA256)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != c.SHA256 {
		return ErrMetadata
	}
	u, err := url.Parse(c.DownloadURL)
	if err != nil || !downloadHost(u) || u.Host != "github.com" || u.Path != "/"+repository+"/releases/download/v"+c.Version+"/"+c.Name || u.RawQuery != "" || u.Fragment != "" {
		return ErrMetadata
	}
	return nil
}
