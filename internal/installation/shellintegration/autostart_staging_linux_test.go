//go:build linux

package shellintegration

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRPMDesktopScanSkipsUnownedHardLink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "autostart")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "other.desktop")
	if err := os.WriteFile(foreign, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, desktopEntryName+"-Zm9v.desktop")
	if err := os.Link(foreign, link); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, desktopEntryName+".desktop")
	entry := "[Desktop Entry]\nType=Application\nName=Sidravia\nComment=Sidravia session autostart\nIcon=/usr/lib64/sidravia/gui-runtime/data/icons/sidravia.png\nTerminal=false\nTryExec=/usr/lib64/sidravia/sidravia_gui\nExec=\"/usr/bin/env\" --chdir \"/usr/lib64/sidravia\" \"SIDRAVIA_NAMESPACE=\" \"./sidravia_gui\" --start-hidden\nX-Sidravia-Owner=linux-desktop-v2\n"
	if err := os.WriteFile(owned, []byte(entry), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := packageAutostartFiles(root, "rpm")
	if err != nil || !slices.Equal(files, []string{owned}) {
		t.Fatalf("owned entries = %v, error = %v", files, err)
	}
	modern := strings.Replace(entry, " --start-hidden", "", 1)
	if !ownsPackageDesktopEntry(modern, "", "/usr/lib64/sidravia") ||
		ownsPackageDesktopEntry(strings.Replace(modern, "\"./sidravia_gui\"", "\"./sidravia_gui\" --vendor-option", 1), "", "/usr/lib64/sidravia") {
		t.Fatal("modern GUI autostart must use exact no-argument ownership")
	}
}

func TestInterruptedAutostartStageIsRemovedWithoutDeletingUnexpectedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), packageAutostartStage("rpm")+desktopEntryName+".desktop-0123456789abcdef0123456789abcdef")
	if !autostartStageName(filepath.Base(dir), packageAutostartStage("rpm")) ||
		autostartStageName(filepath.Base(dir), portableAutostartStage) ||
		autostartStageName(filepath.Base(dir)+"0", packageAutostartStage("rpm")) {
		t.Fatal("incorrect staging name validation")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, "entry")
	if err := os.WriteFile(staged, []byte("[Desktop Ent"), 0600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "user-file")
	if err := os.WriteFile(extra, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	// Inspect the complete directory before unlinking any known entry.
	if err := removeAutostartStage(dir); !errors.Is(err, errRPMDesktopConflict) {
		t.Fatalf("unexpected object: got %v", err)
	}
	for _, path := range []string{extra, staged} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := removeAutostartStage(dir); !errors.Is(err, errRPMDesktopConflict) {
		t.Fatalf("unmarked payload: got %v", err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, autostartStageMarker), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeAutostartStage(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging remains: %v", err)
	}
}

func TestPackageAutostartEraseRetriesAfterWriterReleasesDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "autostart")
	stage := filepath.Join(dir, packageAutostartStage("deb")+desktopEntryName+".desktop-0123456789abcdef0123456789abcdef")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, desktopEntryName+".desktop")
	entry := "[Desktop Entry]\nType=Application\nName=Sidravia\nComment=Sidravia session autostart\nIcon=/usr/lib/sidravia/gui-runtime/data/icons/sidravia.png\nTerminal=false\nTryExec=/usr/lib/sidravia/sidravia_gui\nExec=\"/usr/bin/env\" --chdir \"/usr/lib/sidravia\" \"SIDRAVIA_NAMESPACE=\" \"./sidravia_gui\"\nX-Sidravia-Owner=linux-desktop-v2\n"
	marker := filepath.Join(stage, autostartStageMarker)
	for path, content := range map[string]string{owned: entry, marker: "", filepath.Join(stage, "entry"): "incomplete"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate Flutter's independent descriptor for the same directory inode.
	writer, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := unix.Flock(int(writer.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPackageAutostartRoot(root, "deb"); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatalf("cleanup must report contention for retry, got %v", err)
	}
	for _, path := range []string{owned, marker, stage} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleaned files while writer still held the lock: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPackageAutostartRoot(root, "deb"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{owned, stage} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned path remains after retry: %s %v", path, err)
		}
	}
}
