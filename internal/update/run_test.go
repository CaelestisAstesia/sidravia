package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sidravia/internal/productlayout"
	"sidravia/internal/release"
	"strings"
	"testing"
)

func TestNonMutatingCommandsAndExplicitApplyConfirmation(t *testing.T) {
	applyCode := "apply_unavailable"
	if applyPlatformSupported() && release.PublisherTrust().Configured() {
		applyCode = "confirmation_required"
	}
	for _, item := range []struct {
		args []string
		code string
		exit int
	}{{[]string{"--help"}, "ok", 0}, {[]string{"version"}, "ok", 0}, {[]string{"invalid"}, "invalid_arguments", 1}, {[]string{"download"}, "invalid_arguments", 1}, {[]string{"apply", "--candidate", "1:2"}, applyCode, 1}} {
		var out bytes.Buffer
		exit := Run(context.Background(), item.args, "0.4.0-alpha.1", "fixture", &out)
		var result response
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != item.code || exit != item.exit {
			t.Fatalf("%v %s %d", item.args, result.Code, exit)
		}
	}
}
func TestCacheLockIsExclusive(t *testing.T) {
	path := t.TempDir() + "/lock"
	release, err := lockCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockCache(path); err == nil {
		second()
		release()
		t.Fatal("parallel owner")
	}
	release()
	again, err := lockCache(path)
	if err != nil {
		t.Fatal(err)
	}
	again()
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
	if got := defaultPackageKind("linux", "amd64", layout); got != "core-portable" {
		t.Fatal(got)
	}
	if err := os.Mkdir(filepath.Join(root, "sidravia_gui"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := defaultPackageKind("linux", "amd64", layout); got != "core-portable" {
		t.Fatal("directory treated as GUI")
	}
	if err := os.Remove(filepath.Join(root, "sidravia_gui")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sidravia_gui"), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := defaultPackageKind("linux", "amd64", layout); got != "gui-portable" {
		t.Fatal(got)
	}
	if got := defaultPackageKind("linux", "arm64", layout); got != "core-portable" {
		t.Fatal("unsupported GUI arch")
	}
	layout.Mode = productlayout.ModeInstalled
	if got := defaultPackageKind("windows", "amd64", layout); got != "setup" {
		t.Fatal(got)
	}
}

func TestPublisherTrustFailureHasStableSafeCode(t *testing.T) {
	err := fmt.Errorf("private release details: %w", release.ErrUntrusted)
	if safeCode(err) != "publisher_untrusted" || strings.Contains(safeMessage(err), "private release details") {
		t.Fatal("publisher failure lost its safe public boundary")
	}
}
