//go:build !windows

package desktopowner

func Open(pid int) (Watcher, error) {
	if pid <= 0 {
		return nil, ErrUnsupported
	}
	return nil, ErrUnsupported
}
