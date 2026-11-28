package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	packagefiles "sidravia/internal/release/files"
)

func (t *tool) executeDistribution(stdout, stderr io.Writer) error {
	if err := verifyRepoInputs(t.repoRoot, "distribution"); err != nil {
		return err
	}
	if t.cfg.ifw != "" && runtime.GOOS != "windows" {
		return fmt.Errorf("installer compilation requires a native Windows build host")
	}
	profiles, err := loadOfficialProfileEntries(t.repoRoot)
	if err != nil {
		return err
	}
	var entries []archiveEntry
	for _, name := range []string{"LICENSE", "THIRD-PARTY-NOTICES.txt"} {
		data, err := os.ReadFile(filepath.Join(t.repoRoot, name))
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{name, data, 0644})
	}
	entries = append(entries, profiles...)
	buildInfoIndex := len(entries)
	entries = append(entries, archiveEntry{"BUILD-INFO.txt", []byte("ProductVersion: " + t.cfg.version + "\nBuildID: " + t.cfg.buildID + "\nTarget: " + t.cfg.target + "\nArtifactKind: distribution\n"), 0644})
	targetOS, arch, _ := strings.Cut(t.cfg.target, "-")
	var gui []archiveEntry
	if targetOS == "linux" {
		gui, err = readLinuxGUI(t.cfg.guiDirectory, entries, arch)
	} else {
		gui, err = readGUI(t.cfg.guiDirectory, entries, arch)
	}
	if err != nil {
		return err
	}
	if targetOS == "linux" && len(gui) > 0 {
		var version map[string]interface{}
		for _, entry := range gui {
			if entry.name == "gui-runtime/data/flutter_assets/version.json" {
				err = json.Unmarshal(entry.data, &version)
				if err != nil {
					return fmt.Errorf("invalid Linux GUI version: %w", err)
				}
			}
		}
		if version["version"] != t.cfg.version || version["app_name"] != "sidravia_gui" {
			return fmt.Errorf("Linux GUI version does not match distribution")
		}
	}
	entries = append(entries, gui...)
	out, err := filepath.Abs(t.cfg.output)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return fmt.Errorf("output must not exist: %s", out)
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), ".sidravia-distribution-")
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stage)
		}
	}()
	env := buildEnv(os.Environ())
	for i, value := range env {
		if strings.HasPrefix(value, "GOOS=") {
			env[i] = "GOOS=" + targetOS
		}
		if strings.HasPrefix(value, "GOARCH=") {
			env[i] = "GOARCH=" + arch
		}
	}
	if arch == "arm64" {
		env = append(env, "GOARM64=v8.0")
	}
	suffix := ".exe"
	if targetOS == "linux" {
		suffix = ""
	}
	programs := []string{"sidraviactl", "sidraviad"}
	if targetOS == "linux" && len(gui) > 0 {
		programs = append(programs, "sidravia_gui")
	}
	if targetOS == "windows" {
		programs = append(programs, "sidraviamgr")
	}
	var qtTemplate []byte
	var qtSymbols []string
	if t.cfg.ifw != "" {
		qtTemplate, err = os.ReadFile(filepath.Join(t.cfg.ifw, "installerbase.exe"))
		if err != nil {
			return fmt.Errorf("read Qt maintenance image: %w", err)
		}
		qtSymbols = []string{"sidravia/internal/update/internal/request.QtMaintenanceDigest=" + sha256Hex(qtTemplate)}
	}
	var binarySums strings.Builder
	for _, program := range programs {
		sourcePackage := "./cmd/" + program
		if program == "sidravia_gui" {
			sourcePackage = "./cmd/sidravia-gui-launcher"
		}
		fmt.Fprintln(stderr, "building", program+suffix)
		data, err := t.build(env, buildArgv(t.cfg.goBin, filepath.Join(stage, program+suffix), sourcePackage, t.cfg.version, t.cfg.buildID, qtSymbols...))
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{program + suffix, data, 0755})
		fmt.Fprintf(&binarySums, "BinarySHA256: %s  %s\n", sha256Hex(data), program+suffix)
	}
	if t.cfg.ifw != "" {
		const updater = "sidraviaupdater.exe"
		fmt.Fprintln(stderr, "building", updater)
		data, err := t.build(env, buildArgv(t.cfg.goBin, filepath.Join(stage, updater), "./cmd/sidraviaupdater", t.cfg.version, t.cfg.buildID, qtSymbols...))
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{updater, data, 0755})
	}
	entries[buildInfoIndex].data = append(entries[buildInfoIndex].data, []byte(binarySums.String())...)
	// The ownership index describes product bytes only: runtime data, the
	// portable marker and this index itself are deliberately outside its scope.
	payloadKind := "core"
	if len(gui) > 0 {
		payloadKind = "gui"
	}
	owned := packagefiles.Manifest{SchemaVersion: 1, Version: t.cfg.version, BuildID: t.cfg.buildID, OS: targetOS, Arch: arch, PayloadKind: payloadKind}
	for _, entry := range entries {
		mode := uint32(entry.mode.Perm())
		if targetOS == "windows" {
			mode = 0644
		} // Windows ignores Unix executable bits.
		owned.Files = append(owned.Files, packagefiles.File{Path: entry.name, Size: int64(len(entry.data)), SHA256: sha256Hex(entry.data), Mode: mode})
	}
	ownership, err := packagefiles.Encode(owned)
	if err != nil {
		return fmt.Errorf("encode product file ownership: %w", err)
	}
	entries = append(entries, archiveEntry{packagefiles.Name, ownership, 0644})
	portable, err := portablePayload(entries, owned)
	if err != nil {
		return err
	}
	base := "sidravia-v" + t.cfg.version + "-" + t.cfg.target
	kind := "core-portable"
	if len(gui) > 0 {
		kind = "gui-portable"
	}
	archive := base + "-" + kind + ".zip"
	if targetOS == "linux" {
		archive = base + "-" + kind + ".tar.gz"
		if t.cfg.compression == "xz" {
			archive = base + "-" + kind + ".tar.xz"
		}
		write := writeTar
		if t.cfg.compression == "xz" {
			write = writeTarXZ
		}
		if err := write(filepath.Join(stage, archive), base, portable); err != nil {
			return err
		}
	} else if err := writeZip(filepath.Join(stage, archive), portable); err != nil {
		return err
	}
	outputs := []string{archive}
	var qtOutputs []string
	if t.cfg.ifw != "" {
		qtOutputs, err = buildQtDistribution(t.cfg, stage, base, qtTemplate, entries, gui, stderr)
		if err != nil {
			return err
		}
	}
	final := filepath.Join(stage, "output")
	portableDirectory := final
	if len(qtOutputs) > 0 {
		if err := os.Mkdir(final, 0755); err != nil {
			return err
		}
		portableDirectory = filepath.Join(final, "portable")
	}
	sums, err := writeDistributionFiles(stage, portableDirectory, outputs)
	if err != nil {
		return err
	}
	if len(qtOutputs) > 0 {
		if _, err := writeDistributionFiles(stage, filepath.Join(final, "qt"), qtOutputs); err != nil {
			return err
		}
	}
	if err := t.publish(final, out); err != nil {
		return fmt.Errorf("publish distribution to %s: %w", out, err)
	}
	published = true
	// Stage is owned by this invocation and contains only generated build files.
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("distribution published at %s; remove staging directory %s: %w", out, stage, err)
	}
	if _, err := io.WriteString(stdout, sums); err != nil {
		return fmt.Errorf("distribution published at %s; write checksum summary: %w", out, err)
	}
	return nil
}

func writeDistributionFiles(stage, directory string, outputs []string) (string, error) {
	if err := os.Mkdir(directory, 0755); err != nil {
		return "", err
	}
	sort.Strings(outputs)
	var sums strings.Builder
	for _, name := range outputs {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(data), name)
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(directory, name)); err != nil {
			return "", err
		}
	}
	return sums.String(), os.WriteFile(filepath.Join(directory, "SHA256SUMS.txt"), []byte(sums.String()), 0644)
}
