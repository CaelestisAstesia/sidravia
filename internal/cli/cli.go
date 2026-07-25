package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	if len(args) != 1 || args[0] != "status" {
		return fmt.Errorf("usage: sidravia status")
	}
	return status()
}

func status() error {
	return runStatus(defaultStatusDependencies())
}

// statusDependencies bundles the private seams that the status command relies
// on. Behavior tests substitute individual fields without touching any
// package-global state.
type statusDependencies struct {
	runtimeInfoPath func() (string, error)
	readRuntimeInfo func(path string) (contract.RuntimeInfo, error)
	connectAndPrint func(info contract.RuntimeInfo) error
	startDaemon     func() error
	totalWait       time.Duration
	pollInterval    time.Duration
}

// defaultStatusDependencies wires the status command to its real production
// collaborators.
func defaultStatusDependencies() statusDependencies {
	return statusDependencies{
		runtimeInfoPath: runtimeInfoPath,
		readRuntimeInfo: readRuntimeInfo,
		connectAndPrint: connectAndPrint,
		startDaemon:     startDaemon,
		totalWait:       5 * time.Second,
		pollInterval:    200 * time.Millisecond,
	}
}

// runStatus drives the status command against the supplied dependencies,
// preserving the production control flow: try a hot connection first, start the
// daemon at most once, then poll until the daemon is reachable or totalWait
// elapses.
func runStatus(deps statusDependencies) error {
	infoPath, err := deps.runtimeInfoPath()
	if err != nil {
		return fmt.Errorf("runtime info path: %w", err)
	}

	// Try to connect to an already-running daemon.
	info, err := deps.readRuntimeInfo(infoPath)
	if err == nil {
		if err := deps.connectAndPrint(info); err == nil {
			return nil
		}
	}

	// Daemon not reachable - start it.
	if err := deps.startDaemon(); err != nil {
		return fmt.Errorf("failed to start sidraviad: %w", err)
	}

	// Wait up to totalWait for the daemon to write runtime info and accept connections.
	ctx, cancel := context.WithTimeout(context.Background(), deps.totalWait)
	defer cancel()

	ticker := time.NewTicker(deps.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for sidraviad")
		case <-ticker.C:
			info, err := deps.readRuntimeInfo(infoPath)
			if err != nil {
				continue
			}
			if err := deps.connectAndPrint(info); err != nil {
				continue
			}
			return nil
		}
	}
}

func runtimeInfoPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Sidravia", "runtime.json"), nil
}

func readRuntimeInfo(path string) (contract.RuntimeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	return contract.DecodeRuntimeInfo(data)
}

func connectAndPrint(info contract.RuntimeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := client.Connect(ctx, info.Endpoint, info.Token, info.BuildID)
	if err != nil {
		return err
	}
	defer c.Close()

	resp, err := c.Call(ctx, contract.MethodDaemonStatus, nil)
	if err != nil {
		return err
	}
	return writeStatus(os.Stdout, resp)
}

// writeStatus renders a daemon.status response to w. A non-OK response is a
// daemon error; otherwise the StatusResult is decoded and printed in the
// canonical one-line format. It never writes a partial success line.
func writeStatus(w io.Writer, resp contract.Response) error {
	if !resp.OK {
		return fmt.Errorf("daemon error: %s", resp.Error.Message)
	}

	var result contract.StatusResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return err
	}

	fmt.Fprintf(w, "sidraviad %s (%s) pid=%d status=%s\n",
		result.ProductVersion, result.BuildID, result.PID, result.Status)
	return nil
}

func startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	daemonPath := filepath.Join(dir, "sidraviad.exe")

	cmd := exec.Command(daemonPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
