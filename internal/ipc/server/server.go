package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"sidravia/internal/ipc/contract"

	"github.com/coder/websocket"
)

type Handler func(ctx context.Context, method string, payload json.RawMessage) (json.RawMessage, *contract.Error)

// Operational event codes and fixed Simplified Chinese messages for the IPC
// server. Messages are constant summaries; they are never constructed from an
// error, request, response or peer-supplied string. Only the attributes listed
// in the stable schema ever appear on a record.
const (
	eventIPCUpgradeFailed    = "ipc_upgrade_failed"
	eventIPCConnectionOpened = "ipc_connection_opened"
	eventIPCRequestCompleted = "ipc_request_completed"
	eventIPCRequestRejected  = "ipc_request_rejected"
	eventIPCResponseFailed   = "ipc_response_failed"

	msgIPCUpgradeFailed    = "IPC 连接升级失败"
	msgIPCConnectionOpened = "IPC 连接已建立"
	msgIPCRequestCompleted = "IPC 请求已完成"
	msgIPCRequestRejected  = "IPC 请求被拒绝"
	msgIPCResponseFailed   = "IPC 响应失败"

	// methodUnknown is the normalized method for malformed requests and for any
	// method value that is not part of the current IPC contract.
	methodUnknown = "unknown"
	// errorCodeInternal is the normalized error code for any value that is not
	// part of the current IPC contract.
	errorCodeInternal = contract.ErrorCodeInternalError
	// stageEncode and stageWrite are the only response-failure stages.
	stageEncode = "encode"
	stageWrite  = "write"
)

// allowedMethods are the only method values that may appear in logs. Every
// other method supplied by a peer is normalized to methodUnknown so an
// arbitrary peer string can never reach the log.
var allowedMethods = map[string]struct{}{
	contract.MethodDaemonStatus:              {},
	contract.MethodDaemonStop:                {},
	contract.MethodSessionStartOneShot:       {},
	contract.MethodSessionStop:               {},
	contract.MethodSessionEnsureRunning:      {},
	contract.MethodSessionRestart:            {},
	contract.MethodSessionRemove:             {},
	contract.MethodSessionGet:                {},
	contract.MethodSessionList:               {},
	contract.MethodProfileList:               {},
	contract.MethodConfigurationList:         {},
	contract.MethodConfigurationGet:          {},
	contract.MethodConfigurationCreate:       {},
	contract.MethodConfigurationUpdate:       {},
	contract.MethodConfigurationSetPassword:  {},
	contract.MethodConfigurationRemove:       {},
	contract.MethodSessionStartConfiguration: {},
}

// allowedErrorCodes are the only error_code values that may appear in logs.
// Every other code supplied by a handler is normalized to errorCodeInternal so
// an arbitrary diagnostic string can never reach the log.
var allowedErrorCodes = map[string]struct{}{
	contract.ErrorCodeUnknownMethod:                          {},
	contract.ErrorCodeInternalError:                          {},
	contract.ErrorCodeMalformed:                              {},
	contract.ErrorCodeInvalidArgument:                        {},
	contract.ErrorCodeProfileNotFound:                        {},
	contract.ErrorCodeProtocolNotFound:                       {},
	contract.ErrorCodeProfileOperationFailed:                 {},
	contract.ErrorCodeSessionOperationFailed:                 {},
	contract.ErrorCodeSessionNotFound:                        {},
	contract.ErrorCodeSessionActiveConflict:                  {},
	contract.ErrorCodeSessionStateConflict:                   {},
	contract.ErrorCodeConfigurationNotFound:                  {},
	contract.ErrorCodeConfigurationConflict:                  {},
	contract.ErrorCodeConfigurationOperationFailed:           {},
	contract.ErrorCodeConfigurationSessionInvalidationFailed: {},
	contract.ErrorCodeInsecureStorageConfirmationRequired:    {},
	contract.ErrorCodeConfigurationAutoLoginConflict:         {},
}

type Server struct {
	token   string
	buildID string
	handler Handler
	logger  *slog.Logger
	// responseCommitted is invoked after a success response has been written
	// successfully. It is the only hook by which a daemon.stop success reaches
	// runtime lifecycle: a failed encode/write never invokes it. The callback
	// receives the raw request method, which the composition maps to lifecycle
	// semantics; the server itself knows nothing about daemon lifecycle.
	responseCommitted func(method string)

	mu          sync.Mutex
	closing     bool
	connections map[*websocket.Conn]context.CancelFunc
	drained     chan struct{}
}

// NewServer constructs an IPC server. It validates every required dependency
// and returns an error instead of accepting nil or panicking.
func NewServer(token string, buildID string, handler Handler, logger *slog.Logger, responseCommitted func(string)) (*Server, error) {
	if token == "" {
		return nil, errors.New("ipc server: token is required")
	}
	if buildID == "" {
		return nil, errors.New("ipc server: build ID is required")
	}
	if handler == nil {
		return nil, errors.New("ipc server: handler is required")
	}
	if logger == nil {
		return nil, errors.New("ipc server: logger is required")
	}
	drained := make(chan struct{})
	close(drained)
	return &Server{
		token:             token,
		buildID:           buildID,
		handler:           handler,
		logger:            logger,
		responseCommitted: responseCommitted,
		connections:       make(map[*websocket.Conn]context.CancelFunc),
		drained:           drained,
	}, nil
}

