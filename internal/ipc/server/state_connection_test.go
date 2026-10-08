package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

type stateSourceFunc func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error)

func (f stateSourceFunc) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	return f(ctx)
}

type streamItem struct {
	event contract.StateEvent
	err   error
}
type testStateStream struct {
	items              chan streamItem
	closed             chan struct{}
	entered            chan struct{}
	once               sync.Once
	releaseAfterCancel <-chan struct{}
	canceled           chan struct{}
}

func newStateStream() *testStateStream {
	return &testStateStream{items: make(chan streamItem, 8), closed: make(chan struct{}), entered: make(chan struct{}, 8), canceled: make(chan struct{})}
}
func (s *testStateStream) Close() error { s.once.Do(func() { close(s.closed) }); return nil }
func (s *testStateStream) Next(ctx context.Context) (contract.StateEvent, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case item := <-s.items:
		return item.event, item.err
	case <-ctx.Done():
		if s.releaseAfterCancel != nil {
			close(s.canceled)
			<-s.releaseAfterCancel
		}
		return contract.StateEvent{}, context.Cause(ctx)
	case <-s.closed:
		if s.releaseAfterCancel != nil {
			close(s.canceled)
			<-s.releaseAfterCancel
		}
		return contract.StateEvent{}, errors.New("owned stream closed")
	}
}
func stateBootstrap() contract.StateBootstrap {
	return contract.StateBootstrap{Sessions: contract.SessionListResult{Sessions: []contract.SessionResult{}, CleanupRequiredSessionIDs: []string{}}, Network: contract.NetworkInterfacesResult{Interfaces: []contract.NetworkInterfaceResult{}}}
}
func stateSession() contract.SessionResult {
	return contract.SessionResult{AuthenticationSessionID: "s", InstitutionProfileID: "i", InstitutionDisplayName: "Institution", AuthenticationProtocolID: "p", AccountName: "a", Intent: "maintain_authentication", State: "authenticated", Revision: 9007199254740993, UpdatedAt: "2026-10-07T01:02:03Z", ProtocolSocket: contract.NetworkProtocolSocket{State: "not_observed"}}
}
func removedEvent(id string, revision uint64) contract.StateEvent {
	return contract.StateEvent{Method: contract.EventMethodSessionRemoved, SessionRemoved: &contract.SessionRemovedPayload{SessionID: id, Revision: revision}}
}
func stateTestServer(t *testing.T, source contract.StateEventSource, handler Handler, hook func(string)) (*Server, string, *safeBuffer) {
	t.Helper()
	buf := new(safeBuffer)
	if handler == nil {
		handler = func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
			return json.RawMessage(`{}`), nil
		}
	}
	s, err := NewServer(testToken, testBuild, handler, newTestLogger(buf), hook, source)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return s, "ws" + strings.TrimPrefix(h.URL, "http") + "/ipc", buf
}
func readStateFrame(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func stateResponse(t *testing.T, c *websocket.Conn) contract.Response {
	t.Helper()
	resp, err := contract.DecodeResponse(readStateFrame(t, c))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func assertStateError(t *testing.T, c *websocket.Conn, id, code string) {
	t.Helper()
	r := stateResponse(t, c)
	if r.ID != id || r.OK || r.Error == nil || r.Error.Code != code {
		t.Fatalf("response=%#v", r)
	}
}
func assertConnectionClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection did not close: %v", err)
	}
}

