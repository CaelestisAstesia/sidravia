package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type Kind string

const (
	KindRequest  Kind = "request"
	KindResponse Kind = "response"
)

type Request struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

type Response struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func DecodeRequest(data []byte) (Request, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var req Request
	if err := decoder.Decode(&req); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Request{}, fmt.Errorf("decode request: trailing data after envelope")
	} else if err != io.EOF {
		return Request{}, fmt.Errorf("decode request: trailing garbage")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	if req.Kind != string(KindRequest) {
		return Request{}, fmt.Errorf("decode request: kind must be %q", KindRequest)
	}
	if req.ID == "" {
		return Request{}, fmt.Errorf("decode request: empty id")
	}
	if req.Method == "" {
		return Request{}, fmt.Errorf("decode request: empty method")
	}
	if req.Payload == nil || string(req.Payload) == "null" {
		return Request{}, fmt.Errorf("decode request: missing payload")
	}
	return req, nil
}

func EncodeResponse(resp Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode response: %w", err)
	}
	return data, nil
}

func DecodeResponse(data []byte) (Response, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Response{}, fmt.Errorf("decode response: trailing data")
	} else if err != io.EOF {
		return Response{}, fmt.Errorf("decode response: trailing garbage")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.Kind != string(KindResponse) {
		return Response{}, fmt.Errorf("decode response: kind must be %q", KindResponse)
	}
	if resp.OK && resp.Error != nil {
		return Response{}, fmt.Errorf("decode response: ok is true but error is present")
	}
	if !resp.OK && resp.Error == nil {
		return Response{}, fmt.Errorf("decode response: ok is false but error is missing")
	}
	return resp, nil
}

func NewSuccessResponse(id string, result json.RawMessage) Response {
	return Response{
		Kind:   string(KindResponse),
		ID:     id,
		OK:     true,
		Result: result,
	}
}

func NewErrorResponse(id string, code string, message string) Response {
	return Response{
		Kind: string(KindResponse),
		ID:   id,
		OK:   false,
		Error: &Error{
			Code:    code,
			Message: message,
		},
	}
}
