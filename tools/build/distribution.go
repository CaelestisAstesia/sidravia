package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func (t *tool) executeDistribution(stdout, stderr io.Writer) error {
	if err := verifyRepoInputs(t.repoRoot, "release"); err != nil {
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
	entries := append([]zipEntry{{"LICENSE", license, 0644}}, profiles...)
	buildInfoIndex := len(entries)
	entries = append(entries, zipEntry{"BUILD-INFO.txt", []byte("ProductVersion: " + t.cfg.version + "\nBuildID: " + t.cfg.buildID + "\nTarget: " + t.cfg.target + "\nArtifactKind: distribution\n"), 0644})
	if t.cfg.target == "windows-amd64" {
		icon, err := os.ReadFile(filepath.Join(t.repoRoot, "gui", "windows", "runner", "resources", "app_icon.ico"))
		if err != nil {
			return fmt.Errorf("read application icon: %w", err)
		}
		entries = append(entries, zipEntry{"app_icon.ico", icon, 0644})
	}
	gui, err := readGUI(t.cfg.guiDirectory, entries)
	if err != nil {
		return err
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
	suffix := ".exe"
	if t.cfg.target == "linux-amd64" {
		suffix = ""
		for i, v := range env {
			if v == "GOOS=windows" {
				env[i] = "GOOS=linux"
			}
		}
	}
	programs := []struct{ name, pkg string }{{"sidraviactl", "./cmd/sidravia"}, {"sidraviad", "./cmd/sidraviad"}}
	if t.cfg.target == "windows-amd64" {
		programs = append(programs, struct{ name, pkg string }{"sidraviamgr", "./cmd/sidraviamgr"})
	}
	var binarySums strings.Builder
	for _, program := range programs {
		fmt.Fprintln(stderr, "building", program.name+suffix)
		data, err := t.build(env, buildArgv(t.cfg.goBin, filepath.Join(stage, program.name+suffix), program.pkg, t.cfg.version, t.cfg.buildID))
		if err != nil {
			return err
		}
		entries = append(entries, zipEntry{program.name + suffix, data, 0755})
		fmt.Fprintf(&binarySums, "BinarySHA256: %s  %s\n", sha256Hex(data), program.name+suffix)
	}
	entries[buildInfoIndex].data = append(entries[buildInfoIndex].data, []byte(binarySums.String())...)
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
	portable := append(append([]zipEntry(nil), entries...), zipEntry{"sidravia.portable", nil, 0644})
	base := "sidravia-v" + t.cfg.version + "-" + t.cfg.target
	archive := base + "-portable.zip"
	if t.cfg.target == "linux-amd64" {
		archive = base + "-portable.tar.gz"
		if err := writeTar(filepath.Join(stage, archive), base, portable); err != nil {
			return err
		}
	} else if err := writeZip(filepath.Join(stage, archive), portable); err != nil {
		return err
	}
	outputs := []string{archive}
	if t.cfg.iscc != "" {
		template, err := os.ReadFile(filepath.Join(t.repoRoot, "tools", "installer", "sidravia.iss"))
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

func readGUI(root string, existing []zipEntry) ([]zipEntry, error) {
	if root == "" {
		return nil, nil
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("GUI directory must be absolute")
	}
	reserved := map[string]bool{"sidravia.portable": true, "sidraviactl.exe": true, "sidraviad.exe": true, "sidraviamgr.exe": true}
	for _, entry := range existing {
		reserved[strings.ToLower(entry.name)] = true
	}
	var entries []zipEntry
	err := filepath.WalkDir(root, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("GUI payload contains a link")
		}
		if info.IsDir() {
			return nil
		}
		metadata, err := info.Info()
		if err != nil {
			return err
		}
		if !metadata.Mode().IsRegular() {
			return fmt.Errorf("GUI payload contains a non-regular file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if reserved[strings.ToLower(name)] || strings.ContainsAny(name, "\"\r\n;{}") {
			return fmt.Errorf("GUI payload name conflicts with installation content: %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, zipEntry{name, data, metadata.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.name] = true
	}
	if !found["Sidravia.exe"] || !found["flutter_windows.dll"] {
		return nil, fmt.Errorf("GUI payload lacks required Release files")
	}
	return entries, nil
}

func installerManifest(root string, entries, gui []zipEntry) string {
	guiNames := map[string]bool{}
	for _, entry := range gui {
		guiNames[entry.name] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var out strings.Builder
	out.WriteString("[Files]\n")
	for _, entry := range entries {
		component := "core"
		if guiNames[entry.name] {
			component = "gui"
		}
		dir := filepath.Dir(filepath.FromSlash(entry.name))
		dest := "{app}"
		if dir != "." {
			dest += "\\" + dir
		}
		fmt.Fprintf(&out, "Source: \"%s\"; DestDir: \"%s\"; Components: %s; Flags: ignoreversion\n", filepath.Join(root, filepath.FromSlash(entry.name)), dest, component)
	}
	if len(gui) > 0 {
		out.WriteString("\n[InstallDelete]\n")
		for _, entry := range gui {
			fmt.Fprintf(&out, "Type: files; Name: \"{app}\\%s\"; Check: RemoveGUIComponent\n", filepath.FromSlash(entry.name))
		}
	}
	return out.String()
}

func writeTar(path, root string, entries []zipEntry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: root + "/" + entry.name, Mode: int64(entry.mode.Perm()), Size: int64(len(entry.data)), ModTime: zipModTime}); err != nil {
			return err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
