package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

func (t *tool) executeLegacyArchive(stdout, stderr io.Writer) error {
	if err := verifyRepoInputs(t.repoRoot, t.cfg.artifact); err != nil {
		return err
	}
	profiles, err := loadOfficialProfileEntries(t.repoRoot)
	if err != nil {
		return fmt.Errorf("load official institution profiles: %w", err)
	}

	out, err := filepath.Abs(t.cfg.output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	out = filepath.Clean(out)
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("output path already exists: %s", out)
		}
		return fmt.Errorf("stat output path: %w", err)
	}
	parent := filepath.Dir(out)
	pinfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("output parent directory: %w", err)
	}
	if !pinfo.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", parent)
	}

	staging, err := os.MkdirTemp(parent, ".sidravia-build-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	env := buildEnv(os.Environ())
	binDir := filepath.Join(staging, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin staging: %w", err)
	}

	cliArgv := buildArgv(t.cfg.goBin, filepath.Join(binDir, "sidraviactl.exe"), "./cmd/sidraviactl", t.cfg.version, t.cfg.buildID)
	fmt.Fprintln(stderr, "building sidraviactl.exe")
	cliBytes, err := t.build(env, cliArgv)
	if err != nil {
		return fmt.Errorf("build sidraviactl.exe: %w", err)
	}
	daemonArgv := buildArgv(t.cfg.goBin, filepath.Join(binDir, "sidraviad.exe"), "./cmd/sidraviad", t.cfg.version, t.cfg.buildID)
	fmt.Fprintln(stderr, "building sidraviad.exe")
	daemonBytes, err := t.build(env, daemonArgv)
	if err != nil {
		return fmt.Errorf("build sidraviad.exe: %w", err)
	}

	fmt.Fprintln(stderr, "packaging archives")
	if err := t.packageArtifacts(t.repoRoot, staging, t.cfg.version, t.cfg.buildID, t.cfg.artifact, profiles, cliBytes, daemonBytes); err != nil {
		return err
	}
	if err := os.RemoveAll(binDir); err != nil {
		return fmt.Errorf("remove staging bin: %w", err)
	}
	listing, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("read staging: %w", err)
	}
	if len(listing) != 2 {
		return fmt.Errorf("internal error: staging has %d entries, want 2", len(listing))
	}

	fmt.Fprintln(stderr, "publishing output")
	if err := t.publish(staging, out); err != nil {
		return fmt.Errorf("publish output: %w", err)
	}
	published = true
	if err := printManifest(stdout, out, t.cfg.version, t.cfg.artifact); err != nil {
		return fmt.Errorf("print manifest: %w", err)
	}
	return nil
}

func buildInfoBytes(version, buildID, artifact string) []byte {
	return []byte("ProductVersion: " + version + "\n" +
		"BuildID: " + buildID + "\n" +
		"Target: windows/amd64\n" +
		"LayoutMode: portable\n" +
		"ArtifactKind: " + artifact + "\n")
}

func artifactFileName(version, artifact string) string {
	name := "sidravia-v" + version + "-windows-amd64-portable"
	if artifact == "field-validation" {
		name += "-field-validation"
	}
	return name + ".zip"
}

// writeArtifacts builds one selected portable zip and SHA256SUMS.txt in staging.
func writeArtifacts(repoRoot, staging, version, buildID, artifact string, profiles []zipEntry, cliBytes, daemonBytes []byte) error {
	zipName := artifactFileName(version, artifact)
	sumsName := "SHA256SUMS.txt"

	license, err := os.ReadFile(filepath.Join(repoRoot, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read LICENSE: %w", err)
	}
	gettingStarted, err := os.ReadFile(filepath.Join(repoRoot, "docs", "guide.md"))
	if err != nil {
		return fmt.Errorf("read docs/guide.md: %w", err)
	}
	installScript, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install.ps1"))
	if err != nil {
		return fmt.Errorf("read scripts/install.ps1: %w", err)
	}
	uninstallScript, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "uninstall.ps1"))
	if err != nil {
		return fmt.Errorf("read scripts/uninstall.ps1: %w", err)
	}
	var fieldTestScript, cliSmokeScript []byte
	if artifact == "field-validation" {
		fieldTestScript, err = os.ReadFile(filepath.Join(repoRoot, "scripts", "field-test.ps1"))
		if err != nil {
			return fmt.Errorf("read scripts/field-test.ps1: %w", err)
		}
		cliSmokeScript, err = os.ReadFile(filepath.Join(repoRoot, "tools", "cli_smoke.ps1"))
		if err != nil {
			return fmt.Errorf("read tools/cli_smoke.ps1: %w", err)
		}
	}

	internalSums := []byte(sha256Hex(cliBytes) + "  sidraviactl.exe\n" +
		sha256Hex(daemonBytes) + "  sidraviad.exe\n")

	entries := assembleEntries(artifact, license, gettingStarted, profiles, fieldTestScript, cliSmokeScript, installScript, uninstallScript, internalSums, cliBytes, daemonBytes, version, buildID)
	if err := writeZip(filepath.Join(staging, zipName), entries); err != nil {
		return fmt.Errorf("write %s: %w", zipName, err)
	}

	zipBytes, err := os.ReadFile(filepath.Join(staging, zipName))
	if err != nil {
		return fmt.Errorf("read %s: %w", zipName, err)
	}
	externalSums := []byte(sha256Hex(zipBytes) + "  " + zipName + "\n")
	if err := os.WriteFile(filepath.Join(staging, sumsName), externalSums, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", sumsName, err)
	}
	return nil
}

func assembleEntries(artifact string, license, gettingStarted []byte, profiles []zipEntry, fieldTestScript, cliSmokeScript, installScript, uninstallScript, internalSums, cliBytes, daemonBytes []byte, version, buildID string) []zipEntry {
	entries := []zipEntry{
		{"BUILD-INFO.txt", buildInfoBytes(version, buildID, artifact), 0o644},
		{"GETTING-STARTED.md", gettingStarted, 0o644},
		{"LICENSE", license, 0o644},
		{"SHA256SUMS", internalSums, 0o644},
		{"sidraviactl.exe", cliBytes, 0o755},
		{"sidravia.portable", nil, 0o644},
		{"sidraviad.exe", daemonBytes, 0o755},
	}
	entries = append(entries, profiles...)
	entries = append(entries, zipEntry{"scripts/install.ps1", installScript, 0o644})
	entries = append(entries, zipEntry{"scripts/uninstall.ps1", uninstallScript, 0o644})
	if artifact == "field-validation" {
		entries = append(entries, zipEntry{"scripts/field-test.ps1", fieldTestScript, 0o644})
		entries = append(entries, zipEntry{"scripts/cli-smoke.ps1", cliSmokeScript, 0o644})
	}
	return entries
}

// printManifest prints the selected zip and sums file in lexicographic order.
func printManifest(stdout io.Writer, out, version, artifact string) error {
	names := []string{
		artifactFileName(version, artifact),
		"SHA256SUMS.txt",
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s  %s\n", sha256Hex(b), name)
	}
	return nil
}
