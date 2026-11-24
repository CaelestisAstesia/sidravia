// Command build is the single reproducible Sidravia package builder.
//
// It uses the fixed Go 1.26.4 toolchain and shared project codecs. Distribution
// mode builds target-bound Windows/Linux core tools and optional AMD64 GUI
// archives/installers. Legacy release/field-validation modes build a Windows
// amd64 CLI pair. Each mode writes SHA256SUMS.txt. The tool does not run Git,
// derive versions, sign, tag, upload or publish a release.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const requiredGoVersion = "go1.26.4"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, "sidravia-build:", err)
		return 2
	}
	if cfg.help {
		printHelp(stdout)
		return 0
	}
	if err := validateProductVersion(cfg.version); err != nil {
		fmt.Fprintln(stderr, "sidravia-build:", err)
		return 2
	}
	if err := validateBuildID(cfg.buildID); err != nil {
		fmt.Fprintln(stderr, "sidravia-build:", err)
		return 2
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "sidravia-build:", err)
		return 1
	}
	t := newDefaultTool(cfg, repoRoot)
	if err := t.execute(stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "sidravia-build:", err)
		return 1
	}
	return 0
}

// config holds parsed command-line flags.
type config struct {
	version      string
	buildID      string
	output       string
	goBin        string
	artifact     string
	target       string
	guiDirectory string
	iscc         string
	compression  string
	help         bool
}

var knownValueFlags = map[string]bool{
	"--version":       true,
	"--build-id":      true,
	"--output":        true,
	"--go":            true,
	"--artifact":      true,
	"--target":        true,
	"--gui-directory": true,
	"--iscc":          true,
	"--compression":   true,
}

// parseFlags parses the strict flag grammar. It rejects positional arguments,
// repeated flags, unknown flags, empty values and missing values.
func parseFlags(args []string) (config, error) {
	var c config
	c.goBin = "go"
	c.artifact = "release"
	// --help and -h are accepted only as the sole argument; any other token
	// alongside them is a usage error.
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		c.help = true
		return c, nil
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			return c, errors.New("--help must be used alone")
		}
		if !strings.HasPrefix(a, "--") {
			return c, fmt.Errorf("unexpected argument: %q", a)
		}
		name := a
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			name = a[:eq]
			val = a[eq+1:]
			hasVal = true
		}
		if !knownValueFlags[name] {
			return c, fmt.Errorf("unknown flag: %q", name)
		}
		if !hasVal {
			i++
			if i >= len(args) {
				return c, fmt.Errorf("flag %q requires a value", name)
			}
			val = args[i]
		}
		if val == "" {
			return c, fmt.Errorf("flag %q must not be empty", name)
		}
		if seen[name] {
			return c, fmt.Errorf("flag %q repeated", name)
		}
		seen[name] = true
		switch name {
		case "--version":
			c.version = val
		case "--build-id":
			c.buildID = val
		case "--output":
			c.output = val
		case "--go":
			c.goBin = val
		case "--artifact":
			if val != "release" && val != "field-validation" && val != "distribution" {
				return c, fmt.Errorf("flag %q must be release, field-validation or distribution", name)
			}
			c.artifact = val
		case "--target":
			c.target = val
		case "--gui-directory":
			c.guiDirectory = val
		case "--iscc":
			c.iscc = val
		case "--compression":
			c.compression = val
		}
	}
	if c.help {
		return c, nil
	}
	if c.version == "" {
		return c, errors.New("missing required flag --version")
	}
	if c.buildID == "" {
		return c, errors.New("missing required flag --build-id")
	}
	if c.output == "" {
		return c, errors.New("missing required flag --output")
	}
	if c.compression != "" && (c.artifact != "distribution" || !strings.HasPrefix(c.target, "linux-") || (c.compression != "gzip" && c.compression != "xz")) {
		return c, errors.New("compression requires a Linux distribution and gzip or xz")
	}
	if c.artifact == "distribution" {
		if c.target != "windows-amd64" && c.target != "linux-amd64" && c.target != "windows-arm64" && c.target != "linux-arm64" {
			return c, errors.New("distribution requires --target windows-amd64, linux-amd64, windows-arm64 or linux-arm64")
		}
		if c.target != "windows-amd64" && c.iscc != "" {
			return c, errors.New("installer requires windows-amd64")
		}
		if c.target != "windows-amd64" && c.target != "linux-amd64" && c.guiDirectory != "" {
			return c, errors.New("GUI requires windows-amd64 or linux-amd64")
		}
	} else if c.target != "" || c.guiDirectory != "" || c.iscc != "" {
		return c, errors.New("target, GUI and installer flags require --artifact distribution")
	}
	return c, nil
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: go run ./tools/build --version <version> --build-id <build-id> --output <dir> [--go <go>] [--artifact <kind>]

