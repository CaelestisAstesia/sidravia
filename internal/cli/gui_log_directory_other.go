//go:build !windows

package cli

func openLogDirectoryNative(string) error { return errLogDirectoryUnsupported }
