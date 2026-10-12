//go:build windows

package installationmanager

import (
	"os"
	"syscall"
)

func unsafeDataPath(info os.FileInfo) bool {
	// Junctions are ModeIrregular in current Go, while some other reparse
	// tags can look like regular files. Check the Windows attribute itself.
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return !ok || attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
