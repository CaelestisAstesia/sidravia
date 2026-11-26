//go:build linux

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"sidravia/internal/release"
	"sidravia/internal/update/transaction"
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
	fd, err := strconv.Atoi(os.Getenv(gateFD))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatal("runtime lost inherited lease", err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
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
	cmd.Env = append(os.Environ(), "SIDRAVIA_LAUNCH_FIXTURE=launch", "SIDRAVIA_LAUNCH_ROOT="+target, "XDG_CACHE_HOME="+cache)
	return cmd
}
func TestExecKeepsPIDAndRuntimeLease(t *testing.T) {
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
	s, err := transaction.Open(filepath.Join(cache, "Sidravia/updates/operations"), target, release.PublisherTrust())
	if err != nil {
		t.Fatal(err)
	}
	// AcquireInheritedStart is shared, so ordinary launches can coexist.
	start, err := s.AcquireStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start()
	sum := sha256.Sum256([]byte(target))
	file := filepath.Join(cache, "Sidravia/updates/operations", hex.EncodeToString(sum[:]), "execution.lock")
	fd, err := unix.Open(file, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("exec discarded admission lease")
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("runtime exit retained admission lease", err)
	}
}
func TestBlockedRecordRefusesBeforeRuntimeExec(t *testing.T) {
	target, cache := launchFixture(t)
	sum := sha256.Sum256([]byte(target))
	directory := filepath.Join(cache, "Sidravia/updates/operations", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "current.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := launcherProcess(target, cache)
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "runtime ") {
		t.Fatalf("blocked launch executed runtime: %q %v", out, err)
	}
}