func TestStateBootstrapACKBeforeFullEventsAndRPC(t *testing.T) {
	stream := newStateStream()
	boot := stateBootstrap()
	boot.Sessions.Sessions = []contract.SessionResult{stateSession()}
	boot.Sessions.CleanupRequiredSessionIDs = []string{"s"}
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return boot, stream, nil
	})
	_, url, _ := stateTestServer(t, source, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	networkTime := "2026-10-07T01:02:03Z"
	network := contract.NetworkInterfacesResult{Available: true, Revision: 9007199254740993, ObservedAt: &networkTime, Interfaces: []contract.NetworkInterfaceResult{}}
	values := []contract.StateEvent{{Method: contract.EventMethodSessionChanged, SessionChanged: &contract.SessionChangedPayload{Session: stateSession(), CleanupRequired: true}}, {Method: contract.EventMethodNetworkChanged, NetworkChanged: &network}, removedEvent("s", ^uint64(0))}
	for _, value := range values {
		stream.items <- streamItem{event: value}
	}
	writeRequest(t, c, "bootstrap", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	ack := stateResponse(t, c)
	got, err := contract.DecodeStateBootstrap(ack.Result)
	if err != nil || !ack.OK || ack.ID != "bootstrap" || len(got.Sessions.Sessions) != 1 || got.Sessions.Sessions[0].Revision != 9007199254740993 || got.Sessions.Sessions[0].ProtocolSocket.State != "not_observed" || len(got.Sessions.CleanupRequiredSessionIDs) != 1 {
		t.Fatal("initial complete ACK lost", err)
	}
	for _, want := range values {
		raw := readStateFrame(t, c)
		event, err := contract.DecodeStateEvent(raw)
		if err != nil || event.Method != want.Method {
			t.Fatal("typed event lost", err)
		}
		if strings.Contains(string(raw), `"id":`) {
			t.Fatal("event has response id")
		}
		if event.SessionRemoved != nil && event.SessionRemoved.Revision != ^uint64(0) {
			t.Fatal("revision lost")
		}
	}
	writeRequest(t, c, "rpc-between-events", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	r := stateResponse(t, c)
	if r.ID != "rpc-between-events" || !r.OK {
		t.Fatal(r)
	}
	stream.items <- streamItem{event: removedEvent("other", 2)}
	if event, err := contract.DecodeStateEvent(readStateFrame(t, c)); err != nil || event.SessionRemoved.SessionID != "other" {
		t.Fatal(err)
	}
}

func TestStateStrictRequestsDuplicateAndNilSource(t *testing.T) {
	stream := newStateStream()
	var count atomic.Int32
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		count.Add(1)
		return stateBootstrap(), stream, nil
	})
	_, url, buf := stateTestServer(t, source, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	for i, payload := range []string{`null`, `[]`, `{"extra":true}`, `{"Status":1}`, `{"extra":1,"\u0065xtra":2}`, `{} {}`} {
		// Envelope JSON must remain valid to reach the strict payload grammar.
		if !json.Valid([]byte(payload)) {
			continue
		}
		id := fmt.Sprint(i)
		writeRequest(t, c, id, contract.MethodStateSubscribe, json.RawMessage(payload))
		code := contract.ErrorCodeInvalidArgument
		wantID := id
		if payload == `null` || payload == `{"extra":1,"\u0065xtra":2}` {
			code = contract.ErrorCodeMalformed
			wantID = ""
		}
		assertStateError(t, c, wantID, code)
	}
	if count.Load() != 0 {
		t.Fatal("invalid payload reached source")
	}
	writeRequest(t, c, "first", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	if !stateResponse(t, c).OK {
		t.Fatal("subscribe failed")
	}
	writeRequest(t, c, "duplicate", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	assertStateError(t, c, "duplicate", contract.ErrorCodeInvalidArgument)
	if count.Load() != 1 {
		t.Fatal("duplicate replaced source")
	}
	writeRequest(t, c, "bad-unsubscribe", contract.MethodStateUnsubscribe, json.RawMessage(`{"password":"private-secret"}`))
	assertStateError(t, c, "bad-unsubscribe", contract.ErrorCodeInvalidArgument)
	waitForLogEvent(t, buf, "method="+contract.MethodStateUnsubscribe)
	if strings.Contains(buf.String(), "private-secret") || strings.Contains(buf.String(), "bad-unsubscribe") {
		t.Fatal("state logs leak")
	}
	_, nilURL, _ := stateTestServer(t, nil, nil, nil)
	nilConn := dial(t, nilURL)
	defer nilConn.CloseNow()
	for _, m := range []string{contract.MethodStateSubscribe, contract.MethodStateUnsubscribe} {
		writeRequest(t, nilConn, m, m, json.RawMessage(`{}`))
		assertStateError(t, nilConn, m, contract.ErrorCodeUnknownMethod)
	}
}

func TestStateUnsubscribePendingBarrierIdempotenceAndFreshBootstrap(t *testing.T) {
	streams := []*testStateStream{newStateStream(), newStateStream()}
	var calls atomic.Int32
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		i := int(calls.Add(1)) - 1
		boot := stateBootstrap()
		if i == 1 {
			boot.Sessions.Sessions = []contract.SessionResult{stateSession()}
		}
		return boot, streams[i], nil
	})
	rpcEntered := make(chan struct{})
	rpcRelease := make(chan struct{})
	handler := func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *contract.Error) {
		if method == contract.MethodProfileList {
			close(rpcEntered)
			<-rpcRelease
		}
		return json.RawMessage(`{}`), nil
	}
	_, url, _ := stateTestServer(t, source, handler, nil)
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	stateResponse(t, c)
	writeRequest(t, c, "block", contract.MethodProfileList, json.RawMessage(`{}`))
	waitForChannel(t, rpcEntered, "blocking RPC")
	streams[0].items <- streamItem{event: removedEvent("old-one", 1)}
	streams[0].items <- streamItem{event: removedEvent("old-two", 2)}
	// The producer has entered two Next calls, leaving pending old delivery while
	// the sole writer is held in an ordinary RPC.
	waitForChannel(t, streams[0].entered, "first Next")
	waitForChannel(t, streams[0].entered, "second Next")
	writeRequest(t, c, "unsub", contract.MethodStateUnsubscribe, json.RawMessage(`{}`))
	close(rpcRelease)
	seenBlock, seenUnsub := false, false
	for i := 0; i < 4 && !seenUnsub; i++ {
		raw := readStateFrame(t, c)
		r, err := contract.DecodeResponse(raw)
		if err != nil {
			if _, err := contract.DecodeStateEvent(raw); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if r.ID == "block" {
			seenBlock = true
		}
		if r.ID == "unsub" {
			got, err := contract.DecodeStateUnsubscribeResult(r.Result)
			if err != nil || !r.OK || got.Status != "unsubscribed" {
				t.Fatal(err)
			}
			seenUnsub = true
		}
	}
	if !seenBlock || !seenUnsub {
		t.Fatal("ACK sequence missing")
	}
	waitForChannel(t, streams[0].closed, "old stream close")
	writeRequest(t, c, "after-barrier", contract.MethodDaemonStatus, json.RawMessage(`{}`))
	if r := stateResponse(t, c); r.ID != "after-barrier" {
		t.Fatal("old frame after unsubscribe ACK")
	}
	writeRequest(t, c, "again", contract.MethodStateUnsubscribe, json.RawMessage(`{}`))
	if !stateResponse(t, c).OK {
		t.Fatal("unsubscribe not idempotent")
	}
	writeRequest(t, c, "resubscribe", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	r := stateResponse(t, c)
	boot, err := contract.DecodeStateBootstrap(r.Result)
	if err != nil || len(boot.Sessions.Sessions) != 1 || calls.Load() != 2 {
		t.Fatal("fresh bootstrap missing", err)
	}
	streams[1].items <- streamItem{event: removedEvent("new", 3)}
	ev, err := contract.DecodeStateEvent(readStateFrame(t, c))
	if err != nil || ev.SessionRemoved.SessionID != "new" {
		t.Fatal(err)
	}
}

func TestStatePeerCloseAndShutdownDrainOwnedProducer(t *testing.T) {
	for _, mode := range []string{"peer", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			stream := newStateStream()
			stream.releaseAfterCancel = release
			source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				return stateBootstrap(), stream, nil
			})
			s, url, _ := stateTestServer(t, source, nil, nil)
			c := dial(t, url)
			defer c.CloseNow()
			writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
			stateResponse(t, c)
			waitForChannel(t, stream.entered, "Next entered")
			done := make(chan error, 1)
			if mode == "peer" {
				_ = c.CloseNow()
				go func() { done <- s.Shutdown(context.Background()) }()
			} else {
				go func() { done <- s.Shutdown(context.Background()) }()
			}
			waitForChannel(t, stream.canceled, "producer cancellation")
			select {
			case err := <-done:
				t.Fatalf("shutdown returned without producer drain: %v", err)
			default:
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown did not drain")
			}
			waitForChannel(t, stream.closed, "owned close")
			assertConnectionClosed(t, c)
		})
	}
}

