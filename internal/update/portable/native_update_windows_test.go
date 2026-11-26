//go:build windows

package portable

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
	"sidravia/internal/background/maintenance"
	"sidravia/internal/installation/change"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/testfixture"
	"sidravia/internal/update/portable/internal/filetxn"
	"sidravia/internal/update/transaction"
	"sidravia/internal/update/windowsplatform"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Fixed-host source overlays supply a synthetic signing root, not a runtime
// trust/source option. Real CLI/updater/daemon and optional Flutter bytes run
// in owned roots; all assertions and process cleanup are automatic.
func TestNativeWindowsPortableUpdate(t *testing.T) {
	if os.Getenv("SIDRAVIA_WINDOWS_PORTABLE_FIXTURE") != "1" {
		t.Skip("explicit isolated Windows portable fixture required")
	}
	base := os.Getenv("SIDRAVIA_WINDOWS_PORTABLE_TEST_ROOT")
	if base == "" {
		t.Fatal("owned fixture root required")
	}
	repo, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	owned, e := os.MkdirTemp(base, "portable-update-")
	if e != nil {
		t.Fatal(e)
	}
	t.Log("owned evidence", owned)
	source := filepath.Join(owned, "source")
	if e = os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	overlay := testfixture.NewWindowsOverlay(t, repo, owned, source, key.Public().(ed25519.PublicKey))
	overlay.Replace("internal/update/portable/handoff_windows.go", func(s string) string {
		s = strings.Replace(s, "\twait, cancel := context.WithTimeout(ctx, 15*time.Second)", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_DELAY_READY\")==\"1\"{var cancel context.CancelFunc;ctx,cancel=context.WithTimeout(ctx,100*time.Millisecond);defer cancel()}\n\twait, cancel := context.WithTimeout(ctx, 15*time.Second)", 1)
		s = strings.Replace(s, "\tif err = savePrivate(ctx, filepath.Join(dir, \"ready.json\"),", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_DELAY_READY\")==\"1\"{time.Sleep(600*time.Millisecond)}\n\tif err = savePrivate(ctx, filepath.Join(dir, \"ready.json\"),", 1)

		s = strings.Replace(s, "\ts, dir, err := readWindowsSession(ctx, target, id)", "\tdefer func(){if resultErr!=nil{os.WriteFile("+strconv.Quote(filepath.Join(owned, "runner-cause.txt"))+",[]byte(resultErr.Error()),0600)}}()\n\ts, dir, err := readWindowsSession(ctx, target, id)", 1)
		s = strings.Replace(s, "\tpayload, err := filetxn.ExtractRelease(ctx, target, s.Archive, proof, c)", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_ARCHIVE_CHANGED\")==\"1\"{if err:=os.Rename(s.Archive,s.Archive+\".old\");err!=nil{return err};if err:=os.WriteFile(s.Archive,[]byte(strings.Repeat(\"x\",int(c.Size))),0600);err!=nil{return err}}\n\tpayload, err := filetxn.ExtractRelease(ctx, target, s.Archive, proof, c)", 1)
		s = strings.Replace(s, "\tif _, _, err = store.Advance(ctx, id, transaction.Created, transaction.Verified,", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"created\"{os.Exit(17)}\n\tif _, _, err = store.Advance(ctx, id, transaction.Created, transaction.Verified,", 1)
		return strings.Replace(s, "\tcmd, err := windowsplatform.StartIndependent(", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"verified\"{os.Exit(17)}\n\tcmd, err := windowsplatform.StartIndependent(", 1)
	})
	overlay.Replace("internal/update/portable/internal/filetxn/apply.go", func(s string) string {
		return strings.Replace(s, "\t_, changed, err := store.Advance(work, id, transaction.Quiescent,", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"quiescent\"{os.Exit(17)}\n\t_, changed, err := store.Advance(work, id, transaction.Quiescent,", 1)
	})
	overlay.Replace("internal/update/portable/coordinator.go", func(s string) string {
		s = strings.Replace(s, "\t// Cancellation after writing starts", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"applied\"{os.Exit(17)}\n\t// Cancellation after writing starts", 1)
		return strings.Replace(s, "\t_, err = restoreApplication(work,", "\tif os.Getenv(\"SIDRAVIA_FIXTURE_EXIT_STAGE\")==\"resuming\"{os.Exit(17)}\n\t_, err = restoreApplication(work,", 1)
	})
	overlayPath := overlay.Write()
	bundle := os.Getenv("SIDRAVIA_WINDOWS_PORTABLE_GUI_BUNDLE")
	images := []winImage{}
	for _, v := range []struct{ version, build string }{{"1.0.0", "portable-old"}, {"1.1.0", "portable-new"}} {
		dir := filepath.Join(owned, v.build)
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		overlay.BuildProduct(overlayPath, dir, v.version, v.build)
		files := map[string][]byte{}
		readTree := func(root string) {
			t.Helper()
			if e = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				name, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				data, err := os.ReadFile(path)
				if err == nil {
					files[filepath.ToSlash(name)] = data
				}
				return err
			}); e != nil {
				t.Fatal(e)
			}
		}
		readTree(dir)
		if bundle != "" {
			readTree(bundle)
		}
		// No announcement request or real campus endpoint is available to the fixture.
		raw, e := os.ReadFile(filepath.Join(repo, "internal/daemon/configuration/profiles/jlu.json"))
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
		files["institution-profiles/update-loopback.json"] = raw
		files["BUILD-INFO.txt"] = []byte("ProductVersion: " + v.version + "\nBuildID: " + v.build + "\n")
		if v.build == "portable-old" {
			files["retired.dll"] = []byte("old owned library")
		}
		img := windowsImage(t, v.version, v.build, files)
		images = append(images, img)
		manifest, sig := img.verified.Proof()
		releaseDir := filepath.Join(source, v.version)
		os.MkdirAll(releaseDir, 0700)
		for name, data := range map[string][]byte{release.ReleaseManifestName: manifest, release.ReleaseSignatureName: sig, img.Candidate.Name: img.archive} {
			if e = os.WriteFile(filepath.Join(releaseDir, name), data, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	old, next := images[0], images[1]
	c := next.Candidate
	api, _ := json.Marshal([]any{map[string]any{"id": 2, "tag_name": "v1.1.0", "draft": false, "prerelease": false, "assets": []any{map[string]any{"id": 22, "name": c.Name, "size": c.Size, "digest": "sha256:" + c.SHA256, "browser_download_url": c.DownloadURL, "state": "uploaded"}}}})
	if e = os.WriteFile(filepath.Join(source, "api.json"), api, 0600); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"APPDATA", "LOCALAPPDATA"} {
		path := filepath.Join(owned, name)
		if e = os.MkdirAll(path, 0700); e != nil {
			t.Fatal(e)
		}
		t.Setenv(name, path)
	}
	t.Setenv(launchcontract.EnvSkipAutoLogin, "1")
	modes := []string{"stopped", "headless", "archive-changed", "interrupted-applied", "interrupted-resuming", "interrupted-quiescent"}
	if bundle != "" {
		modes = append(modes, "desktop")
	}
	for _, scenario := range modes {
		mode := scenario
		fault := ""
		archiveChanged := scenario == "archive-changed"
		if archiveChanged {
			mode = "stopped"
		}
		if strings.HasPrefix(scenario, "interrupted-") {
			fault = strings.TrimPrefix(scenario, "interrupted-")
			mode = "stopped"
			if fault == "resuming" {
				mode = "headless"
			}
		}
		t.Run(scenario, func(t *testing.T) {
			target := filepath.Join(owned, scenario+" target with spaces")
			winInstall(t, target, old)
			if e = os.WriteFile(filepath.Join(target, "user-notes.txt"), []byte("keep"), 0644); e != nil {
				t.Fatal(e)
			}
			t.Setenv(launchcontract.EnvNamespace, "portable-fixture-"+digest([]byte(owned + scenario))[:16])
			ns, e := productlayout.NewNamespace(os.Getenv(launchcontract.EnvNamespace))
			if e != nil {
				t.Fatal(e)
			}
			layout, e := productlayout.ResolveTarget(target, ns)
			if e != nil {
				t.Fatal(e)
			}
			identity := change.Target{ID: target, Version: "1.0.0", BuildID: "portable-old", Layout: "portable"}
			var original *applicationPeer
			t.Cleanup(func() { stopPortableFixture(t, layout, target) })
			if mode != "stopped" {
				path, args := filepath.Join(target, daemonName()), []string{}
				if mode == "desktop" {
					path = guiEntry(target)
					args = []string{"--skip-auto-login", "--start-visible"}
				} else {
					t.Setenv(launchcontract.EnvMode, "headless")
				}
				if _, e = startApplication(path, args, os.Environ()); e != nil {
					t.Fatal(e)
				}
				for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
					original, e = probeApplication(context.Background(), layout, identity)
					if e == nil && original != nil && original.status.Mode == mode {
						break
					}
				}
				if original == nil || e != nil || original.status.Mode != mode {
					t.Fatal("source generation not ready", e)
				}
				defer original.close()
			}
			count := 0
			run := func(want string, args ...string) json.RawMessage {
				t.Helper()
				count++
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, filepath.Join(target, "sidraviactl.exe"), append([]string{"update"}, args...)...)
				cmd.Env = os.Environ()
				var out, errout bytes.Buffer
				cmd.Stdout = &out
				cmd.Stderr = &errout
				err := cmd.Run()
				if e = os.WriteFile(filepath.Join(owned, fmt.Sprintf("%s-%02d-%s.json", scenario, count, args[0])), out.Bytes(), 0600); e != nil {
					t.Fatal(e)
				}
				var r struct {
					OK     bool            `json:"ok"`
					Code   string          `json:"code"`
					Result json.RawMessage `json:"result"`
				}
				decode := json.Unmarshal(out.Bytes(), &r)
				success := want == "ok" || want == "handed_over"
				if decode != nil || r.Code != want || r.OK != success || (err == nil) != success {
					t.Fatalf("%v: %s %s %v %v", args, out.String(), errout.String(), err, decode)
				}
				return r.Result
			}
			run("confirmation_required", "apply", "--candidate", "2:22")
			check := run("ok", "check")
			if !bytes.Contains(check, []byte(`"canApply":true`)) {
				t.Fatalf("native candidate capability mismatch: %s", check)
			}
			downloaded := run("ok", "download", "--candidate", "2:22")
			var cache struct {
				Downloaded string `json:"downloaded"`
			}
			if e = json.Unmarshal(downloaded, &cache); e != nil {
				t.Fatal(e)
			}
			if mode == "stopped" && fault == "" && !archiveChanged {
				if e = os.WriteFile(cache.Downloaded, bytes.Repeat([]byte{'x'}, len(next.archive)), 0600); e != nil {
					t.Fatal(e)
				}
				run("cache_invalid", "apply", "--candidate", "2:22", "--confirm")
				os.WriteFile(cache.Downloaded, next.archive, 0600)
				for _, phase := range []string{"created", "verified"} {
					cmd := exec.Command(filepath.Join(target, "sidraviactl.exe"), "update", "apply", "--candidate", "2:22", "--confirm")
					cmd.Env = append(os.Environ(), "SIDRAVIA_FIXTURE_EXIT_STAGE="+phase)
					if out, e := cmd.CombinedOutput(); e == nil {
						t.Fatalf("fault %s succeeded: %s", phase, out)
					}
					status := run("ok", "status")
					var view struct {
						Operation struct {
							ID    string `json:"operationId"`
							Phase string `json:"phase"`
						} `json:"operation"`
					}
					if e = json.Unmarshal(status, &view); e != nil || view.Operation.Phase != phase {
						t.Fatalf("fault phase %s: %s %v", phase, status, e)
					}
					run("ok", "recover", "--operation", view.Operation.ID, "--confirm")
				}
			}
			if fault == "" && !archiveChanged {
				t.Setenv("SIDRAVIA_FIXTURE_DELAY_READY", "1")
				run("timeout", "apply", "--candidate", "2:22", "--confirm")
				t.Setenv("SIDRAVIA_FIXTURE_DELAY_READY", "")
				time.Sleep(800 * time.Millisecond) // deterministic late-ready window in the private overlay
				rr, e := os.OpenRoot(target)
				if e != nil {
					t.Fatal(e)
				}
				e = filetxn.VerifyInventory(context.Background(), rr, old.inventory)
				rr.Close()
				if e != nil {
					t.Fatal("cancelled handoff changed files", e)
				}
				if original != nil {
					alive, e := original.daemon.Running()
					if e != nil || !alive {
						t.Fatal("cancelled handoff stopped source", e)
					}
				}
				receipt := map[string]any{"lateReadyAfterTimeoutDidNotUpdate": true, "oldFilesUnchanged": true, "sourceKeptRunning": mode != "stopped"}
				if e = savePrivate(context.Background(), filepath.Join(owned, scenario+"-cancel-receipt.json"), receipt); e != nil {
					t.Fatal(e)
				}
			}
			if fault != "" {
				t.Setenv("SIDRAVIA_FIXTURE_EXIT_STAGE", fault)
			}
			if archiveChanged {
				t.Setenv("SIDRAVIA_FIXTURE_ARCHIVE_CHANGED", "1")
			}
			result := run("handed_over", "apply", "--candidate", "2:22", "--confirm")
			if !bytes.Contains(result, []byte(`"handedOver":true`)) || bytes.Contains(result, []byte(`"filesApplied":true`)) {
				t.Fatal("handoff falsely claimed installation", string(result))
			}
			root, _ := productlayout.OperationRoot()
			store, e := transaction.Open(root, target, next.keys)
			if e != nil {
				t.Fatal(e)
			}
			var record transaction.Record
			for until := time.Now().Add(60 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
				r, exists, e := store.Read(context.Background())
				if e != nil || !exists {
					continue
				}
				record = r
				if fault != "" && string(r.Phase) == fault {
					dir, e := runnerDirectory(target, r.ID)
					if e == nil {
						var ready runnerID
						if readPrivate(context.Background(), filepath.Join(dir, "ready.json"), &ready) == nil {
							alive, e := windowsplatform.GenerationRunning(ready.PID, ready.Created)
							if e == nil && !alive {
								break
							}
						}
					}
				}
				if r.Phase == transaction.Completed || r.Phase == transaction.Aborted || r.Phase == transaction.ResumeFailed || r.Phase == transaction.RepairRequired {
					break
				}
			}
			if archiveChanged {
				if record.Phase != transaction.Aborted || record.FailureCode != "preparation_failed" {
					t.Fatalf("altered archive published or missing rejection: %+v", record)
				}
				if _, e = os.Stat(cache.Downloaded + ".old"); e != nil {
					t.Fatal("runner did not exercise archive replacement after handoff", e)
				}
				rr, err := os.OpenRoot(target)
				if err != nil {
					t.Fatal(err)
				}
				err = errors.Join(filetxn.VerifyInventory(context.Background(), rr, old.inventory), rr.Close())
				if err != nil {
					t.Fatal("altered archive changed the target", err)
				}
				receipt := map[string]any{"handoff": "confirmed", "files": "source_preserved", "applicationRecovery": "not_requested", "authenticationRecovery": "not_applicable"}
				if e = savePrivate(context.Background(), filepath.Join(owned, scenario+"-receipt.json"), receipt); e != nil {
					t.Fatal(e)
				}
				return
			}
			if fault != "" {
				if string(record.Phase) != fault {
					t.Fatalf("fault phase %s expected %s", record.Phase, fault)
				}
				// A sealed target refuses ordinary startup, even with a complete new tree.
				cmd := exec.Command(filepath.Join(target, "sidraviad.exe"), "--version")
				cmd.Env = os.Environ()
				if out, e := cmd.CombinedOutput(); e == nil {
					t.Fatalf("sealed ordinary startup succeeded: %s", out)
				}
				if record.Recovery != nil {
					forged := exec.Command(filepath.Join(target, "sidraviactl.exe"), "update-internal", "setup-admission")
					forged.Env = append(os.Environ(), launchcontract.EnvRecoveryOperation+"="+record.ID, launchcontract.EnvRecoveryToken+"="+record.Recovery.Nonce, launchcontract.EnvRecoveryConfiguration+"="+record.Recovery.Intent.ConfigurationID)
					if out, e := forged.CombinedOutput(); e == nil {
						t.Fatalf("copied recovery token admitted sibling: %s", out)
					}
				}
				leaf := filepath.Join(target, "BUILD-INFO.txt")
				good, e := os.ReadFile(leaf)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(leaf, []byte("bad"), 0644); e != nil {
					t.Fatal(e)
				}
				run("target_changed", "recover", "--operation", record.ID, "--confirm")
				if e = os.WriteFile(leaf, good, 0644); e != nil {
					t.Fatal(e)
				}
				run("ok", "recover", "--operation", record.ID, "--confirm")
				run("ok", "recover", "--operation", record.ID, "--confirm")
				r, _, e := store.Read(context.Background())
				if e != nil || r.Phase != func() transaction.Phase {
					if fault == "quiescent" {
						return transaction.Aborted
					}
					return transaction.ResumeFailed
				}() {
					t.Fatal("recovery terminal", r.Phase, e)
				}
				if _, e = os.Stat(layout.RuntimeInfoPath); !os.IsNotExist(e) {
					t.Fatal("recovery repeated a launch", e)
				}
				if _, e = os.Stat(filepath.Join(target, transaction.StartupSeal)); !os.IsNotExist(e) {
					t.Fatal("verified recovery retained seal", e)
				}
				receipt := map[string]any{"interruptedPhase": fault, "terminal": r.Phase, "corruptTargetRefused": true, "startupBlocked": true, "repeatedRecoveryDidNotLaunch": true}
				if e = savePrivate(context.Background(), filepath.Join(owned, scenario+"-receipt.json"), receipt); e != nil {
					t.Fatal(e)
				}
				return
			}
			if record.Phase != transaction.Completed {
				t.Fatalf("update phase %s", record.Phase)
			}
			rr, e := os.OpenRoot(target)
			if e != nil {
				t.Fatal(e)
			}
			e = filetxn.VerifyInventory(context.Background(), rr, next.inventory)
			if e == nil {
				e = filetxn.VerifyGUI(rr, next.inventory)
			}
			rr.Close()
			if e != nil {
				t.Fatal("target inventory mismatch", e)
			}
			if _, e = os.Stat(filepath.Join(target, transaction.StartupSeal)); !os.IsNotExist(e) {
				t.Fatal("terminal seal retained", e)
			}
			if b, e := os.ReadFile(filepath.Join(target, "user-notes.txt")); e != nil || string(b) != "keep" {
				t.Fatal("user file changed", e)
			}
			if _, e = os.Stat(filepath.Join(target, "retired.dll")); !os.IsNotExist(e) {
				t.Fatal("retired file retained", e)
			}
			if original != nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				e = waitPeers(ctx, original)
				cancel()
				if e != nil {
					t.Fatal("old generation survived", e)
				}
			}
			identity.Version = "1.1.0"
			identity.BuildID = "portable-new"
			restored, e := probeApplication(context.Background(), layout, identity)
			if mode == "stopped" {
				if e != nil || restored != nil {
					t.Fatal("absent app was started", e)
				}
			} else {
				if e != nil || restored == nil || restored.status.Mode != mode {
					t.Fatal("new generation/mode mismatch", e)
				}
				if restored.daemon.PID == original.daemon.PID && restored.daemon.Start == original.daemon.Start {
					t.Fatal("old generation reused")
				}
				if mode == "desktop" && restored.gui == nil {
					t.Fatal("new GUI absent")
				}
				restored.close()
			}
			status := run("ok", "status")
			if e = os.WriteFile(filepath.Join(owned, mode+"-completed.json"), status, 0600); e != nil {
				t.Fatal(e)
			}
			if e = savePrivate(context.Background(), filepath.Join(owned, mode+"-receipt.json"), map[string]any{"phase": record.Phase, "build": "portable-new", "mode": mode, "inventoryMatched": true, "userFilePreserved": true, "oldGenerationExited": true, "sealRemoved": true}); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func stopPortableFixture(t *testing.T, layout productlayout.Layout, target string) {
	t.Helper()
	raw, e := os.ReadFile(layout.RuntimeInfoPath)
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		t.Error(e)
		return
	}
	info, e := contract.DecodeRuntimeInfo(raw)
	if e != nil {
		t.Error(e)
		return
	}
	identity := change.Target{ID: target, Version: info.ProductVersion, BuildID: info.BuildID, Layout: "portable"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	peer, e := probeApplication(ctx, layout, identity)
	if e != nil || peer == nil {
		t.Error("owned cleanup probe", e)
		return
	}
	defer peer.close()
	if peer.gui == nil {
		e = call(ctx, peer.connection, contract.MethodDaemonStop, struct{}{}, nil)
	} else {
		op := strings.Repeat("f", 32)
		interactive := false
		var view maintenance.View
		e = call(ctx, peer.connection, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: op, CoordinatorPID: uint32(os.Getpid()), Interactive: &interactive}, &view)
		for e == nil && view.Phase == "requested" && ctx.Err() == nil {
			time.Sleep(50 * time.Millisecond)
			e = call(ctx, peer.connection, contract.MethodUpdateStatus, struct{}{}, &view)
		}
		if e == nil && view.Phase != "prepared" {
			e = ErrPreparation
		}
		if e == nil {
			e = call(ctx, peer.connection, contract.MethodUpdateCommit, contract.UpdateOperationPayload{OperationID: op}, nil)
		}
	}
	if e != nil {
		t.Error("owned normal stop", e)
		return
	}
	if e = waitPeers(ctx, peer); e != nil {
		t.Error(e)
	}
}
