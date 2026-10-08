package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
	"sidravia/internal/ipc/server"
)

const token = "owned-test-token"
const build = "owned-test-build"

// This is a genuine replacement of the optional StateEventSource boundary.
// The client tests use the real server, not daemon domain mocks or private APIs.
type sourceFunc func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error)

func (f sourceFunc) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	return f(ctx)
}

type sourceStream struct {
	events chan contract.StateEvent
	closed chan struct{}
	once   sync.Once
}

func newSourceStream() *sourceStream {
	return &sourceStream{events: make(chan contract.StateEvent, 8), closed: make(chan struct{})}
}
func (s *sourceStream) Close() error { s.once.Do(func() { close(s.closed) }); return nil }
func (s *sourceStream) Next(ctx context.Context) (contract.StateEvent, error) {
	select {
	case e := <-s.events:
		return e, nil
	case <-s.closed:
		return contract.StateEvent{}, io.EOF
	case <-ctx.Done():
		return contract.StateEvent{}, context.Cause(ctx)
	}
}
func bootstrap() contract.StateBootstrap {
	return contract.StateBootstrap{Sessions: contract.SessionListResult{Sessions: []contract.SessionResult{}, CleanupRequiredSessionIDs: []string{}}, Network: contract.NetworkInterfacesResult{Interfaces: []contract.NetworkInterfaceResult{}}}
}
func session() contract.SessionResult {
	return contract.SessionResult{AuthenticationSessionID: "s", InstitutionProfileID: "i", InstitutionDisplayName: "Institution", AuthenticationProtocolID: "p", AccountName: "a", Intent: "maintain_authentication", State: "authenticated", Revision: 9007199254740993, UpdatedAt: "2026-10-07T01:02:03Z", ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}}
}
func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("owned operation did not finish")
	}
}
func realServer(t *testing.T, source contract.StateEventSource, handler server.Handler) string {
	t.Helper()
	if handler == nil {
		handler = func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
			return json.RawMessage(`{"answer":42}`), nil
		}
	}
	s, err := server.NewServer(token, build, handler, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, source)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return "ws" + strings.TrimPrefix(h.URL, "http") + "/ipc"
}
func connect(t *testing.T, endpoint string) *client.Client {
	t.Helper()
	c, err := client.Connect(bounded(t), endpoint, token, build)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func subscribe(t *testing.T, c *client.Client) (contract.StateBootstrap, contract.StateEventStream) {
	t.Helper()
	boot, s, err := c.SubscribeStateEvents(bounded(t))
	if err != nil {
		t.Fatal(err)
	}
	return boot, s
}
func closed(t *testing.T, c *client.Client) {
	t.Helper()
	if _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); !errors.Is(err, client.ErrClientClosed) {
		t.Fatalf("closed Call: %v", err)
	}
	if _, _, err := c.SubscribeStateEvents(bounded(t)); !errors.Is(err, client.ErrClientClosed) {
		t.Fatalf("closed Subscribe: %v", err)
	}
}

