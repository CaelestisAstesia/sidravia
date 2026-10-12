// Package installationmanager owns short-lived operating-system integration operations.
package installationmanager

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
	PrepareUninstall Operation = "uninstall-prepare"
	CheckUninstall   Operation = "uninstall-check"
	DeleteData       Operation = "data delete"
)

type pathValue struct {
	text   string
	kind   uint32
	exists bool
}
type pathStore interface {
	read() (pathValue, error)
	write(pathValue) error
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
		if value == "" || strings.HasSuffix(value, ";") {
			return value + directory
		}
		return value + ";" + directory
	}
	entries := strings.Split(value, ";")
	kept := entries[:0]
	for _, entry := range entries {
		if !matches(entry, directory) {
			kept = append(kept, entry)
		}
	}
	return strings.Join(kept, ";")
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
		desired := before
		desired.text = edit(before.text, directory, operation == EnablePath)
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
			if err := store.notify(); err != nil {
				r.Warning = "environment_notification_failed"
			}
		}
	}
	r.OK = true
	r.Code = "ok"
	return r, nil
}
