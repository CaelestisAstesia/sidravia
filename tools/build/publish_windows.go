package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// The publisher supplies absolute filesystem paths. Preserve the long-path
// behavior of os.Rename even when the Windows process has not opted in.
func publicationPath(path string) string {
	path = filepath.Clean(path)
	if len(path) < 248 || strings.HasPrefix(path, "\\\\?\\") ||
		strings.HasPrefix(path, "\\??\\") || strings.HasPrefix(path, "\\\\.\\") {
		return path
	}
	if strings.HasPrefix(path, "\\\\") {
		return "\\\\?\\UNC\\" + path[2:]
	}
	return "\\\\?\\" + path
}

func publishDirectory(source, destination string) error {
	from, err := windows.UTF16PtrFromString(publicationPath(source))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(publicationPath(destination))
	if err != nil {
		return err
	}
	// With no replacement flag, either an absent destination receives the
	// directory or Windows leaves the existing destination intact.
	if err := windows.MoveFileEx(from, to, 0); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}
