package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const xzVersion = "5.8.3"

// XZ is a build-only tool. Runtime decoding stays pure Go and needs no system
// xz executable, C library or cgo. Environment options cannot alter the format.
func xzCommand(args ...string) *exec.Cmd {
	command := exec.Command("xz", args...)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "XZ_OPT" && key != "XZ_DEFAULTS" {
			command.Env = append(command.Env, value)
		}
	}
	return command
}

func writeTarXZ(path, root string, entries []archiveEntry) (err error) {
	version, err := xzCommand("--version").Output()
	if err != nil {
		return fmt.Errorf("XZ Utils %s is required: %w", xzVersion, err)
	}
	if strings.SplitN(string(version), "\n", 2)[0] != "xz (XZ Utils) "+xzVersion {
		return fmt.Errorf("XZ Utils %s is required", xzVersion)
	}
	input, err := os.CreateTemp(filepath.Dir(path), "xz-input-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(input.Name())
	writeErr := writeTarContents(input, root, entries)
	closeInput := input.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeInput != nil {
		return closeInput
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
	}()
	command := xzCommand("--threads=1", "--check=crc64", "-6", "--lzma2=dict=8MiB", "--stdout", "--", input.Name())
	command.Stdout = output
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Run(); err != nil {
		return fmt.Errorf("xz compression: %w: %s", err, stderr.String())
	}
	return nil
}
