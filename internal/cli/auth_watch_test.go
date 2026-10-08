package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

type authWatchTestStream struct {
	next     func(context.Context) (contract.StateEvent, error)
	closeErr error
	closed   int
}

func (stream *authWatchTestStream) Next(ctx context.Context) (contract.StateEvent, error) {
	return stream.next(ctx)
}

func (stream *authWatchTestStream) Close() error {
	stream.closed++
	return stream.closeErr
}

type authWatchTestConnection struct {
	subscribe func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error)
	closeErr  error
	closed    int
	calls     int
}

func (connection *authWatchTestConnection) Call(context.Context, string, json.RawMessage) (contract.Response, error) {
	connection.calls++
	return contract.Response{}, errors.New("unexpected RPC call")
}

func (connection *authWatchTestConnection) Close() error {
	connection.closed++
	return connection.closeErr
}

func (connection *authWatchTestConnection) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	return connection.subscribe(ctx)
}

type authWatchTestPlainConnection struct {
	closed int
}

func (connection *authWatchTestPlainConnection) Call(context.Context, string, json.RawMessage) (contract.Response, error) {
	return contract.Response{}, errors.New("unexpected RPC call")
}

func (connection *authWatchTestPlainConnection) Close() error {
	connection.closed++
	return nil
}

func authWatchTestDependencies(connection daemonClient, output io.Writer) listDependencies {
	return listDependencies{
		connection: daemonConnectionDependencies{
			acquire: func(ctx context.Context) (daemonClient, error) {
				if _, ok := ctx.Deadline(); !ok {
					return nil, errors.New("acquisition missing deadline")
				}
				return connection, nil
			},
			callTimeout: time.Second,
		},
		stdout: output,
	}
}

func authWatchTestBootstrap(sessions []contract.SessionResult, cleanup []string, network contract.NetworkInterfacesResult) contract.StateBootstrap {
	if sessions == nil {
		sessions = []contract.SessionResult{}
	}
	if cleanup == nil {
		cleanup = []string{}
	}
	if network.Interfaces == nil {
		network.Interfaces = []contract.NetworkInterfaceResult{}
	}
	return contract.StateBootstrap{
		Sessions: contract.SessionListResult{Sessions: sessions, CleanupRequiredSessionIDs: cleanup},
		Network:  network,
	}
}

func authWatchTestSession() contract.SessionResult {
	socketUpdated := "2026-10-08T01:02:04Z"
	return contract.SessionResult{
		ProtocolSocket: contract.NetworkProtocolSocket{
			State: "open", RunGeneration: ^uint64(0), UpdatedAt: &socketUpdated,
			LocalEndpoint:  &contract.NetworkEndpoint{Address: "192.0.2.5", Port: 1234},
			RemoteEndpoint: &contract.NetworkEndpoint{Address: "198.51.100.7", Port: 53},
		},
		Revision:                 ^uint64(0),
		Intent:                   "maintain_authentication",
		ConfigurationID:          "campus-config",
		AuthenticationSessionID:  "session-watch",
		DisplayName:              "Lab",
		InstitutionProfileID:     "jlu",
		InstitutionDisplayName:   "吉林大学",
		AuthenticationProtocolID: "drcom-5.2.0-d",
		AccountName:              "alice",
		State:                    "authenticated",
		UpdatedAt:                "2026-10-08T01:02:03Z",
	}
}

func authWatchTestNetwork(available bool) contract.NetworkInterfacesResult {
	result := contract.NetworkInterfacesResult{Interfaces: []contract.NetworkInterfaceResult{}}
	if available {
		observed := "2026-10-08T01:02:03Z"
		result.Available = true
		result.Revision = ^uint64(0)
		result.ObservedAt = &observed
	}
	return result
}

