package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/update/packagefiles"
)

func TestDistributionFlags(t *testing.T) {
	base := []string{"--version", "0.3.0-alpha.1", "--build-id", "test", "--output", "out"}
	for _, target := range []string{"windows-amd64", "linux-amd64", "windows-arm64", "linux-arm64"} {
		tail := []string{"--artifact", "distribution", "--target", target}
		if _, err := parseFlags(append(append([]string(nil), base...), tail...)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tail := range [][]string{{"--target", "windows-amd64"}, {"--artifact", "distribution"}, {"--artifact", "distribution", "--target", "windows-arm64", "--gui-directory", "x"}, {"--artifact", "distribution", "--target", "windows-arm64", "--iscc", "x"}, {"--artifact", "distribution", "--target", "linux-arm64", "--iscc", "x"}} {
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
			cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", artifact: "distribution", target: target, output: out}
			if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			want := 3
			if strings.HasPrefix(target, "windows-") {
				want = 4
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
				if !contents["sidraviamgr.exe"] || !contents["sidraviaupdater.exe"] || !contents["sidraviactl.exe"] || !contents["sidraviad.exe"] || contents["app_icon.ico"] {
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
				if !contents["sidraviactl"] || !contents["sidraviad"] || !contents["sidraviaupdater"] || contents["sidraviamgr"] || contents["Sidravia.exe"] {
					t.Fatal("Linux program set")
				}
			}
			if !contents["sidravia.portable"] || !contents["institution-profiles/jlu.json"] || contents["scripts/install.ps1"] {
				t.Fatal("distribution content boundary")
			}
			info := "ProductVersion: " + cfg.version + "\nBuildID: " + cfg.buildID + "\nTarget: " + target + "\nArtifactKind: distribution\n"
			names := []string{"sidraviactl", "sidraviad", "sidraviaupdater"}
			if strings.HasPrefix(target, "windows-") {
				names = []string{"sidraviactl.exe", "sidraviad.exe", "sidraviaupdater.exe", "sidraviamgr.exe"}
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
	writeTestFile(t, filepath.Join(root, "Sidravia.exe"), "gui")
	writeTestFile(t, filepath.Join(root, "flutter_windows.dll"), "dll")
	for _, name := range []string{"data/app.so", "data/icudtl.dat", "data/flutter_assets/AssetManifest.bin", "data/flutter_assets/NOTICES.Z"} {
		writeTestFile(t, filepath.Join(root, name), "runtime")
	}
	writeTestFile(t, filepath.Join(root, "data/flutter_assets/FontManifest.json"), "[]")
	gui, err := readGUI(root, nil)
	if err != nil || len(gui) != 7 {
		t.Fatal(err)
	}
	manifest := installerManifest(filepath.Join(t.TempDir(), "payload"), append([]archiveEntry{{"sidraviamgr.exe", []byte("manager"), 0755}}, gui...), gui)
	if !strings.Contains(manifest, "Components: core") || !strings.Contains(manifest, "Components: gui") || !strings.Contains(manifest, "Check: RemoveGUIComponent") {
		t.Fatal("component ownership missing")
	}
	writeTestFile(t, filepath.Join(root, "sidraviactl.exe"), "collision")
	if _, err := readGUI(root, nil); err == nil {
		t.Fatal("accepted collision")
	}
	if err := os.Remove(filepath.Join(root, "sidraviactl.exe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "Sidravia.exe"), filepath.Join(root, "link.exe")); err == nil {
		if _, err := readGUI(root, nil); err == nil {
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
	cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", artifact: "distribution", target: "linux-amd64", output: filepath.Join(t.TempDir(), "out")}
	if err := newFakeTool(cfg, root, fb).execute(io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(fb.calls) != 3 {
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

func TestLinuxGUIBoundary(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"sidravia_gui", "lib/libflutter_linux_gtk.so", "lib/libapp.so", "data/icudtl.dat", "data/flutter_assets/NOTICES.Z", "data/flutter_assets/version.json", "data/icons/sidravia.png", "FLUTTER-LICENSE"} {
		data := name
		if name == "sidravia_gui" || strings.HasSuffix(name, ".so") {
			header := make([]byte, 20)
			copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1})
			header[18] = 62
			data = string(header)
		}
		writeTestFile(t, filepath.Join(root, name), data)
	}
	entries, err := readLinuxGUI(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.name == "gui-runtime/sidravia_gui" && e.mode != 0755 {
			t.Fatal("GUI not executable")
		}
	}
	writeTestFile(t, filepath.Join(root, "config/configurations.json"), "private")
	if _, err := readLinuxGUI(root, nil); err == nil {
		t.Fatal("accepted user state")
	}
	if err := os.RemoveAll(filepath.Join(root, "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "lib/libapp.so")); err != nil {
		t.Fatal(err)
	}
	if _, err := readLinuxGUI(root, nil); err == nil {
		t.Fatal("accepted incomplete release")
	}
}

func TestInstallerAndPortableHaveIndependentExactInventories(t *testing.T) {
	owned := packagefiles.Manifest{SchemaVersion: 1, Version: "1.0.0", BuildID: "test", OS: "windows", Arch: "amd64", PayloadKind: "gui"}
	entries := []archiveEntry{{"Sidravia.exe", []byte("gui"), 0755}, {"app_icon.ico", []byte("installer icon"), 0644}}
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
	if _, exists := files["app_icon.ico"]; exists {
		t.Fatal("portable retained installer icon")
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
