package app

import (
	"context"
	"time"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/background/maintenance"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/installation/change"
	"sidravia/internal/ipc/contract"
)

// prepareMaintenance publishes the freeze while holding the application
// mutation lock. The IPC handler retains ownership of the coordinator handle.
func (application *Application) prepareMaintenance(ctx context.Context, mode launchcontract.Mode, input contract.UpdatePreparePayload, state *maintenance.Maintenance) error {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	if input.ManagedGUIUpdate {
		err := state.PrepareManagedGUI(input.OperationID, *input.Interactive, time.Now())
		if err == nil {
			application.maintenanceActive.Store(true)
		}
		return err
	}
	view, err := application.listSessionViewLocked(ctx)
	intent := maintenance.Intent{Mode: string(mode)}
	for _, s := range view.Sessions {
		if s.Intent == session.MaintainAuthentication {
			if s.ConfigurationID == "" && input.ServiceSwitch && input.InterruptOneShot {
				continue
			}
			if s.ConfigurationID == "" || intent.Maintain {
				err = change.ErrInvalid
				break
			}
			intent.Maintain = true
			intent.ConfigurationID = string(s.ConfigurationID)
		}
	}
	if err == nil {
		if input.ServiceSwitch {
			err = state.PrepareServiceSwitch(input.OperationID, intent, time.Now())
		} else {
			err = state.Prepare(input.OperationID, *input.Interactive, intent, time.Now())
		}
	}
	if err == nil {
		application.maintenanceActive.Store(true)
	}
	return err
}
