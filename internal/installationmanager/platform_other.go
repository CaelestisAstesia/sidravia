//go:build !windows

package installationmanager

import "errors"

// Run returns Unsupported before opening any storage on unimplemented platforms.
func Run(directory string, operation Operation) (Result, error) {
	return Result{Version: 1, Code: "unsupported", Capabilities: []string{}}, &Failure{Code: "unsupported", Cause: errors.New("platform integration is not implemented")}
}
