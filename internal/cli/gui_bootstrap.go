package cli

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/launchcontract"
)

type guiBootstrapSuccess struct {
	LogLevel       string `json:"logLevel,omitempty"`
	SchemaVersion  int    `json:"schemaVersion"`
	Endpoint       string `json:"endpoint"`
	Token          string `json:"token"`
	ProductVersion string `json:"productVersion"`
	BuildID        string `json:"buildId"`
	DaemonPID      int    `json:"daemonPid"`
	Mode           string `json:"mode"`
}

type guiBootstrapper func(clientbootstrap.Identity, int) (clientbootstrap.DesktopBootstrapResult, error)

func defaultGUIBootstrap(identity clientbootstrap.Identity, ownerPID int) (clientbootstrap.DesktopBootstrapResult, error) {
	return clientbootstrap.BootstrapDesktop(identity, ownerPID)
}

func guiBootstrap(identity clientbootstrap.Identity, ownerPID int, output io.Writer, bootstrap guiBootstrapper) error {
	if ownerPID <= 0 {
		return invalidGUIBootstrapArguments(errCommandUsage)
	}
	result, err := bootstrap(identity, ownerPID)
	if err != nil {
		return classifyGUIBootstrapFailure(err)
	}
	if result.Status.ProductVersion != identity.ProductVersion || result.Status.BuildID != identity.BuildID ||
		result.Status.Mode != string(launchcontract.ModeDesktop) || result.Status.DesktopOwnerPID == nil || *result.Status.DesktopOwnerPID != ownerPID ||
		result.Status.PID <= 0 || result.Info.PID <= 0 || result.Status.PID != result.Info.PID {
		return unconfirmedGUIBootstrap(errors.New("GUI bootstrap authoritative confirmation failed"))
	}
	value := guiBootstrapSuccess{LogLevel: result.Status.LogLevel, SchemaVersion: 1, Endpoint: result.Info.Endpoint, Token: result.Info.Token, ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, DaemonPID: result.Status.PID, Mode: string(launchcontract.ModeDesktop)}
	encoded, err := json.Marshal(value)
	if err != nil {
		return outputGUIBootstrapFailure(err)
	}
	encoded = append(encoded, '\n')
	if n, err := output.Write(encoded); err != nil || n != len(encoded) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return outputGUIBootstrapFailure(err)
	}
	return nil
}

func newGUIBootstrapCommand(identity clientbootstrap.Identity, output io.Writer, bootstrap guiBootstrapper) *cobra.Command {
	var ownerPID string
	var logLevel string
	bootstrapCommand := &cobra.Command{
		Use:    "bootstrap",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			pid, err := strconv.Atoi(ownerPID)
			if err != nil || pid <= 0 {
				return wrapCommandOperation(invalidGUIBootstrapArguments(errCommandUsage))
			}
			selected := bootstrap
			if logLevel != "" {
				if logLevel != "info" && logLevel != "debug" && logLevel != "trace" {
					return wrapCommandOperation(invalidGUIBootstrapArguments(errCommandUsage))
				}
				selected = func(id clientbootstrap.Identity, owner int) (clientbootstrap.DesktopBootstrapResult, error) {
					return clientbootstrap.BootstrapDesktopWithLogLevel(id, owner, logLevel, false)
				}
			}
			return wrapCommandOperation(guiBootstrap(identity, pid, output, selected))
		},
	}
	bootstrapCommand.Flags().StringVar(&ownerPID, "owner-pid", "", "")
	bootstrapCommand.MarkFlagRequired("owner-pid")
	bootstrapCommand.Flags().StringVar(&logLevel, "log-level", "", "")
	gui := &cobra.Command{Use: "gui", Hidden: true, Args: cobra.NoArgs}
	gui.AddCommand(bootstrapCommand, newGUILogDirectoryCommand(), newGUIRestartCommand(identity, output))
	return gui
}
