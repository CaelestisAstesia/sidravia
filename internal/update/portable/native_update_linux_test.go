//go:build linux

package portable

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	launchcontract "sidravia/internal/background/launch"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/update"
	"sidravia/internal/update/internal/testfixture"
	"sidravia/internal/update/operations"
	"sidravia/internal/update/packagefiles"
)

// Opt-in production-chain fixture. The ONLY source overlay supplies a synthetic
// public root to private test executables. No environment trust override or
// synthetic key enters production source, packages, CI roots or a real account.
func TestNativePortableUpdate(t *testing.T) {
	if os.Getenv("SIDRAVIA_PORTABLE_UPDATE_FIXTURE") != "1" {
		t.Skip("explicit private native update fixture required")
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	bundle := os.Getenv("SIDRAVIA_PORTABLE_UPDATE_GUI_BUNDLE")
	kinds := []string{"core", "cancel-after-prepare", "unconfirmed-exit", "failed-new-start", "coordinator-death", "saved-configuration"}
	if bundle != "" {
		kinds = append(kinds, "gui")
	}
	if os.Getenv("SIDRAVIA_SYSTEMD_UPDATE_FIXTURE") == "1" {
		kinds = append(kinds, "systemd", "systemd-cancel", "systemd-apply-conflict", "systemd-new-failed", "systemd-interrupted-rollback", "systemd-before-write-recovery")
		if bundle != "" {
			kinds = append(kinds, "systemd-gui")
		}
	}
	baseEnv := os.Environ()
	for _, scenario := range kinds {
		t.Run(scenario, func(t *testing.T) {
			managed := strings.HasPrefix(scenario, "systemd")
			kind := "core"
			if scenario == "gui" || scenario == "systemd-gui" {
				kind = "gui"
			}
			owned, err := os.MkdirTemp(filepath.Join(repo, ".local"), "portable-update-fixture-")
			if err != nil {
				t.Fatal(err)
			}
			t.Log("owned evidence", owned)
			oldDir := filepath.Join(owned, "portable with spaces")
			newDir := filepath.Join(owned, "new-products")
			for _, dir := range []string{oldDir, newDir, filepath.Join(owned, "home")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			overlay := testfixture.WritePublisherOverlay(t, repo, owned, key.Public().(ed25519.PublicKey))
			buildFixtureProducts(t, repo, oldDir, "1.0.0", "native-old", kind, bundle, overlay, baseEnv)
			buildFixtureProducts(t, repo, newDir, "1.1.0", "native-new", kind, bundle, overlay, baseEnv)
			old := fixtureArchive(t, oldDir, "1.0.0", "native-old", kind)
			counter := filepath.Join(owned, "new-launch-count")
			if scenario == "failed-new-start" || scenario == "systemd-new-failed" {
				source := filepath.Join(owned, "fail-main.go")
				code := "package main\nimport \"os\"\nfunc main(){f,_:=os.OpenFile(" + strconv.Quote(counter) + ",os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);f.WriteString(\"started\\n\");f.Close();os.Exit(7)}\n"
				if err := os.WriteFile(source, []byte(code), 0600); err != nil {
					t.Fatal(err)
				}
				command := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", filepath.Join(newDir, "sidraviad"), source)
				command.Env = append(baseEnv, "GOTOOLCHAIN=local", "CGO_ENABLED=0")
				if out, err := command.CombinedOutput(); err != nil {
					t.Fatalf("failure fixture %s %v", out, err)
				}
			}
			const addedPath = "new-product-file.txt"
			if scenario == "systemd-apply-conflict" {
				if err := os.WriteFile(filepath.Join(newDir, addedPath), []byte("new package"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			next := fixtureArchive(t, newDir, "1.1.0", "native-new", kind)
			if err := os.WriteFile(filepath.Join(oldDir, packagefiles.Name), old.inventory.data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(oldDir, "sidravia.portable"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(owned, next.inventory.candidate.Name)
			if err := os.WriteFile(archive, next.archive, 0600); err != nil {
				t.Fatal(err)
			}
			namespace := "native-update-" + kind + "-" + strings.TrimPrefix(filepath.Base(owned), "portable-update-fixture-")
			if managed {
				namespace = ""
			}
			for name, value := range map[string]string{"HOME": filepath.Join(owned, "home"), "XDG_CACHE_HOME": filepath.Join(owned, "cache"), "XDG_CONFIG_HOME": filepath.Join(owned, "config"), "XDG_DATA_HOME": filepath.Join(owned, "data"), "XDG_STATE_HOME": filepath.Join(owned, "state"), launchcontract.EnvNamespace: namespace, launchcontract.EnvSkipAutoLogin: "1"} {
				t.Setenv(name, value)
			}
			ns, err := productlayout.NewNamespace(namespace)
			if err != nil {
				t.Fatal(err)
			}
			layout, err := productlayout.ResolveTarget(oldDir, ns)
			if err != nil {
				t.Fatal(err)
			}
			{ // Every real package has a profile catalog; this fixture uses loopback only.
				socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { socket.Close() })
				data, err := os.ReadFile(filepath.Join(repo, "internal/daemon/configuration/profiles/jlu.json"))
				if err != nil {
					t.Fatal(err)
				}
				var profile map[string]any
				if err := json.Unmarshal(data, &profile); err != nil {
					t.Fatal(err)
				}
				profile["institutionProfileId"] = "update-loopback"
				delete(profile, "announcement")
				protocol := profile["institutionProtocolConfiguration"].(map[string]any)
				protocol["serverAddress"] = "127.0.0.1"
				protocol["serverPort"] = socket.LocalAddr().(*net.UDPAddr).Port
				protocol["localPort"] = map[string]any{"mode": "system_assigned"}
				encoded, _ := json.Marshal(profile)
				if err := os.MkdirAll(layout.InstitutionProfilesDirectory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(layout.InstitutionProfilesDirectory, "update-loopback.json"), encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
			executable := filepath.Join(oldDir, "sidraviad")
			args := []string{}
			if kind == "gui" {
				executable = filepath.Join(oldDir, "sidravia_gui")
				args = []string{"--skip-auto-login", "--start-visible"}
			}
			command := exec.Command(executable, args...)
			command.Env = os.Environ()
			if kind == "core" {
				command.Env = append(command.Env, launchcontract.EnvMode+"=headless")
			}
			if managed {
				ctl := filepath.Join(oldDir, "sidraviactl")
				service := func(action string) ([]byte, error) {
					return exec.Command(ctl, "integration", "service", action).CombinedOutput()
				}
				out, err := service("status")
				if err != nil || !bytes.Contains(out, []byte(`"registered":false`)) {
					t.Fatalf("fixture requires an unused isolated user manager: %s %v", out, err)
				}
				t.Cleanup(func() {
					if out, err := service("unregister"); err != nil {
						t.Errorf("unregister fixture: %s %v", out, err)
					}
				})
				for _, action := range []string{"register", "enable", "start"} {
					if out, err := service(action); err != nil {
						t.Fatalf("%s fixture: %s %v", action, out, err)
					}
				}
			}
			var guiExited chan error
			if !managed || kind == "gui" {
				log, err := os.OpenFile(filepath.Join(owned, "old-start.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				command.Stdout = log
				command.Stderr = log
				if err := command.Start(); err != nil {
					log.Close()
					t.Fatal(err)
				}
				log.Close()
				guiExited = make(chan error, 1)
				go func() { guiExited <- command.Wait() }()
				if managed {
					t.Cleanup(func() { _ = command.Process.Kill() })
				}
			}
			source := update.Target{ID: oldDir, Version: "1.0.0", BuildID: "native-old", Layout: "portable"}
			oldPeer := waitFixturePeer(t, layout, source)
			t.Cleanup(func() {
				if managed {
					oldPeer.close()
					return
				}
				for _, identity := range []update.Target{{ID: oldDir, Version: "1.1.0", BuildID: "native-new", Layout: "portable"}, source} {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					peer, _ := probeApplication(ctx, layout, identity)
					if peer != nil {
						if peer.gui != nil {
							fd, e := unix.PidfdOpen(peer.gui.PID, 0)
							if e == nil {
								for ctx.Err() == nil {
									alive, _ := peer.gui.Running()
									if !alive {
										break
									}
									_ = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
									if err := pause(ctx); err != nil {
										break
									}
								}
								unix.Close(fd)
							}
						} else {
							_ = call(ctx, peer.connection, "daemon.stop", struct{}{}, nil)
						}
						if e := waitPeers(ctx, peer); e != nil {
							t.Error("fixture cleanup", e)
						}
						peer.close()
					}
					cancel()
				}
				oldPeer.close()
			})
			if kind == "gui" && !managed {
				ready, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				attempts := 0
				for {
					attempts++
					id := fmt.Sprintf("%032x", attempts)
					var view update.MaintenanceView
					interactive := false
					err := call(ready, oldPeer.connection, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: id, CoordinatorPID: uint32(os.Getpid()), Interactive: &interactive}, &view)
					for err == nil && view.Phase == "requested" {
						if e := pause(ready); e != nil {
							err = e
							break
						}
						err = call(ready, oldPeer.connection, contract.MethodUpdateStatus, struct{}{}, &view)
					}
					if err != nil {
						cancel()
						t.Fatal("GUI preparation readiness", err)
					}
					approved := view.Phase == "prepared"
					if err := call(ready, oldPeer.connection, contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: id}, nil); err != nil {
						cancel()
						t.Fatal(err)
					}
					if approved {
						break
					}
					if err := pause(ready); err != nil {
						cancel()
						t.Fatal("GUI preparation readiness", err)
					}
				}
				cancel()
			}
			if scenario == "systemd-gui" {
				wait, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				for {
					gui, err := observeManagedGUI(oldDir)
					if err != nil {
						t.Fatal(err)
					}
					if gui != nil {
						gui.Close()
						break
					}
					if err := pause(wait); err != nil {
						t.Fatal(err)
					}
				}
			}
			originalPID := oldPeer.daemon.PID
			savedID := ""
			if scenario == "saved-configuration" {
				var configuration contract.ConfigurationResult
				input := contract.ConfigurationCreatePayload{InstitutionProfileID: "update-loopback", Username: "synthetic-update-user", Password: "synthetic-update-password", AllowInsecureStorage: true, AutoReconnect: true, NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"}}
				if err := call(context.Background(), oldPeer.connection, contract.MethodConfigurationCreate, input, &configuration); err != nil {
					t.Fatal(err)
				}
				savedID = configuration.ConfigurationID
				if err := call(context.Background(), oldPeer.connection, contract.MethodSessionStartConfiguration, map[string]string{"configurationId": savedID}, nil); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "coordinator-death" {
				owner := exec.Command("/usr/bin/sleep", "30")
				if err := owner.Start(); err != nil {
					t.Fatal(err)
				}
				yes := false
				var view update.MaintenanceView
				if err := call(context.Background(), oldPeer.connection, contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: strings.Repeat("f", 32), CoordinatorPID: uint32(owner.Process.Pid), Interactive: &yes}, &view); err != nil {
					t.Fatal(err)
				}
				if view.Phase != "prepared" {
					t.Fatal(view.Phase)
				}
				owner.Process.Signal(os.Interrupt)
				owner.Wait()
				if err := call(context.Background(), oldPeer.connection, contract.MethodUpdateStatus, struct{}{}, &view); err != nil || view.Phase != "idle" {
					t.Fatal("dead coordinator did not thaw live daemon", view.Phase, err)
				}
			}

			root, err := productlayout.UpdateOperationRoot()
			if err != nil {
				t.Fatal(err)
			}
			store, err := operations.Open(root, oldDir, next.keys)
			if err != nil {
				t.Fatal(err)
			}
			r, err := operations.New(strings.Repeat("e", 32), source, next.inventory.candidate, next.verified, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Advance(context.Background(), r.ID, operations.Created, operations.Verified, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			p, err := Extract(context.Background(), oldDir, archive, next.inventory)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if scenario == "coordinator-death" {
				_, _, err := store.Advance(context.Background(), r.ID, operations.Verified, operations.Aborted, "cancelled", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if alive, err := oldPeer.daemon.Running(); err != nil || !alive {
					t.Fatal("thaw stopped old generation", err)
				}
				data, _ := json.Marshal(map[string]any{"passed": true, "scenario": scenario, "oldGenerationThawed": true, "noFileWrite": true})
				os.WriteFile(filepath.Join(owned, "result.json"), data, 0600)
				return
			}
			lease, err := store.AcquireStart(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer lease()
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			defer cancel()

			if scenario == "systemd-interrupted-rollback" || scenario == "systemd-before-write-recovery" {
				lease()
				ctl := filepath.Join(oldDir, "sidraviactl")
				if out, err := exec.Command(ctl, "integration", "service", "stop").CombinedOutput(); err != nil {
					t.Fatalf("stop source: %s %v", out, err)
				}
				interrupted := exec.Command("/usr/bin/sleep", "30")
				if err := interrupted.Start(); err != nil {
					t.Fatal(err)
				}
				owner, err := observeProcess(interrupted.Process.Pid)
				if err != nil {
					_ = interrupted.Process.Kill()
					_ = interrupted.Wait()
					t.Fatal(err)
				}
				defer owner.Close()
				if err := interrupted.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = interrupted.Wait()
				now := time.Now()
				if err := store.SetRecovery(ctx, r.ID, operations.Recovery{
					SourceFilesSHA256: digest(old.inventory.data), Intent: update.RestoreIntent{Mode: "systemd"},
					CoordinatorPID: owner.PID, CoordinatorStart: owner.Start, Nonce: strings.Repeat("d", 32),
					ExpiresAt: now.Add(15 * time.Minute), LogLevel: "info", ManagedService: true,
				}, now); err != nil {
					t.Fatal(err)
				}
				for _, phases := range [][2]operations.Phase{{operations.Verified, operations.Preparing}, {operations.Preparing, operations.Quiescent}} {
					if _, _, err := store.Advance(ctx, r.ID, phases[0], phases[1], "", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				dirs, err := newProductDirectories(oldDir, next.inventory)
				if err != nil {
					t.Fatal(err)
				}
				backup, err := stageServiceRollback(ctx, oldDir, old.inventory, rollbackDirectory(oldDir, r.ID))
				if err != nil {
					t.Fatal(err)
				}
				if err := errors.Join(persistRollbackInputs(ctx, backup, next.inventory, dirs), backup.root.Close()); err != nil {
					t.Fatal(err)
				}
				if scenario == "systemd-before-write-recovery" {
					if err := ReconcileInterrupted(ctx, store, r.ID, oldDir, old.inventory); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := Apply(ctx, store, r.ID, oldDir, old.inventory, p); err != nil {
						t.Fatal(err)
					}
					if _, _, err := store.Advance(ctx, r.ID, operations.Applied, operations.RollingBack, "", time.Now()); err != nil {
						t.Fatal(err)
					}
					// No in-memory snapshot is available to this entrance.
					if err := RecoverManagedService(ctx, store, r.ID, oldDir); err != nil {
						t.Fatal(err)
					}
				}
				peer := waitFixturePeer(t, layout, source)
				defer peer.close()
				if peer.status.Mode != "systemd" {
					t.Fatal("recovered generation escaped systemd")
				}
				if _, err := os.Stat(rollbackDirectory(oldDir, r.ID)); !os.IsNotExist(err) {
					t.Fatal("interrupted recovery did not clean its snapshot", err)
				}
				if scenario == "systemd-interrupted-rollback" {
					// Simulate interruption after terminal cleanup deleted the
					// next index and some no-longer-needed backup product bytes.
					partial, err := stageServiceRollback(ctx, oldDir, old.inventory, rollbackDirectory(oldDir, r.ID))
					if err != nil {
						t.Fatal(err)
					}
					name := old.inventory.manifest.Files[0].Path
					if err := errors.Join(partial.root.Remove(name), partial.root.Close()); err != nil {
						t.Fatal(err)
					}
					if err := RecoverManagedService(ctx, store, r.ID, oldDir); err != nil {
						t.Fatal(err)
					}
					if pending, err := ManagedRollbackPending(oldDir, r.ID); err != nil || pending {
						t.Fatal("repeat recovery left partial cleanup pending", err)
					}
				}
				t.Log("fresh recovery restored the original running service and cleaned the snapshot")
				return
			}
			handoff := lease
			if scenario == "cancel-after-prepare" || scenario == "systemd-cancel" {
				handoff = func() { lease(); cancel() }
			}
			if scenario == "systemd-apply-conflict" {
				handoff = func() {
					lease()
					// Simulate an unrelated file appearing after Preflight and
					// before Apply repeats its path and inventory checks.
					if err := os.WriteFile(filepath.Join(oldDir, addedPath), []byte("keep user file"), 0600); err != nil {
						t.Error(err)
					}
				}
			}
			if scenario == "unconfirmed-exit" {
				handoff = func() {
					lease()
					if err := command.Process.Signal(syscall.SIGSTOP); err != nil {
						t.Error(err)
					}
				}
			}
			executeErr := Execute(ctx, Execution{Store: store, OperationID: r.ID, Target: oldDir, Old: old.inventory, Payload: p, Namespace: ns.String(), ReleaseRuntime: handoff})
			if scenario == "unconfirmed-exit" {
				if err := command.Process.Signal(syscall.SIGCONT); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "systemd-new-failed" {
				current, _, err := store.Read(context.Background())
				if err != nil || executeErr == nil || current.Phase != operations.RolledBack {
					t.Fatalf("managed post-write rollback failed: %v %v %v", current.Phase, err, executeErr)
				}
				rolledBack := waitFixturePeer(t, layout, source)
				defer rolledBack.close()
				if rolledBack.status.Mode != "systemd" || rolledBack.daemon.PID == originalPID {
					t.Fatal("old daemon was not restored by systemd")
				}
				if _, err := os.Stat(counter); err != nil {
					t.Fatal("new generation was never started", err)
				}
				dirs, err := filepath.Glob(filepath.Join(owned, ".sidravia-service-rollback-*"))
				if err != nil || len(dirs) != 0 {
					t.Fatal("rollback retained a temporary snapshot", dirs, err)
				}
				if _, err := os.Stat(filepath.Join(oldDir, operations.StartupSeal)); !os.IsNotExist(err) {
					t.Fatal("rollback retained startup seal", err)
				}
				t.Log("failed new executable was replaced with verified old package, old IPC is reachable")
				return
			}
			if scenario == "systemd-cancel" || scenario == "systemd-apply-conflict" {
				current, _, err := store.Read(context.Background())
				if err != nil || executeErr == nil || current.Phase != operations.Aborted {
					t.Fatalf("managed cancellation failed: %v %v %v", current.Phase, err, executeErr)
				}
				running := waitFixturePeer(t, layout, source)
				defer running.close()
				if running.status.Mode != "systemd" {
					t.Fatal("source generation escaped systemd")
				}
				if scenario == "systemd-apply-conflict" {
					if running.daemon.PID == originalPID {
						t.Fatal("fault did not exercise stop then automatic restoration")
					}
					data, err := os.ReadFile(filepath.Join(oldDir, addedPath))
					if err != nil || string(data) != "keep user file" {
						t.Fatal("rollback changed the conflicting file", err)
					}
					t.Log("pre-write conflict after normal shutdown restored the old running service")
				}
				return
			}
			if scenario == "cancel-after-prepare" || scenario == "unconfirmed-exit" || scenario == "failed-new-start" {
				if executeErr == nil {
					t.Fatal("fault was ignored")
				}
				record, _, err := store.Read(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				expectedPhase := operations.Aborted
				if scenario == "failed-new-start" {
					expectedPhase = operations.ResumeFailed
				}
				if record.Phase != expectedPhase {
					t.Fatal(record.Phase)
				}
				if scenario == "failed-new-start" {
					data, err := os.ReadFile(counter)
					if err != nil || string(data) != "started\n" {
						t.Fatalf("new generation attempt count %q %v", data, err)
					}
					if err := Execute(context.Background(), Execution{Store: store, OperationID: r.ID, Target: oldDir, Old: old.inventory, Payload: p, Namespace: ns.String()}); err == nil {
						t.Fatal("failed cold launch replayed")
					}
				} else {
					if alive, err := oldPeer.daemon.Running(); err != nil || !alive {
						t.Fatal("surviving old generation was replaced", err)
					}
					root, err := os.OpenRoot(oldDir)
					if err != nil {
						t.Fatal(err)
					}
					err = verifyInventory(context.Background(), root, old.inventory)
					root.Close()
					if err != nil {
						t.Fatal("fault wrote product bytes", err)
					}
					if scenario == "cancel-after-prepare" {
						var view update.MaintenanceView
						if err := call(context.Background(), oldPeer.connection, contract.MethodUpdateStatus, struct{}{}, &view); err != nil || view.Phase != "idle" {
							t.Fatal("cancel did not thaw old generation", err)
						}
					}
				}
				data, _ := json.Marshal(map[string]any{"passed": true, "scenario": scenario, "expectedPhase": expectedPhase, "noDuplicateRecovery": true, "noForcedUpdateTermination": true})
				os.WriteFile(filepath.Join(owned, "result.json"), data, 0600)
				return
			}
			if executeErr != nil {
				t.Fatal(executeErr)
			}
			if scenario == "systemd-gui" {
				select {
				case err := <-guiExited:
					if err != nil {
						t.Fatal("GUI did not exit normally", err)
					}
					t.Log("managed update negotiated normal exit of the live Flutter GUI")
				case <-ctx.Done():
					t.Fatal("the GUI did not exit")
				}
			}
			record, _, err := store.Read(ctx)
			if err != nil || record.Phase != operations.Completed {
				t.Fatal(record.Phase, err)
			}
			expectedMode := "headless"
			if kind == "gui" {
				expectedMode = "desktop"
			}
			if !managed && (record.Recovery == nil || record.Recovery.Intent.Mode != expectedMode || record.Recovery.Intent.Maintain != (scenario == "saved-configuration")) {
				t.Fatal("original mode/disconnected intent not retained")
			}
			if running, err := oldPeer.daemon.Running(); err != nil || running {
				t.Fatal("old daemon did not exit", err)
			}
			restored := waitFixturePeer(t, layout, record.Target)
			if managed && restored.status.Mode != "systemd" {
				t.Fatal("updated daemon escaped systemd")
			}
			if restored.daemon.PID == originalPID {
				t.Fatal("restored old generation")
			}
			if scenario == "saved-configuration" {
				var sessions contract.SessionListResult
				if err := call(context.Background(), restored.connection, contract.MethodSessionList, struct{}{}, &sessions); err != nil {
					t.Fatal(err)
				}
				if len(sessions.Sessions) != 1 || sessions.Sessions[0].ConfigurationID != savedID || sessions.Sessions[0].Intent != "maintain_authentication" {
					t.Fatal("saved intent not restored exactly once")
				}
				var duplicate json.RawMessage
				if err := call(context.Background(), restored.connection, contract.MethodUpdateResume, contract.UpdateResumePayload{OperationID: r.ID, ConfigurationID: savedID}, &duplicate); err == nil {
					t.Fatal("duplicate saved intent admitted")
				}
				raw, _ := json.Marshal(record)
				if bytes.Contains(raw, []byte("synthetic-update-password")) || bytes.Contains(raw, []byte("synthetic-update-user")) {
					t.Fatal("authentication material in recovery record")
				}
			}
			restored.close()
			if err := Execute(ctx, Execution{Store: store, OperationID: r.ID, Target: oldDir, Old: old.inventory, Payload: p, Namespace: ns.String()}); err == nil {
				t.Fatal("duplicate execution admitted")
			}
			if managed {
				// Explicit downgrade uses an independently unpacked old package.
				// Keep both directories intact and use the product registration flow.
				downgradeDir := filepath.Join(owned, "restored portable A")
				if err := os.Mkdir(downgradeDir, 0700); err != nil {
					t.Fatal(err)
				}
				archive := filepath.Join(owned, old.inventory.candidate.Name)
				if err := os.WriteFile(archive, old.archive, 0600); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command("/usr/bin/tar", "-xzf", archive, "--strip-components=1", "-C", downgradeDir).CombinedOutput(); err != nil {
					t.Fatalf("extract exact old package: %s %v", out, err)
				}
				root, err := os.OpenRoot(downgradeDir)
				if err != nil {
					t.Fatal(err)
				}
				if err := errors.Join(verifyInventory(ctx, root, old.inventory), root.Close()); err != nil {
					t.Fatal("old package inventory", err)
				}
				ctl := filepath.Join(downgradeDir, "sidraviactl")
				if out, err := exec.Command(ctl, "integration", "service", "replace").CombinedOutput(); err != nil {
					t.Fatalf("replace with exact old package: %s %v", out, err)
				}
				downgradedLayout, err := productlayout.ResolveTarget(downgradeDir, ns)
				if err != nil {
					t.Fatal(err)
				}
				downgraded := waitFixturePeer(t, downgradedLayout, update.Target{ID: downgradeDir, Version: source.Version, BuildID: source.BuildID, Layout: "portable"})
				defer downgraded.close()
				if downgraded.status.Mode != "systemd" {
					t.Fatal("downgraded daemon escaped systemd")
				}
				if _, err := os.Stat(filepath.Join(layout.InstitutionProfilesDirectory, "update-loopback.json")); err != nil {
					t.Fatal("original installation data did not survive downgrade", err)
				}
				t.Log("managed A→B→A with independent portable directories passed")
			}
			receipt := map[string]any{"passed": true, "kind": kind, "normalExit": true, "newBuildReady": true, "originalModeRestored": true, "disconnectedPreserved": scenario != "saved-configuration", "savedConfigurationRestoredOnce": scenario == "saved-configuration", "replayRejected": true, "syntheticTrustOverlayOnly": true, "productionApplyEnabled": false, "campusAuthentication": false}
			data, _ := json.MarshalIndent(receipt, "", "  ")
			if err := os.WriteFile(filepath.Join(owned, "result.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func buildFixtureProducts(t *testing.T, repo, directory, version, build, kind, bundle, overlay string, env []string) {
	t.Helper()
	roles := []string{"sidraviad", "sidraviactl", "sidraviaupdater"}
	if kind == "gui" {
		roles = append(roles, "sidravia_gui")
	}
	for _, role := range roles {
		sourcePackage := "./cmd/" + role
		if role == "sidravia_gui" {
			sourcePackage = "./cmd/sidravia-gui-launcher"
		}
		command := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-overlay", overlay, "-ldflags", "-X main.ProductVersion="+version+" -X main.BuildID="+build, "-o", filepath.Join(directory, role), sourcePackage)
		command.Dir = repo
		command.Env = append(env, "GOTOOLCHAIN=local", "CGO_ENABLED=0")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %s %v", role, out, err)
		}
	}
	if kind == "gui" {
		err := filepath.WalkDir(bundle, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(bundle, path)
			if err != nil {
				return err
			}
			if !(rel == "sidravia_gui" || strings.HasPrefix(rel, "lib/") || strings.HasPrefix(rel, "data/")) {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrInvalid
			}
			dest := filepath.Join(directory, "gui-runtime", rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if rel == "sidravia_gui" {
				mode = 0755
			}
			return os.WriteFile(dest, data, mode)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func fixtureArchive(t *testing.T, directory, version, build, kind string) testImage {
	t.Helper()
	contents := map[string]string{}
	modes := map[string]os.FileMode{}
	m := packagefiles.Manifest{SchemaVersion: 1, Version: version, BuildID: build, OS: "linux", Arch: runtime.GOARCH, PayloadKind: kind}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, _ := filepath.Rel(directory, path)
		name = filepath.ToSlash(name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		contents[name] = string(data)
		modes[name] = info.Mode().Perm()
		m.Files = append(m.Files, packagefiles.File{Path: name, Size: int64(len(data)), SHA256: digest(data), Mode: uint32(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := packagefiles.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	archiveFiles := map[string]string{}
	for k, v := range contents {
		archiveFiles[k] = v
	}
	archiveFiles[packagefiles.Name] = string(data)
	archiveFiles["sidravia.portable"] = ""
	modes[packagefiles.Name] = 0644
	modes["sidravia.portable"] = 0644
	names := []string{}
	for name := range archiveFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	for _, name := range names {
		body := archiveFiles[name]
		if err := writer.WriteHeader(&tar.Header{Name: "sidravia-v" + version + "-linux-" + runtime.GOARCH + "/" + name, Mode: int64(modes[name]), Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return signImage(t, m, data, buffer.Bytes(), contents)
}
func waitFixturePeer(t *testing.T, layout productlayout.Layout, identity update.Target) *applicationPeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		peer, err := probeApplication(ctx, layout, identity)
		if err == nil && peer != nil {
			return peer
		}
		if err := pause(ctx); err != nil {
			t.Fatal("fixture readiness", err)
		}
	}
}
