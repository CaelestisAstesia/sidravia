package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sidravia/internal/release"
	"sidravia/internal/release/qtrepository"
)

func prepareQtManifest(args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet("signrelease qt-manifest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "product version")
	build := flags.String("build-id", "", "product build identity")
	public := flags.String("public-key", "", "base64 Ed25519 public key file")
	if err := flags.Parse(args); err != nil || *version == "" || *build == "" || *public == "" || flags.NArg() == 0 {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, err)
		return 1
	}
	encoded, err := readPublicFile(*public, 256)
	if err != nil {
		return fail(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return fail(err)
	}
	id, err := release.PublisherKeyID(key)
	if err != nil {
		return fail(err)
	}
	manifest := release.ReleaseManifest{
		SchemaVersion: 2, KeyID: id, Version: *version, BuildID: *build, MinimumProtocol: 1,
	}
	manifest.Assets, err = inspectQtArtifacts(flags.Args(), *version, *build, key)
	if err != nil {
		return fail(err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		_, err = release.ValidateReleaseManifest(data)
	}
	if err == nil {
		_, err = fmt.Fprintln(out, string(data))
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

// Use the same repository checks before signing and when verifying staged release bytes.
func inspectQtArtifacts(paths []string, version, buildID string, key ed25519.PublicKey) ([]release.ReleaseAsset, error) {
	var assets []release.ReleaseAsset
	type target struct {
		profiles map[string]bool
		image    string
	}
	targets := map[string]target{}
	for _, repository := range paths {
		asset, image, err := qtReleaseAsset(repository, version, buildID, key)
		if err != nil {
			return nil, fmt.Errorf("review %s: %w", filepath.Base(repository), err)
		}
		group, exists := targets[asset.Arch]
		if !exists {
			group = target{profiles: map[string]bool{}, image: image}
			targets[asset.Arch] = group
		}
		if group.profiles[asset.PayloadKind] || group.image != image {
			return nil, errors.New("each target must have distinct profiles using a single maintenance image")
		}
		group.profiles[asset.PayloadKind] = true
		assets = append(assets, asset)
	}
	for _, group := range targets {
		if len(group.profiles) != 2 {
			return nil, errors.New("each target must include both Core and GUI repositories")
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return assets, nil
}

func qtReleaseAsset(path, version, buildID string, key ed25519.PublicKey) (release.ReleaseAsset, string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	if !before.Mode().IsRegular() {
		return release.ReleaseAsset{}, "", release.ErrMetadata
	}
	file, err := os.Open(path)
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) || info.Size() <= 0 || info.Size() > 2<<30 {
		return release.ReleaseAsset{}, "", release.ErrMetadata
	}
	// Hash exactly the bytes passed to the inner verifier, even when the
	// caller supplies a repository from a writable staging directory.
	snapshot, err := os.CreateTemp("", "sidravia-qt-release-*.zip")
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	defer os.Remove(snapshot.Name())
	defer snapshot.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(snapshot, hash), io.LimitReader(file, info.Size()+1))
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	if size != info.Size() {
		return release.ReleaseAsset{}, "", release.ErrMetadata
	}
	archive, err := zip.NewReader(snapshot, size)
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	index, inventoryHash, err := qtrepository.InspectSigned(archive, key)
	if err != nil {
		return release.ReleaseAsset{}, "", err
	}
	if index.Version != version || index.BuildID != buildID {
		return release.ReleaseAsset{}, "", release.ErrMetadata
	}
	after, err := os.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(info, after) ||
		after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return release.ReleaseAsset{}, "", errors.New("repository changed during verification")
	}
	kind := release.QtCoreRepository
	if index.Profile == "gui" {
		kind = release.QtGUIRepository
	}
	return release.ReleaseAsset{
		ManifestAsset: release.ManifestAsset{
			OS: "windows", Arch: strings.TrimPrefix(index.Target, "windows-"), Kind: kind,
			Name: filepath.Base(path), Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)),
		},
		FilesSHA256: inventoryHash, PayloadKind: index.Profile,
	}, index.MaintenanceSHA256, nil
}
