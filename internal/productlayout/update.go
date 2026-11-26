package productlayout

import (
	"os"
	"path/filepath"
)

// UpdateCacheRoot returns the existing update cache location, separate from daemon
// data directories and from the install tree being replaced.
func UpdateCacheRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Sidravia", "updates"), nil
}

// OperationRoot is shared by updater status and portable process admission.
func OperationRoot() (string, error) {
	root, err := UpdateCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "operations"), nil
}
