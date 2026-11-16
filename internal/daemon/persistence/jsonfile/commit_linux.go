//go:build linux

package jsonfile

import "golang.org/x/sys/unix"

// commitNewFile publishes without replacing a name that appeared since inspection.
// RENAME_NOREPLACE does not require the filesystem to support hard links.
func commitNewFile(tempPath, destinationPath string) (bool, error) {
	if err := unix.Renameat2(unix.AT_FDCWD, tempPath, unix.AT_FDCWD, destinationPath, unix.RENAME_NOREPLACE); err != nil {
		return false, err
	}
	return true, nil
}
