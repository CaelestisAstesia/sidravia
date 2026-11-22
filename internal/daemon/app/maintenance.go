package app

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
	"sidravia/internal/persistence/jsonfile"
	"sidravia/internal/update"
)

type ipcHandler func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error)

// One application boundary serializes prepare/snapshot with all IPC mutations.
// Session actors keep their existing ownership and reconnect behavior.
func withMaintenance(application *Application, options launchcontract.Options, next ipcHandler) ipcHandler {
	return maintenanceWithCoordinator(application, options, next, openMaintenanceCoordinator)
}
func maintenanceWithCoordinator(application *Application, options launchcontract.Options, next ipcHandler, open func(uint32) (func() bool, func(), error)) ipcHandler {
	restoreAvailable := options.SkipAutoLogin
	var mu sync.Mutex
	var state update.Maintenance
	var alive func() bool
	var closeOwner func()
	resetOwner := func() {
		if closeOwner != nil {
			closeOwner()
			closeOwner = nil
			alive = nil
		}
		application.maintenanceActive.Store(false)
	}
	failure := func(code string) (json.RawMessage, *contract.Error) {
		return nil, &contract.Error{Code: code, Message: "更新准备未完成，请取消更新或正常退出后重试。"}
	}
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		if options.Mode == launchcontract.ModeSystemd {
			switch method {
			case contract.MethodDaemonStop:
				return nil, &contract.Error{
					Code:    contract.ErrorCodeMaintenance,
					Message: "后台由 systemd 托管，请使用用户服务管理入口。",
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if alive != nil && state.Expire(time.Now(), alive()) {
			resetOwner()
		}
		output := func() (json.RawMessage, *contract.Error) {
			b, err := json.Marshal(state.Status())
			if err != nil {
				return failure(contract.ErrorCodeInternalError)
			}
			return b, nil
		}
		switch method {
		case contract.MethodUpdateStatus:
			if contract.DecodeEmptyPayload(payload) != nil {
				return failure(contract.ErrorCodeInvalidArgument)
			}
			return output()
		case contract.MethodUpdateResume:
			var input contract.UpdateResumePayload
			if jsonfile.DecodeStrict(payload, &input) != nil || !update.ValidOperationID(input.OperationID) || input.ConfigurationID == "" || !restoreAvailable || state.Status().Phase != "idle" {
				return failure(contract.ErrorCodeMaintenance)
			}
			if options.SwitchOperation != "" && (input.OperationID != options.SwitchOperation || input.ConfigurationID != options.SwitchConfiguration) {
				return failure(contract.ErrorCodeMaintenance)
			}
			if options.RecoveryOperation != "" && (input.OperationID != options.RecoveryOperation || input.ConfigurationID != options.RecoveryConfiguration) {
				return failure(contract.ErrorCodeMaintenance)
			}
			restoreAvailable = false
			data, _ := json.Marshal(map[string]string{"configurationId": input.ConfigurationID})
			return next(ctx, contract.MethodSessionStartConfiguration, data)
		case contract.MethodUpdatePrepare:
			var input contract.UpdatePreparePayload
			if jsonfile.DecodeStrict(payload, &input) != nil || !update.ValidOperationID(input.OperationID) || input.CoordinatorPID == 0 || input.Interactive == nil || state.Status().Phase != "idle" {
				return failure(contract.ErrorCodeInvalidArgument)
			}
			if (options.Mode == launchcontract.ModeSystemd && !input.ServiceSwitch && !input.ManagedGUIUpdate) ||
				(input.ManagedGUIUpdate && (options.Mode != launchcontract.ModeSystemd || input.ServiceSwitch)) ||
				(input.ServiceSwitch && (*input.Interactive || options.Namespace != "")) ||
				(input.InterruptOneShot && !input.ServiceSwitch) {
				return failure(contract.ErrorCodeMaintenance)
			}
			live, close, err := open(input.CoordinatorPID)
			if err != nil {
				return failure(contract.ErrorCodeMaintenance)
			}
			err = application.prepareMaintenance(ctx, options.Mode, input, &state)
			if err != nil {
				close()
				return failure(contract.ErrorCodeMaintenance)
			}
			alive, closeOwner = live, close
			return output()
		case contract.MethodUpdateAcknowledge:
			var input contract.UpdateAcknowledgePayload
			if jsonfile.DecodeStrict(payload, &input) != nil || input.Accepted == nil || input.Visible == nil || state.Acknowledge(input.OperationID, *input.Accepted, *input.Visible) != nil {
				return failure(contract.ErrorCodeInvalidArgument)
			}
			return output()
		case contract.MethodUpdateAbort:
			var input contract.UpdateOperationPayload
			if jsonfile.DecodeStrict(payload, &input) != nil || state.Abort(input.OperationID) != nil {
				return failure(contract.ErrorCodeInvalidArgument)
			}
			resetOwner()
			return output()
		case contract.MethodUpdateCommit:
			var input contract.UpdateOperationPayload
			if jsonfile.DecodeStrict(payload, &input) != nil || state.Commit(input.OperationID) != nil {
				return failure(contract.ErrorCodeInvalidArgument)
			}
			return output()
		}
		if state.Status().Phase != "idle" {
			switch method {
			case contract.MethodDaemonStatus, contract.MethodProfileList, contract.MethodConfigurationList, contract.MethodConfigurationGet, contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride, contract.MethodSessionList, contract.MethodSessionGet, contract.MethodSessionBusinessGet, contract.MethodNetworkInterfaces:
			case contract.MethodDaemonStop:
				if state.Status().Phase != "committed" {
					return failure(contract.ErrorCodeMaintenance)
				}
			default:
				return failure(contract.ErrorCodeMaintenance)
			}
		}
		switch method {
		case contract.MethodDaemonStatus, contract.MethodProfileList, contract.MethodConfigurationList, contract.MethodConfigurationGet, contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride, contract.MethodSessionList, contract.MethodSessionGet, contract.MethodSessionBusinessGet, contract.MethodNetworkInterfaces, contract.MethodDiagnosticsExport:
		default:
			restoreAvailable = false
		}
		return next(ctx, method, payload)
	}
}
