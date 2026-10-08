package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"sidravia/internal/ipc/client"
	"sidravia/internal/ipc/contract"
)

var errAuthWatchUnavailable = errors.New("daemon does not provide state watching")

type authWatchFailure struct {
	message string
	cause   error
}

func (failure *authWatchFailure) Error() string { return failure.message }
func (failure *authWatchFailure) Unwrap() error { return failure.cause }

func safeAuthWatchFailure(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &authWatchFailure{message: message, cause: cause}
}

func runAuthWatch(ctx context.Context, deps listDependencies) error {
	if deps.connection.acquire == nil {
		return safeAuthWatchFailure("无法连接已有 daemon；请确认 daemon 已启动", errors.New("missing read-only acquisition"))
	}
	acquireCtx, cancelAcquire := context.WithTimeout(ctx, deps.connection.callTimeout)
	connection, err := deps.connection.acquire(acquireCtx)
	acquireContextErr := acquireCtx.Err()
	acquireCause := context.Cause(acquireCtx)
	cancelAcquire()
	if err != nil {
		return safeAuthWatchFailure("无法连接已有 daemon；请确认 daemon 已启动", errors.Join(err, acquireContextErr, acquireCause, context.Cause(ctx)))
	}
	if connection == nil {
		return safeAuthWatchFailure("无法连接已有 daemon；请确认 daemon 已启动", errors.New("daemon connection unavailable"))
	}

	source, ok := connection.(contract.StateEventSource)
	if !ok {
		return errors.Join(safeAuthWatchFailure("当前 daemon 不支持实时状态监听", errAuthWatchUnavailable), closeAuthWatchConnection(connection))
	}

	ackCtx, cancelAck := context.WithTimeout(ctx, deps.connection.callTimeout)
	bootstrap, stream, subscribeErr := source.SubscribeStateEvents(ackCtx)
	ackContextErr := ackCtx.Err()
	ackCause := context.Cause(ackCtx)
	cancelAck()
	if subscribeErr != nil {
		cause := errors.Join(subscribeErr, ackContextErr, ackCause, context.Cause(ctx))
		var requestFailure *client.RequestFailure
		if errors.As(subscribeErr, &requestFailure) {
			subscribeErr = safeAuthWatchFailure(ipcErrorText(requestFailure.Code), cause)
		} else {
			subscribeErr = safeAuthWatchFailure("订阅 daemon 状态失败", cause)
		}
		return errors.Join(subscribeErr, closeAuthWatchStream(stream), closeAuthWatchConnection(connection))
	}
	if stream == nil {
		return errors.Join(
			safeAuthWatchFailure("daemon 状态监听不可用", errors.New("nil state event stream")),
			closeAuthWatchConnection(connection),
		)
	}

	operationErr := watchAuthState(ctx, stream, bootstrap, deps.stdout)
	return errors.Join(operationErr, closeAuthWatchStream(stream), closeAuthWatchConnection(connection))
}

func watchAuthState(ctx context.Context, stream contract.StateEventStream, bootstrap contract.StateBootstrap, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	if err := writeAuthWatchBlock(output, renderAuthWatchBootstrap(bootstrap)); err != nil {
		return safeAuthWatchFailure("写入 daemon 初始状态失败", err)
	}
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return safeAuthWatchFailure("daemon 状态连接已断开；请重新运行 sidraviactl auth watch", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		block, err := renderAuthWatchEvent(event)
		if err != nil {
			return safeAuthWatchFailure("daemon 返回了无效状态事件", err)
		}
		if err := writeAuthWatchBlock(output, block); err != nil {
			return safeAuthWatchFailure("写入 daemon 状态事件失败", err)
		}
	}
}

func renderAuthWatchBootstrap(bootstrap contract.StateBootstrap) string {
	var output strings.Builder
	if len(bootstrap.Sessions.Sessions) == 0 {
		output.WriteString(renderSessionList(newPlainPresentation(), &bootstrap.Sessions))
	} else {
		fmt.Fprintf(&output, "当前 Session 状态（%d）：\n", len(bootstrap.Sessions.Sessions))
		for _, session := range bootstrap.Sessions.Sessions {
			output.WriteString(renderAuthWatchSession(session, containsSessionID(bootstrap.Sessions.CleanupRequiredSessionIDs, session.AuthenticationSessionID)))
		}
	}
	output.WriteString(renderNetworkInterfaces(bootstrap.Network))
	return output.String()
}

