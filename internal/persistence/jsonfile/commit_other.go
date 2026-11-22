//go:build !linux && !windows

package jsonfile

import "os"

// Unsupported desktop targets retain atomic no-replacement semantics where
// hard links are available. There is no unsafe replacement fallback.
func commitNewFile(tempPath, destinationPath string) (bool, error) {
	if err := os.Link(tempPath, destinationPath); err != nil {
		return false, err
	}
	return true, os.Remove(tempPath)
}
