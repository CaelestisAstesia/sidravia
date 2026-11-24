package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sidravia/internal/ipc/contract"
)

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

func TestStateReadLimitClosesOversizeRequest(t *testing.T) {
	_, url, _ := stateTestServer(t, nil, nil, nil)
	c := dial(t, url)
	defer c.CloseNow()
	writeRequest(t, c, "huge", contract.MethodDaemonStatus, json.RawMessage(`{"extra":"`+strings.Repeat("x", contract.StateFrameLimit)+`"}`))
	assertConnectionClosed(t, c)
}

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
