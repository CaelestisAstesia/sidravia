package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	packagefiles "sidravia/internal/release/files"
)

type closedOutput struct{}

func (closedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestThirdPartyNoticesCoverPinnedGoDependencies(t *testing.T) {
	root := filepath.Join("..", "..")
	notices, err := os.ReadFile(filepath.Join(root, "THIRD-PARTY-NOTICES.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.Split(string(notices), "\n===============================================================================\n")
	if !strings.Contains(sections[0], "\nGo "+requiredGoVersion+"\n") {
		t.Fatalf("third-party notices do not identify the standard library for %s", requiredGoVersion)
	}
	byModule := map[string]string{}
	for _, section := range sections[1:] {
		for _, line := range strings.Split(section, "\n") {
			if identity, ok := strings.CutPrefix(line, "Module: "); ok {
				if _, exists := byModule[identity]; exists {
					t.Fatalf("duplicate notice for %s", identity)
				}
				byModule[identity] = section
			}
		}
	}

	modules := map[string]string{}
	for osName, platformCommand := range map[string]string{
		"linux": "./cmd/sidravia-gui-launcher", "windows": "./cmd/sidraviamgr",
	} {
		for _, arch := range []string{"amd64", "arm64"} {
			cmd := exec.Command("go", "list", "-mod=readonly", "-deps", "-f",
				"{{with .Module}}{{if .Version}}{{.Path}}@{{.Version}}\t{{.Dir}}{{end}}{{end}}",
				"./cmd/sidraviactl", "./cmd/sidraviad", platformCommand)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOOS="+osName, "GOARCH="+arch, "CGO_ENABLED=0")
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("list dependencies for %s/%s: %v", osName, arch, err)
			}
			for _, line := range strings.Split(string(output), "\n") {
				if identity, dir, ok := strings.Cut(line, "\t"); ok {
					if !filepath.IsAbs(dir) {
						t.Fatalf("module %s has no resolved source directory", identity)
					}
					modules[identity] = dir
				}
			}
		}
	}
	want := slices.Sorted(maps.Keys(modules))
	got := slices.Sorted(maps.Keys(byModule))
	if !slices.Equal(want, got) {
		t.Fatalf("third-party notice modules differ from the release dependencies\nwant: %v\ngot:  %v", want, got)
	}
	for identity, dir := range modules {
		verifyNoticeTexts(t, identity, dir, byModule[identity])
	}
	// Check the standard library's license and patent grant.
	for _, name := range []string{"LICENSE", "PATENTS"} {
		verifyNoticeText(t, requiredGoVersion, runtime.GOROOT(), name, sections[0])
	}
}

func verifyNoticeTexts(t *testing.T, identity, dir, section string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := file.Name()
		if !file.IsDir() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "NOTICE") ||
			strings.HasPrefix(name, "PATENTS") || strings.HasPrefix(name, "COPYING")) {
			verifyNoticeText(t, identity, dir, name, section)
		}
	}
}

func verifyNoticeText(t *testing.T, identity, dir, name, section string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(section, "\n--- "+name+" ---\n\n"+string(data)) {
		t.Errorf("%s notice is missing or differs from source: %s", identity, name)
	}
}

func TestDistributionReportsPublishedOutputWhenChecksumSummaryFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", target: "linux-amd64", output: out}
	err := newFakeTool(cfg, setupRepoRoot(t), &fakeBuilder{}).execute(closedOutput{}, io.Discard)
	if !errors.Is(err, io.ErrClosedPipe) || !strings.Contains(err.Error(), "distribution published at "+out) {
		t.Fatalf("expected error identifying the published distribution, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "SHA256SUMS.txt")); err != nil {
		t.Fatalf("published checksum file missing: %v", err)
	}
}

