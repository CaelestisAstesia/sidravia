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
	profiles := map[string]int{}
	for _, repository := range flags.Args() {
		asset, err := qtReleaseAsset(repository, *version, key)
		if err != nil {
			return fail(fmt.Errorf("review %s: %w", filepath.Base(repository), err))
		}
		profiles[asset.Arch]++
		manifest.Assets = append(manifest.Assets, asset)
	}
	for _, count := range profiles {
		if count != 2 {
			return fail(errors.New("each target must include both Core and GUI repositories"))
		}
	}
	sort.Slice(manifest.Assets, func(i, j int) bool { return manifest.Assets[i].Name < manifest.Assets[j].Name })
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

func qtReleaseAsset(path, version string, key ed25519.PublicKey) (release.ReleaseAsset, error) {
	file, err := os.Open(path)
	if err != nil {
		return release.ReleaseAsset{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 2<<30 {
		return release.ReleaseAsset{}, release.ErrMetadata
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return release.ReleaseAsset{}, err
	}
	index, inventoryHash, err := qtrepository.InspectSigned(archive, key)
	if err != nil {
		return release.ReleaseAsset{}, err
	}
	if index.Version != version {
		return release.ReleaseAsset{}, release.ErrMetadata
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, info.Size()+1))
	if err != nil || size != info.Size() {
		return release.ReleaseAsset{}, release.ErrMetadata
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
	}, nil
}
