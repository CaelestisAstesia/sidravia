package daemonlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testWriter(t *testing.T, dir string, now *time.Time, policy retentionPolicy, fallback *bytes.Buffer) *Writer {
	t.Helper()
	p, err := newDaemonLogProtection()
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(dir, fallback, p, policy, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func writeRecord(t *testing.T, w *Writer, s string) {
	t.Helper()
	if n, err := w.Write([]byte(s)); err != nil || n != len(s) {
		t.Fatalf("write n=%d err=%v", n, err)
	}
}

func TestWriterRotatesBeforeSizeAndAcrossLocalDateAndAppendsAfterRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 23, 59, 0, 0, time.FixedZone("local", 8*3600))
	var fallback bytes.Buffer
	policy := retentionPolicy{10, 100, 7}
	w := testWriter(t, dir, &now, policy, &fallback)
	writeRecord(t, w, "first\n")
	writeRecord(t, w, "two\n")
	writeRecord(t, w, "next\n")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = testWriter(t, dir, &now, policy, &fallback)
	writeRecord(t, w, "more\n")
	now = now.Add(time.Minute)
	writeRecord(t, w, "day\n")
	for name, want := range map[string]string{"sidraviad-2026-10-12-000001.log": "first\ntwo\n", "sidraviad-2026-10-12-000002.log": "next\nmore\n", "sidraviad-2026-10-13-000001.log": "day\n"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
	if fallback.Len() != 0 {
		t.Fatalf("unexpected degradation: %s", fallback.String())
	}
}

func TestWriterRetentionUsesCalendarDaysAndPreservesUnownedFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for name := range map[string]bool{"sidraviad-2026-10-05-000001.log": true, "sidraviad-2026-10-06-000001.log": true, "sidraviad-2026-10-31-000001.log": true, "sidraviad-2026-02-30-000001.log": true, "other.log": true} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "sidraviad-2026-01-01-000001.log")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "other.log"), link); err != nil {
			t.Fatal(err)
		}
	}
	var fallback bytes.Buffer
	w := testWriter(t, dir, &now, retentionPolicy{10, 100, 7}, &fallback)
	writeRecord(t, w, "new\n")
	if _, err := os.Stat(filepath.Join(dir, "sidraviad-2026-10-05-000001.log")); !os.IsNotExist(err) {
		t.Fatalf("expired log retained: %v", err)
	}
	for _, name := range []string{"sidraviad-2026-10-06-000001.log", "sidraviad-2026-10-31-000001.log", "sidraviad-2026-02-30-000001.log", "other.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink changed: %v", err)
		}
	}
}

func TestWriterBudgetDeletesOldestAndCountsBootstrap(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"sidraviad.log", "sidraviad-2026-10-10-000001.log", "sidraviad-2026-10-11-000001.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("12345678"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var fallback bytes.Buffer
	w := testWriter(t, dir, &now, retentionPolicy{10, 30, 7}, &fallback)
	writeRecord(t, w, "1234567\n")
	if _, err := os.Stat(filepath.Join(dir, "sidraviad-2026-10-10-000001.log")); !os.IsNotExist(err) {
		t.Fatal("oldest segment not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "sidraviad.log")); err != nil {
		t.Fatal("active bootstrap removed", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sidraviad-2026-10-11-000001.log")); err != nil {
		t.Fatal("newer segment removed", err)
	}
}

func TestWriterFailureWarnsOnceAndRecoversWithoutCancellingCaller(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var fallback bytes.Buffer
	w := testWriter(t, dir, &now, retentionPolicy{10, 20, 7}, &fallback)
	writeRecord(t, w, "ok\n")
	bootstrap := filepath.Join(dir, "sidraviad.log")
	if err := os.WriteFile(bootstrap, []byte(strings.Repeat("x", 20)), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := w.Write([]byte("next\n")); err == nil {
			t.Fatal("over-budget record accepted")
		}
	}
	if strings.Count(fallback.String(), "daemon_log_write_failed") != 1 {
		t.Fatalf("notice count: %s", fallback.String())
	}
	if err := os.Truncate(bootstrap, 0); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, w, "back\n")
	if _, err := w.Write([]byte(strings.Repeat("x", 11))); err == nil {
		t.Fatal("oversized record accepted")
	}
	if strings.Count(fallback.String(), "daemon_log_write_failed") != 2 {
		t.Fatal("recovered writer did not reset notice")
	}
	writeRecord(t, w, "ok\n")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("closed\n")); err == nil {
		t.Fatal("closed writer reopened")
	}
}

func TestWriterConcurrentRecordsRemainWhole(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var fallback bytes.Buffer
	w := testWriter(t, dir, &now, retentionPolicy{1000, 10000, 7}, &fallback)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := fmt.Sprintf("record-%03d\n", i)
			if _, err := w.Write([]byte(s)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := w.inventory()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		if f.size > 1000 {
			t.Fatal("segment too large")
		}
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			if seen[line] {
				t.Fatal("duplicate", line)
			}
			seen[line] = true
		}
	}
	for i := range 100 {
		if !seen[fmt.Sprintf("record-%03d", i)] {
			t.Fatal("missing record", i)
		}
	}
}

func TestWriterRejectsUnprotectedOutputBeforeAppending(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var fallback bytes.Buffer
	protection := daemonLogProtectionFunc{directory: func(string) error { return nil }, file: func(string) error { return fmt.Errorf("private filesystem detail") }}
	w, err := newWriter(dir, &fallback, protection, retentionPolicy{10, 20, 7}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("secret\n")); err == nil {
		t.Fatal("unprotected write accepted")
	}
	if strings.Contains(fallback.String(), "private") || strings.Contains(fallback.String(), "secret") {
		t.Fatal("unsafe notice")
	}
	files, err := w.inventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.size != 0 {
			t.Fatal("data appended before protection")
		}
	}
}
