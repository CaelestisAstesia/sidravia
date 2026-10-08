package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"sidravia/internal/ipc/contract"
)

const stateWriteTimeout = 5 * time.Second

// A connection is the sole response/event writer. Its one reader and optional
// producer each have a bounded one-frame handoff, cancellation and an exit wait.
type stateConnection struct {
	server       *Server
	socket       *websocket.Conn
	ctx          context.Context
	cancel       context.CancelCauseFunc
	requests     chan []byte
	readerDone   chan struct{}
	subscription *connectionSubscription
}

type connectionSubscription struct {
	stream contract.StateEventStream
	cancel context.CancelFunc
	frames chan []byte
	done   chan struct{}
}

func (s *Server) serveConn(parent context.Context, socket *websocket.Conn) {
	ctx, cancel := context.WithCancelCause(parent)
	c := &stateConnection{server: s, socket: socket, ctx: ctx, cancel: cancel, requests: make(chan []byte, 1), readerDone: make(chan struct{})}
	socket.SetReadLimit(contract.StateFrameLimit)
	go c.readRequests()
	defer func() {
		cancel(context.Canceled)
		_ = socket.CloseNow()
		c.stopSubscription()
		<-c.readerDone
	}()
	for {
		var frames <-chan []byte
		if c.subscription != nil {
			frames = c.subscription.frames
		}
		select {
		case <-ctx.Done():
			return
		case data := <-c.requests:
			if err := c.request(data); err != nil {
				cancel(err)
				return
			}
		case data := <-frames:
			if err := c.write(data); err != nil {
				cancel(err)
				return
			}
		}
	}
}