// normalizeMethod returns the method if it is part of the current IPC contract,
// otherwise methodUnknown. It is the only path a method value reaches a log.
func normalizeMethod(method string) string {
	if _, ok := allowedMethods[method]; ok {
		return method
	}
	return methodUnknown
}

// normalizeErrorCode returns the code if it is part of the current IPC contract,
// otherwise errorCodeInternal. It is the only path an error code reaches a log.
func normalizeErrorCode(code string) string {
	if _, ok := allowedErrorCodes[code]; ok {
		return code
	}
	return errorCodeInternal
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
		s.logger.Warn(msgIPCUpgradeFailed, slog.String("event", eventIPCUpgradeFailed))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	if !s.register(conn, cancel) {
		cancel()
		_ = conn.CloseNow()
		return
	}
	defer s.unregister(conn)

	s.logger.Debug(msgIPCConnectionOpened, slog.String("event", eventIPCConnectionOpened))
	s.serveConn(ctx, conn)
}

func (s *Server) register(conn *websocket.Conn, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	if len(s.connections) == 0 {
		s.drained = make(chan struct{})
	}
	s.connections[conn] = cancel
	return true
}

func (s *Server) unregister(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.connections[conn]; !ok {
		return
	}
	delete(s.connections, conn)
	if len(s.connections) == 0 {
		close(s.drained)
	}
}

// Shutdown closes the admission gate, then cancels and closes every upgraded
// connection registered before the gate closed. It waits for each ServeHTTP
// handler to return, bounded by ctx, without starting a waiter goroutine.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ipc server: shutdown context is required")
	}

	s.mu.Lock()
	s.closing = true
	drained := s.drained
	connections := make([]struct {
		conn   *websocket.Conn
		cancel context.CancelFunc
	}, 0, len(s.connections))
	for conn, cancel := range s.connections {
		connections = append(connections, struct {
			conn   *websocket.Conn
			cancel context.CancelFunc
		}{conn: conn, cancel: cancel})
	}
	s.mu.Unlock()

	var closeErr error
	for _, connection := range connections {
		connection.cancel()
		if err := connection.conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = errors.Join(closeErr, fmt.Errorf("ipc server: close connection: %w", err))
		}
	}

	select {
	case <-drained:
		return closeErr
	case <-ctx.Done():
		return errors.Join(closeErr, fmt.Errorf("ipc server: shutdown: %w", ctx.Err()))
	}
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		req, decodeErr := contract.DecodeRequest(data)

		// A decode failure is a malformed request: it logs ipc_request_rejected
		// with method unknown and error code malformed_request, then retains the
		// existing response behavior. The decode error itself never reaches the
		// log; it only travels in the IPC response message.
		var resp contract.Response
		method := methodUnknown
		rejected := false
		var rawErrorCode string

		if decodeErr != nil {
			resp = contract.NewErrorResponse("", contract.ErrorCodeMalformed, decodeErr.Error())
			rejected = true
			rawErrorCode = contract.ErrorCodeMalformed
		} else {
			method = req.Method
			result, rpcErr := s.handler(ctx, req.Method, req.Payload)
			if rpcErr != nil {
				resp = contract.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message)
				rejected = true
				rawErrorCode = rpcErr.Code
			} else {
				resp = contract.NewSuccessResponse(req.ID, result)
			}
		}

		respData, encodeErr := contract.EncodeResponse(resp)
		if encodeErr != nil {
			s.logger.Warn(msgIPCResponseFailed,
				slog.String("event", eventIPCResponseFailed),
				slog.String("stage", stageEncode),
			)
			// A handler result may contain invalid JSON even though the request
			// was accepted. Return one fixed, known-encodable response carrying
			// the original request ID, then continue serving the connection.
			fallback := contract.NewErrorResponse(resp.ID, contract.ErrorCodeInternalError, "internal error")
			fallbackData, fallbackErr := contract.EncodeResponse(fallback)
			if fallbackErr != nil {
				// This is unreachable for the fixed fallback, but preserve the
				// existing transport policy if that invariant ever changes.
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, fallbackData); err != nil {
				s.logger.Warn(msgIPCResponseFailed,
					slog.String("event", eventIPCResponseFailed),
					slog.String("stage", stageWrite),
				)
				return
			}
			continue
		}
		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			s.logger.Warn(msgIPCResponseFailed,
				slog.String("event", eventIPCResponseFailed),
				slog.String("stage", stageWrite),
			)
			return
		}

		if rejected {
			s.logger.Warn(msgIPCRequestRejected,
				slog.String("event", eventIPCRequestRejected),
				slog.String("method", normalizeMethod(method)),
				slog.String("error_code", normalizeErrorCode(rawErrorCode)),
			)
		} else {
			s.logger.Debug(msgIPCRequestCompleted,
				slog.String("event", eventIPCRequestCompleted),
				slog.String("method", normalizeMethod(method)),
			)
			// The success response has been written successfully; notify the
			// committed hook only now, so a daemon.stop client receives its
			// response before the daemon begins shutting down.
			if s.responseCommitted != nil {
				s.responseCommitted(method)
			}
		}
	}
}
