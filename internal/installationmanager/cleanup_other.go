//go:build !windows

package installationmanager

import "os"

func unsafeDataPath(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
