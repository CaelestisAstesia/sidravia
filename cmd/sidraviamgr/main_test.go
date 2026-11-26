package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/installation/shellintegration"
)

func TestArgumentsAndJSON(t *testing.T) {
	for _, tc := range []struct {
		args []string
		op   shellintegration.Operation
		exit int
	}{
		{[]string{"status"}, shellintegration.Status, 0},
		{[]string{"path", "enable"}, shellintegration.EnablePath, 0},
		{[]string{"path", "disable"}, shellintegration.DisablePath, 0},
		{[]string{"autostart", "set", "gui"}, shellintegration.StartGUI, 0},
		{[]string{"autostart", "set", "daemon"}, shellintegration.StartDaemon, 0},
		{[]string{"autostart", "disable"}, shellintegration.DisableAutostart, 0},
		{[]string{"uninstall-prepare"}, "", 2},
		{[]string{"uninstall-check"}, "", 2},
		{[]string{"data", "delete"}, "", 2},
		{[]string{"autostart"}, "", 2},
		{[]string{"status", "extra"}, "", 2},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			resolved := false
			exe := filepath.Join(t.TempDir(), "中文 directory", "sidraviamgr")
			code := run(tc.args, &out, func() (string, error) { resolved = true; return exe, nil }, func(dir string, op shellintegration.Operation) (shellintegration.Result, error) {
				calls++
				if dir != filepath.Dir(exe) || op != tc.op {
					t.Fatal("wrong dispatch")
				}
				return shellintegration.Result{Version: 1, OK: true, Code: "ok", Capabilities: []string{"user_path"}, Directory: dir}, nil
			})
			if code != tc.exit {
				t.Fatalf("exit=%d", code)
			}
			var result shellintegration.Result
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Version != 1 {
				t.Fatalf("JSON %s %v", out.Bytes(), err)
			}
			if tc.exit == 2 && (calls != 0 || resolved || result.OK) {
				t.Fatal("invalid arguments dispatched")
			}
		})
	}
}
func TestHelpAndSafeFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		var out bytes.Buffer
		if run(args, &out, func() (string, error) { t.Fatal("help resolves executable"); return "", nil }, nil) != 0 || !strings.Contains(out.String(), "belong to the installer") {
			t.Fatal("help")
		}
	}
	var out bytes.Buffer
	code := run([]string{"status"}, &out, func() (string, error) { return filepath.Join(t.TempDir(), "mgr"), nil }, func(string, shellintegration.Operation) (shellintegration.Result, error) {
		return shellintegration.Result{Version: 1, Capabilities: []string{}}, &shellintegration.Failure{Code: "path_read_failed", Cause: errors.New("SECRET native path")}
	})
	if code != 1 || strings.Contains(out.String(), "SECRET") || !strings.Contains(out.String(), "path_read_failed") {
		t.Fatalf("%d %s", code, out.String())
	}
}

func TestStatusDirectoryIsReadOnlyAndUsesSameJSON(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := run([]string{"status", "--directory", dir}, &out, func() (string, error) { t.Fatal("must use query context"); return "", nil }, func(got string, op shellintegration.Operation) (shellintegration.Result, error) {
		if got != dir || op != shellintegration.Status {
			t.Fatal("wrong query")
		}
		return shellintegration.Result{Version: 1, OK: true, Code: "ok", Capabilities: []string{"user_path", "user_autostart"}, Autostart: "daemon"}, nil
	})
	var result shellintegration.Result
	if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || result.Autostart != "daemon" {
		t.Fatal("status contract changed")
	}
	for _, args := range [][]string{{"status", "--directory", "relative"}, {"path", "enable", "--directory", dir}, {"--setup-state", dir}} {
		out.Reset()
		if run(args, &out, nil, func(string, shellintegration.Operation) (shellintegration.Result, error) {
			t.Fatal("invalid query/write ran")
			return shellintegration.Result{}, nil
		}) != 2 {
			t.Fatal("invalid arguments accepted")
		}
	}
}