func TestOwnedBootstrapFullEventsAndFreshConnect(t *testing.T) {
	streams := make(chan *sourceStream, 2)
	var generation atomic.Uint64
	expected := bootstrap()
	expected.Sessions.Sessions = []contract.SessionResult{session()}
	expected.Sessions.CleanupRequiredSessionIDs = []string{"s"}
	source := sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		boot := expected
		boot.Sessions.Sessions = append([]contract.SessionResult(nil), expected.Sessions.Sessions...)
		boot.Sessions.Sessions[0].Revision += generation.Add(1) - 1
		s := newSourceStream()
		streams <- s
		return boot, s, nil
	})
	url := realServer(t, source, nil)
	c := connect(t, url)
	ackCtx, cancelACK := context.WithCancel(context.Background())
	boot, s, err := c.SubscribeStateEvents(ackCtx)
	cancelACK() // ACK lifetime ends here; the owned reader has its own operation contexts.
	if err != nil || !reflect.DeepEqual(boot, expected) {
		t.Fatalf("bootstrap=%#v err=%v", boot, err)
	}
	upstream := <-streams
	stamp := "2026-10-07T01:02:03Z"
	network := contract.NetworkInterfacesResult{Available: true, Revision: ^uint64(0), ObservedAt: &stamp, Interfaces: []contract.NetworkInterfaceResult{}}
	final := session()
	final.State = "suspended"
	final.Intent = "suspend_authentication"
	final.Revision = ^uint64(0)
	events := []contract.StateEvent{
		{Method: contract.EventMethodSessionChanged, SessionChanged: &contract.SessionChangedPayload{Session: session(), CleanupRequired: true}},
		{Method: contract.EventMethodNetworkChanged, NetworkChanged: &network},
		{Method: contract.EventMethodSessionChanged, SessionChanged: &contract.SessionChangedPayload{Session: final, CleanupRequired: false}},
		{Method: contract.EventMethodSessionRemoved, SessionRemoved: &contract.SessionRemovedPayload{SessionID: "s", Revision: ^uint64(0)}},
	}
	for _, want := range events {
		upstream.events <- want
		got, err := s.Next(bounded(t))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("event=%#v want=%#v err=%v", got, want, err)
		}
	}
	if _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); !errors.Is(err, client.ErrStateSubscriptionActive) {
		t.Fatal(err)
	}
	if _, _, err := c.SubscribeStateEvents(bounded(t)); !errors.Is(err, client.ErrStateSubscriptionActive) {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wait(t, upstream.closed)
	closed(t, c)
	if _, err := s.Next(bounded(t)); !errors.Is(err, client.ErrClientClosed) {
		t.Fatal(err)
	}
	c2 := connect(t, url)
	recovered, _ := subscribe(t, c2)
	if recovered.Sessions.Sessions[0].Revision != expected.Sessions.Sessions[0].Revision+1 {
		t.Fatal("fresh bootstrap was not acquired")
	}
}

func TestNetworkEventWithoutSessions(t *testing.T) {
	stream := newSourceStream()
	c := connect(t, realServer(t, sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return bootstrap(), stream, nil
	}), nil))
	boot, s := subscribe(t, c)
	stamp := "2026-10-07T01:02:03Z"
	network := contract.NetworkInterfacesResult{Available: true, Revision: 1, ObservedAt: &stamp, Interfaces: []contract.NetworkInterfaceResult{}}
	stream.events <- contract.StateEvent{Method: contract.EventMethodNetworkChanged, NetworkChanged: &network}
	e, err := s.Next(bounded(t))
	if err != nil || len(boot.Sessions.Sessions) != 0 || !reflect.DeepEqual(e.NetworkChanged, &network) {
		t.Fatalf("network=%#v err=%v", e, err)
	}
}

func TestBusinessACKCodesLeaveOrdinaryRPCReusable(t *testing.T) {
	for _, test := range []struct {
		name, code string
		source     contract.StateEventSource
	}{
		{"unsupported", contract.ErrorCodeUnknownMethod, nil},
		{"unavailable", contract.ErrorCodeStateSnapshotUnavailable, sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
			return contract.StateBootstrap{}, nil, errors.New("private-source-diagnostic")
		})},
		{"too-large", contract.ErrorCodeStateSnapshotTooLarge, sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
			return contract.StateBootstrap{}, nil, contract.ErrStateFrameLimit
		})},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := connect(t, realServer(t, test.source, nil))
			_, s, err := c.SubscribeStateEvents(bounded(t))
			var failure *client.RequestFailure
			if s != nil || !errors.As(err, &failure) || failure.Code != test.code || strings.Contains(err.Error(), "private") {
				t.Fatalf("failure=%v stream=%v", err, s)
			}
			r, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil)
			if err != nil || !r.OK || r.ID != "1" || string(r.Result) != `{"answer":42}` {
				t.Fatalf("RPC=%#v err=%v", r, err)
			}
		})
	}
}

