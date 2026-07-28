package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareDaemonLogCreatesAndAppends(t *testing.T) {
	root := t.TempDir()
	file, err := prepareDaemonLog(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("first\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	file, err = prepareDaemonLog(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(root, "Sidravia", "logs", "sidraviad.log")
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
	logPath := filepath.Join(root, "Sidravia", "logs", "sidraviad.log")
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

	file, err := prepareDaemonLog(root)
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
	file, err = prepareDaemonLog(root)
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
	_, err := prepareDaemonLog(blocker)
	if err == nil {
		t.Fatal("expected filesystem error")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error does not preserve PathError: %v", err)
	}
}
