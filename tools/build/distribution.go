package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"sidravia/internal/update/packagefiles"
)

func (t *tool) executeDistribution(stdout, stderr io.Writer) error {
	if err := verifyRepoInputs(t.repoRoot, "distribution"); err != nil {
		return err
	}
	if t.cfg.iscc != "" && runtime.GOOS != "windows" {
		return fmt.Errorf("installer compilation requires a native Windows build host")
	}
	profiles, err := loadOfficialProfileEntries(t.repoRoot)
	if err != nil {
		return err
	}
	license, err := os.ReadFile(filepath.Join(t.repoRoot, "LICENSE"))
	if err != nil {
		return err
	}
	entries := append([]archiveEntry{{"LICENSE", license, 0644}}, profiles...)
	buildInfoIndex := len(entries)
	entries = append(entries, archiveEntry{"BUILD-INFO.txt", []byte("ProductVersion: " + t.cfg.version + "\nBuildID: " + t.cfg.buildID + "\nTarget: " + t.cfg.target + "\nArtifactKind: distribution\n"), 0644})
	if t.cfg.iscc != "" {
		icon, err := os.ReadFile(filepath.Join(t.repoRoot, "gui", "windows", "runner", "resources", "app_icon.ico"))
		if err != nil {
			return fmt.Errorf("read application icon: %w", err)
		}
		entries = append(entries, archiveEntry{"app_icon.ico", icon, 0644})
	}
	var gui []archiveEntry
	if t.cfg.target == "linux-amd64" {
		gui, err = readLinuxGUI(t.cfg.guiDirectory, entries)
	} else {
		gui, err = readGUI(t.cfg.guiDirectory, entries)
	}
	if err != nil {
		return err
	}
	if t.cfg.target == "linux-amd64" && len(gui) > 0 {
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
	target := strings.Split(t.cfg.target, "-")
	env := buildEnv(os.Environ())
	for i, value := range env {
		if strings.HasPrefix(value, "GOOS=") {
			env[i] = "GOOS=" + target[0]
		}
		if strings.HasPrefix(value, "GOARCH=") {
			env[i] = "GOARCH=" + target[1]
		}
	}
	if target[1] == "arm64" {
		env = append(env, "GOARM64=v8.0")
	}
	suffix := ".exe"
	if target[0] == "linux" {
		suffix = ""
	}
	programs := []string{"sidraviactl", "sidraviad", "sidraviaupdater"}
	if target[0] == "linux" && len(gui) > 0 {
		programs = append(programs, "sidravia_gui")
	}
	if strings.HasPrefix(t.cfg.target, "windows-") {
		programs = append(programs, "sidraviamgr")
	}
	var binarySums strings.Builder
	for _, program := range programs {
		sourcePackage := "./cmd/" + program
		if program == "sidravia_gui" {
			sourcePackage = "./cmd/sidravia-gui-launcher"
		}
		fmt.Fprintln(stderr, "building", program+suffix)
		data, err := t.build(env, buildArgv(t.cfg.goBin, filepath.Join(stage, program+suffix), sourcePackage, t.cfg.version, t.cfg.buildID))
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{program + suffix, data, 0755})
		fmt.Fprintf(&binarySums, "BinarySHA256: %s  %s\n", sha256Hex(data), program+suffix)
	}
	entries[buildInfoIndex].data = append(entries[buildInfoIndex].data, []byte(binarySums.String())...)
	// The ownership index describes product bytes only: runtime data, the
	// portable marker and this index itself are deliberately outside its scope.
	payloadKind := "core"
	if len(gui) > 0 {
		payloadKind = "gui"
	}
	owned := packagefiles.Manifest{SchemaVersion: 1, Version: t.cfg.version, BuildID: t.cfg.buildID, OS: target[0], Arch: target[1], PayloadKind: payloadKind}
	for _, entry := range entries {
		mode := uint32(entry.mode.Perm())
		if target[0] == "windows" {
			mode = 0644
		} // Windows ignores Unix executable bits.
		owned.Files = append(owned.Files, packagefiles.File{Path: entry.name, Size: int64(len(entry.data)), SHA256: sha256Hex(entry.data), Mode: mode})
	}
	ownership, err := packagefiles.Encode(owned)
	if err != nil {
		return fmt.Errorf("encode product file ownership: %w", err)
	}
	entries = append(entries, archiveEntry{packagefiles.Name, ownership, 0644})
	// All installation files have explicit ownership; payload contains no
	// portable marker, generated user data or legacy integration scripts.
	payload := filepath.Join(stage, "payload")
	if err := os.Mkdir(payload, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(payload, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, entry.data, entry.mode); err != nil {
			return err
		}
	}
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
	if strings.HasPrefix(t.cfg.target, "linux-") {
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
	if t.cfg.iscc != "" {
		template, err := os.ReadFile(filepath.Join(t.repoRoot, "packaging", "windows", "sidravia.iss"))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, "sidravia.iss"), template, 0644); err != nil {
			return err
		}
		manifest := installerManifest(payload, entries, gui)
		if err := os.WriteFile(filepath.Join(stage, "files.iss"), []byte(manifest), 0644); err != nil {
			return err
		}
		hasGUI := "0"
		if len(gui) > 0 {
			hasGUI = "1"
		}
		cmd := exec.Command(t.cfg.iscc, "/DProductVersion="+t.cfg.version, "/DHasGUI="+hasGUI, "/O"+stage, "/F"+base+"-setup", filepath.Join(stage, "sidravia.iss"))
		cmd.Stdout = stderr
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("compile Inno installer: %w", err)
		}
		outputs = append(outputs, base+"-setup.exe")
	}
	final := filepath.Join(stage, "output")
	if err := os.Mkdir(final, 0755); err != nil {
		return err
	}
	sort.Strings(outputs)
	var sums strings.Builder
	for _, name := range outputs {
		data, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", sha256Hex(data), name)
		if err := os.Rename(filepath.Join(stage, name), filepath.Join(final, name)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(final, "SHA256SUMS.txt"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	if err := t.publish(final, out); err != nil {
		return err
	}
	published = true
	// Stage is owned by this invocation and contains only generated build files.
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, sums.String())
	return err
}
