//go:build !windows

package jsonfile

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

const (
	secureTempAttempts    = 16
	secureTempRandomBytes = 16
)

type otherSecureFileOperations struct{}

func init() {
	platformOperationsFactory = func() secureFileOperations { return newOtherSecureFileOperations() }
}

func newOtherSecureFileOperations() *otherSecureFileOperations {
	return &otherSecureFileOperations{}
}

func (*otherSecureFileOperations) resolveOwner(string) (resolvedOwner, error) {
	return resolvedOwner{}, nil
}

func (*otherSecureFileOperations) ensureDirectory(path string, _ resolvedOwner) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

func (*otherSecureFileOperations) inspectDestination(path string) (bool, error) {
	information, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
		return false, os.ErrPermission
	}
	return true, nil
}

func (*otherSecureFileOperations) hardenDestination(path string, _ resolvedOwner) error {
	return os.Chmod(path, 0600)
}

func (*otherSecureFileOperations) openForRead(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (*otherSecureFileOperations) createTemp(directory string, _ resolvedOwner) (writableTemp, error) {
	for attempt := 0; attempt < secureTempAttempts; attempt++ {
		random := make([]byte, secureTempRandomBytes)
		if _, err := io.ReadFull(rand.Reader, random); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, secureTempBasename(random))
		if !validDirectChild(directory, path) {
			return nil, os.ErrInvalid
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return file, nil
	}
	return nil, os.ErrExist
}

func secureTempBasename(random []byte) string {
	return ".sidravia-" + hex.EncodeToString(random) + ".tmp"
}

func (*otherSecureFileOperations) commit(tempPath, destinationPath string, _ bool) (bool, error) {
	if err := os.Rename(tempPath, destinationPath); err != nil {
		return false, err
	}
	directory, err := os.Open(filepath.Dir(destinationPath))
	if err != nil {
		return true, err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return true, syncErr
	}
	return true, closeErr
}

func (*otherSecureFileOperations) removeTemp(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