func TestStateSourceFailureClassificationAndSafeLogs(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{fmt.Errorf("private-capacity: %w", contract.ErrStateResourceCapacity), contract.ErrorCodeStateSnapshotTooLarge}, {fmt.Errorf("private-frame: %w", contract.ErrStateFrameLimit), contract.ErrorCodeStateSnapshotTooLarge}, {errors.New("private-password-token"), contract.ErrorCodeStateSnapshotUnavailable}} {
		t.Run(tc.code+tc.err.Error()[:7], func(t *testing.T) {
			stream := newStateStream()
			source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				return contract.StateBootstrap{}, stream, tc.err
			})
			_, url, buf := stateTestServer(t, source, nil, nil)
			c := dial(t, url)
			defer c.CloseNow()
			writeRequest(t, c, "private-id", contract.MethodStateSubscribe, json.RawMessage(`{}`))
			r := stateResponse(t, c)
			if r.OK || r.Error.Code != tc.code || strings.Contains(r.Error.Message, "private") {
				t.Fatal(r)
			}
			waitForChannel(t, stream.closed, "prepared close")
			waitForLogEvent(t, buf, "error_code="+tc.code)
			if strings.Contains(buf.String(), "private") || strings.Contains(buf.String(), testToken) {
				t.Fatal("private cause leaked")
			}
			writeRequest(t, c, "still-open", contract.MethodDaemonStatus, json.RawMessage(`{}`))
			if !stateResponse(t, c).OK {
				t.Fatal("failed subscribe ended RPC")
			}
		})
	}
}

