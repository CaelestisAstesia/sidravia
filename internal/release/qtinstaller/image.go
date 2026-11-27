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
	_, _, err := inspect(path, imageSize, imageSHA256, false)
	return err
}

// Inspect checks the prefix and hashes the complete offline installer in one pass.
func Inspect(path string, imageSize int64, imageSHA256 string) (int64, string, error) {
	return inspect(path, imageSize, imageSHA256, true)
}

func inspect(path string, imageSize int64, imageSHA256 string, full bool) (int64, string, error) {
	if imageSize <= 0 {
		return 0, "", ErrImageMismatch
	}
	before, err := os.Lstat(path)
	if err != nil {
		return 0, "", err
	}
	if !before.Mode().IsRegular() || before.Size() <= imageSize {
		return 0, "", ErrImageMismatch
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, "", err
	}
	if !os.SameFile(before, info) || info.Size() <= imageSize || (full && info.Size() > 2<<30) {
		return 0, "", ErrImageMismatch
	}
	hash := sha256.New()
	complete := sha256.New()
	var source io.Reader = file
	if full {
		source = io.TeeReader(file, complete)
	}
	if _, err := io.CopyN(hash, source, imageSize); err != nil {
		return 0, "", err
	}
	if hex.EncodeToString(hash.Sum(nil)) != imageSHA256 {
		return 0, "", ErrImageMismatch
	}
	if !full {
		return 0, "", nil
	}
	size, err := io.Copy(complete, io.LimitReader(file, info.Size()-imageSize+1))
	if err != nil {
		return 0, "", err
	}
	after, err := os.Lstat(path)
	if err != nil || size != info.Size()-imageSize || !os.SameFile(info, after) ||
		after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return 0, "", errors.New("installer changed during verification")
	}
	return info.Size(), hex.EncodeToString(complete.Sum(nil)), nil
}
