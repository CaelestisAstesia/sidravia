//go:build linux

package app

import "sidravia/internal/update/linuxplatform"

func openMaintenanceCoordinator(pid uint32) (func() bool, func(), error) {
	peer, err := linuxplatform.Observe(int(pid))
	if err != nil {
		return nil, nil, err
	}
	return func() bool { running, err := peer.Running(); return err == nil && running }, func() { _ = peer.Close() }, nil
}