func TestRunAuthWatchRendersFullBootstrapAndDoesNotCallLifecycleRPC(t *testing.T) {
	for _, test := range []struct {
		name      string
		bootstrap contract.StateBootstrap
		contains  []string
	}{
		{
			name:      "empty sessions and unavailable network",
			bootstrap: authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false)),
			contains:  []string{"没有 Session。", "尚无已接受的网络观察。"},
		},
		{
			name: "full session, cleanup, exact revision and network",
			bootstrap: authWatchTestBootstrap(
				[]contract.SessionResult{authWatchTestSession()},
				[]string{"session-watch"},
				authWatchTestNetwork(true),
			),
			contains: []string{
				"session-watch", "revision：18446744073709551615", "意图：maintain_authentication",
				"配置：campus-config", "协议 Socket：open", "协议 Socket runGeneration：18446744073709551615",
				"协议 Socket 更新时间：2026-10-08T01:02:04Z", "协议 Socket 本地端点：192.0.2.5:1234",
				"协议 Socket 远端端点：198.51.100.7:53", "需要清理：运行 sidraviactl auth remove",
				"观察 revision：18446744073709551615", "已观察到空网卡列表。",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			enteredNext := make(chan struct{})
			stream := &authWatchTestStream{next: func(ctx context.Context) (contract.StateEvent, error) {
				close(enteredNext)
				<-ctx.Done()
				return contract.StateEvent{}, context.Cause(ctx)
			}}
			connection := &authWatchTestConnection{
				subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
					return test.bootstrap, stream, nil
				},
			}
			var output bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- runAuthWatch(ctx, authWatchTestDependencies(connection, &output)) }()
			select {
			case <-enteredNext:
			case <-time.After(time.Second):
				t.Fatal("watch did not finish initial output and enter Next")
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("runAuthWatch = %v", err)
			}
			for _, text := range test.contains {
				if !strings.Contains(output.String(), text) {
					t.Errorf("bootstrap output %q missing %q", output.String(), text)
				}
			}
			if connection.calls != 0 {
				t.Errorf("watch issued %d ordinary RPC calls", connection.calls)
			}
			if stream.closed != 1 || connection.closed != 1 {
				t.Errorf("stream/connection close = %d/%d, want 1/1", stream.closed, connection.closed)
			}
		})
	}
}

func TestRunAuthWatchRendersEveryPublicEvent(t *testing.T) {
	network := authWatchTestNetwork(true)
	events := []contract.StateEvent{
		{Method: contract.EventMethodSessionChanged, SessionChanged: &contract.SessionChangedPayload{Session: authWatchTestSession(), CleanupRequired: true}},
		{Method: contract.EventMethodSessionRemoved, SessionRemoved: &contract.SessionRemovedPayload{SessionID: "removed\nmarker", Revision: ^uint64(0)}},
		{Method: contract.EventMethodNetworkChanged, NetworkChanged: &network},
	}
	stream := &authWatchTestStream{next: func(context.Context) (contract.StateEvent, error) {
		if len(events) == 0 {
			return contract.StateEvent{}, io.EOF
		}
		event := events[0]
		events = events[1:]
		return event, nil
	}}
	connection := &authWatchTestConnection{subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false)), stream, nil
	}}
	var output bytes.Buffer
	err := runAuthWatch(context.Background(), authWatchTestDependencies(connection, &output))
	if err == nil || !strings.Contains(err.Error(), "请重新运行 sidraviactl auth watch") {
		t.Fatalf("disconnect error = %v, want fixed rerun guidance", err)
	}
	for _, text := range []string{
		"意图：maintain_authentication", "需要清理：运行 sidraviactl auth remove",
		"Session 已移除：removed�marker；revision：18446744073709551615", "观察 revision：18446744073709551615",
	} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("event output %q missing %q", output.String(), text)
		}
	}
	if strings.Contains(output.String(), "removed\nmarker") {
		t.Fatal("event output included an unsanitized Session ID")
	}
	if stream.closed != 1 || connection.closed != 1 {
		t.Fatalf("stream/connection close = %d/%d, want 1/1", stream.closed, connection.closed)
	}
}

