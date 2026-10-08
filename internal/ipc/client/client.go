package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

var (
	ErrClientClosed            = errors.New("IPC client is closed")
	ErrClientBusy              = errors.New("IPC client already has an operation in progress")
	ErrStateSubscriptionActive = errors.New("IPC client has an active state subscription")
)

type Client struct {
	conn      *websocket.Conn
	buildID   string
	mu        sync.Mutex
	closed    bool
	busy      bool
	owner     *stateSubscription
	active    sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func Connect(ctx context.Context, endpoint string, token string, buildID string) (*Client, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Sidravia-Build-ID", buildID)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return nil, fmt.Errorf("connect: authentication failed (401)")
			case http.StatusConflict:
				return nil, fmt.Errorf("connect: build ID mismatch (409)")
			}
		}
		return nil, fmt.Errorf("connect: %w", err)
	}
	conn.SetReadLimit(contract.StateFrameLimit)
	return &Client{conn: conn, buildID: buildID}, nil
}

// acquire admits one synchronous operation. Closing and admission use the same
// gate, so Close's Wait cannot race a new WaitGroup Add.
func (c *Client) acquire(owner *stateSubscription) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	if c.owner != owner {
		return ErrStateSubscriptionActive
	}
	if c.busy {
		return ErrClientBusy
	}
	c.busy = true
	c.active.Add(1)
	return nil
}

func (c *Client) release() {
	c.mu.Lock()
	c.busy = false
	c.mu.Unlock()
	c.active.Done()
}

// discontinue interrupts transport work without waiting for its own operation.
// Only public Close waits for all admitted work after closing the admission gate.
func (c *Client) discontinue() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeOnce.Do(func() { c.closeErr = c.conn.CloseNow() })
}

func transportError(ctx context.Context, action string, err error) error {
	return fmt.Errorf("%s: %w", action, errors.Join(err, ctx.Err(), context.Cause(ctx)))
}

// exchange is called only by the admitted sole reader/writer.
func (c *Client) exchange(ctx context.Context, method string, payload json.RawMessage) (contract.Response, error) {
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	req := contract.Request{Kind: string(contract.KindRequest), ID: "1", Method: method, Payload: payload}
	data, err := json.Marshal(req)
	if err != nil {
		return contract.Response{}, fmt.Errorf("call: %w", err)
	}
	if len(data) > contract.StateFrameLimit {
		return contract.Response{}, fmt.Errorf("call: %w", contract.ErrStateFrameLimit)
	}
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.discontinue()
		return contract.Response{}, transportError(ctx, "call: write", err)
	}
	kind, data, err := c.conn.Read(ctx)
	if err != nil {
		c.discontinue()
		return contract.Response{}, transportError(ctx, "call: read", err)
	}
	if kind != websocket.MessageText || len(data) > contract.StateFrameLimit {
		c.discontinue()
		return contract.Response{}, errors.New("call: invalid response frame")
	}
	resp, err := contract.DecodeResponse(data)
	if err != nil {
		c.discontinue()
		return contract.Response{}, fmt.Errorf("call: %w", err)
	}
	if resp.ID != "1" {
		c.discontinue()
		return contract.Response{}, errors.New("call: response id mismatch")
	}
	// DecodeResponse preserves ordinary RPC semantics. State acquisition also
	// requires the explicit, non-null ACK fields and exactly one result/error.
	if method == contract.MethodStateSubscribe {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			c.discontinue()
			return contract.Response{}, fmt.Errorf("subscribe: %w", err)
		}
		for _, name := range []string{"kind", "id", "ok"} {
			value, found := fields[name]
			if !found || string(value) == "null" {
				c.discontinue()
				return contract.Response{}, errors.New("subscribe: invalid acknowledgement")
			}
		}
		_, result := fields["result"]
		_, failure := fields["error"]
		if resp.OK && (!result || failure) || !resp.OK && (result || !failure || resp.Error.Code == "") {
			c.discontinue()
			return contract.Response{}, errors.New("subscribe: invalid acknowledgement")
		}
	}
	return resp, nil
}

func (c *Client) Call(ctx context.Context, method string, payload json.RawMessage) (contract.Response, error) {
	if err := c.acquire(nil); err != nil {
		return contract.Response{}, err
	}
	defer c.release()
	return c.exchange(ctx, method, payload)
}

// Close owns the whole connection, interrupts blocked operations, and waits for
// their synchronous reads to return. It is safe to call concurrently and again.
func (c *Client) Close() error {
	c.discontinue()
	c.active.Wait()
	return c.closeErr
}
