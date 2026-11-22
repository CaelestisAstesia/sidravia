package cli

import (
	"os"
	"path/filepath"
	"runtime"
)

func siblingExecutable(name string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(executable), name), nil
}