func TestOrdinaryRPCResultErrorAndOutgoingFrameBound(t *testing.T) {
	var calls atomic.Int32
	c := connect(t, realServer(t, nil, func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		calls.Add(1)
		if method == "reject" {
			return nil, &contract.Error{Code: contract.ErrorCodeInvalidArgument, Message: "original business message"}
		}
		return json.RawMessage(`{"answer":42}`), nil
	}))
	r, err := c.Call(bounded(t), "reject", nil)
	if err != nil || r.OK || r.ID != "1" || r.Error.Code != contract.ErrorCodeInvalidArgument || r.Error.Message != "original business message" {
		t.Fatalf("response=%#v err=%v", r, err)
	}
	req := contract.Request{Kind: "request", ID: "1", Method: contract.MethodDaemonStatus, Payload: json.RawMessage(`{"value":""}`)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"value":"` + strings.Repeat("x", contract.StateFrameLimit-len(data)) + `"}`)
	r, err = c.Call(bounded(t), contract.MethodDaemonStatus, payload)
	if err != nil || !r.OK {
		t.Fatalf("exact-bound call: %v", err)
	}
	payload = json.RawMessage(`{"value":"` + strings.Repeat("x", contract.StateFrameLimit-len(data)+1) + `"}`)
	if _, err = c.Call(bounded(t), contract.MethodDaemonStatus, payload); !errors.Is(err, contract.ErrStateFrameLimit) {
		t.Fatalf("oversized outgoing frame: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatal("oversized request reached handler")
	}
	if _, err = c.Call(bounded(t), contract.MethodDaemonStatus, json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed outgoing JSON accepted")
	}
	r, err = c.Call(bounded(t), contract.MethodDaemonStatus, nil)
	if err != nil || !r.OK {
		t.Fatal("local encoding failure invalidated connection", err)
	}
}

// Raw peers exercise wire failures that the real production server refuses to emit.
func rawServer(t *testing.T, script func(context.Context, *websocket.Conn, contract.Request)) string {
	t.Helper()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		req, err := contract.DecodeRequest(data)
		if err != nil {
			t.Error(err)
			return
		}
		script(ctx, conn, req)
	}))
	t.Cleanup(h.Close)
	return "ws" + strings.TrimPrefix(h.URL, "http") + "/ipc"
}
func write(t *testing.T, ctx context.Context, c *websocket.Conn, data []byte) {
	t.Helper()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Error(err)
	}
}
func ack(t *testing.T, id string) []byte {
	t.Helper()
	boot, err := contract.MarshalStateBootstrap(bootstrap())
	if err != nil {
		t.Fatal(err)
	}
	data, err := contract.EncodeResponse(contract.NewSuccessResponse(id, boot))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func peerWait(ctx context.Context, c *websocket.Conn) { _, _, _ = c.Read(ctx) }

func TestMalformedACKDiscontinues(t *testing.T) {
	good := string(ack(t, "1"))
	for _, test := range []struct{ name, data string }{
		{"wrong-id", string(ack(t, "other"))},
		{"null-bootstrap", `{"kind":"response","id":"1","ok":true,"result":null}`},
		{"unexpected-event", `{"kind":"event","method":"session.removed","payload":{"sessionId":"s","revision":1}}`},
		{"unknown-bootstrap-field", `{"kind":"response","id":"1","ok":true,"result":{"sessions":{"sessions":[],"cleanupRequiredSessionIds":[],"extra":true},"network":{"available":false,"revision":0,"interfaces":[]}}}`},
		{"missing-ok", `{"kind":"response","id":"1","error":{"code":"unknown_method","message":"private"}}`},
		{"null-ok", `{"kind":"response","id":"1","ok":null,"error":{"code":"unknown_method","message":"private"}}`},
		{"empty-code", `{"kind":"response","id":"1","ok":false,"error":{"code":"","message":"private"}}`},
		{"mixed-result-error", `{"kind":"response","id":"1","ok":false,"result":{},"error":{"code":"unknown_method","message":"private"}}`},
		{"duplicate-key", strings.Replace(good, `"ok":true`, `"ok":true,"ok":true`, 1)},
		{"truncated", good[:len(good)-1]},
		{"oversize", strings.Repeat(" ", contract.StateFrameLimit+1) + good},
	} {
		if test.data == good {
			t.Fatalf("malformed ACK fixture %s equals valid ACK", test.name)
		}
		t.Run(test.name, func(t *testing.T) {
			c := connect(t, rawServer(t, func(ctx context.Context, conn *websocket.Conn, _ contract.Request) {
				write(t, ctx, conn, []byte(test.data))
				peerWait(ctx, conn)
			}))
			_, s, err := c.SubscribeStateEvents(bounded(t))
			if err == nil || s != nil {
				t.Fatalf("invalid ACK accepted: %v", err)
			}
			closed(t, c)
		})
	}
}

func TestMalformedEventDiscontinues(t *testing.T) {
	for _, test := range []struct{ name, data string }{
		{"response", string(ack(t, "1"))},
		{"unknown", `{"kind":"event","method":"unknown","payload":{}}`},
		{"null", `{"kind":"event","method":"session.removed","payload":null}`},
		{"id", `{"kind":"event","id":"1","method":"session.removed","payload":{"sessionId":"s","revision":1}}`},
		{"missing-revision", `{"kind":"event","method":"session.removed","payload":{"sessionId":"s"}}`},
		{"oversize", strings.Repeat(" ", contract.StateFrameLimit+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := connect(t, rawServer(t, func(ctx context.Context, conn *websocket.Conn, req contract.Request) {
				write(t, ctx, conn, ack(t, req.ID))
				write(t, ctx, conn, []byte(test.data))
				peerWait(ctx, conn)
			}))
			_, s := subscribe(t, c)
			if _, err := s.Next(bounded(t)); err == nil {
				t.Fatal("invalid event accepted")
			}
			closed(t, c)
			if _, err := s.Next(bounded(t)); !errors.Is(err, client.ErrClientClosed) {
				t.Fatal(err)
			}
		})
	}
}

func TestBusinessFailureSafeMessage(t *testing.T) {
	c := connect(t, rawServer(t, func(ctx context.Context, conn *websocket.Conn, req contract.Request) {
		data, err := contract.EncodeResponse(contract.NewErrorResponse(req.ID, "fixed_code", "private-server-message"))
		if err != nil {
			t.Error(err)
			return
		}
		write(t, ctx, conn, data)
		_, data, err = conn.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		next, err := contract.DecodeRequest(data)
		if err != nil {
			t.Error(err)
			return
		}
		data, err = contract.EncodeResponse(contract.NewSuccessResponse(next.ID, json.RawMessage(`{}`)))
		if err != nil {
			t.Error(err)
			return
		}
		write(t, ctx, conn, data)
		peerWait(ctx, conn)
	}))
	_, _, err := c.SubscribeStateEvents(bounded(t))
	var failure *client.RequestFailure
	if !errors.As(err, &failure) || failure.Code != "fixed_code" || strings.Contains(err.Error(), "private-server-message") {
		t.Fatal(err)
	}
	if r, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); err != nil || !r.OK {
		t.Fatal(err)
	}
}

// Done signals that Read has begun, after the public admission gate; this keeps
// overlap/Close tests deterministic without reaching into client implementation.
type observedContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}
func observe(ctx context.Context) *observedContext {
	return &observedContext{Context: ctx, entered: make(chan struct{})}
}

func TestNextOverlapCancellationCauseAndFreshBootstrap(t *testing.T) {
	streams := make(chan *sourceStream, 2)
	var subscriptions atomic.Int32
	url := realServer(t, sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		subscriptions.Add(1)
		s := newSourceStream()
		streams <- s
		return bootstrap(), s, nil
	}), nil)
	c := connect(t, url)
	_, s := subscribe(t, c)
	upstream := <-streams
	base, cancel := context.WithCancelCause(bounded(t))
	ctx := observe(base)
	done := make(chan error, 1)
	go func() { _, err := s.Next(ctx); done <- err }()
	wait(t, ctx.entered)
	if _, err := s.Next(bounded(t)); !errors.Is(err, client.ErrClientBusy) {
		t.Fatalf("overlapping Next: %v", err)
	}
	if _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); !errors.Is(err, client.ErrStateSubscriptionActive) {
		t.Fatal(err)
	}
	cause := errors.New("private custom cancellation")
	cancel(cause)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
			t.Fatalf("cause lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled Next stuck")
	}
	closed(t, c)
	wait(t, upstream.closed)
	c2 := connect(t, url)
	subscribe(t, c2)
	if subscriptions.Load() != 2 {
		t.Fatal("did not acquire fresh bootstrap")
	}
}

func TestConcurrentCloseInterruptsBlockedNext(t *testing.T) {
	stream := newSourceStream()
	c := connect(t, realServer(t, sourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return bootstrap(), stream, nil
	}), nil))
	_, s := subscribe(t, c)
	ctx := observe(bounded(t))
	done := make(chan error, 1)
	go func() { _, err := s.Next(ctx); done <- err }()
	wait(t, ctx.entered)
	var closers sync.WaitGroup
	for i := 0; i < 8; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	closeDone := make(chan struct{})
	go func() { closers.Wait(); close(closeDone) }()
	wait(t, closeDone)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked Next survived Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close failed to await Next")
	}
	closed(t, c)
	wait(t, stream.closed)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCallOverlapAndCloseInterrupt(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	url := realServer(t, nil, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		close(entered)
		<-release
		return json.RawMessage(`{}`), nil
	})
	c := connect(t, url)
	done := make(chan error, 1)
	go func() { _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); done <- err }()
	wait(t, entered)
	if _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); !errors.Is(err, client.ErrClientBusy) {
		t.Fatal(err)
	}
	if _, _, err := c.SubscribeStateEvents(bounded(t)); !errors.Is(err, client.ErrClientBusy) {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked Call survived Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close/Call deadlock")
	}
	closed(t, c)
}

func TestSubscribeCloseAndCancellationInterruptACK(t *testing.T) {
	for _, mode := range []string{"close", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			c := connect(t, rawServer(t, func(ctx context.Context, conn *websocket.Conn, _ contract.Request) {
				close(entered)
				peerWait(ctx, conn)
			}))
			base, cancel := context.WithCancelCause(bounded(t))
			defer cancel(nil)
			done := make(chan error, 1)
			go func() { _, _, err := c.SubscribeStateEvents(base); done <- err }()
			wait(t, entered)
			if _, _, err := c.SubscribeStateEvents(bounded(t)); !errors.Is(err, client.ErrClientBusy) {
				t.Fatal(err)
			}
			cause := errors.New("ACK private cancellation")
			if mode == "close" {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel(cause)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("blocked ACK survived interruption")
				}
				if mode == "cancel" && (!errors.Is(err, context.Canceled) || !errors.Is(err, cause)) {
					t.Fatal("ACK cause lost", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("ACK deadlock")
			}
			closed(t, c)
		})
	}
}

func TestPeerDisconnectAndOrdinaryProtocolFailure(t *testing.T) {
	for _, mode := range []string{"watch-peer-close", "ordinary-wrong-id", "ordinary-oversize", "ordinary-malformed"} {
		t.Run(mode, func(t *testing.T) {
			c := connect(t, rawServer(t, func(ctx context.Context, conn *websocket.Conn, req contract.Request) {
				switch mode {
				case "watch-peer-close":
					write(t, ctx, conn, ack(t, req.ID))
					_ = conn.CloseNow()
				case "ordinary-wrong-id":
					write(t, ctx, conn, ack(t, "other"))
					peerWait(ctx, conn)
				case "ordinary-oversize":
					write(t, ctx, conn, []byte(strings.Repeat(" ", contract.StateFrameLimit+1)))
					peerWait(ctx, conn)
				case "ordinary-malformed":
					write(t, ctx, conn, []byte(`{`))
					peerWait(ctx, conn)
				}
			}))
			if mode == "watch-peer-close" {
				_, s := subscribe(t, c)
				if _, err := s.Next(bounded(t)); err == nil {
					t.Fatal("peer disconnect invisible")
				}
			} else {
				if _, err := c.Call(bounded(t), contract.MethodDaemonStatus, nil); err == nil {
					t.Fatal("invalid RPC accepted")
				}
			}
			closed(t, c)
		})
	}
}
