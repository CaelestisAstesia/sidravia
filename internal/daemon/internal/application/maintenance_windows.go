//go:build windows

package application

import (
	"os"

	"sidravia/internal/process/winproc"
)

func openMaintenanceCoordinator(pid uint32) (func() bool, func(), error) {
	peer, err := winproc.Observe(pid)
	if err != nil {
		return nil, nil, err
	}
	self, err := winproc.Observe(uint32(os.Getpid()))
	if err != nil {
		peer.Close()
		return nil, nil, err
	}
	defer self.Close()
	if peer.SID != self.SID || peer.Integrity != self.Integrity {
		peer.Close()
		return nil, nil, winproc.ErrPeer
	}
	return peer.Running, func() { peer.Close() }, nil
}
