package daemonconn

import (
	"context"
	"fmt"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/terminal"
)

type Client = daemonctl.Client

type Connection struct {
	Acquire func(context.Context) (Client, error)
	Timeout time.Duration
}

// MayStart delegates all discovery, launch and
// readiness work to bootstrap. Auth owns only operation deadlines and cleanup.
func MayStart(identity daemonctl.Identity) Connection {
	return Connection{
		Acquire: func(ctx context.Context) (Client, error) {
			return daemonctl.AcquireHeadless(ctx, identity, "")
		},
		Timeout: 2 * time.Second,
	}
}

func Existing(identity daemonctl.Identity) Connection {
	return Connection{
		Acquire: func(ctx context.Context) (Client, error) {
			return daemonctl.ConnectExistingHeadless(ctx, identity)
		},
		Timeout: 2 * time.Second,
	}
}

func With(deps Connection, operation func(Client) error) error {
	connection, err := Connect(deps)
	if err != nil {
		if daemonctl.IsReadinessUnconfirmed(err) {
			return err
		}
		return terminal.Failure("无法连接 sidraviad（请确认 daemon 已启动；若刚增删过 sidravia.portable 标记，请先停止并重启 daemon）", err)
	}

	operationErr := operation(connection)
	closeErr := connection.Close()
	if operationErr != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; %w", operationErr, terminal.Failure("清理 sidraviad 连接", closeErr))
		}
		return operationErr
	}
	// The operation succeeded; a connection cleanup failure must not turn a
	// successful result into a user-visible error.
	_ = closeErr
	return nil
}

func Connect(deps Connection) (Client, error) {
	acquired, err := deps.Acquire(context.Background())
	if err != nil {
		return nil, err
	}
	if acquired == nil {
		return nil, fmt.Errorf("无法连接 daemon（未找到运行中的 daemon）")
	}
	return acquired, nil
}
