//go:build !windows && !linux

package clientbootstrap

func desktopBootstrapSupported() bool { return false }
