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
	"strings"

	"sidravia/internal/update/releases"
)

// inspect/verify are public-material operations. Neither reads stdin, generates
// keys, rewrites signed bytes, invokes installation nor changes product roots.
func publicRun(mode string, args []string, out, stderr io.Writer) int {
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "exact v2 manifest")
	public := flags.String("public-key", "", "base64 Ed25519 public key file")
	signature := flags.String("signature", "", "raw detached signature (verify only)")
	machine := flags.Bool("json", false, "JSON result")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *manifest == "" || *public == "" || (mode == "verify") != (*signature != "") {
		fmt.Fprintln(stderr, "manifest/public-key and verify signature required")
		return 2
	}
	fail := func(message string) int { fmt.Fprintln(stderr, message); return 1 }
	data, err := readPublicFile(*manifest, releases.ReleaseManifestLimit)
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
	id, err := releases.PublisherKeyID(key)
	if err != nil {
		return fail("invalid publisher key identity")
	}
	m, err := releases.ValidateReleaseManifest(data)
	if err != nil {
		return fail("invalid release manifest structure")
	}
	if m.KeyID != id {
		return fail("manifest publisher key identity mismatch")
	}
	status := "not_checked"
	if mode == "verify" {
		sig, err := readPublicFile(*signature, ed25519.SignatureSize)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return fail("invalid detached signature file")
		}
		keys, err := releases.NewPublisherKeys([]ed25519.PublicKey{key})
		if err != nil {
			return fail("invalid public key")
		}
		if _, err = keys.Verify(data, sig, m.Version); err != nil {
			return fail("release signature or metadata rejected")
		}
		status = "passed"
	}
	if *machine {
		result := map[string]any{"schemaVersion": 1, "ok": true, "version": m.Version, "buildId": m.BuildID, "keyId": id, "assets": len(m.Assets), "signature": status, "artifactBytes": "not_checked"}
		if json.NewEncoder(out).Encode(result) != nil {
			return fail("unable to write result")
		}
	} else {
		fmt.Fprintf(out, "Release manifest %s; signature %s; no artifact files executed\n", mode, status)
	}
	return 0
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