Builds reproducible Sidravia packages using the fixed Go toolchain and shared
project ownership codec.
The tool runs only from the Sidravia repository root.

Required flags:
  --version    canonical product version, e.g. 0.1.0-alpha.2 (no leading v)
  --build-id   stable build identifier, 1-64 chars, [0-9A-Za-z][0-9A-Za-z._-]*
  --output     output directory; must not exist, its parent must exist

Optional flags:
  --go         go executable to use (default: go); must report go1.26.4
  --artifact   release (default) or field-validation
               distribution: Windows archive/optional installer or Linux archive
  --target     distribution only: windows-amd64, linux-amd64, windows-arm64 or linux-arm64
  --compression  Linux distribution only: gzip (default) or xz; xz requires XZ Utils 5.8.3
  --gui-directory  optional fresh Windows or Linux AMD64 Flutter Release directory
  --iscc       optional Inno Setup 7.1.0 ISCC executable (native Windows host)
  --help       show this help and exit without building

Distribution mode builds the target's core tools, optional AMD64 GUI payload,
and Windows installer when --iscc is supplied. Archives use gui-portable or
core-portable suffixes and include the selected version and build identifier.

Legacy release/field-validation modes build the Windows amd64 CLI pair into a
portable zip. Release contains install/uninstall scripts; field-validation adds
the guided validation tools. Every mode writes SHA256SUMS.txt. The tool does not
sign, upload or publish a release.
`)
}

// validateProductVersion accepts the canonical SemVer subset used by Sidravia
// without a leading v, build metadata, whitespace, path separators, control
// characters or non-ASCII characters.
func validateProductVersion(s string) error {
	if s == "" {
		return errors.New("version must not be empty")
	}
	if s[0] == 'v' || s[0] == 'V' {
		return errors.New("version must not have a leading v")
	}
	for _, r := range s {
		if !isVersionRune(r) {
			return fmt.Errorf("version contains invalid character %q", r)
		}
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return fmt.Errorf("version core must be MAJOR.MINOR.PATCH, got %q", core)
	}
	for _, p := range parts {
		if !isNumericNoLeadingZero(p) {
			return fmt.Errorf("version numeric segment %q must be 0 or have no leading zero", p)
		}
	}
	if hasPre {
		if pre == "" {
			return errors.New("version prerelease must not be empty")
		}
		for _, seg := range strings.Split(pre, ".") {
			if seg == "" {
				return errors.New("version prerelease segment must not be empty")
			}
			for _, r := range seg {
				if !isASCIILetter(r) && !isASCIIDigit(r) && r != '-' {
					return fmt.Errorf("version prerelease segment %q contains invalid character", seg)
				}
			}
			if allDigits(seg) && !isNumericNoLeadingZero(seg) {
				return fmt.Errorf("version numeric prerelease segment %q must not have a leading zero", seg)
			}
		}
	}
	return nil
}

// isVersionRune allows only ASCII digits, letters, dot and dash.
func isVersionRune(r rune) bool {
	return isASCIIDigit(r) || isASCIILetter(r) || r == '.' || r == '-'
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isASCIIDigit(r) {
			return false
		}
	}
	return true
}

// isNumericNoLeadingZero accepts "0" or a non-empty all-digit string with no
// leading zero.
func isNumericNoLeadingZero(s string) bool {
	if s == "" || !allDigits(s) {
		return false
	}
	return s == "0" || s[0] != '0'
}

// validateBuildID accepts a 1-64 character identifier matching
// [0-9A-Za-z][0-9A-Za-z._-]*.
func validateBuildID(s string) error {
	if len(s) < 1 || len(s) > 64 {
		return errors.New("build-id must be 1-64 characters")
	}
	for i, r := range s {
		ok := isASCIIAlnum(r) || r == '.' || r == '_' || r == '-'
		if i == 0 {
			ok = isASCIIAlnum(r)
		}
		if !ok {
			return fmt.Errorf("build-id contains invalid character %q at position %d", r, i)
		}
	}
	return nil
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }
func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}
func isASCIIAlnum(r rune) bool { return isASCIILetter(r) || isASCIIDigit(r) }

// conflictGoVars are removed case-insensitively from the parent environment
// before adding the fixed build environment.
var conflictGoVars = map[string]bool{
	"GOOS": true, "GOARCH": true, "CGO_ENABLED": true, "GOAMD64": true,
	"GOEXPERIMENT": true, "GOFIPS140": true, "GOFLAGS": true, "GOWORK": true,
	"GOENV": true, "GOTOOLCHAIN": true, "GOARM64": true,
}

// buildEnv returns a new environment derived from parent with the conflicting
// Go variables removed case-insensitively and the fixed build variables added.
// It does not modify the input slice.
func buildEnv(parent []string) []string {
	out := make([]string, 0, len(parent)+10)
	for _, kv := range parent {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			out = append(out, kv)
			continue
		}
		if conflictGoVars[strings.ToUpper(key)] {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"GOOS=windows",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
		"GOAMD64=v1",
		"GOEXPERIMENT=",
		"GOFIPS140=off",
		"GOFLAGS=",
		"GOWORK=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
	)
	return out
}

// buildArgv returns the full go build argv for one binary.
func buildArgv(goBin, outPath, pkgPath, version, buildID string) []string {
	ldflags := "-s -w -buildid= -X main.ProductVersion=" + version + " -X main.BuildID=" + buildID
	return []string{
		goBin, "build",
		"-mod=readonly",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags", ldflags,
		"-o", outPath,
		pkgPath,
	}
}

// outputFromArgv returns the path of the -o flag value in a build argv.
func outputFromArgv(argv []string) (string, error) {
	for i := 1; i < len(argv); i++ {
		if argv[i] == "-o" && i+1 < len(argv) {
			return argv[i+1], nil
		}
	}
	return "", errors.New("build argv missing -o output")
}

// verifyRepoInputs verifies the tool is running from the Sidravia repository
// root by checking go.mod and the expected local inputs.
func verifyRepoInputs(root, artifact string) error {
	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	if !moduleIsSidravia(modBytes) {
		return errors.New("go.mod does not declare module sidravia")
	}
	type inputCheck struct {
		path string
		dir  bool
	}
	checks := []inputCheck{
		{"LICENSE", false},
		{"docs/guide.md", false},
		{"scripts/install.ps1", false},
		{"scripts/uninstall.ps1", false},
		{"cmd/sidraviactl", true},
		{"cmd/sidraviad", true},
	}
	if artifact == "distribution" {
		checks = []inputCheck{
			{"LICENSE", false},
			{"cmd/sidraviactl", true},
			{"cmd/sidraviad", true},
			{"cmd/sidraviaupdater", true},
		}
	}
	if artifact == "field-validation" {
		checks = append(checks,
			inputCheck{"scripts/field-test.ps1", false},
			inputCheck{"tools/cli_smoke.ps1", false},
		)
	}
	for _, c := range checks {
		info, err := os.Stat(filepath.Join(root, c.path))
		if err != nil {
			return fmt.Errorf("repo input %s: %w", c.path, err)
		}
		if c.dir != info.IsDir() {
			kind := "directory"
			if !c.dir {
				kind = "regular file"
			}
			return fmt.Errorf("repo input %s is not a %s", c.path, kind)
		}
	}

	// Release scripts ship byte-for-byte with a UTF-8 BOM so Windows PowerShell
	// 5.1 reads their UTF-8 content correctly. Reject before building.
	var releaseScripts []string
	if artifact != "distribution" {
		releaseScripts = []string{"scripts/install.ps1", "scripts/uninstall.ps1"}
	}
	for _, script := range releaseScripts {
		b, err := os.ReadFile(filepath.Join(root, script))
		if err != nil {
			return fmt.Errorf("read %s: %w", script, err)
		}
		if !hasUTF8BOM(b) {
			return fmt.Errorf("release script %s must start with a UTF-8 BOM", script)
		}
	}
	return nil
}

// hasUTF8BOM reports whether b starts with a UTF-8 byte order mark.
func hasUTF8BOM(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}

func moduleIsSidravia(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(line[len("module "):]) == "sidravia"
		}
	}
	return false
}

// buildFunc builds one binary and returns its bytes.
type buildFunc func(env []string, argv []string) ([]byte, error)

// tool holds the configuration and injectable seams used by execute.
type tool struct {
	cfg              config
	repoRoot         string
	build            buildFunc
	packageArtifacts func(repoRoot, staging, version, buildID, artifact string, profiles []zipEntry, cliBytes, daemonBytes []byte) error
	publish          func(staging, out string) error
	runtimeVersion   func() string
	probeGoVersion   func(goBin string) (string, error)
}

func newDefaultTool(cfg config, repoRoot string) *tool {
	return &tool{
		cfg:              cfg,
		repoRoot:         repoRoot,
		build:            realBuildFunc(repoRoot),
		packageArtifacts: writeArtifacts,
		publish:          os.Rename,
		runtimeVersion:   func() string { return runtime.Version() },
		probeGoVersion:   realProbeGoVersion,
	}
}

// execute runs the full build and package transaction. It fails before any
// build or output mutation if the Go toolchain version does not match exactly.
func (t *tool) execute(stdout, stderr io.Writer) error {
	if v := t.runtimeVersion(); v != requiredGoVersion {
		return fmt.Errorf("build tool runtime go version %q is not %q", v, requiredGoVersion)
	}
	reported, err := t.probeGoVersion(t.cfg.goBin)
	if err != nil {
		return fmt.Errorf("probe go toolchain: %w", err)
	}
	if reported != requiredGoVersion {
		return fmt.Errorf("go toolchain reported %q, need %q", reported, requiredGoVersion)
	}
	if t.cfg.artifact == "distribution" {
		return t.executeDistribution(stdout, stderr)
	}
	return t.executeLegacyArchive(stdout, stderr)
}

// realBuildFunc returns a buildFunc that runs the go subprocess and reads the
// produced binary. It preserves the command cause on failure.
func realBuildFunc(repoRoot string) buildFunc {
	return func(env []string, argv []string) ([]byte, error) {
		outPath, err := outputFromArgv(argv)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = env
		cmd.Dir = repoRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("go build failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return os.ReadFile(outPath)
	}
}

func realProbeGoVersion(goBin string) (string, error) {
	out, err := exec.Command(goBin, "version").Output()
	if err != nil {
		return "", fmt.Errorf("run %s version: %w", goBin, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return "", fmt.Errorf("unexpected go version output: %q", strings.TrimSpace(string(out)))
	}
	return fields[2], nil
}
