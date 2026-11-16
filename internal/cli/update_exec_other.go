//go:build !linux

package cli

func handoffUpdater(string, []string) (bool, error) { return false, nil }
