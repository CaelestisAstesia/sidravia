package application

import (
	"context"
	"encoding/json"
	"testing"

	"sidravia/internal/background/maintenance"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func TestApplicationMaintenanceBlocksMutationsAndRestoresOnAbort(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	live := true
	closed := 0
	options := launchcontract.Options{Mode: launchcontract.ModeDesktop, DesktopOwnerPID: 123}
	dispatcher := newIPCDispatcher(setup.application, "version", "build", options, func(uint32) (func() bool, func(), error) {
		return func() bool { return live }, func() { closed++ }, nil
	})
	invoke := func(method string, value any) (maintenance.View, *contract.Error) {
		t.Helper()
		payload, _ := json.Marshal(value)
		b, e := dispatcher.handle(context.Background(), method, payload)
		var v maintenance.View
		if e == nil {
			json.Unmarshal(b, &v)
		}
		return v, e
	}
	id := "0123456789abcdef0123456789abcdef"
	no := false
	yes := true
	view, e := invoke(contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: id, CoordinatorPID: 1, Interactive: &no})
	if e != nil || view.Phase != "requested" || !setup.application.maintenanceActive.Load() {
		t.Fatalf("%+v %+v", view, e)
	}
	if _, e = invoke(contract.MethodConfigurationRemove, map[string]string{"configurationId": "configuration-1"}); e == nil || e.Code != contract.ErrorCodeMaintenance {
		t.Fatal("mutation passed preparation")
	}
	if _, e = invoke(contract.MethodDaemonStop, struct{}{}); e == nil {
		t.Fatal("stop before commit")
	}
	if _, e = invoke(contract.MethodUpdateCommit, contract.UpdateOperationPayload{OperationID: id}); e == nil {
		t.Fatal("GUI approval bypassed")
	}
	if _, e = invoke(contract.MethodUpdateAcknowledge, contract.UpdateAcknowledgePayload{OperationID: id, Accepted: &yes, Visible: &no}); e != nil {
		t.Fatal(e)
	}
	if _, e = invoke(contract.MethodUpdateAbort, contract.UpdateOperationPayload{OperationID: id}); e != nil || closed != 1 || setup.application.maintenanceActive.Load() {
		t.Fatal("not thawed")
	}
	if _, e = invoke(contract.MethodConfigurationRemove, struct{}{}); e == nil || e.Code != contract.ErrorCodeInvalidArgument {
		t.Fatal("not restored")
	}
	// The session/configuration remain untouched by preparation and cancellation.
	configs, err := setup.catalog.List(context.Background())
	if err != nil || len(configs) != 1 {
		t.Fatal("configuration changed")
	}
	invoke(contract.MethodUpdatePrepare, contract.UpdatePreparePayload{OperationID: id, CoordinatorPID: 1, Interactive: &no})
	live = false
	view, e = invoke(contract.MethodUpdateStatus, struct{}{})
	if e != nil || view.Phase != "idle" || closed != 2 || setup.application.maintenanceActive.Load() {
		t.Fatal("coordinator death did not thaw")
	}
}

func TestRecoveryIntentIsOnceAndUserMutationWins(t *testing.T) {
	for _, newChoice := range []bool{false, true} {
		setup := newApplicationTestSetup(t)
		handler := newIPCDispatcher(setup.application, "version", "build", launchcontract.Options{Mode: launchcontract.ModeHeadless, SkipAutoLogin: true}, nil).handle
		if newChoice {
			handler(context.Background(), contract.MethodSessionStop, json.RawMessage(`{"sessionId":"new-choice"}`))
		}
		data, _ := json.Marshal(contract.UpdateResumePayload{OperationID: "0123456789abcdef0123456789abcdef", ConfigurationID: "configuration-1"})
		_, err := handler(context.Background(), contract.MethodUpdateResume, data)
		if newChoice && err == nil || !newChoice && err != nil {
			t.Fatalf("new choice %v: %v", newChoice, err)
		}
		_, err = handler(context.Background(), contract.MethodUpdateResume, data)
		if err == nil || err.Code != contract.ErrorCodeMaintenance {
			t.Fatal("replayed old intent")
		}
		setup.cleanup()
	}
}

func TestRecoveryIsBoundToOperationAndConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	operation := "0123456789abcdef0123456789abcdef"
	handler := newIPCDispatcher(setup.application, "version", "build", launchcontract.Options{Mode: launchcontract.ModeHeadless, SkipAutoLogin: true, RecoveryOperation: operation, RecoveryConfiguration: "configuration-1"}, nil).handle
	for _, input := range []contract.UpdateResumePayload{{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ConfigurationID: "configuration-1"}, {OperationID: operation, ConfigurationID: "other"}} {
		data, _ := json.Marshal(input)
		if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err == nil {
			t.Fatal("foreign restore accepted")
		}
	}
	data, _ := json.Marshal(contract.UpdateResumePayload{OperationID: operation, ConfigurationID: "configuration-1"})
	if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err == nil || err.Code != contract.ErrorCodeMaintenance {
		t.Fatal("restore replayed")
	}
}

func TestRecoveryConsumesIntentBeforeFailedStart(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	handler := newIPCDispatcher(setup.application, "version", "build", launchcontract.Options{
		Mode: launchcontract.ModeHeadless, SkipAutoLogin: true,
	}, nil).handle
	const operation = "0123456789abcdef0123456789abcdef"
	invoke := func(configID string) *contract.Error {
		data, err := json.Marshal(contract.UpdateResumePayload{OperationID: operation, ConfigurationID: configID})
		if err != nil {
			t.Fatal(err)
		}
		_, publicErr := handler(context.Background(), contract.MethodUpdateResume, data)
		return publicErr
	}
	if err := invoke("missing"); err == nil || err.Code == contract.ErrorCodeMaintenance {
		t.Fatalf("first request did not reach configuration start: %v", err)
	}
	if err := invoke("configuration-1"); err == nil || err.Code != contract.ErrorCodeMaintenance {
		t.Fatalf("failed start left restore permission available: %v", err)
	}
}

func TestMaintenanceRejectsMalformedMutationAfterConcurrentPrepare(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	opening := make(chan struct{})
	allowOpen := make(chan struct{})
	dispatcher := newIPCDispatcher(setup.application, "version", "build", launchcontract.Headless(),
		func(uint32) (func() bool, func(), error) {
			close(opening)
			<-allowOpen
			return func() bool { return true }, func() {}, nil
		})
	defer dispatcher.close()
	prepared := make(chan *contract.Error, 1)
	go func() {
		_, err := dispatcher.handle(context.Background(), contract.MethodUpdatePrepare,
			json.RawMessage(`{"operationId":"0123456789abcdef0123456789abcdef","coordinatorPid":1,"interactive":false}`))
		prepared <- err
	}()
	<-opening
	mutation := make(chan *contract.Error, 1)
	go func() {
		_, err := dispatcher.handle(context.Background(), contract.MethodConfigurationUpdate, json.RawMessage(`{}`))
		mutation <- err
	}()
	close(allowOpen)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if err := <-mutation; err == nil || err.Code != contract.ErrorCodeMaintenance {
		t.Fatalf("mutation was decoded before maintenance admission: %v", err)
	}
}
