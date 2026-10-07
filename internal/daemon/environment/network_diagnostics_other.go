//go:build !windows

package environment

import (
	"context"
	"fmt"
)

func diagnoseNetwork(context.Context, NetworkDiagnosticQuery) (NetworkDiagnosticResult, error) {
	return NetworkDiagnosticResult{}, fmt.Errorf("diagnose network: %w", ErrUnsupported)
}
