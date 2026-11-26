package application

import (
	"context"
	"encoding/json"
	"testing"

	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
)

type ipcHandler = func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error)

func testIPCHandler(t *testing.T, app *Application, version, build string, opts launchcontract.Options) ipcHandler {
	t.Helper()
	dispatcher := newIPCDispatcher(app, version, build, opts, openMaintenanceCoordinator)
	t.Cleanup(dispatcher.close)
	return dispatcher.handle
}

func testStatusHandler(productVersion string, buildID string, options launchcontract.Options) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchStatus(productVersion, buildID, options, ctx, method, payload)
	}
}

func testDaemonHandler() ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchDaemon(ctx, method, payload)
	}
}

func testSessionHandler(application sessionApplication) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchSession(application, ctx, method, payload)
	}
}

func testBusinessHandler(application businessApplication) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchBusiness(application, ctx, method, payload)
	}
}

func testProfileHandler(application profileApplication) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchProfile(application, ctx, method, payload)
	}
}

func testConfigurationHandler(application configurationApplication) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchConfiguration(application, ctx, method, payload)
	}
}

func testDiagnosticsExportHandler(application *Application, productVersion, buildID string) ipcHandler {
	return func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error) {
		return dispatchDiagnosticsExport(application, productVersion, buildID, ctx, method, payload)
	}
}
