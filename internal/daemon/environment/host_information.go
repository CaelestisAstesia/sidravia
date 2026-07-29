package environment

import "errors"

// ErrUnsupported signals that the current platform does not expose the real
// production path for system facts. Linux and Windows each provide a real
// ReadSystemHostInformation and Environment Observer; non-Linux/non-Windows
// builds return this sentinel from ReadSystemHostInformation and from
// NewSystemObserver().Observe rather than reporting an apparently valid empty
// result.
var ErrUnsupported = errors.New("sidravia environment: unsupported platform")

type SystemHostInformation struct {
	HostName               string
	OperatingSystemFamily  string
	OperatingSystemRelease string
	MachineArchitecture    string
}