func TestDistributionPreservesAnEmptyDestinationCreatedDuringBuild(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", target: "linux-amd64", output: out}
	distribution := newFakeTool(cfg, setupRepoRoot(t), &fakeBuilder{})
	distribution.publish = func(stage, destination string) error {
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		return publishDirectory(stage, destination)
	}
	if err := distribution.execute(io.Discard, io.Discard); err == nil {
		t.Fatal("replaced an empty output created by another caller")
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 0 {
		t.Fatalf("contending directory did not remain intact: %v, %v", entries, err)
	}
}

func TestDistributionFlags(t *testing.T) {
	base := []string{"--version", "0.3.0-alpha.1", "--build-id", "test", "--output", "out"}
	for _, target := range []string{"windows-amd64", "linux-amd64", "windows-arm64", "linux-arm64"} {
		tail := []string{"--artifact", "distribution", "--target", target, "--gui-directory", "gui"}
		if _, err := parseFlags(append(append([]string(nil), base...), tail...)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tail := range [][]string{{"--target", "unsupported"}, {"--artifact", "distribution"}, {"--artifact", "distribution", "--target", "linux-arm64", "--ifw", "x"}} {
		if _, err := parseFlags(append(append([]string(nil), base...), tail...)); err == nil {
			t.Fatal("accepted invalid target combination")
		}
	}
}
func TestDistributionContents(t *testing.T) {
	t.Setenv("GOARM64", "v9.0,lse,crypto")
	for _, target := range []string{"windows-amd64", "linux-amd64", "windows-arm64", "linux-arm64"} {
		t.Run(target, func(t *testing.T) {
			root := setupRepoRoot(t)
			fb := &fakeBuilder{}
			out := filepath.Join(t.TempDir(), "dist")
			cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", target: target, output: out}
			if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			want := 2
			if strings.HasPrefix(target, "windows-") {
				want = 3
			}
			if len(fb.calls) != want {
				t.Fatalf("builds=%d", len(fb.calls))
			}
			for _, env := range fb.envs {
				if strings.HasSuffix(target, "-arm64") && !strings.Contains(strings.Join(env, "\n"), "GOARM64=v8.0") {
					t.Fatal("ARM baseline was not fixed")
				}
				if !strings.Contains(strings.Join(env, "\n"), "GOOS="+strings.Split(target, "-")[0]) || !strings.Contains(strings.Join(env, "\n"), "GOARCH="+strings.Split(target, "-")[1]) {
					t.Fatal("wrong target env")
				}
			}
			contents := map[string]bool{}
			bytesByName := map[string][]byte{}
			if strings.HasPrefix(target, "windows-") {
				bytesByName = readZipContents(t, filepath.Join(out, "sidravia-v0.3.0-alpha.1-"+target+"-core-portable.zip"))
				for name := range bytesByName {
					contents[name] = true
				}
				if !contents["sidraviamgr.exe"] || contents["sidraviaupdater.exe"] || !contents["sidraviactl.exe"] || !contents["sidraviad.exe"] || contents["app_icon.ico"] {
					t.Fatal("missing manager or core")
				}
			} else {
				f, err := os.Open(filepath.Join(out, "sidravia-v0.3.0-alpha.1-"+target+"-core-portable.tar.gz"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				gz, err := gzip.NewReader(f)
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				tr := tar.NewReader(gz)
				for {
					h, err := tr.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					name := strings.TrimPrefix(h.Name, "sidravia-v0.3.0-alpha.1-"+target+"/")
					contents[name] = true
					data, err := io.ReadAll(tr)
					if err != nil {
						t.Fatal(err)
					}
					bytesByName[name] = data
					if name == "sidraviactl" || name == "sidraviad" {
						if h.Mode != 0755 {
							t.Fatal("lost executable mode")
						}
					}
				}
				if !contents["sidraviactl"] || !contents["sidraviad"] || contents["sidraviaupdater"] || contents["sidraviamgr"] || contents["Sidravia.exe"] {
					t.Fatal("Linux program set")
				}
			}
			if !contents["sidravia.portable"] || !contents["institution-profiles/jlu.json"] || contents["scripts/install.ps1"] {
				t.Fatal("distribution content boundary")
			}
			if string(bytesByName["THIRD-PARTY-NOTICES.txt"]) != "third-party notices\n" {
				t.Fatal("distribution is missing its source third-party notices")
			}
			info := "ProductVersion: " + cfg.version + "\nBuildID: " + cfg.buildID + "\nTarget: " + target + "\nArtifactKind: distribution\n"
			names := []string{"sidraviactl", "sidraviad"}
			if strings.HasPrefix(target, "windows-") {
				names = []string{"sidraviactl.exe", "sidraviad.exe", "sidraviamgr.exe"}
			}
			for _, name := range names {
				info += "BinarySHA256: " + sha256Hex(bytesByName[name]) + "  " + name + "\n"
			}
			if string(bytesByName["BUILD-INFO.txt"]) != info {
				t.Fatal("binary provenance does not match actual archive bytes")
			}
			ownership, err := packagefiles.Decode(bytesByName[packagefiles.Name])
			if err != nil || ownership.Version != cfg.version || ownership.BuildID != cfg.buildID || len(ownership.Files) != len(bytesByName)-2 {
				t.Fatalf("ownership does not cover exact product bytes: %+v %v", ownership, err)
			}
			for _, file := range ownership.Files {
				data, found := bytesByName[file.Path]
				if !found || int64(len(data)) != file.Size || sha256Hex(data) != file.SHA256 {
					t.Fatal("ownership hash/size mismatch", file.Path)
				}
			}
			if _, err := os.Stat(filepath.Join(out, "SHA256SUMS.txt")); err != nil {
				t.Fatal(err)
			}
			if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err == nil {
				t.Fatal("overwrote output")
			}
		})
	}
}
func TestGUIPayloadRejectsLinksAndCollisions(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Sidravia.exe"), testPE(0x8664))
	writeTestFile(t, filepath.Join(root, "flutter_windows.dll"), testPE(0x8664))
	for _, name := range []string{"msvcp140.dll", "vcruntime140.dll", "vcruntime140_1.dll"} {
		writeTestFile(t, filepath.Join(root, name), testPE(0x8664))
	}
	for _, name := range []string{"data/icudtl.dat", "data/flutter_assets/AssetManifest.bin", "data/flutter_assets/NOTICES.Z"} {
		writeTestFile(t, filepath.Join(root, name), "runtime")
	}
	writeTestFile(t, filepath.Join(root, "data/app.so"), testELF(62))
	writeTestFile(t, filepath.Join(root, "data/flutter_assets/FontManifest.json"), "[]")
	gui, err := readGUI(root, nil, "amd64")
	if err != nil || len(gui) != 10 {
		t.Fatal(err)
	}
	if _, err := readGUI(root, nil, "arm64"); err == nil {
		t.Fatal("accepted x64 GUI in an ARM64 distribution")
	}
	writeTestFile(t, filepath.Join(root, "Sidravia.exe"), testPE(0xaa64))
	writeTestFile(t, filepath.Join(root, "flutter_windows.dll"), testPE(0xaa64))
	for _, name := range []string{"msvcp140.dll", "vcruntime140.dll", "vcruntime140_1.dll"} {
		writeTestFile(t, filepath.Join(root, name), testPE(0xaa64))
	}
	writeTestFile(t, filepath.Join(root, "data/app.so"), testELF(183))
	if _, err := readGUI(root, nil, "arm64"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "vcruntime140_1.dll"), testPE(0x8664))
	if _, err := readGUI(root, nil, "arm64"); err == nil {
		t.Fatal("accepted x64 VC++ runtime in an ARM64 distribution")
	}
	if err := os.Remove(filepath.Join(root, "vcruntime140_1.dll")); err != nil {
		t.Fatal(err)
	}
	if _, err := readGUI(root, nil, "arm64"); err != nil {
		t.Fatalf("ARM64 does not require the x64-compatible exception DLL: %v", err)
	}
	for _, name := range []string{"msvcp140.dll", "vcruntime140.dll"} {
		writeTestFile(t, filepath.Join(root, name), testPE(0x8664))
	}
	writeTestFile(t, filepath.Join(root, "Sidravia.exe"), testPE(0x8664))
	writeTestFile(t, filepath.Join(root, "flutter_windows.dll"), testPE(0x8664))
	writeTestFile(t, filepath.Join(root, "data/app.so"), testELF(62))
	if _, err := readGUI(root, nil, "amd64"); err == nil {
		t.Fatal("accepted a missing x64 VC++ exception runtime")
	}
	writeTestFile(t, filepath.Join(root, "vcruntime140_1.dll"), testPE(0x8664))
	writeTestFile(t, filepath.Join(root, "sidraviactl.exe"), "collision")
	if _, err := readGUI(root, nil, "amd64"); err == nil {
		t.Fatal("accepted collision")
	}
	if err := os.Remove(filepath.Join(root, "sidraviactl.exe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "Sidravia.exe"), filepath.Join(root, "link.exe")); err == nil {
		if _, err := readGUI(root, nil, "amd64"); err == nil {
			t.Fatal("accepted link")
		}
	}
}

func TestDistributionDoesNotDependOnLegacyWindowsScripts(t *testing.T) {
	root := setupRepoRoot(t)
	for _, name := range []string{"scripts", "docs"} {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	fb := &fakeBuilder{}
	cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", target: "linux-amd64", output: filepath.Join(t.TempDir(), "out")}
	if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(fb.calls) != 2 {
		t.Fatalf("core builds = %d", len(fb.calls))
	}
}

func TestGUIPayloadRejectsUnusedBuildAndWebContent(t *testing.T) {
	missingFont := []archiveEntry{{name: "data/flutter_assets/FontManifest.json", data: []byte(`[{"fonts":[{"asset":"assets/fonts/missing.otf"}]}]`)}}
	if err := checkGUIRuntime(missingFont, true); err == nil {
		t.Fatal("accepted missing declared font")
	}
	for _, windows := range []bool{false, true} {
		for _, name := range []string{"debug.pdb", "data/build.log", "data/flutter_assets/AssetManifest.bin.json", "data/flutter_assets/NOTICES", "data/flutter_assets/assets/old.ttf", "licenses/.DS_Store", "lib/notes.txt", "libdartjni.dll", "lib/libdartjni.so"} {
			t.Run(fmt.Sprintf("windows=%t/%s", windows, name), func(t *testing.T) {
				entries := []archiveEntry{{name: "data/flutter_assets/FontManifest.json", data: []byte("[]")}, {name: name, data: []byte("unused")}}
				if err := checkGUIRuntime(entries, windows); err == nil {
					t.Fatalf("accepted non-runtime input: %s", name)
				}
			})
		}
	}
	for _, windows := range []bool{false, true} {
		library := "lib/plugin.so"
		if windows {
			library = "plugin.dll"
		}
		entries := []archiveEntry{
			{name: library},
			{name: "licenses/plugin/LICENSE.txt"},
			{name: "data/flutter_assets/FontManifest.json", data: []byte(`[{"fonts":[{"asset":"assets/fonts/code.otf"}]}]`)},
			{name: "data/flutter_assets/assets/fonts/code.otf"},
		}
		if err := checkGUIRuntime(entries, windows); err != nil {
			t.Fatal(err)
		}
	}
}

func testPE(machine uint16) string {
	header := make([]byte, 90)
	copy(header, "MZ")
	header[60] = 64
	copy(header[64:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(header[68:], machine)
	binary.LittleEndian.PutUint16(header[88:], 0x20b)
	return string(header)
}

func testELF(machine uint16) string {
	header := make([]byte, 20)
	copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1})
	binary.LittleEndian.PutUint16(header[18:], machine)
	return string(header)
}

func TestLinuxGUIBoundary(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"sidravia_gui", "lib/libflutter_linux_gtk.so", "lib/libapp.so", "data/icudtl.dat", "data/flutter_assets/NOTICES.Z", "data/flutter_assets/version.json", "data/icons/sidravia.png", "FLUTTER-LICENSE"} {
		data := name
		if name == "sidravia_gui" || strings.HasSuffix(name, ".so") {
			data = testELF(62)
		}
		writeTestFile(t, filepath.Join(root, name), data)
	}
	entries, err := readLinuxGUI(root, nil, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.name == "gui-runtime/sidravia_gui" && e.mode != 0755 {
			t.Fatal("GUI not executable")
		}
	}
	if _, err := readLinuxGUI(root, nil, "arm64"); err == nil {
		t.Fatal("accepted x64 ELF runner in ARM64 distribution")
	}
	armPaths := []string{"sidravia_gui", "lib/libflutter_linux_gtk.so", "lib/libapp.so"}
	for _, name := range armPaths {
		writeTestFile(t, filepath.Join(root, name), testELF(183))
	}
	if _, err := readLinuxGUI(root, nil, "arm64"); err != nil {
		t.Fatal(err)
	}
	for _, name := range armPaths {
		writeTestFile(t, filepath.Join(root, name), testELF(62))
	}
	writeTestFile(t, filepath.Join(root, "config/configurations.json"), "private")
	if _, err := readLinuxGUI(root, nil, "amd64"); err == nil {
		t.Fatal("accepted user state")
	}
	if err := os.RemoveAll(filepath.Join(root, "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "lib/libapp.so")); err != nil {
		t.Fatal(err)
	}
	if _, err := readLinuxGUI(root, nil, "amd64"); err == nil {
		t.Fatal("accepted incomplete release")
	}
}

func TestInstallerAndPortableHaveIndependentExactInventories(t *testing.T) {
	owned := packagefiles.Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "test", OS: "windows", Arch: "amd64", PayloadKind: "gui"}
	entries := []archiveEntry{{"Sidravia.exe", []byte("gui"), 0755}, {"sidraviaupdater.exe", []byte("installer only"), 0644}}
	for _, entry := range entries {
		owned.Files = append(owned.Files, packagefiles.File{Path: entry.name, Size: int64(len(entry.data)), SHA256: sha256Hex(entry.data), Mode: 0644})
	}
	raw, err := packagefiles.Encode(owned)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, archiveEntry{packagefiles.Name, raw, 0644})
	portable, err := portablePayload(entries, owned)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range portable {
		files[entry.name] = entry.data
	}
	if _, exists := files["sidraviaupdater.exe"]; exists {
		t.Fatal("portable retained installed updater")
	}
	index, err := packagefiles.Decode(files[packagefiles.Name])
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range index.Files {
		b, exists := files[f.Path]
		if !exists || sha256Hex(b) != f.SHA256 {
			t.Fatal("portable index declares absent/wrong bytes", f.Path)
		}
	}
	if len(index.Files) != len(files)-2 {
		t.Fatal("portable inventory incomplete")
	}
	setup, err := packagefiles.Decode(raw)
	if err != nil || len(setup.Files) != 2 {
		t.Fatal("setup inventory changed", err)
	}
	if string(raw) == string(files[packagefiles.Name]) {
		t.Fatal("different product formats shared one inventory")
	}
	if len(owned.Files) != 2 {
		t.Fatal("portable generation mutated setup inventory")
	}
}
