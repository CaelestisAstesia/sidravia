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
		result.Status.Mode != string(launchcontract.ModeDesktop) || result.Status.DesktopOwnerPID == nil || *result.Status.DesktopOwnerPID != ownerPID {
		return unconfirmedGUIBootstrap(errors.New("GUI bootstrap authoritative confirmation failed"))
	}
	value := guiBootstrapSuccess{SchemaVersion: 1, Endpoint: result.Info.Endpoint, Token: result.Info.Token, ProductVersion: identity.ProductVersion, BuildID: identity.BuildID, DaemonPID: result.Status.PID, Mode: string(launchcontract.ModeDesktop)}
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
	bootstrapCommand := &cobra.Command{
		Use:    "bootstrap",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			pid, err := strconv.Atoi(ownerPID)
			if err != nil || pid <= 0 {
				return wrapCommandOperation(invalidGUIBootstrapArguments(errCommandUsage))
			}
			return wrapCommandOperation(guiBootstrap(identity, pid, output, bootstrap))
		},
	}
	bootstrapCommand.Flags().StringVar(&ownerPID, "owner-pid", "", "")
	bootstrapCommand.MarkFlagRequired("owner-pid")
	gui := &cobra.Command{Use: "gui", Hidden: true, Args: cobra.NoArgs}
	gui.AddCommand(bootstrapCommand)
	return gui
}
