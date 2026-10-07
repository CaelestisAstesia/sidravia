//go:build windows

package app

import (
	"context"
	"testing"
	"time"

	"sidravia/internal/daemon/authentication/protocol/drcom/d520"
	"sidravia/internal/daemon/authentication/session"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/launchcontract"
)

func TestNetworkDiagnosisWindowsRealAppLoopbackRouteAndEcho(t *testing.T) {
	setup := diagnosticApplication(t, diagnosticLoopbackProfile(t), d520.NewFactory(), session.NetworkBindingPolicy{Mode: session.AutomaticallySelectLatestAvailable})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	handler := IPCHandler(setup.application, "v", "b", launchcontract.Headless())
	raw, rpc := handler(ctx, contract.MethodNetworkDiagnose, []byte(`{"configurationId":"configuration-1","probe":true}`))
	if rpc != nil {
		t.Fatal("real App diagnosis failed")
	}
	result, err := contract.DecodeNetworkDiagnoseResult(raw)
	if err != nil || result.Status != "available" || result.SelectionBasis != "os_route_proposal" || result.Target.Address != "127.0.0.1" || result.Route == nil || result.Route.SourceIPv4 != "127.0.0.1" || result.Probe == nil || result.Probe.Status != "reachable" || result.Probe.RoundTripTimeMs == nil || result.ProtocolSocket != nil {
		t.Fatal("real loopback diagnosis facts incomplete")
	}
	sessions, err := setup.supervisor.List(ctx)
	if err != nil || len(sessions) != 0 {
		t.Fatal("native read started authentication")
	}
}
