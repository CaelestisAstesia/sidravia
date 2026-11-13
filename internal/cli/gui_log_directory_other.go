//go:build !windows && !linux

package cli

func openLogDirectoryNative(string) error { return errLogDirectoryUnsupported }
