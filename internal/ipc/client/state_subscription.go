package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

var _ contract.StateEventSource = (*Client)(nil)

// RequestFailure exposes the server's stable business code without exposing its
// arbitrary message as a user-facing error. Business rejection retains the socket.
type RequestFailure struct{ Code string }

func (e *RequestFailure) Error() string { return "state subscription request rejected" }

type stateSubscription struct{ client *Client }

// SubscribeStateEvents transfers the connection to one owned synchronous event
// reader only after a matching full bootstrap ACK. A new Connect is required
// after closing or canceling this stream; ordinary calls cannot reuse it.
func (c *Client) SubscribeStateEvents(ctx context.Context) (contract.StateBootstrap, contract.StateEventStream, error) {
	if err := c.acquire(nil); err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	defer c.release()
	resp, err := c.exchange(ctx, contract.MethodStateSubscribe, nil)
	if err != nil {
		return contract.StateBootstrap{}, nil, err
	}
	if !resp.OK {
		return contract.StateBootstrap{}, nil, &RequestFailure{Code: resp.Error.Code}
	}
	boot, err := contract.DecodeStateBootstrap(resp.Result)
	if err != nil {
		c.discontinue()
		return contract.StateBootstrap{}, nil, fmt.Errorf("subscribe: invalid bootstrap: %w", err)
	}
	stream := &stateSubscription{client: c}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return contract.StateBootstrap{}, nil, ErrClientClosed
	}
	c.owner = stream
	return boot, stream, nil
}

func (s *stateSubscription) Next(ctx context.Context) (contract.StateEvent, error) {
	c := s.client
	if err := c.acquire(s); err != nil {
		return contract.StateEvent{}, err
	}
	defer c.release()
	kind, data, err := c.conn.Read(ctx)
	if err != nil {
		c.discontinue()
		return contract.StateEvent{}, transportError(ctx, "state: read", err)
	}
	if kind != websocket.MessageText {
		c.discontinue()
		return contract.StateEvent{}, errors.New("state: invalid event frame")
	}
	value, err := contract.DecodeStateEvent(data)
	if err != nil {
		c.discontinue()
		return contract.StateEvent{}, fmt.Errorf("state: invalid event: %w", err)
	}
	return value, nil
}

func (s *stateSubscription) Close() error { return s.client.Close() }
