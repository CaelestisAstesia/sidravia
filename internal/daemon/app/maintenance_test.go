package app

import (
	"context"
	"encoding/json"
	launchcontract "sidravia/internal/background/launch"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/update"
	"testing"
)

func TestApplicationMaintenanceBlocksMutationsAndRestoresOnAbort(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	live := true
	closed := 0
	options := launchcontract.Options{Mode: launchcontract.ModeDesktop, DesktopOwnerPID: 123}
	called := 0
	handler := maintenanceWithCoordinator(setup.application, options, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		called++
		return json.RawMessage(`{}`), nil
	}, func(uint32) (func() bool, func(), error) {
		return func() bool { return live }, func() { closed++ }, nil
	})
	invoke := func(method string, value any) (update.MaintenanceView, *contract.Error) {
		t.Helper()
		payload, _ := json.Marshal(value)
		b, e := handler(context.Background(), method, payload)
		var v update.MaintenanceView
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
	if _, e = invoke(contract.MethodConfigurationRemove, map[string]string{"configurationId": "configuration-1"}); e == nil || e.Code != contract.ErrorCodeMaintenance || called != 0 {
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
	if _, e = invoke(contract.MethodConfigurationRemove, struct{}{}); e != nil || called != 1 {
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
		called := 0
		handler := maintenanceWithCoordinator(setup.application, launchcontract.Options{Mode: launchcontract.ModeHeadless, SkipAutoLogin: true}, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
			called++
			return json.RawMessage(`{}`), nil
		}, nil)
		if newChoice {
			handler(context.Background(), contract.MethodSessionStop, json.RawMessage(`{"sessionId":"new-choice"}`))
		}
		data, _ := json.Marshal(contract.UpdateResumePayload{OperationID: "0123456789abcdef0123456789abcdef", ConfigurationID: "configuration-1"})
		_, err := handler(context.Background(), contract.MethodUpdateResume, data)
		if newChoice && err == nil || !newChoice && err != nil {
			t.Fatalf("new choice %v: %v", newChoice, err)
		}
		_, err = handler(context.Background(), contract.MethodUpdateResume, data)
		if err == nil || called != 1 {
			t.Fatal("replayed old intent")
		}
		setup.cleanup()
	}
}

func TestRecoveryIsBoundToOperationAndConfiguration(t *testing.T) {
	setup := newApplicationTestSetup(t)
	defer setup.cleanup()
	called := 0
	operation := "0123456789abcdef0123456789abcdef"
	handler := maintenanceWithCoordinator(setup.application, launchcontract.Options{Mode: launchcontract.ModeHeadless, SkipAutoLogin: true, RecoveryOperation: operation, RecoveryConfiguration: "saved"}, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		called++
		return json.RawMessage(`{}`), nil
	}, nil)
	for _, input := range []contract.UpdateResumePayload{{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ConfigurationID: "saved"}, {OperationID: operation, ConfigurationID: "other"}} {
		data, _ := json.Marshal(input)
		if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err == nil {
			t.Fatal("foreign restore accepted")
		}
	}
	data, _ := json.Marshal(contract.UpdateResumePayload{OperationID: operation, ConfigurationID: "saved"})
	if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), contract.MethodUpdateResume, data); err == nil || called != 1 {
		t.Fatal("restore replayed")
	}
}
