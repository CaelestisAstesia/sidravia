//go:build linux

package protection_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"sidravia/internal/daemon/logging/internal/protection"
)

func TestFileRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logs, "sidraviad.log")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := protection.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.File(path); err == nil {
		t.Fatal("FIFO accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated data changed: %q, %v", data, err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("unrelated file permissions = %o, want 644", got)
	}
}