func TestStateDiscontinuityClosesWithoutPartialEvents(t *testing.T) {
	for _, mode := range []string{"overflow", "invalid", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			stream := newStateStream()
			var sourceCtx context.Context
			source := stateSourceFunc(func(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				sourceCtx = ctx
				return stateBootstrap(), stream, nil
			})
			_, url, buf := stateTestServer(t, source, nil, nil)
			c := dial(t, url)
			defer c.CloseNow()
			writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
			stateResponse(t, c)
			private := errors.New("private-source-overflow")
			switch mode {
			case "overflow":
				stream.items <- streamItem{err: private}
			case "invalid":
				stream.items <- streamItem{event: contract.StateEvent{}}
			case "oversize":
				stream.items <- streamItem{event: removedEvent(strings.Repeat("x", contract.StateFrameLimit), 1)}
			}
			assertConnectionClosed(t, c)
			waitForChannel(t, stream.closed, "source cleanup")
			if mode == "overflow" && !errors.Is(context.Cause(sourceCtx), private) {
				t.Fatal("source failure identity lost")
			}
			if strings.Contains(buf.String(), "private-source-overflow") {
				t.Fatal("private source failure logged")
			}
		})
	}
}

func TestStateBootstrapEnvelopeLimitIncludesRequestID(t *testing.T) {
	for _, excess := range []int{0, 1} {
		t.Run(fmt.Sprint(excess), func(t *testing.T) {
			boot := stateBootstrap()
			boot.Sessions.Sessions = []contract.SessionResult{stateSession()}
			// Keep the request itself below its 64KiB read limit; only the combined
			// full bootstrap ACK crosses the boundary.
			id := strings.Repeat("i", 2000)
			boot.Sessions.Sessions[0].DisplayName = "d"
			body, err := contract.MarshalStateBootstrap(boot)
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := contract.EncodeResponse(contract.NewSuccessResponse(id, body))
			if err != nil {
				t.Fatal(err)
			}
			boot.Sessions.Sessions[0].DisplayName = strings.Repeat("d", contract.StateFrameLimit-len(baseline)+1+excess)
			stream := newStateStream()
			source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
				return boot, stream, nil
			})
			_, url, _ := stateTestServer(t, source, nil, nil)
			c := dial(t, url)
			c.SetReadLimit(contract.StateFrameLimit)
			defer c.CloseNow()
			writeRequest(t, c, id, contract.MethodStateSubscribe, json.RawMessage(`{}`))
			raw := readStateFrame(t, c)
			r, err := contract.DecodeResponse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if excess == 0 {
				if !r.OK || len(raw) != contract.StateFrameLimit {
					t.Fatalf("exact envelope len=%d ok=%v", len(raw), r.OK)
				}
			} else {
				if r.OK || r.Error.Code != contract.ErrorCodeStateSnapshotTooLarge || r.ID != id {
					t.Fatal("ID envelope not bounded", r)
				}
				waitForChannel(t, stream.closed, "oversized prepared close")
			}
		})
	}
}

