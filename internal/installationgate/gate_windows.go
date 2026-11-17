//go:build windows

package installationgate

import "sidravia/internal/update/windowsplatform"

var ErrBusy = windowsplatform.ErrInstallationBusy

// AcquireStart must be released on the same goroutine (Windows mutex ownership).
func AcquireStart() (func(), error) { return windowsplatform.AcquireInstallation() }
