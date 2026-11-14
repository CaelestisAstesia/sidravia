//go:build !linux

package productlayout

import "os"

func platformUserConfigDir() (string, error) { return os.UserConfigDir() }
func platformUserCacheDir() (string, error)  { return os.UserCacheDir() }
