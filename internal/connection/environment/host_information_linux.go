//go:build linux

package environment

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// hostnameFunc returns the host name. Tests inject a private replacement; the
// production path uses os.Hostname. No exported seam is introduced.
type hostnameFunc func() (string, error)

// unameFunc populates a Utsname with kernel facts. Tests inject a private
// replacement; the production path uses unix.Uname.
type unameFunc func(*unix.Utsname) error

var (
	defaultHostname hostnameFunc = os.Hostname
	defaultUname    unameFunc    = unix.Uname
)

// ReadSystemHostInformation returns the real Linux host facts. It reads a
// nonempty host name from os.Hostname and converts the NUL-terminated kernel
// release from unix.Uname without shelling out or using unsafe memory, reports
// Linux as the operating system family, and uses runtime.GOARCH as the machine
// architecture. An empty host name or release is rejected rather than invented.
func ReadSystemHostInformation() (SystemHostInformation, error) {
	return readSystemHostInformation(defaultHostname, defaultUname, runtime.GOARCH)
}

func readSystemHostInformation(hostname hostnameFunc, uname unameFunc, arch string) (SystemHostInformation, error) {
	name, err := hostname()
	if err != nil {
		return SystemHostInformation{}, fmt.Errorf("读取主机名: %w", err)
	}
	if name == "" {
		return SystemHostInformation{}, errors.New("主机名为空")
	}

	var uts unix.Utsname
	if err := uname(&uts); err != nil {
		return SystemHostInformation{}, fmt.Errorf("读取内核版本: %w", err)
	}
	release := utsString(uts.Release)
	if release == "" {
		return SystemHostInformation{}, errors.New("内核版本为空")
	}

	return SystemHostInformation{
		HostName:               name,
		OperatingSystemFamily:  "Linux",
		OperatingSystemRelease: release,
		MachineArchitecture:    arch,
	}, nil
}

// utsString converts a NUL-terminated Utsname field to a string without using
// unsafe memory or a shell command.
func utsString(field [65]byte) string {
	return string(bytes.TrimRight(field[:], "\x00"))
}
