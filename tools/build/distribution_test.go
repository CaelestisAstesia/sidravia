package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDistributionFlags(t *testing.T) {
	base := []string{"--version", "0.3.0-alpha.1", "--build-id", "test", "--output", "out"}
	for _, target := range []string{"windows-amd64", "linux-amd64", "windows-arm64", "linux-arm64"} {
		tail := []string{"--artifact", "distribution", "--target", target}
		if _, err := parseFlags(append(append([]string(nil), base...), tail...)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tail := range [][]string{{"--target", "windows-amd64"}, {"--artifact", "distribution"}, {"--artifact", "distribution", "--target", "linux-amd64", "--gui-directory", "x"}, {"--artifact", "distribution", "--target", "windows-arm64", "--gui-directory", "x"}, {"--artifact", "distribution", "--target", "windows-arm64", "--iscc", "x"}, {"--artifact", "distribution", "--target", "linux-arm64", "--iscc", "x"}} {
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
			writeTestFile(t, filepath.Join(root, "gui", "windows", "runner", "resources", "app_icon.ico"), "icon-fixture")
			fb := &fakeBuilder{}
			out := filepath.Join(t.TempDir(), "dist")
			cfg := config{version: "0.3.0-alpha.1", buildID: "test", goBin: "go", artifact: "distribution", target: target, output: out}
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
				z, err := zip.OpenReader(filepath.Join(out, "sidravia-v0.3.0-alpha.1-"+target+"-portable.zip"))
				if err != nil {
					t.Fatal(err)
				}
				defer z.Close()
				for _, f := range z.File {
					contents[f.Name] = true
					stream, err := f.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(stream)
					closeErr := stream.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("archive read: %v / %v", err, closeErr)
					}
					bytesByName[f.Name] = data
				}
				if !contents["sidraviamgr.exe"] || !contents["sidraviactl.exe"] || !contents["sidraviad.exe"] || !contents["app_icon.ico"] {
					t.Fatal("missing manager or core")
				}
			} else {
				f, err := os.Open(filepath.Join(out, "sidravia-v0.3.0-alpha.1-"+target+"-portable.tar.gz"))
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
				if !contents["sidraviactl"] || !contents["sidraviad"] || contents["sidraviamgr"] || contents["Sidravia.exe"] {
					t.Fatal("Linux program set")
				}
			}
			if !contents["sidravia.portable"] || !contents["institution-profiles/jlu.json"] || contents["scripts/install.ps1"] {
				t.Fatal("distribution content boundary")
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
	gui, err := readGUI(root, nil)
	if err != nil || len(gui) != 2 {
		t.Fatal(err)
	}
	manifest := installerManifest(filepath.Join(t.TempDir(), "payload"), append([]zipEntry{{"sidraviamgr.exe", []byte("manager"), 0755}}, gui...), gui)
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
