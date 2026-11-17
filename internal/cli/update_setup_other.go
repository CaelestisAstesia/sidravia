//go:build !windows

package cli

import (
	"github.com/spf13/cobra"
	"sidravia/internal/clientbootstrap"
)

func addSetupStages(*cobra.Command, clientbootstrap.Identity) {}