func TestRunAuthWatchCancelsACKContextWithoutCancelingStream(t *testing.T) {
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	ackCanceled := make(chan struct{})
	stream := &authWatchTestStream{next: func(ctx context.Context) (contract.StateEvent, error) {
		<-ackCanceled
		if ctx != watchCtx || ctx.Err() != nil {
			t.Errorf("Next context = %v, want long-lived watch context", ctx.Err())
		}
		cancelWatch()
		<-ctx.Done()
		return contract.StateEvent{}, context.Cause(ctx)
	}}
	connection := &authWatchTestConnection{}
	var output bytes.Buffer
	connection.subscribe = func(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("ACK context missing bounded deadline")
		}
		result := authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false))
		go func() {
			for ctx.Err() == nil {
				time.Sleep(time.Millisecond)
			}
			close(ackCanceled)
		}()
		return result, stream, nil
	}
	if err := runAuthWatch(watchCtx, authWatchTestDependencies(connection, &output)); err != nil {
		t.Fatalf("runAuthWatch = %v", err)
	}
	if stream.closed != 1 || connection.closed != 1 {
		t.Fatalf("stream/connection close = %d/%d, want 1/1", stream.closed, connection.closed)
	}
}

func TestRunAuthWatchAcquisitionAndACKFailuresKeepCausesSafe(t *testing.T) {
	t.Run("acquisition cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		cause := errors.New("private acquisition marker")
		deps := listDependencies{connection: daemonConnectionDependencies{
			callTimeout: time.Second,
			acquire: func(ctx context.Context) (daemonClient, error) {
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatalf("acquire context error = %v", ctx.Err())
				}
				return nil, errors.Join(cause, ctx.Err())
			},
		}}
		err := runAuthWatch(ctx, deps)
		if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
			t.Fatalf("acquisition error = %v, want retained causes", err)
		}
		if strings.Contains(err.Error(), cause.Error()) {
			t.Fatalf("acquisition error leaked private text: %v", err)
		}
	})

	t.Run("ACK cancellation", func(t *testing.T) {
		cause := errors.New("private ACK marker")
		connection := &authWatchTestConnection{subscribe: func(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
			return contract.StateBootstrap{}, nil, errors.Join(cause, context.Canceled)
		}}
		err := runAuthWatch(context.Background(), authWatchTestDependencies(connection, io.Discard))
		if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) {
			t.Fatalf("ACK error = %v, want retained causes", err)
		}
		if strings.Contains(err.Error(), cause.Error()) || connection.closed != 1 {
			t.Fatalf("ACK error/cleanup = %v/%d", err, connection.closed)
		}
	})
}

func TestRunAuthWatchMapsBusinessFailureWithoutPrivateMessage(t *testing.T) {
	for _, code := range []string{contract.ErrorCodeStateSnapshotTooLarge, contract.ErrorCodeStateSnapshotUnavailable} {
		t.Run(code, func(t *testing.T) {
			private := &client.RequestFailure{Code: code}
			connection := &authWatchTestConnection{subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				return contract.StateBootstrap{}, nil, fmt.Errorf("private response message marker: %w", private)
			}}
			err := runAuthWatch(context.Background(), authWatchTestDependencies(connection, io.Discard))
			if err == nil || err.Error() != ipcErrorText(code) {
				t.Fatalf("business failure = %v", err)
			}
			var got *client.RequestFailure
			if !errors.As(err, &got) || got.Code != code {
				t.Fatalf("business failure lost RequestFailure: %v", err)
			}
			if strings.Contains(err.Error(), "private response message marker") || connection.closed != 1 {
				t.Fatalf("business failure/cleanup = %v/%d", err, connection.closed)
			}
		})
	}
}

