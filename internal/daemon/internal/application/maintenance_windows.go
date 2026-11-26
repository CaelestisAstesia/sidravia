//go:build windows

package application

import (
	"os"

	"sidravia/internal/update/windowsplatform"
)

func openMaintenanceCoordinator(pid uint32) (func() bool, func(), error) {
	peer, err := windowsplatform.Observe(pid)
	if err != nil {
		return nil, nil, err
	}
	self, err := windowsplatform.Observe(uint32(os.Getpid()))
	if err != nil {
		peer.Close()
		return nil, nil, err
	}
	defer self.Close()
	if peer.SID != self.SID || peer.Integrity != self.Integrity {
		peer.Close()
		return nil, nil, windowsplatform.ErrPeer
	}
	return peer.Running, func() { peer.Close() }, nil
}
