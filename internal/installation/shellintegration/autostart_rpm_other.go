//go:build !linux

package shellintegration

func TrackRPMAutostart(string, bool) error { return errRPMDesktopUnavailable }
func RecoverAutostartStaging(string) error { return errRPMDesktopUnavailable }
