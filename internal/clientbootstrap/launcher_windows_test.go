//go:build windows

package clientbootstrap

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"sidravia/internal/productlayout"
)

func TestWindowsLaunchObservesOneWait(t *testing.T) {
	log, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("wait")
	starts, waits := 0, 0
	got, err := launchDaemonProcessWith("info", windowsDaemonLauncherDeps{
		resolveLayout: func() (productlayout.Layout, error) {
			return productlayout.Layout{ExecutableDirectory: `C:\\Sidravia`, DaemonLogPath: log.Name()}, nil
		},
		prepareLog: func(string) (*os.File, error) { return log, nil }, parentEnv: func() []string { return nil },
		start: func(*exec.Cmd) error { starts++; return nil }, wait: func(*exec.Cmd) error { waits++; return cause },
	})
	if err != nil || got.exited == nil {
		t.Fatalf("launch = %#v, %v", got, err)
	}
	if cap(got.exited) != 1 {
		t.Fatalf("exit observation capacity = %d, want 1", cap(got.exited))
	}
	if gotErr := <-got.exited; !errors.Is(gotErr, cause) || starts != 1 || waits != 1 {
		t.Fatalf("start=%d wait=%d err=%v", starts, waits, gotErr)
	}
	if _, err := log.Write([]byte("closed")); err == nil {
		t.Fatal("parent log remains open after successful start")
	}
}

func TestWindowsLaunchFailuresPreserveOwnership(t *testing.T) {
	t.Run("layout", func(t *testing.T) {
		cause := errors.New("layout")
		_, err := launchDaemonProcessWith("info", windowsDaemonLauncherDeps{
			resolveLayout: func() (productlayout.Layout, error) { return productlayout.Layout{}, cause },
		})
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "解析 sidravia 运行目录") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("log", func(t *testing.T) {
		cause := errors.New("log")
		_, err := launchDaemonProcessWith("info", windowsDaemonLauncherDeps{
			resolveLayout: func() (productlayout.Layout, error) { return productlayout.Layout{}, nil },
			prepareLog:    func(string) (*os.File, error) { return nil, cause },
		})
		if !errors.Is(err, cause) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("start", func(t *testing.T) {
		log, err := os.CreateTemp(t.TempDir(), "log")
		if err != nil {
			t.Fatal(err)
		}
		cause := errors.New("start")
		waits := 0
		launch, err := launchDaemonProcessWith("info", windowsDaemonLauncherDeps{
			resolveLayout: func() (productlayout.Layout, error) { return productlayout.Layout{DaemonLogPath: log.Name()}, nil },
			prepareLog:    func(string) (*os.File, error) { return log, nil },
			parentEnv:     func() []string { return nil },
			start:         func(*exec.Cmd) error { return cause },
			wait:          func(*exec.Cmd) error { waits++; return nil },
		})
		if launch.exited != nil || !errors.Is(err, cause) || waits != 0 {
			t.Fatalf("launch=%#v error=%v waits=%d", launch, err, waits)
		}
		if _, err := log.Write([]byte("closed")); err == nil {
			t.Fatal("parent log remains open after failed start")
		}
	})
}

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
