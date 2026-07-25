//go:build windows

package environment

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/windows"
)

// ReadSystemHostInformation reads real Windows host facts: the standard-library
// host name, the Windows operating-system family, the version reported by
// RtlGetVersion, and the running GOARCH. A host-name read failure is wrapped
// and preserved; no fallback facts are invented.
func ReadSystemHostInformation() (SystemHostInformation, error) {
	hostName, err := os.Hostname()
	if err != nil {
		return SystemHostInformation{}, fmt.Errorf("read system host information: %w", err)
	}
	if hostName == "" {
		return SystemHostInformation{}, fmt.Errorf("read system host information: host name is empty")
	}

	version := windows.RtlGetVersion()
	release := strconv.FormatUint(uint64(version.MajorVersion), 10) +
		"." + strconv.FormatUint(uint64(version.MinorVersion), 10) +
		"." + strconv.FormatUint(uint64(version.BuildNumber), 10)

	return SystemHostInformation{
		HostName:               hostName,
		OperatingSystemFamily:  "Windows",
		OperatingSystemRelease: release,
		MachineArchitecture:    runtime.GOARCH,
	}, nil
}
