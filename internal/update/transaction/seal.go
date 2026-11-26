package transaction

import (
	"os"
	"path/filepath"
)

// StartupSeal stays with a directory moved after a partial write. Execution
// records remain outside product files; this conservative presence-only seal
// prevents moving the target or clearing the cache from bypassing crash safety.
const StartupSeal = "sidravia.update"

func targetSealed(target string) bool {
	_, err := os.Lstat(filepath.Join(target, StartupSeal))
	return !os.IsNotExist(err)
}
