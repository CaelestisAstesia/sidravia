//go:build !windows

package daemon

import "context"

func collectNetworkTrace(context.Context, networkTraceRequest) networkTraceResult {
	return networkTraceResult{Status: "unsupported"}
}
