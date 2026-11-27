// Package qtinstaller checks the executable prefix copied by Qt IFW into the
// installed maintenance program.
package qtinstaller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

var ErrImageMismatch = errors.New("installer maintenance image does not match")

// VerifyPrefix compares the exact maintenance image at the start of an offline
// installer. The installer must contain a payload after the image.
func VerifyPrefix(path string, imageSize int64, imageSHA256 string) error {
	if imageSize <= 0 {
		return ErrImageMismatch
	}
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() <= imageSize {
		return ErrImageMismatch
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, info) || info.Size() <= imageSize {
		return ErrImageMismatch
	}
	hash := sha256.New()
	if _, err := io.CopyN(hash, file, imageSize); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != imageSHA256 {
		return ErrImageMismatch
	}
	return nil
}
