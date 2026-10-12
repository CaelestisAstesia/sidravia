package installationmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var ownedLogName = regexp.MustCompile(`^sidraviad-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{6}\.log$`)

// checkedPath refuses links/reparse points in the complete existing ancestry.
// Cleanup has no recursive deletion and cannot traverse namespace directories.
func checkedPath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("data path must be absolute")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse linked data path")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func removeData(configRoot, cacheRoot string) (bool, error) {
	candidates := []string{filepath.Join(configRoot, "configurations.json"), filepath.Join(configRoot, "gui-settings.json"), filepath.Join(cacheRoot, "runtime.json")}
	logs := filepath.Join(cacheRoot, "logs")
	if err := checkedPath(logs); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(logs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "sidraviad.log" || name == "sidraviad.log.1" || ownedLogName.MatchString(name) {
			candidates = append(candidates, filepath.Join(logs, name))
		}
	}
	// Validate every candidate before the first irreversible removal.
	for _, path := range candidates {
		if err := checkedPath(path); err != nil {
			return false, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("refuse non-regular data file")
		}
	}
	changed := false
	for _, path := range candidates {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
