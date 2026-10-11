package cli

import (
	"github.com/spf13/cobra"
	"io"
	"sidravia/internal/clientbootstrap"
	"strconv"
)

// runGUIRestart refuses a foreign owner before sending any stop request.
func runGUIRestart(owner int, level string, deps stopDependencies, start guiBootstrapper, identity clientbootstrap.Identity, output io.Writer) error {
	if owner <= 0 || (level != "info" && level != "debug" && level != "trace") {
		return invalidGUIBootstrapArguments(errCommandUsage)
	}
	result, err := deps.ops.inspect()
	if err != nil {
		return classifyGUIBootstrapFailure(err)
	}
	if result.State == clientbootstrap.ProbeStopped {
		return guiBootstrap(identity, owner, output, start)
	}
	if result.State != clientbootstrap.ProbeReachable || result.Status == nil || result.Status.Mode != "desktop" || result.Status.DesktopOwnerPID == nil || *result.Status.DesktopOwnerPID != owner {
		return classifyGUIBootstrapFailure(clientbootstrap.ErrModeConflict)
	}
	if err := stopGeneration(deps, result.Info); err != nil {
		return classifyGUIBootstrapFailure(err)
	}
	return guiBootstrap(identity, owner, output, start)
}
func newGUIRestartCommand(identity clientbootstrap.Identity, output io.Writer) *cobra.Command {
	var ownerText, level string
	c := &cobra.Command{Use: "restart", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		owner, err := strconv.Atoi(ownerText)
		if err != nil {
			return wrapCommandOperation(invalidGUIBootstrapArguments(errCommandUsage))
		}
		start := func(id clientbootstrap.Identity, pid int) (clientbootstrap.DesktopBootstrapResult, error) {
			return clientbootstrap.BootstrapDesktopWithLogLevel(id, pid, level, true)
		}
		return wrapCommandOperation(runGUIRestart(owner, level, defaultStopDependencies(identity), start, identity, output))
	}}
	c.Flags().StringVar(&ownerText, "owner-pid", "", "")
	c.MarkFlagRequired("owner-pid")
	c.Flags().StringVar(&level, "log-level", "info", "")
	return c
}
