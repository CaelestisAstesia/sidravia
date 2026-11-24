package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sidravia/internal/daemon/logging/internal/protection"
)

// Writer owns the daemon's dated diagnostics, independently of inherited
// bootstrap stderr. No timer or background goroutine owns its lifecycle.
type Writer struct {
	mu             sync.Mutex
	dir            string
	file           *os.File
	path, date     string
	closed, warned bool
	policy         retentionPolicy
	now            func() time.Time
	protection     protection.Policy
	fallback       io.Writer
}

// NewWriter protects the directory before accepting any diagnostic records.
// Only exact owned filenames are eligible for retention cleanup.
func NewWriter(dir string, fallback io.Writer) (*Writer, error) {
	p, err := protection.New()
	if err != nil {
		return nil, err
	}
	return newWriter(dir, fallback, p, retentionPolicy{10 * 1024 * 1024, 100 * 1024 * 1024, 7}, time.Now)
}

func newWriter(dir string, fallback io.Writer, fs protection.Policy, policy retentionPolicy, now func() time.Time) (*Writer, error) {
	if policy.fileBytes <= 0 || policy.totalBytes < policy.fileBytes || policy.days < 1 {
		return nil, errors.New("invalid diagnostic retention policy")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("diagnostic directory is not a regular directory")
	}
	if err := fs.Directory(dir); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, policy: policy, now: now, protection: fs, fallback: fallback}
	// Retention runs on the first startup record. A retention failure must
	// degrade diagnostics rather than prevent authentication startup.
	return w, nil
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	defer func() {
		if err == nil {
			w.warned = false
			return
		}
		if w.file != nil {
			err = errors.Join(err, w.file.Close())
			w.file = nil
			w.path = ""
		}
		if !w.warned && w.fallback != nil {
			// A fixed safe notice avoids recursion through the failed logger and
			// never exposes paths, packet bytes or filesystem error strings.
			_, noticeErr := io.WriteString(w.fallback, "level=WARN msg=\"诊断日志写入失败，认证继续运行\" event=daemon_log_write_failed\n")
			err = errors.Join(err, noticeErr)
			w.warned = true
		}
	}()
	if int64(len(p)) > w.policy.fileBytes {
		return 0, errors.New("diagnostic record exceeds segment limit")
	}
	now := w.now()
	date := now.Format("2006-01-02")
	if w.file != nil {
		info, e := w.file.Stat()
		if e != nil {
			return 0, e
		}
		if date != w.date || info.Size()+int64(len(p)) > w.policy.fileBytes {
			if e := w.file.Close(); e != nil {
				return 0, e
			}
			w.file = nil
			w.path = ""
		}
	}
	if err = w.prune(now, int64(len(p))); err != nil {
		return 0, err
	}
	if w.file == nil {
		if err = w.open(date, int64(len(p))); err != nil {
			return 0, err
		}
	}
	n, err = w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (w *Writer) open(date string, incoming int64) error {
	files, err := w.inventory()
	if err != nil {
		return err
	}
	seq := 1
	for _, f := range files {
		if f.legacy || f.date.Format("2006-01-02") != date {
			continue
		}
		if f.seq >= seq {
			seq = f.seq
			if f.size+incoming > w.policy.fileBytes {
				seq++
			}
		}
	}
	if seq > 999999 {
		return errors.New("diagnostic segment sequence exhausted")
	}
	path := filepath.Join(w.dir, fmt.Sprintf("sidraviad-%s-%06d.log", date, seq))
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("diagnostic segment is not a regular file")
		}
		if err := w.protection.File(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := w.protection.File(path); err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.path = path
	w.date = date
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}
