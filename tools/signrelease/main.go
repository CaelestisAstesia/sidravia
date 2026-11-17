// Command signrelease signs an exact, reviewed release manifest offline. It
// never creates keys, contacts a source, uploads files or enables an updater.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"sidravia/internal/update/releases"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(args []string, input io.Reader, out, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "inspect" || args[0] == "verify") {
		return publicRun(args[0], args[1:], out, stderr)
	}
	flags := flag.NewFlagSet("signrelease", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "exact reviewed v2 release manifest")
	output := flags.String("output", "", "new signature output file (must not exist)")
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: signrelease --manifest FILE --output FILE\nReads one base64 Ed25519 32-byte seed from standard input; writes a raw 64-byte signature.\nNo key generation, key storage, network or publication. Input manifest bytes are not rewritten.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *manifest == "" || *output == "" || flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	fail := func(message string) int { fmt.Fprintln(stderr, message); return 1 }
	if _, err := os.Lstat(*output); !os.IsNotExist(err) {
		return fail("signature output must not exist")
	}
	file, err := os.Open(*manifest)
	if err != nil {
		return fail("unable to open release manifest")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > releases.ReleaseManifestLimit {
		file.Close()
		return fail("invalid release manifest file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, releases.ReleaseManifestLimit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > releases.ReleaseManifestLimit {
		return fail("unable to read bounded release manifest")
	}
	encoded, err := io.ReadAll(io.LimitReader(input, 257))
	defer clear(encoded)
	if err != nil || len(encoded) > 256 {
		return fail("unable to read bounded signing seed")
	}
	trimmed := bytes.TrimSpace(encoded)
	seed := make([]byte, base64.StdEncoding.DecodedLen(len(trimmed)))
	defer clear(seed)
	n, err := base64.StdEncoding.Decode(seed, trimmed)
	if err != nil || n != ed25519.SeedSize {
		return fail("invalid signing seed")
	}
	private := ed25519.NewKeyFromSeed(seed[:n])
	defer clear(private)
	signature := ed25519.Sign(private, data)
	roots, err := releases.NewPublisherKeys([]ed25519.PublicKey{private.Public().(ed25519.PublicKey)})
	if err != nil {
		return fail("invalid signing key")
	}
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &version) != nil {
		return fail("invalid release manifest")
	}
	if _, err := roots.Verify(data, signature, version.Version); err != nil {
		return fail("release manifest or publisher key identity rejected")
	}
	destination, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fail("signature output must be a new writable file")
	}
	createdInfo, inspectErr := destination.Stat()
	if inspectErr != nil {
		destination.Close()
		return fail("unable to inspect signature output")
	}
	complete := false
	defer func() {
		if !complete {
			current, err := os.Lstat(*output)
			if err == nil && os.SameFile(createdInfo, current) {
				_ = os.Remove(*output)
			}
		}
	}()
	_, writeErr := io.Copy(destination, bytes.NewReader(signature))
	syncErr := destination.Sync()
	closeErr = destination.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return fail("unable to persist signature")
	}
	complete = true
	fmt.Fprintln(out, "Release signature written; no files were uploaded")
	return 0
}
