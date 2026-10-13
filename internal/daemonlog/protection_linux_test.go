//go:build linux

package daemonlog

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func checkLogMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	s, e := os.Stat(path)
	if e != nil || s.Mode().Perm() != mode {
		t.Fatalf("log permissions=%v error=%v, want %v", s, e, mode)
	}
}
func TestLinuxExistingLogsPrivate(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(map[bool]string{false: "append", true: "rotate"}[rotate], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "logs")
			if e := os.Mkdir(dir, 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.Chmod(dir, 0755); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "sidraviad.log")
			size := int64(1)
			if rotate {
				size = daemonLogRotateThreshold
			}
			if e := os.WriteFile(path, []byte("x"), 0644); e != nil {
				t.Fatal(e)
			}
			if e := os.Truncate(path, size); e != nil {
				t.Fatal(e)
			}
			if e := os.Chmod(path, 0644); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(path+".1", []byte("old"), 0644); e != nil {
				t.Fatal(e)
			}
			if e := os.Chmod(path+".1", 0644); e != nil {
				t.Fatal(e)
			}
			f, e := PrepareBootstrap(path)
			if e != nil {
				t.Fatal(e)
			}
			if e := f.Close(); e != nil {
				t.Fatal(e)
			}
			checkLogMode(t, dir, 0700)
			checkLogMode(t, path, 0600)
			checkLogMode(t, path+".1", 0600)
		})
	}
}
func TestLinuxDiagnosticSegmentPrivate(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	path := filepath.Join(dir, "sidraviad-"+now.Format("2006-01-02")+"-000001.log")
	if e := os.WriteFile(path, []byte("old"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	w, e := NewWriter(dir, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := w.Write([]byte("new")); e != nil {
		t.Fatal(e)
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	checkLogMode(t, dir, 0700)
	checkLogMode(t, path, 0600)
}
func TestLinuxLogRefusesRedirectedObjects(t *testing.T) {
	for _, kind := range []string{"symlink", "broken-symlink", "fifo", "hardlink", "directory", "parent-symlink", "backup-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "logs")
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			outside := filepath.Join(root, "outside")
			if e := os.WriteFile(outside, []byte("unrelated"), 0644); e != nil {
				t.Fatal(e)
			}
			if e := os.Chmod(outside, 0644); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "sidraviad.log")
			target := outside
			switch kind {
			case "parent-symlink":
				if e := os.Symlink(dir, filepath.Join(root, "redirect")); e != nil {
					t.Fatal(e)
				}
				path = filepath.Join(root, "redirect", "sidraviad.log")
			case "symlink":
				if e := os.Symlink(outside, path); e != nil {
					t.Fatal(e)
				}
			case "broken-symlink":
				target = filepath.Join(root, "not-created")
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			case "backup-symlink":
				if e := os.Symlink(outside, path+".1"); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				if e := syscall.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(outside, path); e != nil {
					t.Fatal(e)
				}
			case "directory":
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			}
			// Avoid blocking the baseline FIFO open; known protection entry is tested directly.
			if kind == "fifo" {
				p, e := newDaemonLogProtection()
				if e != nil {
					t.Fatal(e)
				}
				if e := p.protectFile(path); e == nil {
					t.Fatal("FIFO accepted")
				}
			} else {
				f, e := PrepareBootstrap(path)
				if f != nil {
					f.Close()
				}
				if e == nil {
					t.Error("redirected/nonregular log accepted")
				}
			}
			b, e := os.ReadFile(outside)
			if e != nil || string(b) != "unrelated" {
				t.Fatal("unrelated data changed")
			}
			checkLogMode(t, outside, 0644)
			if strings.HasPrefix(kind, "broken") {
				if _, e := os.Lstat(target); !os.IsNotExist(e) {
					t.Fatal("broken symlink target was created")
				}
			}
		})
	}
}
