//go:build windows

package cli

import (
	"os"
	"strings"
	"testing"
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
