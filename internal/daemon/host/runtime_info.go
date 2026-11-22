package host

import (
	"context"
	"fmt"
	"net"
	"os"

	"sidravia/internal/persistence/jsonfile"

	"sidravia/internal/ipc/contract"
)

type runtimeInfoReader interface {
	Read(context.Context, string, int64) ([]byte, bool, error)
}

func cleanupRuntimeInfo(path string, info contract.RuntimeInfo, reader runtimeInfoReader, remove func(string) error) {
	data, exists, err := reader.Read(context.Background(), path, 4096)
	if err != nil || !exists {
		return
	}
	stored, err := contract.DecodeRuntimeInfo(data)
	if err != nil {
		return
	}
	if stored.PID == info.PID && stored.Token == info.Token {
		_ = remove(path)
	}
}

func publishRuntimeInfo(ctx context.Context, cfg Config, listener net.Listener, store jsonfile.Store) (contract.RuntimeInfo, error) {
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return contract.RuntimeInfo{}, fmt.Errorf("host: parse listener address: %w", err)
	}

	info := contract.RuntimeInfo{
		SchemaVersion:  1,
		Endpoint:       "ws://127.0.0.1:" + portStr + "/ipc",
		PID:            os.Getpid(),
		Token:          cfg.Token,
		ProductVersion: cfg.ProductVersion,
		BuildID:        cfg.BuildID,
	}
	infoData, err := contract.EncodeRuntimeInfo(info)
	if err != nil {
		return contract.RuntimeInfo{}, fmt.Errorf("host: encode runtime info: %w", err)
	}
	if err := store.Replace(ctx, cfg.RuntimeInfoPath, infoData); err != nil {
		return contract.RuntimeInfo{}, fmt.Errorf("host: write runtime info: %w", err)
	}
	return info, nil
}
