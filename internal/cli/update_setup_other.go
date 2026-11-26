//go:build !windows

package cli

import (
	"github.com/spf13/cobra"
	"sidravia/internal/background/daemonctl"
)

func addSetupStages(*cobra.Command, daemonctl.Identity) {}
