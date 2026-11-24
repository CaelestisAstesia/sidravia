package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

func TestSessionStartOneShotThroughComposedHandler(t *testing.T) {
	fixture := composeRuntimeWithJLUProfile(t, discardLogger())
	rt := fixture.runtime
	defer func() {
		rt.shutdown.Close()
		rt.shutdown.Wait()
	}()

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"
	ctx := context.Background()
	conn, err := client.Connect(ctx, wsURL, runtimeFixtureToken, runtimeFixtureBuildID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	usernameMarker := "fictional-user-7B2F0D91"
	passwordMarker := "fictional-password-4A8C6E13"
	startPayload, _ := json.Marshal(contract.SessionStartOneShotPayload{
		DisplayName:          "Test Session",
		InstitutionProfileID: "jlu",
		Username:             usernameMarker,
		Password:             passwordMarker,
		NetworkBindingPolicy: contract.NetworkBindingPolicy{Mode: "automatically_select_latest_available"},
	})

	startResponse, err := conn.Call(ctx, contract.MethodSessionStartOneShot, startPayload)
	if err != nil {
		t.Fatalf("session.startOneShot call: %v", err)
	}
	if !startResponse.OK {
		t.Fatalf("session.startOneShot error: %+v", startResponse.Error)
	}
	result := startResponse.Result
	if !bytes.Contains(result, []byte(usernameMarker)) {
		t.Fatal("session.startOneShot response omitted the full account name")
	}
	if bytes.Contains(result, []byte(passwordMarker)) {
		t.Fatal("raw session.startOneShot response contains password marker")
	}

	var startResult contract.SessionStartResult
	if err := json.Unmarshal(result, &startResult); err != nil {
		t.Fatalf("unmarshal session start result: %v", err)
	}
	if startResult.Outcome != "created" {
		t.Fatalf("session start outcome = %q, want created", startResult.Outcome)
	}
	sessionResult := startResult.Session

	if sessionResult.State != "waiting_for_network" {
		t.Fatalf("session.State = %q, want waiting_for_network", sessionResult.State)
	}

	if sessionResult.AuthenticationProtocolID != "drcom-5.2.0-d" {
		t.Fatalf("session.AuthenticationProtocolID = %q, want drcom-5.2.0-d", sessionResult.AuthenticationProtocolID)
	}

	if sessionResult.AccountName != usernameMarker {
		t.Fatalf("session.AccountName = %q, want complete username %q", sessionResult.AccountName, usernameMarker)
	}

	ensureResponse, err := conn.Call(ctx, contract.MethodSessionEnsureRunning, []byte(`{"sessionId":"`+sessionResult.AuthenticationSessionID+`"}`))
	if err != nil {
		t.Fatalf("session.ensureRunning call: %v", err)
	}
	if !ensureResponse.OK {
		t.Fatalf("session.ensureRunning error: %+v", ensureResponse.Error)
	}
	if bytes.Contains(ensureResponse.Result, []byte(passwordMarker)) {
		t.Fatal("raw session.ensureRunning response contains password marker")
	}
	var ensured contract.SessionStartResult
	if err := json.Unmarshal(ensureResponse.Result, &ensured); err != nil {
		t.Fatalf("unmarshal ensure result: %v", err)
	}
	if ensured.Outcome != "already_running" || ensured.Session.AuthenticationSessionID != sessionResult.AuthenticationSessionID || ensured.Session.AccountName != usernameMarker {
		t.Fatalf("ensure result = %#v", ensured)
	}

	listResponse, err := conn.Call(ctx, contract.MethodSessionList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("session.list call: %v", err)
	}
	if !listResponse.OK {
		t.Fatalf("session.list error: %+v", listResponse.Error)
	}
	listedData := listResponse.Result
	var listed contract.SessionListResult
	if err := json.Unmarshal(listedData, &listed); err != nil {
		t.Fatalf("unmarshal session.list: %v", err)
	}
	if len(listed.Sessions) != 1 ||
		listed.Sessions[0].AuthenticationSessionID != sessionResult.AuthenticationSessionID {
		t.Fatalf("session.list result = %#v", listed.Sessions)
	}

	stopPayload, _ := json.Marshal(contract.SessionStopPayload{
		SessionID: sessionResult.AuthenticationSessionID,
	})
	stopResponse, err := conn.Call(ctx, contract.MethodSessionStop, stopPayload)
	if err != nil {
		t.Fatalf("session.stop call: %v", err)
	}
	if !stopResponse.OK {
		t.Fatalf("session.stop error: %+v", stopResponse.Error)
	}

	if fixture.hostRunner.wasCalled() {
		t.Fatal("host runner was called during IPC session test")
	}
}

// TestCompositionDaemonStopTriggersRuntimeShutdown proves a committed
// daemon.stop response (written via the IPC server) signals the runtime-owned
// stop channel, which cancels the child context and runs the existing graceful
// shutdown path. The response reaches the client before shutdown begins.
func TestCompositionDaemonStopTriggersRuntimeShutdown(t *testing.T) {
	fixture := composeRuntimeWithJLUProfile(t, discardLogger())
	rt := fixture.runtime

	httpServer := httptest.NewServer(rt.hostCfg.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ipc"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := runRuntime(rt, context.Background())
	waitForSignal(t, fixture.hostRunner.started, "host start")
	waitForSignal(t, fixture.observer.started, "observer start")

	conn, err := client.Connect(ctx, wsURL, runtimeFixtureToken, runtimeFixtureBuildID)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	idle, err := client.Connect(ctx, wsURL, runtimeFixtureToken, runtimeFixtureBuildID)
	if err != nil {
		t.Fatalf("connect idle: %v", err)
	}
	defer idle.Close()

	resp, err := conn.Call(ctx, contract.MethodDaemonStop, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("daemon.stop call: %v", err)
	}
	if !resp.OK {
		t.Fatalf("daemon.stop error: %+v", resp.Error)
	}
	var stopResult contract.DaemonStopResult
	if err := json.Unmarshal(resp.Result, &stopResult); err != nil {
		t.Fatalf("unmarshal stop result: %v", err)
	}
	if stopResult.Status != "stopping" {
		t.Errorf("status = %q, want stopping", stopResult.Status)
	}
	if err := waitForRuntimeResult(t, result); err != nil {
		t.Fatalf("runtime did not shut down cleanly after daemon.stop: %v", err)
	}
	if _, err := idle.Call(ctx, contract.MethodDaemonStatus, json.RawMessage(`{}`)); err == nil {
		t.Fatal("idle IPC connection remained open after runtime shutdown")
	}
}
