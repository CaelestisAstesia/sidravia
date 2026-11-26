package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"sidravia/internal/cli/internal/authcmd"
)

func newAuthStartCommand(deps commandDependencies) *cobra.Command {
	return &cobra.Command{
		Use:                "start",
		Short:              "启动一次性认证 Session",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHelpRequest(args) {
				return wrapCommandOperation(renderHelpCompletion(cmd))
			}
			options, err := authcmd.ParseStart(args)
			if err != nil {
				return err
			}
			return wrapCommandOperation(deps.authStart(options))
		},
	}
}

func newSessionCommand(name string, description string, operation func(string) error) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              description,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHelpRequest(args) {
				return wrapCommandOperation(renderHelpCompletion(cmd))
			}
			sessionID, err := parseSessionID(args)
			if err != nil {
				return errCommandUsage
			}
			return wrapCommandOperation(operation(sessionID))
		},
	}
}

func parseSessionID(args []string) (string, error) {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return "", errCommandUsage
	}
	return args[0], nil
}
