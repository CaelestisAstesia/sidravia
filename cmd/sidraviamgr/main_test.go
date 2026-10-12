package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sidravia/internal/installationmanager"
)

func TestArgumentsAndJSON(t *testing.T) {
	for _, tc := range []struct {
		args []string
		op   installationmanager.Operation
		exit int
	}{
		{[]string{"status"}, installationmanager.Status, 0},
		{[]string{"path", "enable"}, installationmanager.EnablePath, 0},
		{[]string{"path", "disable"}, installationmanager.DisablePath, 0},
		{[]string{"autostart", "set", "gui"}, installationmanager.StartGUI, 0},
		{[]string{"autostart", "set", "daemon"}, installationmanager.StartDaemon, 0},
		{[]string{"autostart", "disable"}, installationmanager.DisableAutostart, 0},
		{[]string{"uninstall-prepare"}, installationmanager.PrepareUninstall, 0},
		{[]string{"uninstall-check"}, installationmanager.CheckUninstall, 0},
		{[]string{"data", "delete"}, installationmanager.DeleteData, 0},
		{[]string{"autostart"}, "", 2},
		{[]string{"status", "extra"}, "", 2},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			resolved := false
			exe := filepath.Join(t.TempDir(), "中文 directory", "sidraviamgr")
			code := run(tc.args, &out, func() (string, error) { resolved = true; return exe, nil }, func(dir string, op installationmanager.Operation) (installationmanager.Result, error) {
				calls++
				if dir != filepath.Dir(exe) || op != tc.op {
					t.Fatal("wrong dispatch")
				}
				return installationmanager.Result{Version: 1, OK: true, Code: "ok", Capabilities: []string{"user_path"}, Directory: dir}, nil
			})
			if code != tc.exit {
				t.Fatalf("exit=%d", code)
			}
			var result installationmanager.Result
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
		if run(args, &out, func() (string, error) { t.Fatal("help resolves executable"); return "", nil }, nil) != 0 || !strings.Contains(out.String(), "not yet available") {
			t.Fatal("help")
		}
	}
	var out bytes.Buffer
	code := run([]string{"status"}, &out, func() (string, error) { return filepath.Join(t.TempDir(), "mgr"), nil }, func(string, installationmanager.Operation) (installationmanager.Result, error) {
		return installationmanager.Result{Version: 1, Capabilities: []string{}}, &installationmanager.Failure{Code: "path_read_failed", Cause: errors.New("SECRET native path")}
	})
	if code != 1 || strings.Contains(out.String(), "SECRET") || !strings.Contains(out.String(), "path_read_failed") {
		t.Fatalf("%d %s", code, out.String())
	}
}