func (c *stateConnection) readRequests() {
	defer close(c.readerDone)
	for {
		_, data, err := c.socket.Read(c.ctx)
		if err != nil {
			c.cancel(fmt.Errorf("ipc connection read: %w", err))
			return
		}
		select {
		case c.requests <- data:
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *stateConnection) stopSubscription() {
	sub := c.subscription
	if sub == nil {
		return
	}
	// Cancel/close/wait before dropping this epoch. The serial writer cannot
	// deliver an old buffered frame once an unsubscribe acknowledgement is sent.
	sub.cancel()
	if err := sub.stream.Close(); err != nil {
		c.cancel(fmt.Errorf("ipc state close: %w", err))
	}
	<-sub.done
	c.subscription = nil
}

func (c *stateConnection) startSubscription(stream contract.StateEventStream) {
	ctx, cancel := context.WithCancel(c.ctx)
	sub := &connectionSubscription{stream: stream, cancel: cancel, frames: make(chan []byte, 1), done: make(chan struct{})}
	c.subscription = sub
	go func() {
		defer close(sub.done)
		for {
			value, err := stream.Next(ctx)
			if err != nil {
				if ctx.Err() == nil {
					c.cancel(fmt.Errorf("ipc state next: %w", err))
				}
				return
			}
			data, err := contract.EncodeStateEvent(value)
			if err != nil {
				c.cancel(fmt.Errorf("ipc state event: %w", err))
				return
			}
			select {
			case <-ctx.Done():
				return
			case sub.frames <- data:
			}
		}
	}()
}

func (c *stateConnection) request(data []byte) error {
	req, err := contract.DecodeRequest(data)
	method := methodUnknown
	var resp contract.Response
	var prepared contract.StateEventStream
	if err != nil {
		resp = contract.NewErrorResponse("", contract.ErrorCodeMalformed, err.Error())
	} else {
		method = req.Method
		switch method {
		case contract.MethodStateSubscribe, contract.MethodStateUnsubscribe:
			if c.server.stateSource == nil {
				resp = contract.NewErrorResponse(req.ID, contract.ErrorCodeUnknownMethod, "unknown method")
			} else if contract.DecodeEmptyPayload(req.Payload) != nil {
				resp = contract.NewErrorResponse(req.ID, contract.ErrorCodeInvalidArgument, "invalid state request")
			} else if method == contract.MethodStateUnsubscribe {
				c.stopSubscription()
				result, encodeErr := contract.MarshalStateUnsubscribeResult(contract.StateUnsubscribeResult{Status: "unsubscribed"})
				if encodeErr != nil {
					return encodeErr
				}
				resp = contract.NewSuccessResponse(req.ID, result)
			} else if c.subscription != nil {
				resp = contract.NewErrorResponse(req.ID, contract.ErrorCodeInvalidArgument, "state subscription already active")
			} else {
				resp, prepared = c.prepareSubscription(req.ID)
			}
		default:
			result, rpcErr := c.server.handler(c.ctx, method, req.Payload)
			if rpcErr != nil {
				resp = contract.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message)
			} else {
				resp = contract.NewSuccessResponse(req.ID, result)
			}
		}
	}
	if prepared != nil {
		transferred := false
		defer func() {
			if !transferred {
				if err := prepared.Close(); err != nil {
					c.cancel(fmt.Errorf("ipc prepared state close: %w", err))
				}
			}
		}()
		success, err := c.respond(resp, method)
		if err != nil {
			return err
		}
		if success {
			// The full ACK has completed its socket write before Next can run.
			c.startSubscription(prepared)
			transferred = true
		}
		return nil
	}
	_, err = c.respond(resp, method)
	return err
}

func stateSubscriptionError(id string, err error) contract.Response {
	if errors.Is(err, contract.ErrStateFrameLimit) || errors.Is(err, contract.ErrStateResourceCapacity) {
		return contract.NewErrorResponse(id, contract.ErrorCodeStateSnapshotTooLarge, "state snapshot too large")
	}
	return contract.NewErrorResponse(id, contract.ErrorCodeStateSnapshotUnavailable, "state snapshot unavailable")
}

func (c *stateConnection) prepareSubscription(id string) (contract.Response, contract.StateEventStream) {
	value, stream, err := c.server.stateSource.SubscribeStateEvents(c.ctx)
	if err == nil && stream == nil {
		err = errors.New("ipc state source returned no stream")
	}
	var result json.RawMessage
	if err == nil {
		result, err = contract.MarshalStateBootstrap(value)
	}
	resp := contract.NewSuccessResponse(id, result)
	if err == nil {
		var encoded []byte
		encoded, err = contract.EncodeResponse(resp)
		if err == nil && len(encoded) > contract.StateFrameLimit {
			err = contract.ErrStateFrameLimit
		}
	}
	if err != nil {
		if stream != nil {
			if closeErr := stream.Close(); closeErr != nil {
				c.cancel(fmt.Errorf("ipc prepared state close: %w", closeErr))
			}
		}
		return stateSubscriptionError(id, err), nil
	}
	return resp, stream
}

// respond retains same-ID fixed internal-error fallback for invalid handler
// JSON. Both the original response and final fallback include envelope limits.
// Only an actually written original success is committed.
func (c *stateConnection) respond(resp contract.Response, method string) (bool, error) {
	data, err := contract.EncodeResponse(resp)
	if err == nil && len(data) > contract.StateFrameLimit {
		err = contract.ErrStateFrameLimit
	}
	if err != nil {
		c.server.responseFailed(stageEncode)
		fallback := contract.NewErrorResponse(resp.ID, contract.ErrorCodeInternalError, "internal error")
		data, err = contract.EncodeResponse(fallback)
		if err != nil {
			return false, err
		}
		if err = c.write(data); err != nil {
			return false, err
		}
		return false, nil
	}
	if err = c.write(data); err != nil {
		return false, err
	}
	if !resp.OK {
		c.server.logger.Warn(msgIPCRequestRejected, slog.String("event", eventIPCRequestRejected), slog.String("method", normalizeMethod(method)), slog.String("error_code", normalizeErrorCode(resp.Error.Code)))
		return false, nil
	}
	c.server.logger.Debug(msgIPCRequestCompleted, slog.String("event", eventIPCRequestCompleted), slog.String("method", normalizeMethod(method)))
	if c.server.responseCommitted != nil {
		c.server.responseCommitted(method)
	}
	return true, nil
}

func (s *Server) responseFailed(stage string) {
	s.logger.Warn(msgIPCResponseFailed, slog.String("event", eventIPCResponseFailed), slog.String("stage", stage))
}

func (c *stateConnection) write(data []byte) error {
	if len(data) > contract.StateFrameLimit {
		return contract.ErrStateFrameLimit
	}
	ctx, cancel := context.WithTimeout(c.ctx, stateWriteTimeout)
	defer cancel()
	if err := c.socket.Write(ctx, websocket.MessageText, data); err != nil {
		c.server.responseFailed(stageWrite)
		return fmt.Errorf("ipc connection write: %w", err)
	}
	return nil
}