func renderAuthWatchSession(session contract.SessionResult, cleanupRequired bool) string {
	p := newPlainPresentation()
	var output strings.Builder
	output.WriteString(renderSessionDetail(p, &session))
	output.WriteString(p.label("revision："))
	output.WriteString(strconv.FormatUint(session.Revision, 10))
	output.WriteString("\n")
	output.WriteString(p.label("意图："))
	output.WriteString(sanitizeDynamicText(session.Intent))
	output.WriteString("\n")
	if session.ConfigurationID != "" {
		output.WriteString(p.label("配置："))
		output.WriteString(sanitizeDynamicText(session.ConfigurationID))
		output.WriteString("\n")
	}
	output.WriteString(p.label("协议 Socket："))
	output.WriteString(sanitizeDynamicText(session.ProtocolSocket.State))
	output.WriteString("\n")
	output.WriteString(p.label("协议 Socket runGeneration："))
	output.WriteString(strconv.FormatUint(session.ProtocolSocket.RunGeneration, 10))
	output.WriteString("\n")
	if session.ProtocolSocket.UpdatedAt != nil {
		output.WriteString(p.label("协议 Socket 更新时间："))
		output.WriteString(sanitizeDynamicText(*session.ProtocolSocket.UpdatedAt))
		output.WriteString("\n")
	}
	appendAuthWatchEndpoint(&output, p, "本地", session.ProtocolSocket.LocalEndpoint)
	appendAuthWatchEndpoint(&output, p, "远端", session.ProtocolSocket.RemoteEndpoint)
	if cleanupRequired {
		output.WriteString("需要清理：运行 sidraviactl auth remove 释放此 Session。\n")
	}
	return output.String()
}

func appendAuthWatchEndpoint(output *strings.Builder, p *presentation, label string, endpoint *contract.NetworkEndpoint) {
	if endpoint == nil {
		return
	}
	output.WriteString(p.label("协议 Socket " + label + "端点："))
	output.WriteString(sanitizeDynamicText(endpoint.Address))
	output.WriteByte(':')
	output.WriteString(strconv.FormatUint(uint64(endpoint.Port), 10))
	output.WriteString("\n")
}

func containsSessionID(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func renderAuthWatchEvent(event contract.StateEvent) (string, error) {
	switch event.Method {
	case contract.EventMethodSessionChanged:
		if event.SessionChanged == nil {
			return "", errors.New("missing Session changed payload")
		}
		return renderAuthWatchSession(event.SessionChanged.Session, event.SessionChanged.CleanupRequired), nil
	case contract.EventMethodSessionRemoved:
		if event.SessionRemoved == nil {
			return "", errors.New("missing Session removed payload")
		}
		var output strings.Builder
		fmt.Fprintf(&output, "Session 已移除：%s；revision：%s\n", sanitizeDynamicText(event.SessionRemoved.SessionID), strconv.FormatUint(event.SessionRemoved.Revision, 10))
		return output.String(), nil
	case contract.EventMethodNetworkChanged:
		if event.NetworkChanged == nil {
			return "", errors.New("missing network changed payload")
		}
		return renderNetworkInterfaces(*event.NetworkChanged), nil
	default:
		return "", errors.New("unknown state event method")
	}
}

func writeAuthWatchBlock(output io.Writer, block string) error {
	return newPresentation(output).complete(block)
}

func closeAuthWatchStream(stream contract.StateEventStream) error {
	if stream == nil {
		return nil
	}
	if err := stream.Close(); err != nil {
		return safeAuthWatchFailure("关闭 daemon 状态监听失败", err)
	}
	return nil
}

func closeAuthWatchConnection(connection daemonClient) error {
	if connection == nil {
		return nil
	}
	if err := connection.Close(); err != nil {
		return safeAuthWatchFailure("关闭 daemon 连接失败", err)
	}
	return nil
}
