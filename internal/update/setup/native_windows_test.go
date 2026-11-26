//go:build windows

package setup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/document"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/update"
	"sidravia/internal/update/internal/testfixture"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
	"sidravia/internal/update/releases"
	"sidravia/internal/update/windowsplatform"
)

// Native production hooks and real CTL/updater binaries. Private build overlays
// supply a synthetic publisher root and an exact fixed-URL file transport; the
// fixture Inno replaces only host integration IO and installation registration.
// No production trust override, real PATH/task/registry or account is exercised.
func TestNativeInstalledUpdate(t *testing.T) {
	if os.Getenv("SIDRAVIA_WINDOWS_SETUP_FIXTURE") != "1" {
		t.Skip("explicit isolated Windows setup fixture required")
	}
	bundle := os.Getenv("SIDRAVIA_WINDOWS_SETUP_GUI_BUNDLE")
	compiler := os.Getenv("SIDRAVIA_ISCC")
	if compiler == "" {
		t.Fatal("pinned native Inno compiler required")
	}
	repo, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	base := os.Getenv("SIDRAVIA_INNO_TEST_ROOT")
	if base == "" {
		t.Fatal("owned native fixture root required")
	}
	owned, e := os.MkdirTemp(base, "installed-update-")
	if e != nil {
		t.Fatal(e)
	}
	t.Log("owned evidence", owned)
	source := filepath.Join(owned, "source")
	oldDir := filepath.Join(owned, "target")
	payload := filepath.Join(owned, "payload")
	for _, d := range []string{source, oldDir, payload} {
		if e = os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	pub := key.Public().(ed25519.PublicKey)
	overlay := testfixture.NewWindowsOverlay(t, repo, owned, source, pub)
	overlay.Replace("internal/update/command/run.go", func(s string) string {
		s = strings.Replace(s, "\"io\"", "\"io\"\n\"os\"", 1)
		return strings.Replace(s, "failSource := func(err error, result any, applying bool) int {", "failSource := func(err error, result any, applying bool) int {\nif applying{os.WriteFile("+strconv.Quote(filepath.Join(owned, "apply-cause.txt"))+",[]byte(err.Error()),0600)}", 1)
	})
	overlay.Replace("cmd/sidraviactl/main.go", func(s string) string {
		return strings.Replace(s, "fmt.Fprintln(os.Stderr, \"安装或更新正在进行，请完成或取消后再运行命令。\")", "fmt.Fprintln(os.Stderr, \"fixture startup cause:\",err)\n fmt.Fprintln(os.Stderr, \"安装或更新正在进行，请完成或取消后再运行命令。\")", 1)
	})
	overlay.Replace("internal/update/setup/begin_windows.go", func(s string) string {
		s = strings.Replace(s, "\tabort := func(cause error)", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"created\"{os.Exit(17)}\n\tabort := func(cause error)", 1)
		return strings.Replace(s, "\toffer, err := windowsplatform.NewOffer", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"verified\"{os.Exit(17)}\n\toffer, err := windowsplatform.NewOffer", 1)
	})
	overlay.Replace("internal/update/windowsplatform/process_windows.go", func(s string) string {
		return strings.Replace(s, "func StartIndependent(path string, args ...string) (*exec.Cmd, error) {", "func StartIndependent(path string, args ...string) (*exec.Cmd, error) {\nargs=append(args,\"/VERYSILENT\",\"/SUPPRESSMSGBOXES\",\"/TASKS=\",\"/LOG=\"+"+strconv.Quote(filepath.Join(owned, "setup.log"))+")", 1)
	})
	overlayPath := overlay.Write()
	for _, v := range []struct{ dir, version, build string }{{oldDir, "1.0.0", "setup-old"}, {payload, "1.1.0", "setup-new"}} {
		overlay.BuildProduct(overlayPath, v.dir, v.version, v.build)
		if bundle != "" {
			copyNativeTree(t, bundle, v.dir)
		}
		info := []byte("ProductVersion: " + v.version + "\nBuildID: " + v.build + "\n")
		if e = os.WriteFile(filepath.Join(v.dir, "BUILD-INFO.txt"), info, 0644); e != nil {
			t.Fatal(e)
		}
		for _, f := range []struct{ source, name string }{{filepath.Join(repo, "LICENSE"), "LICENSE"}, {filepath.Join(repo, "gui/windows/runner/resources/app_icon.ico"), "app_icon.ico"}} {
			data, e := os.ReadFile(f.source)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(v.dir, f.name), data, 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = os.WriteFile(filepath.Join(oldDir, "retired.dll"), []byte("old signed retired product code"), 0644); e != nil {
		t.Fatal(e)
	}
	oldIndex := nativeIndex(t, oldDir, "1.0.0", "setup-old")
	if e = os.WriteFile(filepath.Join(oldDir, "user-notes.txt"), []byte("keep user document"), 0644); e != nil {
		t.Fatal(e)
	}
	oldSeed := filepath.Join(owned, "old-seed")
	copyNativeTree(t, oldDir, oldSeed)
	newIndex := nativeIndex(t, payload, "1.1.0", "setup-new")
	// Full production Pascal code with only system-integration IO stubbed. The
	// fixture has no uninstall registration, shortcuts or genuine data backup.
	template, e := os.ReadFile(filepath.Join(repo, "packaging/windows/sidravia.iss"))
	if e != nil {
		t.Fatal(e)
	}
	script := string(template)
	iconsStart, runStart := strings.Index(script, "[Icons]"), strings.Index(script, "[Run]")
	if iconsStart < 0 || runStart < iconsStart {
		t.Fatal("installer section shape changed")
	}
	script = script[:iconsStart] + script[runStart:]
	script = strings.Replace(script, "AppId={{EB2663BD-8DEB-446A-958E-43DF637C1F53}", "Uninstallable=no\nCreateUninstallRegKey=no", 1)
	script = strings.Replace(script, "DefaultDirName={localappdata}\\Programs\\Sidravia", "DefaultDirName="+oldDir, 1)
	script = strings.Replace(script, "function QueryManager(", "function RealQueryManager(", 1)
	script = strings.Replace(script, "function RunManager(", "function RealRunManager(", 1)
	script = strings.Replace(script, "function PreserveUpgradeData:", "function RealPreserveUpgradeData:", 1)
	insert := `function QueryManager(IncludeTarget: Boolean): Boolean;
begin ManagerMode := 'disabled'; ManagerPath := False; Result := True; end;
function RunManager(Arguments: String): Boolean;
begin Result := True; end;
function PreserveUpgradeData: Boolean;
begin Result := True; end;
`
	insert = strings.Replace(insert, "function RunManager(Arguments: String): Boolean;\nbegin Result := True; end;", "function RunManager(Arguments: String): Boolean;\nbegin Result := not FileExists('"+strings.ReplaceAll(filepath.Join(owned, "fixture-integration-fail"), "'", "''")+"'); end;", 1)
	script = strings.Replace(script, "function ReadIntegrationState: Boolean;", insert+"function ReadIntegrationState: Boolean;", 1)
	// Original PrepareToInstall sees the fixture's integration/data stubs, while
	// all managed handshake, lifecycle, seal and publication calls stay unchanged.
	if e = os.WriteFile(filepath.Join(owned, "sidravia.iss"), []byte(script), 0644); e != nil {
		t.Fatal(e)
	}
	var files strings.Builder
	files.WriteString("[Files]\n")
	e = filepath.WalkDir(payload, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(payload, path)
		if err != nil {
			return err
		}
		component := "core"
		if bundle != "" && !CoreFile(filepath.ToSlash(relative)) && relative != packagefiles.Name {
			component = "gui"
		}
		dest := "{app}"
		if dir := filepath.Dir(relative); dir != "." {
			dest += "\\" + dir
		}
		fmt.Fprintf(&files, "Source: \"payload\\%s\"; DestDir: \"%s\"; Components: %s; Flags: ignoreversion\n", relative, dest, component)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}

	// QueryManager is stubbed, but Inno compilation still resolves the named leaf.
	files.WriteString("Source: \"payload\\sidraviamgr.exe\"; Flags: dontcopy\n")
	if e = os.WriteFile(filepath.Join(owned, "files.iss"), []byte(files.String()), 0644); e != nil {
		t.Fatal(e)
	}
	hasGUI := "0"
	if bundle != "" {
		hasGUI = "1"
	}
	compile := exec.Command(compiler, "/DProductVersion=1.1.0", "/DHasGUI="+hasGUI, "/O"+source, "/Fsidravia-v1.1.0-windows-amd64-setup", filepath.Join(owned, "sidravia.iss"))
	if out, e := compile.CombinedOutput(); e != nil {
		t.Fatalf("compile Inno: %s %v", out, e)
	}
	installer := filepath.Join(source, "sidravia-v1.1.0-windows-amd64-setup.exe")
	installerBytes, e := os.ReadFile(installer)
	if e != nil {
		t.Fatal(e)
	}
	nativeProof(t, source, "1.0.0", "setup-old", oldIndex, []byte("old-not-downloaded"), key)
	candidate := nativeProof(t, source, "1.1.0", "setup-new", newIndex, installerBytes, key)
	if e = os.MkdirAll(filepath.Join(source, "1.1.0"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "1.1.0", candidate.Name), installerBytes, 0600); e != nil {
		t.Fatal(e)
	}
	api, _ := json.Marshal([]any{map[string]any{"id": 2, "tag_name": "v1.1.0", "draft": false, "prerelease": false, "assets": []any{map[string]any{"id": 22, "name": candidate.Name, "size": candidate.Size, "digest": "sha256:" + candidate.SHA256, "browser_download_url": candidate.DownloadURL, "state": "uploaded"}}}})
	if e = os.WriteFile(filepath.Join(source, "api.json"), api, 0600); e != nil {
		t.Fatal(e)
	}
	for name, value := range map[string]string{"APPDATA": filepath.Join(owned, "roaming"), "LOCALAPPDATA": filepath.Join(owned, "local"), launchcontract.EnvNamespace: "setup-fixture-" + filepath.Base(owned), launchcontract.EnvSkipAutoLogin: "1"} {
		if name == "APPDATA" || name == "LOCALAPPDATA" {
			e = os.MkdirAll(value, 0700)
			if e != nil {
				t.Fatal(e)
			}
		}
		t.Setenv(name, value)
	}
	count := 0
	run := func(want string, args ...string) json.RawMessage {
		t.Helper()
		count++
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(oldDir, "sidraviactl.exe"), append([]string{"update"}, args...)...)
		cmd.Env = os.Environ()
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		e := cmd.Run()
		os.WriteFile(filepath.Join(owned, fmt.Sprintf("%02d-%s.json", count, args[0])), out.Bytes(), 0600)
		var r struct {
			OK     bool            `json:"ok"`
			Code   string          `json:"code"`
			Result json.RawMessage `json:"result"`
		}
		decode := json.Unmarshal(out.Bytes(), &r)
		good := want == "ok" || want == "handed_over"
		if decode != nil || r.Code != want || r.OK != good || (e == nil) != good {
			t.Fatalf("%v: %s %s %v %v", args, out.String(), errout.String(), e, decode)
		}
		return r.Result
	}
	run("confirmation_required", "apply", "--candidate", "2:22")
	run("ok", "check")
	downloaded := run("ok", "download", "--candidate", "2:22")
	var cache struct {
		Downloaded string `json:"downloaded"`
	}
	if e = json.Unmarshal(downloaded, &cache); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cache.Downloaded, bytes.Repeat([]byte{'x'}, len(installerBytes)), 0600); e != nil {
		t.Fatal(e)
	}
	run("cache_invalid", "apply", "--candidate", "2:22", "--confirm")
	if e = os.WriteFile(cache.Downloaded, installerBytes, 0600); e != nil {
		t.Fatal(e)
	}
	// Interrupt the real updater after each initial durable boundary. The
	// source binding must already exist so public recover can revoke either op.
	for _, phase := range []string{"created", "verified"} {
		cmd := exec.Command(filepath.Join(oldDir, "sidraviactl.exe"), "update", "apply", "--candidate", "2:22", "--confirm")
		cmd.Env = append(os.Environ(), "SIDRAVIA_FIXTURE_EXIT_STAGE="+phase)
		if out, e := cmd.CombinedOutput(); e == nil {
			t.Fatalf("fixture exit %s unexpectedly succeeded: %s", phase, out)
		}
		status := run("ok", "status")
		var view struct {
			Operation struct {
				ID    string `json:"operationId"`
				Phase string `json:"phase"`
			} `json:"operation"`
		}
		if e = json.Unmarshal(status, &view); e != nil || view.Operation.Phase != phase || view.Operation.ID == "" {
			t.Fatalf("unstarted %s status: %s %v", phase, status, e)
		}
		run("ok", "recover", "--operation", view.Operation.ID, "--confirm")
	}
	handoff := run("handed_over", "apply", "--candidate", "2:22", "--confirm")
	if !bytes.Contains(handoff, []byte(`"handedOver":true`)) || bytes.Contains(handoff, []byte(`"filesApplied":true`)) {
		t.Fatalf("handoff became installation %s", handoff)
	}
	roots, _ := releases.NewPublisherKeys([]ed25519.PublicKey{pub})
	waitInstall := func() {
		t.Helper()
		root, e := productlayout.UpdateOperationRoot()
		if e != nil {
			t.Fatal(e)
		}
		st, e := operations.Open(root, oldDir, roots)
		if e != nil {
			t.Fatal(e)
		}
		var last operations.Phase
		var snapshotError error
		for until := time.Now().Add(45 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
			r, exists, e := st.Read(context.Background())
			if e != nil {
				snapshotError = e
				continue
			}
			if !exists {
				continue
			}
			last = r.Phase
			if r.Phase == operations.Completed {
				return
			}
			if r.Phase == operations.Aborted || r.Phase == operations.RepairRequired || r.Phase == operations.ResumeFailed {
				break
			}
		}
		log, _ := os.ReadFile(filepath.Join(owned, "setup.log"))
		var failure *document.Failure
		var detail error
		if errors.As(snapshotError, &failure) {
			detail = failure.DiagnosticCause()
		}
		t.Fatalf("install phase=%s snapshot=%v detail=%v\n%s", last, snapshotError, detail, log)
	}
	// Read the durable record from this external test process during publication.
	// Starting a target CTL every100ms would itself keep its EXE occupied in Inno.
	waitInstall()
	if e = os.WriteFile(filepath.Join(owned, "installed-stopped.json"), run("ok", "status"), 0600); e != nil {
		t.Fatal(e)
	}

	if _, e = os.Stat(filepath.Join(oldDir, "retired.dll")); !os.IsNotExist(e) {
		t.Fatal("retired signed DLL retained", e)
	}
	if b, e := os.ReadFile(filepath.Join(oldDir, "user-notes.txt")); e != nil || string(b) != "keep user document" {
		t.Fatal("user document changed", e)
	}
	nativeReconcileCases(t, oldDir, source, oldIndex, key)
	// Inno exit1 after verified publication is integration failure, not a
	// failed file installation and not permission to replay a previous choice.
	oldDir = filepath.Join(owned, "integration-target")
	copyNativeTree(t, oldSeed, oldDir)
	marker := filepath.Join(owned, "fixture-integration-fail")
	if e = os.WriteFile(marker, nil, 0600); e != nil {
		t.Fatal(e)
	}
	run("ok", "check")
	run("ok", "download", "--candidate", "2:22")
	run("handed_over", "apply", "--candidate", "2:22", "--confirm")
	waitInstall()
	integrationStatus := run("ok", "status")
	var result struct {
		Operation struct {
			Installer struct {
				ExitCode    *uint32 `json:"exitCode"`
				Integration string  `json:"integration"`
			} `json:"installer"`
		} `json:"operation"`
	}
	if e = json.Unmarshal(integrationStatus, &result); e != nil || result.Operation.Installer.ExitCode == nil || *result.Operation.Installer.ExitCode != 1 || result.Operation.Installer.Integration != "failed" {
		t.Fatalf("integration outcome lost: %s %v", integrationStatus, e)
	}
	if e = os.WriteFile(filepath.Join(owned, "installed-integration-failed.json"), integrationStatus, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	// Reuse the exact built/signed source bytes for a running disconnected daemon.
	oldDir = filepath.Join(owned, "live-target")
	copyNativeTree(t, oldSeed, oldDir)
	ns, e := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
	if e != nil {
		t.Fatal(e)
	}
	layout, e := productlayout.ResolveTarget(oldDir, ns)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(layout.InstitutionProfilesDirectory, 0700); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(repo, "internal/connection/profile/data/jlu.json"))
	if e != nil {
		t.Fatal(e)
	}
	var profile map[string]any
	if e = json.Unmarshal(raw, &profile); e != nil {
		t.Fatal(e)
	}
	profile["institutionProfileId"] = "update-loopback"
	delete(profile, "announcement")
	protocol := profile["institutionProtocolConfiguration"].(map[string]any)
	protocol["serverAddress"] = "127.0.0.1"
	protocol["serverPort"] = 9
	protocol["localPort"] = map[string]any{"mode": "system_assigned"}
	raw, _ = json.Marshal(profile)
	if e = os.WriteFile(filepath.Join(layout.InstitutionProfilesDirectory, "update-loopback.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	life := func(action string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(oldDir, "sidraviactl.exe"), "daemon", action)
		cmd.Env = os.Environ()
		out, e := cmd.CombinedOutput()
		if e != nil {
			return fmt.Errorf("daemon %s: %s %w", action, out, e)
		}
		return nil
	}
	t.Cleanup(func() {
		data, e := os.ReadFile(layout.RuntimeInfoPath)
		if os.IsNotExist(e) {
			return
		}
		if e != nil {
			t.Error(e)
			return
		}
		info, e := contract.DecodeRuntimeInfo(data)
		if e != nil {
			t.Error(e)
			return
		}
		peer, e := windowsplatform.Observe(uint32(info.PID))
		if e != nil {
			t.Error(e)
			return
		}
		defer peer.Close()
		if !strings.EqualFold(peer.Path, filepath.Join(oldDir, "sidraviad.exe")) {
			t.Error("owned cleanup target changed")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, e := client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
		if e != nil {
			t.Error(e)
			return
		}
		defer conn.Close()
		statusReply, e := conn.Call(ctx, contract.MethodDaemonStatus, []byte(`{}`))
		if e != nil || !statusReply.OK {
			t.Error("owned status unavailable", e)
			return
		}
		var status contract.StatusResult
		if e = json.Unmarshal(statusReply.Result, &status); e != nil {
			t.Error(e)
			return
		}
		var r contract.Response
		var guiPeer *windowsplatform.Process
		if status.Mode == "desktop" {
			if status.DesktopOwnerPID == nil {
				t.Error("owned GUI PID absent")
				return
			}
			guiPeer, e = windowsplatform.Observe(uint32(*status.DesktopOwnerPID))
			if e != nil {
				t.Error(e)
				return
			}
			defer guiPeer.Close()
			if !strings.EqualFold(guiPeer.Path, filepath.Join(oldDir, "Sidravia.exe")) {
				t.Error("owned GUI path changed")
				return
			}
			op := strings.Repeat("f", 32)
			payload, _ := json.Marshal(contract.UpdatePreparePayload{OperationID: op, CoordinatorPID: uint32(os.Getpid()), Interactive: new(bool)})
			r, e = conn.Call(ctx, contract.MethodUpdatePrepare, payload)
			for until := time.Now().Add(10 * time.Second); e == nil && r.OK && time.Now().Before(until); time.Sleep(50 * time.Millisecond) {
				var view maintenance.View
				if json.Unmarshal(r.Result, &view) == nil && view.Phase == "prepared" {
					break
				}
				r, e = conn.Call(ctx, contract.MethodUpdateStatus, []byte(`{}`))
			}
			if e == nil && r.OK {
				payload, _ = json.Marshal(contract.UpdateOperationPayload{OperationID: op})
				r, e = conn.Call(ctx, contract.MethodUpdateCommit, payload)
			}
		} else {
			r, e = conn.Call(ctx, contract.MethodDaemonStop, []byte(`{}`))
		}
		if e != nil || !r.OK {
			t.Error("owned normal stop unconfirmed", e)
			return
		}
		if e = peer.Wait(ctx); e != nil {
			t.Error(e)
		}
		if guiPeer != nil {
			if e = guiPeer.Wait(ctx); e != nil {
				t.Error(e)
			}
		}
	})
	if bundle == "" {
		if e = life("start"); e != nil {
			t.Fatal(e)
		}
	} else {
		gui := exec.Command(filepath.Join(oldDir, "Sidravia.exe"), "--skip-auto-login", "--start-visible")
		gui.Env = os.Environ()
		log, e := os.OpenFile(filepath.Join(owned, "gui-source.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { log.Close() })
		gui.Stdout = log
		gui.Stderr = log
		if e = gui.Start(); e != nil {
			t.Fatal(e)
		}
		ownerPID := gui.Process.Pid
		gui.Process.Release()
		ready := false
		for until := time.Now().Add(30 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
			b, e := os.ReadFile(layout.RuntimeInfoPath)
			if e != nil {
				continue
			}
			info, e := contract.DecodeRuntimeInfo(b)
			if e != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			conn, e := client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
			if e == nil {
				r, e := conn.Call(ctx, contract.MethodDaemonStatus, []byte(`{}`))
				if e == nil && r.OK {
					var status contract.StatusResult
					if json.Unmarshal(r.Result, &status) == nil && status.Mode == "desktop" && status.DesktopOwnerPID != nil && *status.DesktopOwnerPID == ownerPID {
						ready = true
					}
				}
				conn.Close()
			}
			cancel()
			if ready {
				break
			}
		}
		if !ready {
			t.Fatal("real source GUI not ready")
		}
	}
	run("ok", "check")
	run("ok", "download", "--candidate", "2:22")
	run("handed_over", "apply", "--candidate", "2:22", "--confirm")
	waitInstall()
	if e = os.WriteFile(filepath.Join(owned, "installed-running.json"), run("ok", "status"), 0600); e != nil {
		t.Fatal(e)
	}

	raw, e = os.ReadFile(layout.RuntimeInfoPath)
	if e != nil {
		t.Fatal(e)
	}
	info, e := contract.DecodeRuntimeInfo(raw)
	if e != nil || info.ProductVersion != "1.1.0" || info.BuildID != "setup-new" {
		t.Fatalf("new generation %+v %v", info, e)
	}
	if e = life("status"); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(owned, "passed"), []byte("stopped + running disconnected installed CLI/Inno update; exact target recovery, cache/confirmation and seal fault guards\n"), 0600); e != nil {
		t.Fatal(e)
	}
}
func nativeIndex(t *testing.T, dir, version, build string) []byte {
	t.Helper()
	kind := "core"
	if _, e := os.Stat(filepath.Join(dir, "Sidravia.exe")); e == nil {
		kind = "gui"
	}
	m := packagefiles.Manifest{SchemaVersion: 1, Version: version, BuildID: build, OS: "windows", Arch: "amd64", PayloadKind: kind}
	e := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, packagefiles.File{Path: filepath.ToSlash(relative), Size: int64(len(data)), SHA256: sum(data), Mode: 0644})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}

	data, e := packagefiles.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, packagefiles.Name), data, 0644); e != nil {
		t.Fatal(e)
	}
	return data
}
func nativeProof(t *testing.T, root, version, build string, index, archive []byte, key ed25519.PrivateKey) releases.Candidate {
	t.Helper()
	c := releases.Candidate{ID: "2:22", Version: version, Name: "sidravia-v" + version + "-windows-amd64-setup.exe", Size: int64(len(archive)), SHA256: sum(archive)}
	c.DownloadURL = "https://github.com/CaelestisAstesia/sidravia/releases/download/v" + version + "/" + c.Name
	id, _ := releases.PublisherKeyID(key.Public().(ed25519.PublicKey))
	ownership, e := packagefiles.Decode(index)
	if e != nil {
		t.Fatal(e)
	}
	m := releases.ReleaseManifest{SchemaVersion: 2, KeyID: id, Version: version, BuildID: build, MinimumProtocol: 1, Assets: []releases.ReleaseAsset{{ManifestAsset: releases.ManifestAsset{OS: "windows", Arch: "amd64", Kind: "setup", Name: c.Name, Size: c.Size, SHA256: c.SHA256}, FilesSHA256: sum(index), PayloadKind: ownership.PayloadKind}}}
	b, _ := json.Marshal(m)
	dir := filepath.Join(root, version)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for name, data := range map[string][]byte{releases.ReleaseManifestName: b, releases.ReleaseSignatureName: ed25519.Sign(key, b)} {
		if e := os.WriteFile(filepath.Join(dir, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return c
}

func copyNativeTree(t *testing.T, from, to string) {
	t.Helper()
	if e := os.MkdirAll(to, 0700); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(from)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		if f.IsDir() {
			copyNativeTree(t, filepath.Join(from, f.Name()), filepath.Join(to, f.Name()))
			continue
		}
		b, e := os.ReadFile(filepath.Join(from, f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(to, f.Name()), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func nativeReconcileCases(t *testing.T, target, source string, oldIndex []byte, key ed25519.PrivateKey) {
	t.Helper()
	roots, _ := releases.NewPublisherKeys([]ed25519.PublicKey{key.Public().(ed25519.PublicKey)})
	manifest, e := os.ReadFile(filepath.Join(source, "1.1.0", releases.ReleaseManifestName))
	if e != nil {
		t.Fatal(e)
	}
	signature, e := os.ReadFile(filepath.Join(source, "1.1.0", releases.ReleaseSignatureName))
	if e != nil {
		t.Fatal(e)
	}
	proof, e := roots.Verify(manifest, signature, "1.1.0")
	if e != nil {
		t.Fatal(e)
	}
	c, e := proof.InstalledAsset("windows", "amd64", "setup")
	if e != nil {
		t.Fatal(e)
	}
	c.ID = "2:22"
	oldManifest, e := os.ReadFile(filepath.Join(source, "1.0.0", releases.ReleaseManifestName))
	if e != nil {
		t.Fatal(e)
	}
	oldSignature, e := os.ReadFile(filepath.Join(source, "1.0.0", releases.ReleaseSignatureName))
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command("cmd.exe", "/d", "/c", "ping -n 2 127.0.0.1 >nul")
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	peer, e := windowsplatform.Observe(uint32(child.Process.Pid))
	if e != nil {
		child.Process.Kill()
		child.Wait()
		t.Fatal(e)
	}
	dead := ProcessID{peer.PID, peer.Created}
	child.Wait()
	peer.Close()
	root, e := productlayout.UpdateOperationRoot()
	if e != nil {
		t.Fatal(e)
	}
	store, e := operations.Open(root, target, roots)
	if e != nil {
		t.Fatal(e)
	}
	original, e := os.ReadFile(filepath.Join(target, "sidraviad.exe"))
	if e != nil {
		t.Fatal(e)
	}
	for i, phase := range []operations.Phase{operations.Applied, operations.Resuming, operations.Completed, operations.Applying} {
		id := fmt.Sprintf("%032x", i+100)
		record, e := operations.New(id, update.Target{ID: target, Version: "1.0.0", BuildID: "setup-old", Layout: "installed"}, c, proof, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = store.Create(context.Background(), record); e != nil {
			t.Fatal(e)
		}
		if _, _, e = store.Advance(context.Background(), id, operations.Created, operations.Verified, "", time.Now()); e != nil {
			t.Fatal(e)
		}
		rec := operations.Recovery{SourceFilesSHA256: sum(oldIndex), Intent: update.RestoreIntent{Mode: "none"}, CoordinatorPID: int(dead.PID), CoordinatorStart: strconv.FormatUint(dead.Created, 10), Nonce: strings.Repeat("c", 32), ExpiresAt: time.Now().Add(20 * time.Minute), LogLevel: "info"}
		if e = store.SetRecovery(context.Background(), id, rec, time.Now()); e != nil {
			t.Fatal(e)
		}
		previous := operations.Verified
		for _, to := range []operations.Phase{operations.Preparing, operations.Quiescent, operations.Applying, operations.Applied, operations.Resuming, operations.Completed} {
			if _, _, e = store.Advance(context.Background(), id, previous, to, "", time.Now()); e != nil {
				t.Fatal(e)
			}
			previous = to
			if to == phase {
				break
			}
		}
		hasGUI := false
		if _, e := os.Stat(filepath.Join(target, "Sidravia.exe")); e == nil {
			hasGUI = true
		}
		session := Session{SchemaVersion: 1, OperationID: id, HasGUI: hasGUI, Source: record.Source, Updater: dead, Coordinator: dead, Finalizer: dead, Namespace: os.Getenv(launchcontract.EnvNamespace), Expires: time.Now().Add(20 * time.Minute), SourceManifest: oldManifest, SourceSignature: oldSignature, SourceIndex: oldIndex, Integration: "pending"}
		if e = SaveSession(context.Background(), session); e != nil {
			t.Fatal(e)
		}
		seal := filepath.Join(target, operations.StartupSeal)
		if e = os.WriteFile(seal, []byte("sidravia-update-v1\n"+id+"\n"), 0644); e != nil {
			t.Fatal(e)
		}
		invoke := func(want string) {
			t.Helper()
			cmd := exec.Command(filepath.Join(target, "sidraviactl.exe"), "update", "recover", "--operation", id, "--confirm")
			cmd.Env = os.Environ()
			out, e := cmd.CombinedOutput()
			var r struct {
				Code string `json:"code"`
			}
			line := bytes.SplitN(out, []byte("\n"), 2)[0]
			if x := json.Unmarshal(line, &r); x != nil || r.Code != want || (want == "ok") != (e == nil) {
				t.Fatalf("recover %s: %s %v", phase, out, e)
			}
		}
		if phase == operations.Applied {
			if e = os.WriteFile(filepath.Join(target, "sidraviad.exe"), bytes.Repeat([]byte{'x'}, len(original)), 0644); e != nil {
				t.Fatal(e)
			}
			invoke("target_changed")
			if _, e = os.Stat(seal); e != nil {
				t.Fatal("bad bytes cleared seal")
			}
			if e = os.WriteFile(filepath.Join(target, "sidraviad.exe"), original, 0644); e != nil {
				t.Fatal(e)
			}
		}
		invoke("ok")
		if _, e = os.Stat(seal); !os.IsNotExist(e) {
			t.Fatal("verified recovery retained seal", e)
		}
		invoke("ok")
	}
}
