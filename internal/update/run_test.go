package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/release"
	"sidravia/internal/update/internal/request"
)

func TestNonMutatingCommandsAndExplicitApplyConfirmation(t *testing.T) {
	applyCode := "apply_unavailable"
	if request.ApplyPlatformSupported() && release.PublisherTrust().Configured() {
		applyCode = "confirmation_required"
	}
	for _, item := range []struct {
		args []string
		code string
		exit int
	}{{[]string{"--help"}, "ok", 0}, {[]string{"version"}, "ok", 0}, {[]string{"invalid"}, "invalid_arguments", 1}, {[]string{"download"}, "invalid_arguments", 1}, {[]string{"apply", "--candidate", "1:2"}, applyCode, 1}} {
		var out bytes.Buffer
		exit := Run(context.Background(), item.args, "0.4.0-alpha.1", "fixture", &out)
		var result request.Outcome
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != item.code || exit != item.exit {
			t.Fatalf("%v %s %d", item.args, result.Code, exit)
		}
	}
}

func TestRPMOwnershipRequiresExactInstalledMarker(t *testing.T) {
	root := t.TempDir()
	layout := productlayout.Layout{Mode: productlayout.ModeInstalled, ExecutableDirectory: root}
	if productlayout.RPMManaged(layout) {
		t.Fatal("unmarked installation treated as RPM")
	}
	path := filepath.Join(root, "sidravia.rpm")
	for _, value := range []string{"", "other-package", "sidravia-rpm-v1\n"} {
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
		if productlayout.RPMManaged(layout) != (value == "sidravia-rpm-v1\n") {
			t.Fatal("marker ownership mismatch")
		}
	}
	layout.Mode = productlayout.ModePortable
	if productlayout.RPMManaged(layout) {
		t.Fatal("portable treated as RPM")
	}
}

func TestDefaultPackageKindPreservesGUIIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux executable permission semantics")
	}
	root := t.TempDir()
	layout := productlayout.Layout{Mode: productlayout.ModePortable, ExecutableDirectory: root}
	if got := request.DefaultPackageKind("linux", "amd64", layout); got != "core-portable" {
		t.Fatal(got)
	}
	if err := os.Mkdir(filepath.Join(root, "sidravia_gui"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := request.DefaultPackageKind("linux", "amd64", layout); got != "core-portable" {
		t.Fatal("directory treated as GUI")
	}
	if err := os.Remove(filepath.Join(root, "sidravia_gui")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sidravia_gui"), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := request.DefaultPackageKind("linux", "amd64", layout); got != "gui-portable" {
		t.Fatal(got)
	}
	if got := request.DefaultPackageKind("linux", "arm64", layout); got != "core-portable" {
		t.Fatal("unsupported GUI arch")
	}
	layout.Mode = productlayout.ModeInstalled
	if got := request.DefaultPackageKind("windows", "amd64", layout); got != "setup" {
		t.Fatal(got)
	}
}

func TestPublisherTrustFailureHasStableSafeCode(t *testing.T) {
	err := fmt.Errorf("private release details: %w", release.ErrUntrusted)
	if request.SourceCode(err) != "publisher_untrusted" || strings.Contains(request.SourceMessage(err), "private release details") {
		t.Fatal("publisher failure lost its safe public boundary")
	}
}
