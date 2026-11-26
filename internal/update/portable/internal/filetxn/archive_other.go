//go:build !windows

package filetxn

import (
	"context"
	"os"
)

func openArchive(_ context.Context, _, path string, size int64) (*os.File, int64, error) {
	f, err := openArchiveSource(path, size)
	return f, size, err
}
