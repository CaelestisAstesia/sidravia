package host

import (
	"context"

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
