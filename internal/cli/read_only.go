package cli

import (
	"io"
	"os"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/daemonconn"
)

type readOnlyCommandDependencies struct {
	connection daemonconn.Connection
	stdout     io.Writer
}

func defaultReadOnlyCommandDependencies(identity daemonctl.Identity) readOnlyCommandDependencies {
	return readOnlyCommandDependencies{
		connection: daemonconn.Existing(identity),
		stdout:     os.Stdout,
	}
}
