// Package qtrepository prepares a publisher-authenticated repository for Qt.
package qtrepository

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sidravia/internal/release"
	packagefiles "sidravia/internal/release/files"
)

const Inventory = "sidravia-ifw-inventory.json"
const Signature = "sidravia-ifw-inventory.sig"

// Stage extracts into a new caller-owned directory. The release proof binds the
// downloaded archive and its repository inventory. Qt verifies the inner
// signature again before consuming any staged metadata or payload.
func Stage(ctx context.Context, source, directory string, proof release.VerifiedRelease, candidate release.Candidate) error {
	asset, err := proof.Asset(candidate)
	if err != nil {
		return err
	}
	if release.QtRepositoryProfile(asset.Kind) == "" {
		return release.ErrUnsupported
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(input, candidate.Size+1))
	if err != nil {
		return err
	}
	if n != candidate.Size || hex.EncodeToString(hash.Sum(nil)) != candidate.SHA256 {
		return release.ErrUntrusted
	}
	archive, err := zip.NewReader(input, n)
	if err != nil {
		return err
	}

	members := make(map[string]*zip.File, len(archive.File))
	names := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		folded := strings.ToLower(entry.Name)
		if names[folded] || !packagefiles.SafePath(entry.Name) || !entry.Mode().IsRegular() {
			return release.ErrMetadata
		}
		names[folded] = true
		members[entry.Name] = entry
	}

	index, err := readMember(members[Inventory], release.ReleaseManifestLimit)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(index)
	if hex.EncodeToString(sum[:]) != asset.FilesSHA256 {
		return release.ErrUntrusted
	}
	signature, err := readMember(members[Signature], ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return release.ErrUntrusted
	}
	var hashes map[string]string
	if json.Unmarshal(index, &hashes) != nil || len(hashes)+2 != len(members) || !packagefiles.ValidSHA256(hashes["Updates.xml"]) {
		return release.ErrMetadata
	}
	for name := range hashes {
		if name == Inventory || name == Signature || members[name] == nil || !packagefiles.ValidSHA256(hashes[name]) {
			return release.ErrMetadata
		}
	}

	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for name, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := stageMember(root, members[name], hash); err != nil {
			return err
		}
	}
	if err := writeMetadata(root, Inventory, index); err != nil {
		return err
	}
	return writeMetadata(root, Signature, signature)
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
	// Repository assets are already compressed and signed. Bound extraction by
	// the same asset size limit used by the release transport.
	if entry.UncompressedSize64 > 2<<30 {
		return release.ErrMetadata
	}
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	name := filepath.FromSlash(entry.Name)
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	target, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(hash, target), io.LimitReader(source, int64(entry.UncompressedSize64)+1))
	if err := errors.Join(err, target.Close()); err != nil {
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
