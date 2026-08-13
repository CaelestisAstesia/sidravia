//go:build !windows

package clientbootstrap

func desktopBootstrapSupported() bool { return false }
