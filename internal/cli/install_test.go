package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sidravia/internal/productlayout"
)

func TestPathWithEntry(t *testing.T) {
	tests := []struct {
		name  string
		value string
		entry string
		want  string
	}{
		{"empty value", "", `C:\Program Files\Sidravia`, `C:\Program Files\Sidravia`},
		{"already present exact", `C:\A;C:\Program Files\Sidravia`, `C:\Program Files\Sidravia`, `C:\A;C:\Program Files\Sidravia`},
		{"already present case-insensitive", `C:\A;C:\program files\sidravia`, `C:\Program Files\Sidravia`, `C:\A;C:\program files\sidravia`},
		{"already present trailing separator", `C:\A;C:\Program Files\Sidravia\;`, `C:\Program Files\Sidravia`, `C:\A;C:\Program Files\Sidravia\;`},
		{"append trailing separator", `C:\A;`, `C:\Program Files\Sidravia`, `C:\A;C:\Program Files\Sidravia`},
		{"append no trailing separator", `C:\A`, `C:\Program Files\Sidravia`, `C:\A;C:\Program Files\Sidravia`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithEntry(tc.value, tc.entry); got != tc.want {
				t.Errorf("pathWithEntry(%q, %q) = %q, want %q", tc.value, tc.entry, got, tc.want)
			}
		})
	}
}

func TestPathWithoutEntry(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		entry       string
		wantValue   string
		wantChanged bool
	}{
		{"present exact", `C:\A;C:\Program Files\Sidravia`, `C:\Program Files\Sidravia`, `C:\A`, true},
		{"present case-insensitive", `C:\A;C:\program files\sidravia`, `C:\Program Files\Sidravia`, `C:\A`, true},
		{"present trailing separator", `C:\A;C:\Program Files\Sidravia;`, `C:\Program Files\Sidravia`, `C:\A`, true},
		{"present only", `C:\Program Files\Sidravia`, `C:\Program Files\Sidravia`, ``, true},
		{"removed only first", `C:\A;C:\Program Files\Sidravia;C:\Program Files\Sidravia;C:\B`, `C:\Program Files\Sidravia`, `C:\A;C:\Program Files\Sidravia;C:\B`, true},
		{"absent", `C:\A`, `C:\Program Files\Sidravia`, `C:\A`, false},
		{"empty value absent", "", `C:\Program Files\Sidravia`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotValue, gotChanged := pathWithoutEntry(tc.value, tc.entry)
			if gotValue != tc.wantValue || gotChanged != tc.wantChanged {
				t.Errorf("pathWithoutEntry(%q, %q) = (%q, %v), want (%q, %v)", tc.value, tc.entry, gotValue, gotChanged, tc.wantValue, tc.wantChanged)
			}
		})
	}
}

func TestPathValueKind(t *testing.T) {
	if got := pathValueKind(0); got != pathKindExpandSZ {
		t.Errorf("pathValueKind(0) = %d, want EXPAND_SZ", got)
	}
	if got := pathValueKind(pathKindSZ); got != pathKindSZ {
		t.Errorf("pathValueKind(SZ) = %d, want SZ", got)
	}
	if got := pathValueKind(pathKindExpandSZ); got != pathKindExpandSZ {
		t.Errorf("pathValueKind(EXPAND_SZ) = %d, want EXPAND_SZ", got)
	}
	if got := pathValueKind(99); got != pathKindExpandSZ {
		t.Errorf("pathValueKind(99) = %d, want EXPAND_SZ", got)
	}
}

func TestTaskAction(t *testing.T) {
	if got := taskAction(`C:\Program Files\Sidravia`, "debug"); got != `"C:\Program Files\Sidravia\sidravia.exe" daemon start --log-level debug` {
		t.Errorf("taskAction = %q", got)
	}
	if got := taskAction(`C:\Sidravia`, ""); got != `"C:\Sidravia\sidravia.exe" daemon start --log-level info` {
		t.Errorf("taskAction default = %q", got)
	}
}

func TestSchTasksArgs(t *testing.T) {
	const action = `"C:\Program Files\Sidravia\sidravia.exe" daemon start --log-level trace`
	create, query, del := schTasksArgs(action, installTaskName)
	wantCreate := []string{"/Create", "/F", "/TN", installTaskName, "/TR", action, "/SC", "ONLOGON"}
	wantQuery := []string{"/Query", "/TN", installTaskName}
	wantDelete := []string{"/Delete", "/TN", installTaskName, "/F"}
	if !reflect.DeepEqual(create, wantCreate) {
		t.Errorf("create = %v, want %v", create, wantCreate)
	}
	if !reflect.DeepEqual(query, wantQuery) {
		t.Errorf("query = %v, want %v", query, wantQuery)
	}
	if !reflect.DeepEqual(del, wantDelete) {
		t.Errorf("delete = %v, want %v", del, wantDelete)
	}
}

func TestResolveInstallDirectory(t *testing.T) {
	installed := func() (productlayout.Layout, error) {
		return productlayout.Layout{Mode: productlayout.ModeInstalled, ExecutableDirectory: `C:\Sidravia`}, nil
	}
	dir, err := resolveInstallDirectory(installed)
	if err != nil {
		t.Fatalf("installed: %v", err)
	}
	if dir != `C:\Sidravia` {
		t.Errorf("dir = %q, want C:\\Sidravia", dir)
	}

	portable := func() (productlayout.Layout, error) {
		return productlayout.Layout{Mode: productlayout.ModePortable, ExecutableDirectory: `C:\portable`}, nil
	}
	if _, err := resolveInstallDirectory(portable); err == nil {
		t.Error("portable mode: expected error")
	}

	boom := func() (productlayout.Layout, error) { return productlayout.Layout{}, errors.New("boom") }
	if _, err := resolveInstallDirectory(boom); !strings.Contains(err.Error(), "boom") {
		t.Errorf("resolve error cause not preserved: %v", err)
	}
}

func TestVerifyInstallDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sidraviad.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	if err := verifyInstallDirectory(dir, os.Stat); err != nil {
		t.Errorf("complete dir: %v", err)
	}
	if err := verifyInstallDirectory(t.TempDir(), os.Stat); err == nil {
		t.Error("missing sibling: expected error")
	}
}

// fakeInstallSystem records every call and lets tests assert orchestration.
type fakeInstallSystem struct {
	pathValue string
	pathKind  uint32
	broadcast int
	exists    bool
	writes    []string
	created   []string
	deleted   []string
}

func (f *fakeInstallSystem) readUserPath() (string, uint32, error) {
	return f.pathValue, f.pathKind, nil
}
func (f *fakeInstallSystem) writeUserPath(value string, kind uint32) error {
	f.writes = append(f.writes, value)
	f.pathValue = value
	f.pathKind = kind
	return nil
}
func (f *fakeInstallSystem) broadcastEnvironmentChange() error { f.broadcast++; return nil }
func (f *fakeInstallSystem) taskExists(string) (bool, error)   { return f.exists, nil }
func (f *fakeInstallSystem) createTask(name, action string) error {
	f.created = append(f.created, name+"|"+action)
	return nil
}
func (f *fakeInstallSystem) deleteTask(name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

func TestRunInstall(t *testing.T) {
	t.Run("writes path and creates task", func(t *testing.T) {
		sys := &fakeInstallSystem{}
		if err := runInstall(sys, `C:\Sidravia`, "debug"); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
		if len(sys.writes) != 1 || sys.writes[0] != `C:\Sidravia` {
			t.Errorf("writes = %v, want [C:\\Sidravia]", sys.writes)
		}
		if sys.pathKind != pathKindExpandSZ {
			t.Errorf("kind = %d, want EXPAND_SZ", sys.pathKind)
		}
		if sys.broadcast != 1 {
			t.Errorf("broadcast = %d, want 1", sys.broadcast)
		}
		wantAction := `"C:\Sidravia\sidravia.exe" daemon start --log-level debug`
		if !reflect.DeepEqual(sys.created, []string{installTaskName + "|" + wantAction}) {
			t.Errorf("created = %v", sys.created)
		}
	})

	t.Run("idempotent path", func(t *testing.T) {
		sys := &fakeInstallSystem{pathValue: `C:\Sidravia`, pathKind: pathKindSZ}
		if err := runInstall(sys, `C:\Sidravia`, "info"); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
		if len(sys.writes) != 0 {
			t.Errorf("writes = %v, want none", sys.writes)
		}
		if sys.broadcast != 0 {
			t.Errorf("broadcast = %d, want 0", sys.broadcast)
		}
		if len(sys.created) != 1 {
			t.Errorf("created = %v, want 1 task", sys.created)
		}
	})

	t.Run("preserves existing kind", func(t *testing.T) {
		sys := &fakeInstallSystem{pathValue: `C:\A`, pathKind: pathKindSZ}
		if err := runInstall(sys, `C:\Sidravia`, "info"); err != nil {
			t.Fatalf("runInstall: %v", err)
		}
		if sys.pathKind != pathKindSZ {
			t.Errorf("kind = %d, want SZ preserved", sys.pathKind)
		}
		if len(sys.writes) != 1 || sys.writes[0] != `C:\A;C:\Sidravia` {
			t.Errorf("writes = %v", sys.writes)
		}
	})
}

func TestRunUninstall(t *testing.T) {
	t.Run("removes path and deletes task", func(t *testing.T) {
		sys := &fakeInstallSystem{pathValue: `C:\A;C:\Sidravia`, pathKind: pathKindExpandSZ, exists: true}
		if err := runUninstall(sys, `C:\Sidravia`); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
		if len(sys.writes) != 1 || sys.writes[0] != `C:\A` {
			t.Errorf("writes = %v, want [C:\\A]", sys.writes)
		}
		if sys.broadcast != 1 {
			t.Errorf("broadcast = %d, want 1", sys.broadcast)
		}
		if !reflect.DeepEqual(sys.deleted, []string{installTaskName}) {
			t.Errorf("deleted = %v", sys.deleted)
		}
	})

	t.Run("missing task is success", func(t *testing.T) {
		sys := &fakeInstallSystem{pathValue: `C:\Sidravia`, exists: false}
		if err := runUninstall(sys, `C:\Sidravia`); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
		if len(sys.writes) != 1 {
			t.Errorf("writes = %v", sys.writes)
		}
		if len(sys.deleted) != 0 {
			t.Errorf("deleted = %v, want none", sys.deleted)
		}
	})

	t.Run("absent path entry keeps writes empty but deletes task", func(t *testing.T) {
		sys := &fakeInstallSystem{pathValue: `C:\A`, exists: true}
		if err := runUninstall(sys, `C:\Sidravia`); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
		if len(sys.writes) != 0 {
			t.Errorf("writes = %v, want none", sys.writes)
		}
		if sys.broadcast != 0 {
			t.Errorf("broadcast = %d, want 0", sys.broadcast)
		}
		if !reflect.DeepEqual(sys.deleted, []string{installTaskName}) {
			t.Errorf("deleted = %v", sys.deleted)
		}
	})
}
