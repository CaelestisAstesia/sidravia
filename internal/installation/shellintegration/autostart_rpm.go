package shellintegration

import "errors"

var errRPMDesktopConflict = errors.New("user service registration conflicts with existing state")
var errRPMDesktopUnavailable = errors.New("user service manager unavailable")
