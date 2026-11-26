package application

import (
	"context"
	"encoding/json"
	"sync"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

type ipcHandler func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error)
type openCoordinator func(uint32) (alive func() bool, close func(), err error)

type ipcDispatcher struct {
	application    *Application
	options        launchcontract.Options
	open           openCoordinator
	status         ipcHandler
	daemon         ipcHandler
	sessions       ipcHandler
	business       ipcHandler
	profiles       ipcHandler
	configurations ipcHandler
	network        ipcHandler
	diagnostics    ipcHandler

	mu               sync.Mutex // held through admission and execution; precedes application.operationMu
	state            maintenance.Maintenance
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
		status:           StatusHandler(productVersion, buildID, options),
		daemon:           DaemonHandler(),
		sessions:         SessionHandler(application),
		business:         BusinessHandler(application),
		profiles:         ProfileHandler(application),
		configurations:   ConfigurationHandler(application),
		network:          NetworkHandler(application),
		diagnostics:      DiagnosticsExportHandler(application, productVersion, buildID),
	}
}

func IPCHandler(application *Application, productVersion, buildID string, options launchcontract.Options) ipcHandler {
	return newIPCDispatcher(application, productVersion, buildID, options, openMaintenanceCoordinator).handle
}

func (dispatcher *ipcDispatcher) dispatch(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
	switch method {
	case contract.MethodSessionBusinessGet:
		return dispatcher.business(ctx, method, payload)
	case contract.MethodDiagnosticsExport:
		return dispatcher.diagnostics(ctx, method, payload)
	case contract.MethodDaemonStatus:
		return dispatcher.status(ctx, method, payload)
	case contract.MethodDaemonStop:
		return dispatcher.daemon(ctx, method, payload)
	case contract.MethodSessionStartOneShot,
		contract.MethodSessionStartConfiguration,
		contract.MethodSessionStop,
		contract.MethodSessionEnsureRunning,
		contract.MethodSessionRestart,
		contract.MethodSessionRemove,
		contract.MethodSessionGet,
		contract.MethodSessionList:
		return dispatcher.sessions(ctx, method, payload)
	case contract.MethodConfigurationGetOverride, contract.MethodConfigurationValidateOverride:
		return dispatcher.application.handleConfigurationOverride(ctx, method, payload)
	case contract.MethodConfigurationList,
		contract.MethodConfigurationGet,
		contract.MethodConfigurationCreate,
		contract.MethodConfigurationUpdate,
		contract.MethodConfigurationSetPassword,
		contract.MethodConfigurationRemove:
		return dispatcher.configurations(ctx, method, payload)
	case contract.MethodNetworkInterfaces:
		return dispatcher.network(ctx, method, payload)
	case contract.MethodProfileList:
		return dispatcher.profiles(ctx, method, payload)
	default:
		return nil, &contract.Error{
			Code:    contract.ErrorCodeUnknownMethod,
			Message: "unsupported method",
		}
	}
}
