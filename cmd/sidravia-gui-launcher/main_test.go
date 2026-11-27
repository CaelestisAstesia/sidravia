//go:build linux

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLauncherChild(t *testing.T) {
	mode := os.Getenv("SIDRAVIA_LAUNCH_FIXTURE")
	if mode == "launch" {
		env := os.Environ()
		for n, value := range env {
			if strings.HasPrefix(value, "SIDRAVIA_LAUNCH_FIXTURE=") {
				env[n] = "SIDRAVIA_LAUNCH_FIXTURE=runtime"
			}
		}
		if err := launch(os.Getenv("SIDRAVIA_LAUNCH_ROOT"), []string{"-test.run=^TestLauncherChild$"}, env); err != nil {
			fmt.Fprintln(os.Stderr, "refused")
			os.Exit(7)
		}
		os.Exit(0)
	}
	if mode != "runtime" {
		return
	}
	if _, exists := os.LookupEnv(gateFD); exists {
		t.Fatal("obsolete update lease inherited")
	}
	fmt.Printf("runtime %d\n", os.Getpid())
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}
func launchFixture(t *testing.T) (string, string) {
	t.Helper()
	target := t.TempDir()
	cache := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "gui-runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(target, "gui-runtime/sidravia_gui"), os.O_WRONLY|os.O_EXCL|os.O_CREATE, 0755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return target, cache
}
func launcherProcess(target, cache string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLauncherChild$")
	cmd.Env = append(os.Environ(), "SIDRAVIA_LAUNCH_FIXTURE=launch", "SIDRAVIA_LAUNCH_ROOT="+target, "XDG_CACHE_HOME="+cache, gateFD+"=99")
	return cmd
}
func TestExecKeepsPIDWithoutRetiredLease(t *testing.T) {
	target, cache := launchFixture(t)
	cmd := launcherProcess(target, cache)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != fmt.Sprintf("runtime %d\n", cmd.Process.Pid) {
			t.Fatalf("exec changed process identity: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exec timed out")
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestLegacySealRefusesBeforeRuntimeExec(t *testing.T) {
	target, cache := launchFixture(t)
	if err := os.WriteFile(filepath.Join(target, "sidravia.update"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := launcherProcess(target, cache)
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "runtime ") {
		t.Fatalf("blocked launch executed runtime: %q %v", out, err)
	}
}
