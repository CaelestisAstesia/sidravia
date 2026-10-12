package daemonlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

var datedName = regexp.MustCompile(`^sidraviad-(\d{4}-\d{2}-\d{2})-(\d{6})\.log$`)

type retentionPolicy struct {
	fileBytes, totalBytes int64
	days                  int
}

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
	protection     daemonLogProtection
	fallback       io.Writer
}

// NewWriter protects the directory before accepting any diagnostic records.
// Only exact owned filenames are eligible for retention cleanup.
func NewWriter(dir string, fallback io.Writer) (*Writer, error) {
	p, err := newDaemonLogProtection()
	if err != nil {
		return nil, err
	}
	return newWriter(dir, fallback, p, retentionPolicy{10 * 1024 * 1024, 100 * 1024 * 1024, 7}, time.Now)
}

func newWriter(dir string, fallback io.Writer, protection daemonLogProtection, policy retentionPolicy, now func() time.Time) (*Writer, error) {
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
	if err := protection.protectDirectory(dir); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, policy: policy, now: now, protection: protection, fallback: fallback}
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

type logFile struct {
	name   string
	size   int64
	date   time.Time
	seq    int
	legacy bool
}

func (w *Writer) inventory() ([]logFile, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}
	var files []logFile
	for _, entry := range entries {
		name := entry.Name()
		m := datedName.FindStringSubmatch(name)
		legacy := name == "sidraviad.log" || name == "sidraviad.log.1"
		if m == nil && !legacy {
			continue
		}
		info, err := os.Lstat(filepath.Join(w.dir, name))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		f := logFile{name: name, size: info.Size(), legacy: legacy, date: info.ModTime()}
		if !legacy {
			f.date, err = time.ParseInLocation("2006-01-02", m[1], w.now().Location())
			if err != nil {
				continue
			}
			f.seq, _ = strconv.Atoi(m[2])
			if f.seq < 1 {
				continue
			}
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].date.Equal(files[j].date) {
			return files[i].name < files[j].name
		}
		return files[i].date.Before(files[j].date)
	})
	return files, nil
}

func (w *Writer) prune(now time.Time, incoming int64) error {
	files, err := w.inventory()
	if err != nil {
		return err
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cutoff := day.AddDate(0, 0, -w.policy.days+1)
	var retained []logFile
	var total int64
	for _, f := range files {
		active := f.name == "sidraviad.log" || filepath.Join(w.dir, f.name) == w.path
		if !active && f.date.Before(cutoff) {
			if err := os.Remove(filepath.Join(w.dir, f.name)); err != nil {
				return err
			}
			continue
		}
		retained = append(retained, f)
		total += f.size
	}
	for _, f := range retained {
		if total+incoming <= w.policy.totalBytes {
			return nil
		}
		if f.name == "sidraviad.log" || filepath.Join(w.dir, f.name) == w.path {
			continue
		}
		if err := os.Remove(filepath.Join(w.dir, f.name)); err != nil {
			return err
		}
		total -= f.size
	}
	if total+incoming > w.policy.totalBytes {
		return errors.New("active logs exhaust diagnostic budget")
	}
	return nil
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
		if err := w.protection.protectFile(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := w.protection.protectFile(path); err != nil {
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
