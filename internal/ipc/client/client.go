package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"sidravia/internal/ipc/contract"

	"github.com/coder/websocket"
)

type Client struct {
	conn    *websocket.Conn
	buildID string
}

func Connect(ctx context.Context, endpoint string, token string, buildID string) (*Client, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("Sidravia-Build-ID", buildID)

	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: header,
	})
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

	return &Client{conn: conn, buildID: buildID}, nil
}

func (c *Client) Call(ctx context.Context, method string, payload json.RawMessage) (contract.Response, error) {
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	req := contract.Request{
		Kind:    string(contract.KindRequest),
		ID:      "1",
		Method:  method,
		Payload: payload,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return contract.Response{}, fmt.Errorf("call: %w", err)
	}

	if err := c.conn.Write(ctx, websocket.MessageText, reqData); err != nil {
		return contract.Response{}, fmt.Errorf("call: write: %w", err)
	}

	_, respData, err := c.conn.Read(ctx)
	if err != nil {
		return contract.Response{}, fmt.Errorf("call: read: %w", err)
	}

	resp, err := contract.DecodeResponse(respData)
	if err != nil {
		return contract.Response{}, fmt.Errorf("call: %w", err)
	}
	if resp.ID != "1" {
		return contract.Response{}, fmt.Errorf("call: response id mismatch")
	}
	return resp, nil
}

func (c *Client) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}
