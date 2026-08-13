package clientbootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type daemonLogProtectionFunc struct {
	directory func(string) error
	file      func(string) error
}

func (protection daemonLogProtectionFunc) protectDirectory(path string) error {
	return protection.directory(path)
}

func (protection daemonLogProtectionFunc) protectFile(path string) error {
	return protection.file(path)
}

func TestPrepareDaemonLogCreatesAndAppends(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "logs", "sidraviad.log")
	file, err := prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	file, err = prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Fatalf("log = %q", got)
	}
	if _, err := os.Stat(logPath + ".1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected backup: %v", err)
	}
}

func TestPrepareDaemonLogRotatesAtThresholdAndReplacesBackup(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "logs", "sidraviad.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(logPath, daemonLogRotateThreshold); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath+".1", []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("new log size = %d", info.Size())
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.Stat(logPath + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if backup.Size() != daemonLogRotateThreshold {
		t.Fatalf("backup size = %d", backup.Size())
	}

	if err := os.Truncate(logPath, daemonLogRotateThreshold); err != nil {
		t.Fatal(err)
	}
	file, err = prepareDaemonLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(logPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("log entries = %d, want current plus one backup", len(entries))
	}
}

func TestPrepareDaemonLogPreservesFilesystemCause(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareDaemonLog(filepath.Join(blocker, "sidraviad.log"))
	if err == nil {
		t.Fatal("expected filesystem error")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error does not preserve PathError: %v", err)
	}
}

func TestPrepareDaemonLogProtectsDirectoryAndExistingLogsBeforeRotation(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "logs", "sidraviad.log")
	backupPath := logPath + ".1"
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(logPath, daemonLogRotateThreshold); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var calls []string
	operations := defaultDaemonLogOperations(daemonLogProtectionFunc{
		directory: func(path string) error { calls = append(calls, "directory:"+path); return nil },
		file:      func(path string) error { calls = append(calls, "file:"+path); return nil },
	})
	remove := operations.remove
	operations.remove = func(path string) error { calls = append(calls, "remove:"+path); return remove(path) }
	rename := operations.rename
	operations.rename = func(oldPath, newPath string) error {
		calls = append(calls, "rename:"+oldPath)
		return rename(oldPath, newPath)
	}

	file, err := prepareDaemonLogWith(logPath, operations)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"directory:" + filepath.Dir(logPath),
		"file:" + logPath,
		"file:" + backupPath,
		"remove:" + backupPath,
		"rename:" + logPath,
		"file:" + logPath,
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls[%d] = %q, want %q; all calls = %#v", index, calls[index], want[index], calls)
		}
	}
}

func TestPrepareDaemonLogExistingLogProtectionFailureStopsRotation(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		failingPath func(logPath string) string
		wantCalls   func(logPath string) []string
	}{
		{
			name:        "current",
			failingPath: func(logPath string) string { return logPath },
			wantCalls: func(logPath string) []string {
				return []string{"directory:" + filepath.Dir(logPath), "file:" + logPath}
			},
		},
		{
			name:        "backup",
			failingPath: func(logPath string) string { return logPath + ".1" },
			wantCalls: func(logPath string) []string {
				return []string{"directory:" + filepath.Dir(logPath), "file:" + logPath, "file:" + logPath + ".1"}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			logPath := filepath.Join(root, "logs", "sidraviad.log")
			backupPath := logPath + ".1"
			if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(logPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(logPath, daemonLogRotateThreshold); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backupPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}

			cause := errors.New("protect existing log")
			var calls []string
			operations := defaultDaemonLogOperations(daemonLogProtectionFunc{
				directory: func(path string) error { calls = append(calls, "directory:"+path); return nil },
				file: func(path string) error {
					calls = append(calls, "file:"+path)
					if path == testCase.failingPath(logPath) {
						return cause
					}
					return nil
				},
			})
			operations.remove = func(string) error { t.Fatal("remove after existing-log protection failure"); return nil }
			operations.rename = func(string, string) error { t.Fatal("rename after existing-log protection failure"); return nil }
			operations.openFile = func(string, int, os.FileMode) (*os.File, error) {
				t.Fatal("open after existing-log protection failure")
				return nil, nil
			}

			_, err := prepareDaemonLogWith(logPath, operations)
			if !errors.Is(err, cause) {
				t.Fatalf("error = %v, want preserved cause", err)
			}
			want := testCase.wantCalls(logPath)
			if len(calls) != len(want) {
				t.Fatalf("calls = %#v, want %#v", calls, want)
			}
			for index := range want {
				if calls[index] != want[index] {
					t.Fatalf("calls[%d] = %q, want %q; all calls = %#v", index, calls[index], want[index], calls)
				}
			}
		})
	}
}

func TestPrepareDaemonLogProtectionFailureStopsBeforeStatAndClosesNewFile(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "logs", "sidraviad.log")
	cause := errors.New("protect log")
	var opened *os.File
	operations := defaultDaemonLogOperations(daemonLogProtectionFunc{
		directory: func(string) error { return nil },
		file:      func(string) error { return cause },
	})
	operations.stat = func(string) (os.FileInfo, error) { t.Fatal("stat after directory protection failure"); return nil, nil }
	operations.openFile = func(path string, flag int, perm os.FileMode) (*os.File, error) {
		file, err := os.OpenFile(path, flag, perm)
		opened = file
		return file, err
	}
	operations.protection = daemonLogProtectionFunc{
		directory: func(string) error { return cause },
		file:      func(string) error { return nil },
	}
	_, err := prepareDaemonLogWith(logPath, operations)
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want preserved cause", err)
	}
	if opened != nil {
		t.Fatal("opened a log after directory protection failure")
	}

	operations = defaultDaemonLogOperations(daemonLogProtectionFunc{
		directory: func(string) error { return nil },
		file:      func(string) error { return cause },
	})
	operations.stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	operations.openFile = func(path string, flag int, perm os.FileMode) (*os.File, error) {
		file, openErr := os.OpenFile(path, flag, perm)
		opened = file
		return file, openErr
	}
	_, err = prepareDaemonLogWith(logPath, operations)
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want preserved cause", err)
	}
	if opened == nil {
		t.Fatal("new log was not opened")
	}
	if _, statErr := opened.Stat(); statErr == nil {
		t.Fatal("new log handle remains open after protection failure")
	}
}
