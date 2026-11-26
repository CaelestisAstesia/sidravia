package release

import (
	"encoding/hex"
	"net/url"
	"strings"
)

// DownloadHost restricts artifact redirects to official GitHub hosts.
func DownloadHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	switch u.Host {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return false
}

// ValidateCandidate checks a fixed release identity and location without I/O.
func ValidateCandidate(c Candidate) error {
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
	if err != nil || !DownloadHost(u) || u.Host != "github.com" || u.Path != "/"+Repository+"/releases/download/v"+c.Version+"/"+c.Name || u.RawQuery != "" || u.Fragment != "" {
		return ErrMetadata
	}
	return nil
}
