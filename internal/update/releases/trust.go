package releases

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/update/packagefiles"
)

const ReleaseManifestName = "sidravia-release.json"
const ReleaseSignatureName = "sidravia-release.sig"
const XZReleaseManifestName = "sidravia-release-xz.json"
const XZReleaseSignatureName = "sidravia-release-xz.sig"
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

// publisherPublicKey is the reviewed Ed25519 release publisher public key.
// Key ID: c68f12717c77cdf1fc732185e2a5977340970e69da491da1847b5faf19ddeb9a
// Private material is never part of the product or runtime configuration.
const publisherPublicKey = "\x81\x9b\x30\x86\x4c\xac\xfa\x36\x1b\x32\xd8\x29\x53\x18\x0a\x4f\xb8\x83\xca\x15\x7a\x13\x65\xc7\xd7\x36\xdd\x64\x77\x0f\x58\xfe"

// PublisherTrust returns fresh immutable copies of the build-pinned root.
func PublisherTrust() *PublisherKeys {
	keys, _ := NewPublisherKeys([]ed25519.PublicKey{ed25519.PublicKey(publisherPublicKey)})
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
	m, err := ValidateReleaseManifest(data)
	if err != nil {
		return VerifiedRelease{}, err
	}
	if m.Version != version {
		return VerifiedRelease{}, ErrMetadata
	}
	return VerifiedRelease{manifest: m, data: append([]byte(nil), data...), signature: append([]byte(nil), signature...)}, nil
}

// ValidateReleaseManifest checks public structure only. It does not establish
// publisher trust and cannot construct a VerifiedRelease or authorize updates.
// The release preparer and signature verifier share this exact v2 contract.
func ValidateReleaseManifest(data []byte) (ReleaseManifest, error) {
	if len(data) == 0 || len(data) > ReleaseManifestLimit {
		return ReleaseManifest{}, ErrMetadata
	}
	var m ReleaseManifest
	if jsonfile.DecodeStrict(data, &m) != nil || !releaseShape(data) || m.SchemaVersion != 2 || m.MinimumProtocol != 1 || len(m.BuildID) == 0 || len(m.BuildID) > 64 || len(m.Assets) == 0 || len(m.Assets) > 32 {
		return ReleaseManifest{}, ErrMetadata
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return ReleaseManifest{}, ErrMetadata
	}
	for i, r := range m.BuildID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r == '.' || r == '_' || r == '-')) {
			return ReleaseManifest{}, ErrMetadata
		}
	}
	seen := map[string]bool{}
	ownership := map[string]ReleaseAsset{}
	for _, a := range m.Assets {
		selection := Selection{OS: a.OS, Arch: a.Arch, Kind: a.Kind, Channel: "preview"}
		c := Candidate{Version: m.Version, Name: a.Name, Size: a.Size, SHA256: a.SHA256, DownloadURL: "https://github.com/" + Repository + "/releases/download/v" + m.Version + "/" + a.Name}
		if (a.PayloadKind != "core" && a.PayloadKind != "gui") || (a.Kind == "core-portable" && a.PayloadKind != "core") || (a.Kind == "gui-portable" && a.PayloadKind != "gui") || (a.PayloadKind == "gui" && a.Arch != "amd64") || !AssetNameValid(m.Version, selection, a.Name) || ValidateCandidate(c) != nil || !packagefiles.ValidSHA256(a.FilesSHA256) || seen[a.Name] {
			return ReleaseManifest{}, ErrMetadata
		}
		target := a.OS + "/" + a.Arch + "/" + a.Kind
		if previous, ok := ownership[target]; ok && (previous.FilesSHA256 != a.FilesSHA256 || previous.PayloadKind != a.PayloadKind) {
			return ReleaseManifest{}, ErrMetadata
		}
		ownership[target] = a
		seen[a.Name] = true
	}
	return m, nil
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

// Configured describes build-selected trust, never a runtime key override.
func (keys *PublisherKeys) Configured() bool { return keys != nil && len(keys.keys) > 0 }

// InstalledAsset selects the signed canonical asset for an existing layout.
// IDs and download paths from local cache documents do not grant this trust.
func (v VerifiedRelease) InstalledAsset(osName, arch, kind string) (Candidate, error) {
	for _, asset := range v.manifest.Assets {
		if asset.OS == osName && asset.Arch == arch && asset.Kind == kind {
			return Candidate{ID: "0:0", Version: v.manifest.Version, Name: asset.Name, Size: asset.Size, SHA256: asset.SHA256}, nil
		}
	}
	return Candidate{}, ErrUntrusted
}