func TestStateFinalResponseLimitAndFallbackCommittedGuards(t *testing.T) {
	for _, mode := range []string{"exact", "oversize", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var hooks atomic.Int32
			var requests atomic.Int32
			id := "same-id"
			base, _ := contract.EncodeResponse(contract.NewSuccessResponse(id, json.RawMessage(`""`)))
			handler := func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
				if requests.Add(1) > 1 {
					return json.RawMessage(`{}`), nil
				}
				if mode == "invalid" {
					return json.RawMessage(`{"private-invalid"`), nil
				}
				n := contract.StateFrameLimit - len(base)
				if mode == "oversize" {
					n++
				}
				return json.RawMessage(`"` + strings.Repeat("x", n) + `"`), nil
			}
			_, url, _ := stateTestServer(t, nil, handler, func(string) { hooks.Add(1) })
			c := dial(t, url)
			c.SetReadLimit(contract.StateFrameLimit)
			defer c.CloseNow()
			writeRequest(t, c, id, contract.MethodDaemonStatus, json.RawMessage(`{}`))
			raw := readStateFrame(t, c)
			r, err := contract.DecodeResponse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if r.ID != id {
				t.Fatal("fallback changed ID")
			}
			if mode == "exact" {
				if !r.OK || len(raw) != contract.StateFrameLimit {
					t.Fatal("exact response rejected")
				}
			} else {
				if r.OK || r.Error.Code != contract.ErrorCodeInternalError {
					t.Fatal("fixed fallback lost")
				}
			}
			writeRequest(t, c, "next", contract.MethodDaemonStatus, json.RawMessage(`{}`))
			if !stateResponse(t, c).OK {
				t.Fatal("fallback broke subsequent RPC")
			}
			// Shutdown waits for the serial handler and its committed callback to finish.
			_ = c.CloseNow()
			want := int32(1)
			if mode == "exact" {
				want = 2
			}
			deadline := time.Now().Add(time.Second)
			for hooks.Load() < want && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if hooks.Load() != want {
				t.Fatalf("committed=%d want=%d", hooks.Load(), want)
			}
		})
	}
}

