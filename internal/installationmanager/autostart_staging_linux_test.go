//go:build linux

package installationmanager

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
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
	files, err := rpmAutostartFiles(root)
	if err != nil || !slices.Equal(files, []string{owned}) {
		t.Fatalf("owned entries = %v, error = %v", files, err)
	}
}

func TestInterruptedAutostartStageIsRemovedWithoutDeletingUnexpectedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), rpmAutostartStage+desktopEntryName+".desktop-0123456789abcdef0123456789abcdef")
	if !autostartStageName(filepath.Base(dir), rpmAutostartStage) ||
		autostartStageName(filepath.Base(dir), portableAutostartStage) ||
		autostartStageName(filepath.Base(dir)+"0", rpmAutostartStage) {
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
	if err := removeAutostartStage(dir); !errors.Is(err, ErrServiceConflict) {
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
	if err := removeAutostartStage(dir); !errors.Is(err, ErrServiceConflict) {
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
