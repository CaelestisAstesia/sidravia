//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestDaemonEnvForLevelSetsChildLogLevel proves a non-empty log level appends
// exactly one SIDRAVIA_LOG_LEVEL entry to the child environment without
// mutating the parent environment.
func TestDaemonEnvForLevelSetsChildLogLevel(t *testing.T) {
	parentCount := countLogLevel(os.Environ())
	env := daemonEnvForLevel(os.Environ(), "trace")
	if got := countLogLevel(env); got != 1 {
		t.Errorf("child SIDRAVIA_LOG_LEVEL count = %d, want 1", got)
	}
	found := false
	for _, e := range env {
		if e == "SIDRAVIA_LOG_LEVEL=trace" {
			found = true
		}
	}
	if !found {
		t.Errorf("child env missing SIDRAVIA_LOG_LEVEL=trace: %v", env)
	}
	// Parent environment must be unchanged.
	if got := countLogLevel(os.Environ()); got != parentCount {
		t.Errorf("parent SIDRAVIA_LOG_LEVEL count changed: got %d, want %d", got, parentCount)
	}
}

func TestDaemonProcessCommandOwnsBackgroundOutput(t *testing.T) {
	logFile, err := os.OpenFile(filepath.Join(t.TempDir(), "daemon.log"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := daemonProcessCommand(`C:\Sidravia\sidraviad.exe`, logFile, []string{"PATH=value", "sidravia_log_level=debug"}, "trace")
	if cmd.Stdin != nil {
		t.Fatal("child inherited stdin")
	}
	if cmd.Stdout != logFile || cmd.Stderr != logFile {
		t.Fatal("child output is not owned by the background log")
	}
	if countLogLevel(cmd.Env) != 1 || !containsEnvironmentEntry(cmd.Env, "SIDRAVIA_LOG_LEVEL=trace") {
		t.Fatalf("child environment = %v", cmd.Env)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags != windows.CREATE_NO_WINDOW {
		t.Fatalf("creation flags = %#v", cmd.SysProcAttr)
	}
}

// TestDaemonEnvForLevelEmptyLeavesDefault proves an empty log level does not
// append a SIDRAVIA_LOG_LEVEL entry, leaving the child at the daemon default.
func TestDaemonEnvForLevelEmptyLeavesDefault(t *testing.T) {
	env := daemonEnvForLevel(os.Environ(), "")
	if got := countLogLevel(env); got != 1 {
		t.Errorf("empty logLevel SIDRAVIA_LOG_LEVEL count = %d, want 1", got)
	}
	if !containsEnvironmentEntry(env, "SIDRAVIA_LOG_LEVEL=info") {
		t.Errorf("empty logLevel did not resolve to info: %v", env)
	}
}

func countLogLevel(env []string) int {
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "SIDRAVIA_LOG_LEVEL=") {
			count++
		}
	}
	return count
}

func containsEnvironmentEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