// A real upgraded websocket over a controlled net.Conn makes the server's
// transport write block until CloseNow. No production timeout is shortened.
type blockedWriteConn struct {
	net.Conn
	block   atomic.Bool
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *blockedWriteConn) Write(p []byte) (int, error) {
	if c.block.Load() {
		select {
		case c.entered <- struct{}{}:
		default:
		}
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}
func (c *blockedWriteConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type blockedWriteListener struct {
	net.Listener
	accepted chan *blockedWriteConn
}

func (l *blockedWriteListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &blockedWriteConn{Conn: c, entered: make(chan struct{}, 1), closed: make(chan struct{})}
	l.accepted <- wrapped
	return wrapped, nil
}
func TestStateSlowWriteTimeoutClosesAndDrainsWithoutCommit(t *testing.T) {
	stream := newStateStream()
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return stateBootstrap(), stream, nil
	})
	var buf safeBuffer
	var hooks atomic.Int32
	server, err := NewServer(testToken, testBuild, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		return json.RawMessage(`{}`), nil
	}, newTestLogger(&buf), func(string) { hooks.Add(1) }, source)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewUnstartedServer(server)
	listener := &blockedWriteListener{Listener: h.Listener, accepted: make(chan *blockedWriteConn, 1)}
	h.Listener = listener
	h.Start()
	t.Cleanup(h.Close)
	c := dial(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/ipc")
	defer c.CloseNow()
	transport := <-listener.accepted
	writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	stateResponse(t, c)
	waitForChannel(t, stream.entered, "producer ready")
	transport.block.Store(true)
	stream.items <- streamItem{event: removedEvent("s", 1)}
	waitForChannel(t, transport.entered, "blocked write")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	select {
	case <-transport.closed:
	case <-ctx.Done():
		t.Fatal("fixed write timeout did not close transport")
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	waitForChannel(t, stream.closed, "slow writer source drain")
	if hooks.Load() != 1 {
		t.Fatal("event/failed write committed RPC")
	}
	waitForLogEvent(t, &buf, "stage=write")
	assertConnectionClosed(t, c)
}

func TestStateNoSessionNetworkAndFailedWriteCommitGuard(t *testing.T) {
	stream := newStateStream()
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return stateBootstrap(), stream, nil
	})
	_, url, _ := stateTestServer(t, source, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "empty", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	stateResponse(t, c)
	stamp := "2026-10-07T01:02:03Z"
	network := contract.NetworkInterfacesResult{Available: true, Revision: 1, ObservedAt: &stamp, Interfaces: []contract.NetworkInterfaceResult{}}
	stream.items <- streamItem{event: contract.StateEvent{Method: contract.EventMethodNetworkChanged, NetworkChanged: &network}}
	ev, err := contract.DecodeStateEvent(readStateFrame(t, c))
	if err != nil || ev.NetworkChanged == nil || ev.NetworkChanged.Revision != 1 {
		t.Fatal("no-session network lost", err)
	}
	var buf safeBuffer
	var committed atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	rpcServer, err := NewServer(testToken, testBuild, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		close(entered)
		<-release
		return json.RawMessage(`{}`), nil
	}, newTestLogger(&buf), func(string) { committed.Add(1) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewUnstartedServer(rpcServer)
	accepted := make(chan net.Conn, 1)
	h.Listener = &trackingListener{Listener: h.Listener, accepted: accepted}
	h.Start()
	t.Cleanup(h.Close)
	rpc := dial(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/ipc")
	defer rpc.CloseNow()
	transport := <-accepted
	writeRequest(t, rpc, "stop", contract.MethodDaemonStop, json.RawMessage(`{}`))
	waitForChannel(t, entered, "stop handler")
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForLogEvent(t, &buf, "stage=write")
	if committed.Load() != 0 {
		t.Fatal("failed stop write committed lifecycle")
	}
}

func TestStateReadLimitClosesOversizeRequest(t *testing.T) {
	_, url, _ := stateTestServer(t, nil, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "huge", contract.MethodDaemonStatus, json.RawMessage(`{"extra":"`+strings.Repeat("x", contract.StateFrameLimit)+`"}`))
	assertConnectionClosed(t, c)
}

var _ http.Handler = (*Server)(nil)

func TestStateOversizeSameIDFallbackClosesPreparedStream(t *testing.T) {
	boot := stateBootstrap()
	stream := newStateStream()
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return boot, stream, nil
	})
	_, url, _ := stateTestServer(t, source, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	req := contract.Request{Kind: "request", ID: "", Method: contract.MethodStateSubscribe, Payload: json.RawMessage(`{}`)}
	base, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("i", contract.StateFrameLimit-len(base))
	writeRequest(t, c, id, contract.MethodStateSubscribe, json.RawMessage(`{}`))
	assertConnectionClosed(t, c)
	waitForChannel(t, stream.closed, "same-ID fallback prepared close")
}

func TestStateUnsubscribeWaitsProducerBeforeACK(t *testing.T) {
	release := make(chan struct{})
	stream := newStateStream()
	stream.releaseAfterCancel = release
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return stateBootstrap(), stream, nil
	})
	committed := make(chan struct{}, 1)
	_, url, _ := stateTestServer(t, source, nil, func(method string) {
		if method == contract.MethodStateUnsubscribe {
			committed <- struct{}{}
		}
	})
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	stateResponse(t, c)
	waitForChannel(t, stream.entered, "owned Next")
	writeRequest(t, c, "unsub", contract.MethodStateUnsubscribe, json.RawMessage(`{}`))
	waitForChannel(t, stream.canceled, "old producer cancellation")
	select {
	case <-committed:
		t.Fatal("unsubscribe ACK committed before old Next exit")
	default:
	}
	close(release)
	r := stateResponse(t, c)
	if !r.OK || r.ID != "unsub" {
		t.Fatal(r)
	}
	waitForChannel(t, committed, "unsubscribe written")
}

