// Package release owns local release metadata and publisher trust without network I/O.
package release

import (
	"errors"
	"strings"
)

// Repository is the single upstream identity used by metadata and transport.
const Repository = "CaelestisAstesia/sidravia"

var (
	ErrMetadata    = errors.New("invalid update release metadata")
	ErrUnsupported = errors.New("update target unsupported")
)

type Selection struct {
	OS             string
	Arch           string
	Kind           string
	Channel        string
	CurrentVersion string
}
type Candidate struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"downloadUrl"`
	ReleaseURL  string `json:"releaseUrl"`
	Notes       string `json:"notes"`

	// The transport digest alone never authorizes installation.
	CanApply    bool   `json:"canApply"`
	ApplyReason string `json:"applyReason"`
}

type CheckResult struct {
	State     string     `json:"state"`
	Candidate *Candidate `json:"candidate,omitempty"`

	// PublisherVerified describes this discovery only. Apply verifies again.
	PublisherVerified bool `json:"-"`
}

func DesktopArchitecture(arch string) bool { return arch == "amd64" || arch == "arm64" }

// PackageSuffix defines the supported distribution names used by discovery and signed metadata.
func PackageSuffix(s Selection) (string, error) {
	if s.Channel != "stable" && s.Channel != "preview" {
		return "", ErrUnsupported
	}
	if !DesktopArchitecture(s.Arch) {
		return "", ErrUnsupported
	}
	switch s.Kind {
	case "setup":
		if s.OS == "windows" {
			return "setup.exe", nil
		}
	case "gui-portable":
		if s.OS == "windows" {
			return "gui-portable.zip", nil
		}
		if s.OS == "linux" {
			return "gui-portable.tar.gz", nil
		}
	case "core-portable":
		if s.OS == "windows" {
			return "core-portable.zip", nil
		}
		if s.OS == "linux" {
			return "core-portable.tar.gz", nil
		}
	}
	return "", ErrUnsupported
}

// PackageSuffixes orders compatible encodings for new clients; gzip remains supported.
func PackageSuffixes(s Selection) ([]string, error) {
	legacy, err := PackageSuffix(s)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(legacy, ".tar.gz") {
		return []string{strings.TrimSuffix(legacy, ".gz") + ".xz", legacy}, nil
	}
	return []string{legacy}, nil
}

// AssetNameValid binds a supported archive encoding to the exact release target.
func AssetNameValid(version string, s Selection, name string) bool {
	suffixes, err := PackageSuffixes(s)
	if err != nil {
		return false
	}
	for _, suffix := range suffixes {
		if name == "sidravia-v"+version+"-"+s.OS+"-"+s.Arch+"-"+suffix {
			return true
		}
	}
	return false
}
