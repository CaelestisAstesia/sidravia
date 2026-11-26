package application

import (
	"context"
	"encoding/json"
	"sync"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
)

type openCoordinator func(uint32) (alive func() bool, close func(), err error)

type ipcDispatcher struct {
	application    *Application
	options        launchcontract.Options
	open           openCoordinator
	productVersion string
	buildID        string

	mu               sync.Mutex // held through admission and execution; precedes application.operationMu
	state            maintenanceState
	restoreAvailable bool
	alive            func() bool
	closeOwner       func()
	closed           bool
}

func newIPCDispatcher(application *Application, productVersion, buildID string, options launchcontract.Options, open openCoordinator) *ipcDispatcher {
	return &ipcDispatcher{
		application:      application,
		options:          options,
		open:             open,
		restoreAvailable: options.SkipAutoLogin,
		productVersion:   productVersion, buildID: buildID,
	}
}

func (dispatcher *ipcDispatcher) dispatch(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	switch method {
	case contract.MethodSessionBusinessGet:
		return dispatchBusiness(dispatcher.application, ctx, method, payload)
	case contract.MethodDiagnosticsExport:
		return dispatchDiagnosticsExport(dispatcher.application, dispatcher.productVersion, dispatcher.buildID, ctx, method, payload)
	case contract.MethodDaemonStatus:
		return dispatchStatus(dispatcher.productVersion, dispatcher.buildID, dispatcher.options, ctx, method, payload)
	case contract.MethodDaemonStop:
		return dispatchDaemon(ctx, method, payload)
	case contract.MethodSessionStartOneShot,
		contract.MethodSessionStartConfiguration,
		contract.MethodSessionStop,
		contract.MethodSessionEnsureRunning,
		contract.MethodSessionRestart,
		contract.MethodSessionRemove,
		contract.MethodSessionGet,
		contract.MethodSessionList:
		return dispatchSession(dispatcher.application, ctx, method, payload)
	case contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride:
		return dispatcher.application.handleConfigurationOverride(ctx, method, payload)
	case contract.MethodConfigurationList,
		contract.MethodConfigurationGet,
		contract.MethodConfigurationCreate,
		contract.MethodConfigurationUpdate,
		contract.MethodConfigurationSetPassword,
		contract.MethodConfigurationRemove:
		return dispatchConfiguration(dispatcher.application, ctx, method, payload)
	case contract.MethodNetworkInterfaces:
		return dispatchNetwork(dispatcher.application, ctx, method, payload)
	case contract.MethodProfileList:
		return dispatchProfile(dispatcher.application, ctx, method, payload)
	default:
		return nil, &contract.Error{
			Code:    contract.ErrorCodeUnknownMethod,
			Message: "unsupported method",
		}
	}
}