func TestStateFailedBootstrapWriteClosesPreparedWithoutStartingProducer(t *testing.T) {
	stream := newStateStream()
	entered, release := make(chan struct{}), make(chan struct{})
	var hooks atomic.Int32
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		close(entered)
		<-release
		return stateBootstrap(), stream, nil
	})
	var buf safeBuffer
	s, err := NewServer(testToken, testBuild, func(context.Context, string, json.RawMessage) (json.RawMessage, *contract.Error) {
		return json.RawMessage(`{}`), nil
	}, newTestLogger(&buf), func(string) { hooks.Add(1) }, source)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewUnstartedServer(s)
	accepted := make(chan net.Conn, 1)
	h.Listener = &trackingListener{Listener: h.Listener, accepted: accepted}
	h.Start()
	t.Cleanup(h.Close)
	c := dial(t, "ws"+strings.TrimPrefix(h.URL, "http")+"/ipc")
	defer c.CloseNow()
	transport := <-accepted
	writeRequest(t, c, "sub", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	waitForChannel(t, entered, "prepared source")
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForChannel(t, stream.closed, "failed ACK prepared cleanup")
	if hooks.Load() != 0 {
		t.Fatal("failed bootstrap committed")
	}
	select {
	case <-stream.entered:
		t.Fatal("producer started before ACK write")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStateInvalidBootstrapProjectionUnavailable(t *testing.T) {
	stream := newStateStream()
	source := stateSourceFunc(func(context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
		return contract.StateBootstrap{}, stream, nil
	})
	_, url, _ := stateTestServer(t, source, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "invalid", contract.MethodStateSubscribe, json.RawMessage(`{}`))
	assertStateError(t, c, "invalid", contract.ErrorCodeStateSnapshotUnavailable)
	waitForChannel(t, stream.closed, "invalid projection cleanup")
}

func TestStateExactRequestReadLimitAccepted(t *testing.T) {
	_, url, _ := stateTestServer(t, nil, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	req := contract.Request{Kind: "request", ID: "exact", Method: contract.MethodDaemonStatus, Payload: json.RawMessage(`{"extra":""}`)}
	base, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	writeRequest(t, c, "exact", contract.MethodDaemonStatus, json.RawMessage(`{"extra":"`+strings.Repeat("x", contract.StateFrameLimit-len(base))+`"}`))
	if r := stateResponse(t, c); !r.OK || r.ID != "exact" {
		t.Fatal("exact request limit rejected")
	}
}
