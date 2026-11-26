//go:build windows

package startgate

import "sidravia/internal/installation/winmutex"

var ErrBusy = winmutex.ErrInstallationBusy

// AcquireStart must be released on the same goroutine (Windows mutex ownership).
func AcquireStart() (func(), error) { return winmutex.AcquireInstallation() }
