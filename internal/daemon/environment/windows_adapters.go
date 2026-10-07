//go:build windows

package environment

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

// readWindowsAdapters owns the backing storage for the returned linked list.
// Callers must keep buffer alive until every retained fact has been copied.
func readWindowsAdapters() ([]byte, *windows.IpAdapterAddresses, error) {
	const flags = windows.GAA_FLAG_INCLUDE_PREFIX | windows.GAA_FLAG_INCLUDE_GATEWAYS
	const maxResizeAttempts = 4

	size := uint32(15000)
	var buffer []byte
	succeeded := false
	for attempt := 0; attempt < maxResizeAttempts; attempt++ {
		buffer = make([]byte, size)
		err := windows.GetAdaptersAddresses(
			windows.AF_INET,
			flags,
			0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0])),
			&size,
		)
		if err == nil {
			succeeded = true
			break
		}
		if err == windows.ERROR_NO_DATA {
			// The requested family has no address data, which is a valid
			// empty collection rather than a fatal observation error.
			return nil, nil, nil
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil, nil, fmt.Errorf("read windows network interfaces: %w", err)
		}
		if size <= uint32(len(buffer)) {
			return nil, nil, fmt.Errorf("read windows network interfaces: %w", err)
		}
	}
	if !succeeded {
		return nil, nil, fmt.Errorf(
			"read windows network interfaces: buffer overflow persisted after %d attempts",
			maxResizeAttempts,
		)
	}

	return buffer, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0])), nil
}
