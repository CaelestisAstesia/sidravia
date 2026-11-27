// Package qtrepository prepares a publisher-authenticated repository for Qt.
package qtrepository

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
)

const stagedPrefix = "qt-repository-"

const Inventory = "sidravia-ifw-inventory.json"
const Signature = "sidravia-ifw-inventory.sig"

var ErrMaintenanceImage = errors.New("Qt maintenance image requires offline replacement")

// Stage extracts into a new caller-owned directory. The release proof binds the
// downloaded archive and its repository inventory. Qt verifies the inner
// signature again before consuming any staged metadata or payload.
func Stage(ctx context.Context, source, parent, maintenanceSHA256 string, proof release.VerifiedRelease, candidate release.Candidate) (staged string, retErr error) {
	asset, err := proof.Asset(candidate)
	if err != nil {
		return "", err
	}
	if release.QtRepositoryProfile(asset.Kind) == "" {
		return "", release.ErrUnsupported
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(input, candidate.Size+1))
	if err != nil {
		return "", err
	}
	if n != candidate.Size || hex.EncodeToString(hash.Sum(nil)) != candidate.SHA256 {
		return "", release.ErrUntrusted
	}
	archive, err := zip.NewReader(input, n)
	if err != nil {
		return "", err
	}

	repository, err := readArchive(archive, true)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(repository.raw)
	if hex.EncodeToString(sum[:]) != asset.FilesSHA256 {
		return "", release.ErrUntrusted
	}
	inventory := repository.inventory
	if inventory.Target != asset.OS+"-"+asset.Arch ||
		inventory.Profile != release.QtRepositoryProfile(asset.Kind) ||
		inventory.Version != candidate.Version || inventory.BuildID != proof.BuildID() {
		return "", release.ErrMetadata
	}

	if inventory.MaintenanceSHA256 != maintenanceSHA256 {
		return "", ErrMaintenanceImage
	}

	directory, err := os.MkdirTemp(parent, stagedPrefix)
	if err != nil {
		return "", err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, os.RemoveAll(directory))
		}
	}()
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	for name, hash := range inventory.Files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := stageMember(root, repository.members[name], hash); err != nil {
			return "", err
		}
	}
	if err := writeMetadata(root, Inventory, repository.raw); err != nil {
		return "", err
	}
	if err := writeMetadata(root, Signature, repository.signature); err != nil {
		return "", err
	}
	return directory, nil
}

// ClearStaged removes only repository directories previously staged under
// the cache slot. Callers must hold the maintenance process exclusion.
func ClearStaged(parent string) error {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	names, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range names {
		suffix, found := strings.CutPrefix(entry.Name(), stagedPrefix)
		if !found || suffix == "" || strings.ContainsFunc(suffix, func(r rune) bool { return r < '0' || r > '9' }) {
			continue
		}
		if !entry.IsDir() {
			return release.ErrMetadata
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func readMember(entry *zip.File, maximum int64) ([]byte, error) {
	if entry == nil || entry.UncompressedSize64 > uint64(maximum) {
		return nil, release.ErrMetadata
	}
	file, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err == nil && int64(len(data)) > maximum {
		err = release.ErrMetadata
	}
	return data, err
}

func stageMember(root *os.Root, entry *zip.File, expected string) error {
	name := filepath.FromSlash(entry.Name)
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	target, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return errors.Join(copyVerified(entry, target, expected), target.Close())
}

func copyVerified(entry *zip.File, target io.Writer, expected string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(hash, target), io.LimitReader(source, int64(entry.UncompressedSize64)+1))
	if err := errors.Join(err, source.Close()); err != nil {
		return err
	}
	if uint64(n) != entry.UncompressedSize64 || hex.EncodeToString(hash.Sum(nil)) != expected {
		return release.ErrUntrusted
	}
	return nil
}

func writeMetadata(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = bytes.NewReader(data).WriteTo(file)
	return errors.Join(err, file.Close())
}

type archiveIndex struct {
	members        map[string]*zip.File
	inventory      release.QtRepositoryIndex
	raw, signature []byte
}

func readArchive(archive *zip.Reader, signed bool) (archiveIndex, error) {
	members := make(map[string]*zip.File, len(archive.File))
	names := make(map[string]bool, len(archive.File))
	var total uint64
	const maxExpanded = 2 << 30
	if len(archive.File) > release.ReleaseManifestLimit/(sha256.Size*2) {
		return archiveIndex{}, release.ErrMetadata
	}
	for _, entry := range archive.File {
		folded := strings.ToLower(entry.Name)
		if !signed && folded == Signature || names[folded] || !packagefiles.SafePath(entry.Name) || !entry.Mode().IsRegular() ||
			entry.UncompressedSize64 > maxExpanded-total {
			return archiveIndex{}, release.ErrMetadata
		}
		total += entry.UncompressedSize64
		names[folded] = true
		members[entry.Name] = entry
	}

	for name := range names {
		for ancestor := path.Dir(name); ancestor != "."; ancestor = path.Dir(ancestor) {
			if names[ancestor] {
				return archiveIndex{}, release.ErrMetadata
			}
		}
	}
	index, err := readMember(members[Inventory], release.ReleaseManifestLimit)
	if err != nil {
		return archiveIndex{}, err
	}
	inventory, err := release.DecodeQtRepositoryIndex(index)
	if err != nil {
		return archiveIndex{}, err
	}
	overhead := 1
	var signature []byte
	if signed {
		overhead++
		signature, err = readMember(members[Signature], ed25519.SignatureSize)
		if err != nil || len(signature) != ed25519.SignatureSize {
			return archiveIndex{}, release.ErrUntrusted
		}
	}
	if len(inventory.Files)+overhead != len(members) {
		return archiveIndex{}, release.ErrMetadata
	}
	for name := range inventory.Files {
		if name == Inventory || name == Signature || members[name] == nil {
			return archiveIndex{}, release.ErrMetadata
		}
	}
	return archiveIndex{members, inventory, index, signature}, nil
}

// ReviewUnsigned verifies all packaged bytes and returns the exact inventory
// bytes to sign. The signing tool preserves those bytes in the published ZIP.
func ReviewUnsigned(archive *zip.Reader) ([]byte, error) {
	repository, err := readArchive(archive, false)
	if err != nil {
		return nil, err
	}
	for name, expected := range repository.inventory.Files {
		if err := copyVerified(repository.members[name], io.Discard, expected); err != nil {
			return nil, err
		}
	}
	return repository.raw, nil
}

// InspectSigned verifies the inner signature and every file before the release
// preparation tool records the archive digest in the outer manifest.
func InspectSigned(archive *zip.Reader, key ed25519.PublicKey) (release.QtRepositoryIndex, string, error) {
	repository, err := readArchive(archive, true)
	if err != nil {
		return release.QtRepositoryIndex{}, "", err
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, repository.raw, repository.signature) {
		return release.QtRepositoryIndex{}, "", release.ErrUntrusted
	}
	for name, expected := range repository.inventory.Files {
		if err := copyVerified(repository.members[name], io.Discard, expected); err != nil {
			return release.QtRepositoryIndex{}, "", err
		}
	}
	digest := sha256.Sum256(repository.raw)
	return repository.inventory, hex.EncodeToString(digest[:]), nil
}