func TestRunAuthWatchUnsupportedCapabilityAndCleanupErrors(t *testing.T) {
	t.Run("optional source unsupported", func(t *testing.T) {
		connection := &authWatchTestPlainConnection{}
		err := runAuthWatch(context.Background(), authWatchTestDependencies(connection, io.Discard))
		if !errors.Is(err, errAuthWatchUnavailable) || connection.closed != 1 {
			t.Fatalf("unsupported source = %v, close=%d", err, connection.closed)
		}
	})

	t.Run("cancellation still reports both close errors", func(t *testing.T) {
		streamCause := errors.New("private stream close marker")
		connectionCause := errors.New("private connection close marker")
		stream := &authWatchTestStream{closeErr: streamCause, next: func(ctx context.Context) (contract.StateEvent, error) {
			<-ctx.Done()
			return contract.StateEvent{}, context.Cause(ctx)
		}}
		connection := &authWatchTestConnection{
			subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				return authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false)), stream, nil
			},
			closeErr: connectionCause,
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := runAuthWatch(ctx, authWatchTestDependencies(connection, io.Discard))
		if !errors.Is(err, streamCause) || !errors.Is(err, connectionCause) {
			t.Fatalf("cleanup errors = %v", err)
		}
		if strings.Contains(err.Error(), streamCause.Error()) || strings.Contains(err.Error(), connectionCause.Error()) {
			t.Fatalf("cleanup error leaked private text: %v", err)
		}
	})
}

type authWatchShortWriter struct{}

func (authWatchShortWriter) Write([]byte) (int, error) { return 0, nil }

func TestRunAuthWatchOutputFailurePreservesShortWriteAndCloses(t *testing.T) {
	stream := &authWatchTestStream{next: func(context.Context) (contract.StateEvent, error) {
		t.Fatal("watch continued after initial output failure")
		return contract.StateEvent{}, nil
	}}
	connection := &authWatchTestConnection{subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false)), stream, nil
	}}
	err := runAuthWatch(context.Background(), authWatchTestDependencies(connection, authWatchShortWriter{}))
	if !errors.Is(err, io.ErrShortWrite) || stream.closed != 1 || connection.closed != 1 {
		t.Fatalf("output failure = %v, stream/connection close=%d/%d", err, stream.closed, connection.closed)
	}

	writeCause := errors.New("private write marker")
	failingOutput := authWatchErrorWriter{err: writeCause}
	connection = &authWatchTestConnection{subscribe: func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return authWatchTestBootstrap(nil, nil, authWatchTestNetwork(false)), stream, nil
	}}
	stream.closed = 0
	err = runAuthWatch(context.Background(), authWatchTestDependencies(connection, failingOutput))
	if !errors.Is(err, writeCause) || stream.closed != 1 || connection.closed != 1 {
		t.Fatalf("writer error = %v, stream/connection close=%d/%d", err, stream.closed, connection.closed)
	}
	if strings.Contains(err.Error(), writeCause.Error()) {
		t.Fatalf("writer error leaked private text: %v", err)
	}
}

type authWatchErrorWriter struct{ err error }

func (writer authWatchErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestAuthWatchCommandHelpAndInvalidArgumentsNeverDispatch(t *testing.T) {
	called := false
	deps := commandDependencies{
		output: io.Discard,
		authWatch: func(context.Context) error {
			called = true
			return nil
		},
	}
	for _, args := range [][]string{{"auth", "watch", "secret-marker"}, {"auth", "watch", "--secret-marker"}} {
		err := runCommand(args, deps)
		if err == nil {
			t.Fatalf("%q unexpectedly succeeded", args)
		}
		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("%q leaked invalid argument: %v", args, err)
		}
		if called {
			t.Fatalf("%q dispatched watch", args)
		}
	}
	var watchHelp bytes.Buffer
	deps.output = &watchHelp
	if err := runCommand([]string{"help", "auth", "watch"}, deps); err != nil {
		t.Fatalf("watch help: %v", err)
	}
	if called {
		t.Fatal("watch help dispatched watch")
	}
	if !strings.Contains(watchHelp.String(), "Ctrl+C") || !strings.Contains(watchHelp.String(), "重新运行此命令") {
		t.Fatalf("watch help omitted stop or disconnect guidance: %q", watchHelp.String())
	}
	var output bytes.Buffer
	deps.output = &output
	if err := runCommand([]string{"help", "auth"}, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "watch  持续查看最新 Session 与网络状态") {
		t.Fatalf("auth help omitted watch command: %q", output.String())
	}
	if err := runCommand([]string{"auth", "watch"}, deps); err != nil {
		t.Fatalf("valid watch dispatch: %v", err)
	}
	if !called {
		t.Fatal("valid watch did not dispatch")
	}
}
