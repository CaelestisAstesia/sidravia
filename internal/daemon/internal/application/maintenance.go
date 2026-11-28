package application

import (
	"context"
	"encoding/json"
	"time"

	launchcontract "sidravia/internal/background/launch"
	connectionconfig "sidravia/internal/connection/config"
	"sidravia/internal/document/jsondoc"
	"sidravia/internal/installation/change"
	"sidravia/internal/ipc/contract"
)

func (dispatcher *ipcDispatcher) resetOwner() {
	if dispatcher.closeOwner != nil {
		dispatcher.closeOwner()
		dispatcher.closeOwner = nil
		dispatcher.alive = nil
	}
	dispatcher.application.maintenanceActive.Store(false)
}

func (dispatcher *ipcDispatcher) close() {
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	dispatcher.closed = true
	dispatcher.resetOwner()
}

func maintenanceFailure(code string) (json.RawMessage, *contract.Error) {
	return nil, &contract.Error{Code: code, Message: "更新准备未完成，请取消更新或正常退出后重试。"}
}

func (dispatcher *ipcDispatcher) maintenanceStatus() (json.RawMessage, *contract.Error) {
	data, err := json.Marshal(dispatcher.state.Status())
	if err != nil {
		return maintenanceFailure(contract.ErrorCodeInternalError)
	}
	return data, nil
}

// Maintenance admission and typed dispatch share one serialized scope.
func (dispatcher *ipcDispatcher) handle(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	if dispatcher.options.Mode == launchcontract.ModeSystemd {
		switch method {
		case contract.MethodDaemonStop:
			return nil, &contract.Error{
				Code:    contract.ErrorCodeMaintenance,
				Message: "后台由 systemd 托管，请使用用户服务管理入口。",
			}
		}
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.closed {
		return maintenanceFailure(contract.ErrorCodeMaintenance)
	}
	if dispatcher.alive != nil && dispatcher.state.Expire(time.Now(), dispatcher.alive()) {
		dispatcher.resetOwner()
	}
	switch method {
	case contract.MethodUpdateStatus:
		if contract.DecodeEmptyPayload(payload) != nil {
			return maintenanceFailure(contract.ErrorCodeInvalidArgument)
		}
		return dispatcher.maintenanceStatus()
	case contract.MethodUpdateResume:
		var input contract.UpdateResumePayload
		if jsondoc.DecodeStrict(payload, &input) != nil || !change.ValidID(input.OperationID) || input.ConfigurationID == "" || !dispatcher.restoreAvailable || dispatcher.state.Status().Phase != "idle" {
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
		if dispatcher.options.SwitchOperation != "" && (input.OperationID != dispatcher.options.SwitchOperation || input.ConfigurationID != dispatcher.options.SwitchConfiguration) {
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
		dispatcher.restoreAvailable = false
		return startConfigurationIPC(ctx, dispatcher.application, connectionconfig.ID(input.ConfigurationID), "")
	case contract.MethodUpdatePrepare:
		var input contract.UpdatePreparePayload
		if jsondoc.DecodeStrict(payload, &input) != nil || !change.ValidID(input.OperationID) || input.CoordinatorPID == 0 || input.Interactive == nil || dispatcher.state.Status().Phase != "idle" {
			return maintenanceFailure(contract.ErrorCodeInvalidArgument)
		}
		if (dispatcher.options.Mode == launchcontract.ModeSystemd && !input.ServiceSwitch && !input.ManagedGUIUpdate) ||
			(input.ManagedGUIUpdate && (dispatcher.options.Mode != launchcontract.ModeSystemd || input.ServiceSwitch)) ||
			(input.ServiceSwitch && (*input.Interactive || dispatcher.options.Namespace != "")) ||
			(input.InterruptOneShot && !input.ServiceSwitch) {
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
		live, close, err := dispatcher.open(input.CoordinatorPID)
		if err != nil {
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
		err = dispatcher.application.prepareMaintenance(ctx, dispatcher.options.Mode, input, &dispatcher.state)
		if err != nil {
			close()
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
		dispatcher.alive, dispatcher.closeOwner = live, close
		return dispatcher.maintenanceStatus()
	case contract.MethodUpdateAcknowledge:
		var input contract.UpdateAcknowledgePayload
		if jsondoc.DecodeStrict(payload, &input) != nil || input.Accepted == nil || input.Visible == nil || dispatcher.state.Acknowledge(input.OperationID, *input.Accepted, *input.Visible) != nil {
			return maintenanceFailure(contract.ErrorCodeInvalidArgument)
		}
		return dispatcher.maintenanceStatus()
	case contract.MethodUpdateAbort:
		var input contract.UpdateOperationPayload
		if jsondoc.DecodeStrict(payload, &input) != nil || dispatcher.state.Abort(input.OperationID) != nil {
			return maintenanceFailure(contract.ErrorCodeInvalidArgument)
		}
		dispatcher.resetOwner()
		return dispatcher.maintenanceStatus()
	case contract.MethodUpdateCommit:
		var input contract.UpdateOperationPayload
		if jsondoc.DecodeStrict(payload, &input) != nil || dispatcher.state.Commit(input.OperationID) != nil {
			return maintenanceFailure(contract.ErrorCodeInvalidArgument)
		}
		return dispatcher.maintenanceStatus()
	}
	if dispatcher.state.Status().Phase != "idle" {
		switch method {
		case contract.MethodDaemonStatus, contract.MethodProfileList, contract.MethodConfigurationList, contract.MethodConfigurationGet, contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride, contract.MethodSessionList, contract.MethodSessionGet, contract.MethodSessionBusinessGet, contract.MethodNetworkInterfaces:
		case contract.MethodDaemonStop:
			if dispatcher.state.Status().Phase != "committed" {
				return maintenanceFailure(contract.ErrorCodeMaintenance)
			}
		default:
			return maintenanceFailure(contract.ErrorCodeMaintenance)
		}
	}
	switch method {
	case contract.MethodDaemonStatus, contract.MethodProfileList, contract.MethodConfigurationList, contract.MethodConfigurationGet, contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride, contract.MethodSessionList, contract.MethodSessionGet, contract.MethodSessionBusinessGet, contract.MethodNetworkInterfaces, contract.MethodDiagnosticsExport:
	default:
		dispatcher.restoreAvailable = false
	}
	return dispatcher.dispatch(ctx, method, payload)
}
