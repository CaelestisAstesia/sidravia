package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"sidravia/internal/ipc/contract"

	"github.com/coder/websocket"
)

type Handler func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error)

type Server struct {
	token   string
	buildID string
	handler Handler
}

func NewServer(token string, buildID string, handler Handler) *Server {
	return &Server{
		token:   token,
		buildID: buildID,
		handler: handler,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ipc" {
		http.NotFound(w, r)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	buildIDHeader := r.Header.Get("Sidravia-Build-ID")
	if buildIDHeader != s.buildID {
		http.Error(w, "build ID mismatch", http.StatusConflict)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ipc: websocket accept: %v", err)
		return
	}

	s.serveConn(r.Context(), conn)
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		req, err := contract.DecodeRequest(data)
		if err != nil {
			resp := contract.NewErrorResponse("", contract.ErrorCodeMalformed, err.Error())
			respData, _ := contract.EncodeResponse(resp)
			conn.Write(ctx, websocket.MessageText, respData)
			continue
		}

		result, rpcErr := s.handler(ctx, req.Method, req.Payload)

		var resp contract.Response
		if rpcErr != nil {
			resp = contract.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message)
		} else {
			resp = contract.NewSuccessResponse(req.ID, result)
		}

		respData, err := contract.EncodeResponse(resp)
		if err != nil {
			return
		}
		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			return
		}
	}
}
