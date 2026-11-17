//go:build linux || windows

package portable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/productlayout"
	"sidravia/internal/update"
	"sidravia/internal/update/operations"
)

// restoreApplication launches one exact child with auto-login suppressed. The
// runtime generation and optional saved-configuration dispatch are separate
// from file installation; a successful dispatch is not authentication success.
func restoreApplication(ctx context.Context, layout productlayout.Layout, target update.Target, recovery operations.Recovery, operation, nonce string) (contract.RuntimeInfo, error) {
	if recovery.Intent.Mode == "none" {
		return contract.RuntimeInfo{}, nil
	}
	ticketID := operation
	if ticketID == "" {
		ticketID = strings.Repeat("0", 32)
	}
	plan, err := update.PlanRestore(update.ResumeTicket{OperationID: ticketID, Target: target, Intent: recovery.Intent, ExpiresAt: recovery.ExpiresAt}, target, true, time.Now())
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	if _, err := os.Lstat(layout.RuntimeInfoPath); !os.IsNotExist(err) {
		return contract.RuntimeInfo{}, ErrChanged
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case operations.EnvRecoveryOperation, operations.EnvRecoveryToken, operations.EnvRecoveryConfiguration, launchcontract.EnvNamespace, launchcontract.EnvMode, launchcontract.EnvDesktopOwnerPID, launchcontract.EnvDesktopOwnerStart, launchcontract.EnvSkipAutoLogin, launchcontract.EnvLogLevel:
			continue
		}
		env = append(env, entry)
	}
	env = append(env, launchcontract.EnvNamespace+"="+recovery.Namespace, launchcontract.EnvSkipAutoLogin+"=1", launchcontract.EnvLogLevel+"="+recovery.LogLevel)
	if operation != "" {
		env = append(env, operations.EnvRecoveryOperation+"="+operation, operations.EnvRecoveryToken+"="+nonce, operations.EnvRecoveryConfiguration+"="+plan.ConfigurationID)
	}
	executable := filepath.Join(target.ID, daemonName())
	var args []string
	if plan.Mode == "desktop" {
		executable = guiEntry(target.ID)
		visible := "--start-hidden"
		if plan.Visible {
			visible = "--start-visible"
		}
		args = []string{"--skip-auto-login", visible}
	} else {
		env = append(env, launchcontract.EnvMode+"=headless")
	}
	pid, err := startApplication(executable, args, env)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	process, err := observeProcess(pid)
	if err != nil {
		return contract.RuntimeInfo{}, err
	}
	defer process.Close()
	wait, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		alive, err := process.Running()
		if err != nil || !alive {
			return contract.RuntimeInfo{}, errors.Join(ErrRestore, err)
		}
		peer, err := probeApplication(wait, layout, target)
		if err == nil && peer != nil {
			matches := peer.status.Mode == plan.Mode
			if plan.Mode == "desktop" {
				matches = matches && peer.gui != nil && peer.gui.PID == process.PID && peer.gui.Start == process.Start
			} else {
				matches = matches && peer.daemon.PID == process.PID && peer.daemon.Start == process.Start
			}
			if !matches {
				peer.close()
				return contract.RuntimeInfo{}, ErrChanged
			}
			if plan.ConfigurationID != "" {
				// The per-generation IPC owner consumes this once before invoking Session.
				err = call(wait, peer.connection, contract.MethodUpdateResume, contract.UpdateResumePayload{OperationID: ticketID, ConfigurationID: plan.ConfigurationID}, nil)
			}
			info := peer.info
			peer.close()
			return info, err
		}
		if err := pause(wait); err != nil {
			return contract.RuntimeInfo{}, err
		}
	}
}
