package releases

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"sidravia/internal/daemon/persistence/jsonfile"
	"sidravia/internal/update/packagefiles"
)

const ReleaseManifestName = "sidravia-release.json"
const ReleaseSignatureName = "sidravia-release.sig"
const ReleaseManifestLimit = 256 << 10

// ReleaseManifest v2 binds executable identity and the separately authenticated
// product-file ownership index. The index is not trusted merely because it is local.
type ReleaseManifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	KeyID           string         `json:"keyId"`
	Version         string         `json:"version"`
	BuildID         string         `json:"buildId"`
	MinimumProtocol int            `json:"minimumProtocol"`
	Assets          []ReleaseAsset `json:"assets"`
}
type ReleaseAsset struct {
	ManifestAsset
	FilesSHA256 string `json:"filesSha256"`
	PayloadKind string `json:"payloadKind"`
}

// PublisherKeys owns immutable copies of build-selected trust roots. No key is
// learned from a release or from runtime environment variables.
type PublisherKeys struct{ keys map[string]ed25519.PublicKey }

func PublisherKeyID(key ed25519.PublicKey) (string, error) {
	if len(key) != ed25519.PublicKeySize {
		return "", ErrUntrusted
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:]), nil
}
func NewPublisherKeys(keys []ed25519.PublicKey) (*PublisherKeys, error) {
	result := &PublisherKeys{keys: make(map[string]ed25519.PublicKey, len(keys))}
	for _, key := range keys {
		id, err := PublisherKeyID(key)
		if err != nil {
			return nil, err
		}
		if _, found := result.keys[id]; found {
			return nil, ErrUntrusted
		}
		result.keys[id] = append(ed25519.PublicKey(nil), key...)
	}
	return result, nil
}

// PublisherTrust returns the immutable roots selected by the product build.
func PublisherTrust() *PublisherKeys {
	// Intentionally empty until a publisher key is explicitly configured in a
	// reviewed build. Synthetic keys belong only to tests.
	keys, _ := NewPublisherKeys(nil)
	return keys
}

// VerifiedRelease cannot be constructed with trusted contents outside this package.
type VerifiedRelease struct {
	manifest        ReleaseManifest
	data, signature []byte
}

// Proof returns exact signed public bytes for durable storage and later
// revalidation against the then-current publisher roots. It grants no authority.
func (v VerifiedRelease) Proof() (data, signature []byte) {
	return append([]byte(nil), v.data...), append([]byte(nil), v.signature...)
}

func (v VerifiedRelease) BuildID() string { return v.manifest.BuildID }
func (v VerifiedRelease) Asset(c Candidate) (ReleaseAsset, error) {
	if c.Version != v.manifest.Version {
		return ReleaseAsset{}, ErrUntrusted
	}
	for _, a := range v.manifest.Assets {
		if a.Name == c.Name && a.Size == c.Size && a.SHA256 == c.SHA256 {
			return a, nil
		}
	}
	return ReleaseAsset{}, ErrUntrusted
}
func (v VerifiedRelease) VerifyFiles(c Candidate, data []byte) (packagefiles.Manifest, error) {
	if len(data) == 0 || len(data) > packagefiles.SizeLimit {
		return packagefiles.Manifest{}, ErrMetadata
	}
	a, err := v.Asset(c)
	if err != nil {
		return packagefiles.Manifest{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.FilesSHA256 {
		return packagefiles.Manifest{}, ErrUntrusted
	}
	m, err := packagefiles.Decode(data)
	if err != nil || m.Version != v.manifest.Version || m.BuildID != v.manifest.BuildID || m.OS != a.OS || m.Arch != a.Arch || m.PayloadKind != a.PayloadKind {
		return packagefiles.Manifest{}, ErrMetadata
	}
	return m, nil
}

func (keys *PublisherKeys) Verify(data, signature []byte, version string) (VerifiedRelease, error) {
	if keys == nil || len(data) == 0 || len(data) > ReleaseManifestLimit || len(signature) != ed25519.SignatureSize {
		return VerifiedRelease{}, ErrUntrusted
	}
	// The untrusted key ID only selects an already pinned key; it grants nothing.
	var hint struct {
		KeyID string `json:"keyId"`
	}
	if json.Unmarshal(data, &hint) != nil {
		return VerifiedRelease{}, ErrUntrusted
	}
	key := keys.keys[hint.KeyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, data, signature) {
		return VerifiedRelease{}, ErrUntrusted
	}
	var m ReleaseManifest
	if jsonfile.DecodeStrict(data, &m) != nil || !releaseShape(data) || m.SchemaVersion != 2 || m.Version != version || m.MinimumProtocol != 1 || len(m.BuildID) == 0 || len(m.BuildID) > 64 || len(m.Assets) == 0 || len(m.Assets) > 32 {
		return VerifiedRelease{}, ErrMetadata
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return VerifiedRelease{}, ErrMetadata
	}
	for i, r := range m.BuildID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r == '.' || r == '_' || r == '-')) {
			return VerifiedRelease{}, ErrMetadata
		}
	}
	seen := map[string]bool{}
	for _, a := range m.Assets {
		suffix, err := packageSuffix(Selection{OS: a.OS, Arch: a.Arch, Kind: a.Kind, Channel: "preview"})
		c := Candidate{Version: m.Version, Name: a.Name, Size: a.Size, SHA256: a.SHA256, DownloadURL: "https://github.com/" + repository + "/releases/download/v" + m.Version + "/" + a.Name}
		if (a.PayloadKind != "core" && a.PayloadKind != "gui") || (a.Kind == "core-portable" && a.PayloadKind != "core") || (a.Kind == "gui-portable" && a.PayloadKind != "gui") || (a.PayloadKind == "gui" && a.Arch != "amd64") || err != nil || a.Name != "sidravia-v"+m.Version+"-"+a.OS+"-"+a.Arch+"-"+suffix || validateCandidate(c) != nil || !packagefiles.ValidSHA256(a.FilesSHA256) || seen[a.Name] {
			return VerifiedRelease{}, ErrMetadata
		}
		seen[a.Name] = true
	}
	return VerifiedRelease{manifest: m, data: append([]byte(nil), data...), signature: append([]byte(nil), signature...)}, nil
}
func releaseShape(data []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || !releaseFields(root, "schemaVersion", "keyId", "version", "buildId", "minimumProtocol", "assets") {
		return false
	}
	var assets []map[string]json.RawMessage
	if json.Unmarshal(root["assets"], &assets) != nil {
		return false
	}
	for _, a := range assets {
		if !releaseFields(a, "os", "arch", "kind", "name", "size", "sha256", "filesSha256", "payloadKind") {
			return false
		}
	}
	return true
}
func releaseFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if len(fields[name]) == 0 || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return false
		}
	}
	return true
}
