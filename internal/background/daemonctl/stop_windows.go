package daemonctl

import (
	"errors"

	"golang.org/x/sys/windows"
)

func connectionRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED)
}
