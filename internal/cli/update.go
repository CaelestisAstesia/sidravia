package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"sidravia/internal/background/daemonctl"
	productlayout "sidravia/internal/installation/layout"
	"sidravia/internal/update/command"
	"sidravia/internal/update/entry"
)

type updateOutput struct{ data []byte }

func (b *updateOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 2<<20 {
		return 0, errors.New("updater response limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func invokeUpdater(ctx context.Context, args []string, identity daemonctl.Identity) ([]byte, error) {
	if entry.CheckInCTL(args) {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
		ctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
		defer cancel()
		var output updateOutput
		command.Run(ctx, args, identity.ProductVersion, identity.BuildID, &output)
		return output.data, nil
	}
	if runtime.GOOS == "linux" {
		layout, err := productlayout.Resolve()
		if err != nil {
			return nil, err
		}
		if productlayout.RPMManaged(layout) {
			return rpmUpdateOutput(args, identity)
		}
	}
	path, err := siblingExecutable("sidraviaupdater")
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && args[0] == "apply" {
		if handled, err := handoffUpdater(path, args); handled {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	var output updateOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, errors.New("更新操作已取消或超时。")
	}
	return output.data, err
}

// RPM updates belong to the system package manager; no downloader is needed.
func rpmUpdateOutput(args []string, identity daemonctl.Identity) ([]byte, error) {
	if len(args) == 1 && args[0] == "version" {
		return json.Marshal(map[string]any{"schemaVersion": 1, "ok": true, "code": "ok", "result": map[string]string{"productVersion": identity.ProductVersion, "buildId": identity.BuildID}})
	}
	return json.Marshal(map[string]any{"schemaVersion": 1, "ok": false, "code": "system_package_managed", "message": "此安装由 RPM 管理，请使用系统包管理器更新。"})
}

func newUpdateCommand(out io.Writer, invoke func(context.Context, []string) ([]byte, error)) *cobra.Command {
	group := &cobra.Command{Use: "update", Short: "检查、下载和应用更新"}
	operations := []struct{ use, description string }{
		{"check", "检查适用于当前安装的版本"},
		{"download --candidate ID", "下载并校验指定候选"},
		{"status", "查看缓存与更新状态"},
		{"apply --candidate ID --confirm", "应用已确认的更新"},
		{"recover --operation ID --confirm", "核对并处理已中断的更新"},
		{"version", "显示版本与构建标识"},
	}
	for _, item := range operations {
		command := &cobra.Command{Use: item.use, Short: item.description, Args: cobra.NoArgs}
		operation := command.Name()
		var channel, kind, candidate, operationID string
		var confirm bool
		command.RunE = func(cmd *cobra.Command, _ []string) (err error) {
			defer func() { err = wrapCommandOperation(err) }()
			args := []string{operation}
			if channel != "" {
				args = append(args, "--channel", channel)
			}
			if kind != "" {
				args = append(args, "--package", kind)
			}
			if candidate != "" {
				args = append(args, "--candidate", candidate)
			}
			if confirm {
				args = append(args, "--confirm")
			}
			if operationID != "" {
				args = append(args, "--operation", operationID)
			}
			data, processErr := invoke(cmd.Context(), args)
			var result struct {
				SchemaVersion int             `json:"schemaVersion"`
				OK            bool            `json:"ok"`
				Code          string          `json:"code"`
				Message       string          `json:"message,omitempty"`
				Result        json.RawMessage `json:"result,omitempty"`
			}
			if len(data) == 0 {
				return integrationFailure("更新器无法运行，请检查安装是否完整。", processErr)
			}
			if err := json.Unmarshal(data, &result); err != nil || result.SchemaVersion != 1 || result.Code == "" {
				return integrationFailure("更新器返回了无效结果。", err)
			}
			if err := json.NewEncoder(out).Encode(result); err != nil {
				return err
			}
			if processErr != nil || !result.OK {
				return integrationFailure("更新操作未完成，请查看返回结果。", processErr)
			}
			return nil
		}
		if operation != "version" {
			command.Flags().StringVar(&channel, "channel", "", "stable 或 preview")
			command.Flags().StringVar(&kind, "package", "", "更新包类型")
			command.Flags().StringVar(&candidate, "candidate", "", "检查结果中的候选编号")
			if operation != "download" && operation != "apply" {
				// Keep invalid calls reaching the updater's existing JSON error.
				command.Flags().Lookup("candidate").Hidden = true
			}
		}
		if operation == "apply" || operation == "recover" {
			command.Flags().BoolVar(&confirm, "confirm", false, "明确确认本次更新或中断收尾")
		}
		if operation == "recover" {
			command.Flags().StringVar(&operationID, "operation", "", "状态结果中的操作编号")
		}
		group.AddCommand(command)
	}
	return group
}
