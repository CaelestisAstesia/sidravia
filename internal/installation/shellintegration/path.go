// Package shellintegration owns short-lived operating-system integration operations.
package shellintegration

import (
	"errors"
	"fmt"
	"strings"
)

// Result is the versioned, safe machine-readable manager response.
type Result struct {
	Version      int      `json:"version"`
	OK           bool     `json:"ok"`
	Code         string   `json:"code"`
	Capabilities []string `json:"capabilities"`
	Directory    string   `json:"directory,omitempty"`
	PathEnabled  bool     `json:"pathEnabled"`
	Changed      bool     `json:"changed"`
	Warning      string   `json:"warning,omitempty"`
	Autostart    string   `json:"autostart,omitempty"`
	GUIAvailable bool     `json:"guiAvailable"`
}

// Operation selects the only integration capability currently implemented.
type Operation string

const (
	Status           Operation = "status"
	EnablePath       Operation = "path enable"
	DisablePath      Operation = "path disable"
	StartGUI         Operation = "autostart set gui"
	StartDaemon      Operation = "autostart set daemon"
	DisableAutostart Operation = "autostart disable"
)

type pathValue struct {
	text   string
	kind   uint32
	exists bool
}
type pathStore interface {
	read() (pathValue, error)
	write(pathValue) error
	owns(string) (bool, error)
	setOwnership(string, bool) error
	notify() error
}

// Failure retains the operational cause without exposing it through JSON.
type Failure struct {
	Code  string
	Cause error
}

func (f *Failure) Error() string { return fmt.Sprintf("%s: %v", f.Code, f.Cause) }
func (f *Failure) Unwrap() error { return f.Cause }

// Code returns a stable public failure classification.
func Code(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return "integration_failed"
}

func matches(entry, directory string) bool {
	return strings.EqualFold(strings.TrimRight(entry, "\\/"), strings.TrimRight(directory, "\\/"))
}
func contains(value, directory string) bool {
	for _, entry := range strings.Split(value, ";") {
		if entry != "" && matches(entry, directory) {
			return true
		}
	}
	return false
}
func edit(value, directory string, enabled bool) string {
	if enabled {
		if contains(value, directory) {
			return value
		}
		if value == "" {
			return directory
		}
		// Keep any existing empty tail entries so disabling this directory can
		// remove just its segment, even after other PATH entries have changed.
		end := len(strings.TrimRight(value, ";"))
		return value[:end] + ";" + directory + value[end:]
	}
	entries := strings.Split(value, ";")
	for index, entry := range entries {
		if entry == directory {
			return strings.Join(append(entries[:index], entries[index+1:]...), ";")
		}
	}
	return value
}

func operate(store pathStore, directory string, operation Operation) (Result, error) {
	r := Result{Version: 1, Capabilities: []string{"user_path"}, Directory: directory}
	fail := func(code string, err error) (Result, error) {
		r.Code = code
		return r, &Failure{Code: code, Cause: err}
	}
	if operation != Status && operation != EnablePath && operation != DisablePath {
		return fail("invalid_arguments", errors.New("unknown operation"))
	}
	if directory == "" || strings.ContainsAny(directory, ";\x00\r\n") {
		return fail("invalid_directory", errors.New("unrepresentable PATH directory"))
	}
	before, err := store.read()
	if err != nil {
		return fail("path_read_failed", err)
	}
	r.PathEnabled = contains(before.text, directory)
	if operation != Status {
		owned, err := store.owns(directory)
		if err != nil {
			return fail("path_read_failed", err)
		}
		if operation == DisablePath && !owned {
			if r.PathEnabled {
				r.Warning = "path_entry_not_owned"
			}
			r.OK = true
			r.Code = "ok"
			return r, nil
		}
		desired := before
		desired.text = edit(before.text, directory, operation == EnablePath)
		if desired.text != before.text || operation == DisablePath {
			observed, readErr := store.read()
			if readErr != nil {
				return fail("path_verify_failed", readErr)
			}
			if observed != before {
				return fail("path_verify_failed", errors.New("PATH changed before edit"))
			}
		}
		if operation == DisablePath {
			// Retire the ownership before removal. A failed write can leave our
			// old entry present, but a retry cannot remove a later user entry
			// under an ownership marker left behind by an interrupted removal.
			if err := store.setOwnership(directory, false); err != nil {
				return fail("path_write_failed", err)
			}
		}
		if desired.text != before.text {
			desired.exists = true
			if !before.exists {
				desired.kind = 2
			} // Windows REG_EXPAND_SZ.
			// A failed write may have partially changed state: Changed is only
			// authoritative on success, and OK stays false until verified.
			if err := store.write(desired); err != nil {
				return fail("path_write_failed", err)
			}
			r.Changed = true
			after, err := store.read()
			if err != nil {
				return fail("path_verify_failed", err)
			}
			r.PathEnabled = contains(after.text, directory)
			if after != desired {
				return fail("path_verify_failed", errors.New("observed PATH differs from write"))
			}
			if operation == EnablePath {
				// Record ownership only after the exact appended value was observed.
				if err := store.setOwnership(directory, true); err != nil {
					return fail("path_write_failed", err)
				}
			}
			if err := store.notify(); err != nil {
				r.Warning = "environment_notification_failed"
			}
		}
	}
	r.OK = true
	r.Code = "ok"
	return r, nil
}
