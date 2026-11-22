package daemonlog

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var datedName = regexp.MustCompile(`^sidraviad-(\d{4}-\d{2}-\d{2})-(\d{6})\.log$`)

type retentionPolicy struct {
	fileBytes, totalBytes int64
	days                  int
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
