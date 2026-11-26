package cli

import (
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"sidravia/internal/background/daemonctl"
)

// runGUIRestart refuses a foreign owner before sending any stop request.
func runGUIRestart(owner int, level string, deps stopDependencies, start guiBootstrapper, identity daemonctl.Identity, output io.Writer) error {
	if owner <= 0 || (level != "info" && level != "debug" && level != "trace") {
		return invalidGUIBootstrapArguments(errCommandUsage)
	}
	result, err := deps.ops.inspect()
	if err != nil {
		return classifyGUIBootstrapFailure(err)
	}
	if result.State == daemonctl.ProbeStopped {
		return guiBootstrap(identity, owner, output, start)
	}
	if result.State != daemonctl.ProbeReachable || result.Status == nil || result.Status.Mode != "desktop" || result.Status.DesktopOwnerPID == nil || *result.Status.DesktopOwnerPID != owner {
		return classifyGUIBootstrapFailure(daemonctl.ErrModeConflict)
	}
	if err := stopGeneration(deps, result.Info); err != nil {
		return classifyGUIBootstrapFailure(err)
	}
	return guiBootstrap(identity, owner, output, start)
}
func newGUIRestartCommand(identity daemonctl.Identity, output io.Writer) *cobra.Command {
	var ownerText, level string
	c := &cobra.Command{Use: "restart", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		owner, err := strconv.Atoi(ownerText)
		if err != nil {
			return wrapCommandOperation(invalidGUIBootstrapArguments(errCommandUsage))
		}
		start := func(id daemonctl.Identity, pid int) (daemonctl.DesktopBootstrapResult, error) {
			return daemonctl.BootstrapDesktopWithLogLevel(id, pid, level, true)
		}
		return wrapCommandOperation(runGUIRestart(owner, level, defaultStopDependencies(identity), start, identity, output))
	}}
	c.Flags().StringVar(&ownerText, "owner-pid", "", "")
	c.MarkFlagRequired("owner-pid")
	c.Flags().StringVar(&level, "log-level", "debug", "")
	return c
}
