//go:build !linux

package shellintegration

func TrackPackageAutostart(string, bool, string) error { return errRPMDesktopUnavailable }
func RecoverAutostartStaging(string) error             { return errRPMDesktopUnavailable }
