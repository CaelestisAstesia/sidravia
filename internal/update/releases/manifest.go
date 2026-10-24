package releases

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
)

var ErrUntrusted = errors.New("update publisher signature not trusted")

type Manifest struct {
	SchemaVersion   int             `json:"schemaVersion"`
	Version         string          `json:"version"`
	MinimumProtocol int             `json:"minimumProtocol"`
	Assets          []ManifestAsset `json:"assets"`
}
type ManifestAsset struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// VerifyManifest authenticates the exact bytes before decoding any instructions.
// Trusted keys come from the product build, never from the release being read.
func VerifyManifest(data, signature []byte, key ed25519.PublicKey, version string) (Manifest, error) {
	if len(data) == 0 || len(data) > 256<<10 || len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, data, signature) {
		return Manifest{}, ErrUntrusted
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrMetadata
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, ErrMetadata
	}
	if manifest.SchemaVersion != 1 || manifest.MinimumProtocol < 1 || manifest.MinimumProtocol > 1 || manifest.Version != version || len(manifest.Assets) == 0 || len(manifest.Assets) > 32 {
		return Manifest{}, ErrMetadata
	}
	if _, err := ParseVersion(manifest.Version); err != nil {
		return Manifest{}, ErrMetadata
	}
	seen := map[string]bool{}
	for _, asset := range manifest.Assets {
		selection := Selection{OS: asset.OS, Arch: asset.Arch, Kind: asset.Kind, Channel: "preview"}
		suffix, err := packageSuffix(selection)
		if err != nil {
			return Manifest{}, ErrMetadata
		}
		expected := "sidravia-v" + version + "-" + asset.OS + "-" + asset.Arch + "-" + suffix
		candidate := Candidate{Version: version, Name: asset.Name, Size: asset.Size, SHA256: asset.SHA256, DownloadURL: "https://github.com/" + repository + "/releases/download/v" + version + "/" + asset.Name}
		if asset.Name != expected || validateCandidate(candidate) != nil || seen[asset.Name] {
			return Manifest{}, ErrMetadata
		}
		seen[asset.Name] = true
	}
	return manifest, nil
}

func (m Manifest) Authenticates(c Candidate) bool {
	if m.Version != c.Version {
		return false
	}
	for _, a := range m.Assets {
		if a.Name == c.Name && a.Size == c.Size && a.SHA256 == c.SHA256 {
			return true
		}
	}
	return false
}
