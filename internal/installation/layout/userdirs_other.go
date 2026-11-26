//go:build !linux

package layout

import "os"

func platformUserConfigDir() (string, error) { return os.UserConfigDir() }
func platformUserCacheDir() (string, error)  { return os.UserCacheDir() }
