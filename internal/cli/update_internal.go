package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/installation/inno"
)

// Fixed stages only; no URL, script, command or arbitrary output path.
func newUpdateInternalCommand(identity daemonctl.Identity, out io.Writer) *cobra.Command {
	root := &cobra.Command{Use: "update-internal", Hidden: true}
	root.AddCommand(&cobra.Command{Use: "capabilities", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		_, err := fmt.Fprintln(out, `{"schemaVersion":1,"lifecycleProtocol":1}`)
		return err
	}})
	for _, action := range []string{"prepare", "stop", "abort"} {
		var operation string
		var coordinator uint32
		var interactive bool
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 80*time.Second)
			defer cancel()
			if err := inno.Run(ctx, action, operation, coordinator, interactive, identity); err != nil {
				return wrapCommandOperation(integrationFailure("更新生命周期操作未完成；程序文件不会由此命令修改。", err))
			}
			_, err := fmt.Fprintln(out, `{"schemaVersion":1,"ok":true}`)
			return err
		}}
		cmd.Flags().StringVar(&operation, "operation", "", "")
		cmd.Flags().Uint32Var(&coordinator, "coordinator", 0, "")
		cmd.Flags().BoolVar(&interactive, "interactive", false, "")
		root.AddCommand(cmd)
	}
	addSetupStages(root, identity)
	return root
}
