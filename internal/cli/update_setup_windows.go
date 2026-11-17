//go:build windows

package cli

import (
	"context"
	"github.com/spf13/cobra"
	"sidravia/internal/clientbootstrap"
	"sidravia/internal/installationgate"
	"sidravia/internal/update/setup"
	"time"
)

func addSetupStages(root *cobra.Command, identity clientbootstrap.Identity) {
	root.AddCommand(&cobra.Command{Use: "setup-admission", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		release, e := installationgate.AcquireRunIdentityContext(ctx, identity.ProductVersion, identity.BuildID)
		if e != nil {
			return e
		}
		release()
		return nil
	}})
	for _, stage := range []string{"accept", "prepare", "stop", "write", "installed", "abort", "ready"} {
		var operation string
		var coordinator, updater uint32
		var gui bool
		cmd := &cobra.Command{Use: "setup-" + stage, Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) (err error) {
			defer func() { err = wrapCommandOperation(err) }()
			ctx, cancel := context.WithTimeout(cmd.Context(), 80*time.Second)
			defer cancel()
			if err := setup.RunStage(ctx, stage, operation, coordinator, updater, gui, identity); err != nil {
				return integrationFailure("安装更新阶段未完成，请检查更新状态", err)
			}
			return nil
		}}
		cmd.Flags().StringVar(&operation, "operation", "", "")
		cmd.Flags().Uint32Var(&coordinator, "coordinator", 0, "")
		cmd.Flags().Uint32Var(&updater, "updater", 0, "")
		cmd.Flags().BoolVar(&gui, "gui", false, "")
		root.AddCommand(cmd)
	}
}
