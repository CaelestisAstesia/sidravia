//go:build windows

package update

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/launch"
	"sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"
	"sidravia/internal/update/internal/testfixture"
)

func TestCancelQtMaintenance(t *testing.T) {
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	for _, test := range []struct {
		name, script string
		waitForExit  bool
	}{
		{name: "running", script: "Start-Sleep -Seconds 10"},
		{name: "already exited", script: "exit 0", waitForExit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			child := exec.Command(powerShell, "-NoProfile", "-NonInteractive", "-Command", test.script)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if child.ProcessState == nil {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			})
			if test.waitForExit {
				var result uint32
				var waitErr error
				handleErr := child.Process.WithHandle(func(handle uintptr) {
					result, waitErr = windows.WaitForSingleObject(windows.Handle(handle), 5000)
				})
				if handleErr != nil || waitErr != nil || result != windows.WAIT_OBJECT_0 {
					t.Fatalf("child did not exit: result=%d wait=%v handle=%v", result, waitErr, handleErr)
				}
			}
			if err := cancelQtMaintenance(child); err != nil {
				t.Fatal(err)
			}
			if child.ProcessState == nil {
				t.Fatal("maintenance child was not reaped")
			}
		})
	}
}

// TestQtBuildCommandFixture prepares test-only copies of the source release's
// public commands with the disposable publisher and a local release feed.
func TestQtBuildCommandFixture(t *testing.T) {
	path := os.Getenv("SIDRAVIA_TEST_QT_COMMAND_FIXTURE")
	if path == "" {
		t.Skip("requires a Qt command fixture request")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("command fixture request path must be absolute")
	}
	var fixture struct {
		Repo, Feed, SourceVersion, SourceBuildID, MaintenanceSHA256 string
		PublicKey                                                   []byte
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(fixture.Repo) || !filepath.IsAbs(fixture.Feed) ||
		len(fixture.PublicKey) != ed25519.PublicKeySize || len(fixture.MaintenanceSHA256) != 64 {
		t.Fatal("invalid command fixture request")
	}
	output := filepath.Join(filepath.Dir(path), "commands")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	overlay := testfixture.NewWindowsOverlay(t, fixture.Repo, t.TempDir(), fixture.Feed, fixture.PublicKey)
	overlay.BuildCommands(overlay.Write(), output, fixture.SourceVersion, fixture.SourceBuildID,
		fixture.MaintenanceSHA256, "sidraviactl", "sidraviaupdater")
}

// The isolated Windows system suite supplies two actual Qt distribution builds
// and disposable signed release files. The production updater has no test trust
// switch. This test starts the same handoff that its authenticated apply uses.
func TestQtNativeHandoff(t *testing.T) {
	path := os.Getenv("SIDRAVIA_TEST_QT_HANDOFF")
	if path == "" {
		t.Skip("requires isolated Windows Qt release fixtures")
	}
	if os.Getenv(launch.EnvNamespace) == "" {
		t.Fatal("native handoff requires an isolated application namespace")
	}
	var fixture struct {
		Installation, SourceVersion, SourceBuild string
		Archive, Cache, MaintenanceSHA256        string
		PublicKey, Manifest, Signature           []byte
		Candidate                                release.Candidate
		RejectArchive                            bool
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	trust, err := release.NewPublisherKeys([]ed25519.PublicKey{fixture.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := trust.Verify(fixture.Manifest, fixture.Signature, fixture.Candidate.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(fixture.Installation) || !filepath.IsAbs(fixture.Cache) {
		t.Fatal("fixture directories must be explicit and absolute")
	}
	t.Cleanup(func() { request.QtMaintenanceDigest = "" })
	request.QtMaintenanceDigest = fixture.MaintenanceSHA256
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	target := layout.Layout{Mode: layout.ModeInstalled, ExecutableDirectory: fixture.Installation}
	id := daemonctl.Identity{ProductVersion: fixture.SourceVersion, BuildID: fixture.SourceBuild}
	var beforePID int
	if fixture.RejectArchive {
		before, err := daemonctl.Probe(id)
		if err != nil || before.State != daemonctl.ProbeReachable {
			t.Fatalf("old daemon must answer before the failed update: state=%v err=%v", before.State, err)
		}
		beforePID = before.Info.PID
	}
	result, err := applyQt(ctx, fixture.Cache, target, id, fixture.Candidate, proof, fixture.Archive)
	if fixture.RejectArchive {
		if result != nil || !errors.Is(err, release.ErrUntrusted) {
			t.Fatalf("corrupt downloaded repository should be rejected before handoff, got result=%+v err=%v", result, err)
		}
		after, err := daemonctl.Probe(id)
		if err != nil || after.State != daemonctl.ProbeReachable || after.Info.PID != beforePID {
			t.Fatalf("same old daemon must answer after the failed update: state=%v err=%v", after.State, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !result.HandedOver || result.FilesApplied {
		t.Fatalf("unexpected handoff response: %+v", result)
	}
}
