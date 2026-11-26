package registration

import "errors"

var ErrUnavailable = errors.New("user service manager unavailable")
var ErrConflict = errors.New("user service registration conflicts with existing state")
