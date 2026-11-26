//go:build !windows && !linux

package daemonctl

func desktopBootstrapSupported() bool { return false }
