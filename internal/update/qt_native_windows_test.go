//go:build windows

package update

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/launch"
	"sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"
)

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
	result, err := applyQt(ctx, fixture.Cache, target, id, fixture.Candidate, proof, fixture.Archive)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HandedOver || result.FilesApplied {
		t.Fatalf("unexpected handoff response: %+v", result)
	}
}
