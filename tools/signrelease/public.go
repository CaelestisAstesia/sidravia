package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"sidravia/internal/release"
)

// inspect/verify are public-material operations. Neither reads stdin, generates
// keys, rewrites signed bytes, invokes installation nor changes product roots.
func publicRun(mode string, args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "exact v2 manifest")
	public := flags.String("public-key", "", "base64 Ed25519 public key file")
	signature := flags.String("signature", "", "raw detached signature (verify only)")
	published := flags.Bool("published", false, "verify the published manifest and signature filenames")
	artifactDir := flags.String("qt-artifacts", "", "verify the signed Qt repositories in this directory")
	maintenanceDir := flags.String("qt-maintenance", "", "check offline installers against target maintenance images in this directory")
	machine := flags.Bool("json", false, "JSON result")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *maintenanceDir != "" && *artifactDir == "" {
		fmt.Fprintln(stderr, "-qt-maintenance requires -qt-artifacts")
		return 2
	}
	if flags.NArg() != 0 || *manifest == "" || *public == "" || (mode == "verify") != (*signature != "") || ((*published || *artifactDir != "") && mode != "verify") {
		fmt.Fprintln(stderr, "manifest/public-key and verify signature required")
		return 2
	}
	fail := func(message string) int { fmt.Fprintln(stderr, message); return 1 }
	data, err := readPublicFile(*manifest, release.ReleaseManifestLimit)
	if err != nil {
		return fail("invalid bounded manifest file")
	}
	encoded, err := readPublicFile(*public, 256)
	if err != nil {
		return fail("invalid bounded public key file")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fail("invalid Ed25519 public key")
	}
	id, err := release.PublisherKeyID(key)
	if err != nil {
		return fail("invalid publisher key identity")
	}
	m, err := release.ValidateReleaseManifest(data)
	if err != nil {
		return fail("invalid release manifest structure")
	}
	if m.KeyID != id {
		return fail("manifest publisher key identity mismatch")
	}
	status, artifactStatus := "not_checked", "not_checked"
	if mode == "verify" {
		sig, err := readPublicFile(*signature, ed25519.SignatureSize)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return fail("invalid detached signature file")
		}
		keys, err := release.NewPublisherKeys([]ed25519.PublicKey{key})
		if err != nil {
			return fail("invalid public key")
		}
		proof, err := keys.Verify(data, sig, m.Version)
		if err != nil {
			return fail("release signature or metadata rejected")
		}
		if *published && !validPublishedNames(proof, filepath.Base(*manifest), filepath.Base(*signature)) {
			return fail("release metadata does not match its published names")
		}
		if *artifactDir != "" {
			if proof.ValidateLocation(release.QtReleaseManifestName) != nil {
				return fail("Qt artifact verification requires the Qt release manifest")
			}
			paths := make([]string, 0, len(m.Assets))
			for _, asset := range m.Assets {
				paths = append(paths, filepath.Join(*artifactDir, asset.Name))
			}
			actual, err := inspectQtArtifacts(paths, m.Version, m.BuildID, key, *artifactDir, *maintenanceDir)
			if err != nil {
				return fail(err.Error())
			}
			sort.Slice(m.Assets, func(i, j int) bool { return m.Assets[i].Name < m.Assets[j].Name })
			if !slices.Equal(actual, m.Assets) {
				return fail("Qt artifacts do not match the signed release manifest")
			}
			artifactStatus = "passed"
		}
		status = "passed"
	}
	if *machine {
		result := map[string]any{"schemaVersion": 1, "ok": true, "version": m.Version, "buildId": m.BuildID, "keyId": id, "assets": len(m.Assets), "signature": status, "artifactBytes": artifactStatus}
		if *maintenanceDir != "" {
			result["installerMaintenancePrefix"] = "passed"
		}
		if json.NewEncoder(out).Encode(result) != nil {
			return fail("unable to write result")
		}
	} else if *artifactDir != "" {
		fmt.Fprintf(out, "Release manifest %s; signature %s; artifact bytes %s", mode, status, artifactStatus)
		if *maintenanceDir != "" {
			fmt.Fprint(out, "; installer maintenance prefix passed")
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintf(out, "Release manifest %s; signature %s; no artifact files executed\n", mode, status)
	}
	return 0
}

func validPublishedNames(proof release.VerifiedRelease, manifest, signature string) bool {
	for _, pair := range [][2]string{
		{release.ReleaseManifestName, release.ReleaseSignatureName},
		{release.XZReleaseManifestName, release.XZReleaseSignatureName},
		{release.ExtendedReleaseManifestName, release.ExtendedReleaseSignatureName},
		{release.QtReleaseManifestName, release.QtReleaseSignatureName},
	} {
		if manifest == pair[0] && signature == pair[1] {
			return proof.ValidateLocation(manifest) == nil
		}
	}
	return false
}

func readPublicFile(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return nil, errors.New("invalid public file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	current, err := f.Stat()
	if err != nil || !os.SameFile(before, current) {
		f.Close()
		return nil, errors.New("public file identity changed")
	}
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	err = errors.Join(readErr, f.Close())
	if err != nil || int64(len(data)) > limit {
		return nil, errors.Join(errors.New("public file read failed"), err)
	}
	return data, nil
}
