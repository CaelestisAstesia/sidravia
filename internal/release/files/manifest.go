// Package packagefiles defines the product-file ownership boundary shared by
// distribution production and update verification. It performs no filesystem I/O.
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"sidravia/internal/persistence/jsonfile"
)

const Name = "sidravia-files.json"
const SizeLimit = 2 << 20
const MaximumFiles = 20000

var ErrInvalid = errors.New("invalid product file manifest")

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	BuildID       string `json:"buildId"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	PayloadKind   string `json:"payloadKind"`
	Files         []File `json:"files"`
}

// Encode sorts a private copy so distribution bytes do not depend on enumeration order.
func Encode(m Manifest) ([]byte, error) {
	m.Files = append([]File(nil), m.Files...)
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(m)
	if err != nil || len(data) > SizeLimit {
		return nil, ErrInvalid
	}
	return data, nil
}

func Decode(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) == 0 || len(data) > SizeLimit || jsonfile.DecodeStrict(data, &m) != nil || !exactShape(data) {
		return Manifest{}, ErrInvalid
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 || !safeIdentity(m.Version) || !safeIdentity(m.BuildID) ||
		(m.OS != "linux" && m.OS != "windows") || (m.Arch != "amd64" && m.Arch != "arm64") ||
		(m.PayloadKind != "core" && m.PayloadKind != "gui") || (m.PayloadKind == "gui" && m.Arch != "amd64") ||
		len(m.Files) == 0 || len(m.Files) > MaximumFiles {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(m.Files))
	var total int64
	for _, f := range m.Files {
		key := strings.ToLower(f.Path)
		if !SafePath(f.Path) || seen[key] || f.Size < 0 || f.Size > 2<<30 || !ValidSHA256(f.SHA256) || (f.Mode != 0644 && f.Mode != 0755) {
			return ErrInvalid
		}
		// A file cannot also be another file's parent, even with a case alias.
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return ErrInvalid
			}
		}
		seen[key] = true
		total += f.Size
		if total > 4<<30 {
			return ErrInvalid
		}
	}
	// Check parent conflicts independently of input order.
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return ErrInvalid
			}
		}
	}
	return nil
}

// SafePath accepts portable product paths, never user data or the ownership
// document itself. Root directories on disk may still contain spaces or Unicode.
func SafePath(name string) bool {
	if name == "" || len(name) > 1024 || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	for _, r := range name {
		if r < 32 || r > 126 || strings.ContainsRune(`\<>:"|?*`, r) {
			return false
		}
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part || part == "." || part == ".." || strings.HasSuffix(part, ".") {
			return false
		}
		base := strings.ToUpper(strings.TrimRight(strings.SplitN(part, ".", 2)[0], " "))
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	switch strings.ToLower(parts[0]) {
	case "config", "runtime", "logs", "namespaces", "sidravia.portable", "sidravia.update", Name:
		return false
	}
	return true
}

func ValidSHA256(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
}
func safeIdentity(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return false
		}
	}
	return true
}

func exactShape(data []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || !hasFields(root, "schemaVersion", "version", "buildId", "os", "arch", "payloadKind", "files") {
		return false
	}
	var files []map[string]json.RawMessage
	if json.Unmarshal(root["files"], &files) != nil {
		return false
	}
	for _, f := range files {
		if !hasFields(f, "path", "size", "sha256", "mode") {
			return false
		}
	}
	return true
}
func hasFields(fields map[string]json.RawMessage, names ...string) bool {
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
