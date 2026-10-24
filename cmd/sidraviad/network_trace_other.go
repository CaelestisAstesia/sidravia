//go:build !windows

package main

import "context"

func collectNetworkTrace(context.Context, networkTraceRequest) networkTraceResult {
	return networkTraceResult{Status: "unsupported"}
}
