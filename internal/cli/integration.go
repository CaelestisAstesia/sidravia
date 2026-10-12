package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"sidravia/internal/installationmanager"
)

type managerOutput struct{ bytes.Buffer }

type integrationOperationError struct {
	message string
	cause   error
}

func (e *integrationOperationError) Error() string { return e.message }
func (e *integrationOperationError) Unwrap() error { return e.cause }
func integrationFailure(message string, cause error) error {
	return &integrationOperationError{message: message, cause: cause}
}

func (b *managerOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, errors.New("manager output too large")
	}
	return b.Buffer.Write(p)
}

func invokeInstallationManager(ctx context.Context, args []string) ([]byte, error) {
	if runtime.GOOS != "windows" {
		return nil, errors.New("当前平台尚不支持安装集成管理。")
	}
	path, err := os.Executable()
	if err != nil {
		return nil, integrationFailure("无法确定维护工具的位置。", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(path), "sidraviamgr.exe"), args...)
	var output managerOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("维护操作已取消或超时：%w", ctx.Err())
	}
	if err != nil && output.Len() == 0 {
		return nil, integrationFailure("维护工具无法运行，请检查安装是否完整。", err)
	}
	return output.Bytes(), err
}

func newIntegrationCommand(out io.Writer, invoke func(context.Context, []string) ([]byte, error)) *cobra.Command {
	group := &cobra.Command{Use: "integration", Short: "查询或修改当前用户 PATH 与开机自启"}
	run := func(args []string) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			data, processErr := invoke(cmd.Context(), args)
			if len(data) == 0 {
				if processErr != nil {
					return processErr
				}
				return errors.New("维护工具返回了空结果。")
			}
			var r installationmanager.Result
			if err := json.Unmarshal(data, &r); err != nil || r.Version != 1 || r.Code == "" {
				return integrationFailure("维护工具返回了无效结果。", err)
			}
			// Encode only our public result fields, never arbitrary child output.
			if err := json.NewEncoder(out).Encode(r); err != nil {
				return err
			}
			if processErr != nil || !r.OK {
				return integrationFailure(fmt.Sprintf("安装管理操作失败（%s）。", safeManagerCode(r.Code)), processErr)
			}
			return nil
		}
	}
	group.AddCommand(&cobra.Command{Use: "status", Short: "查询实际系统集成状态（JSON）", Args: cobra.NoArgs, RunE: run([]string{"status"})})
	path := &cobra.Command{Use: "path", Short: "管理当前用户 PATH"}
	for _, op := range []string{"enable", "disable"} {
		path.AddCommand(&cobra.Command{Use: op, Short: op + " PATH", Args: cobra.NoArgs, RunE: run([]string{"path", op})})
	}
	startup := &cobra.Command{Use: "autostart", Short: "管理下次登录时的启动模式"}
	startup.AddCommand(&cobra.Command{Use: "disable", Short: "关闭开机自启", Args: cobra.NoArgs, RunE: run([]string{"autostart", "disable"})})
	set := &cobra.Command{Use: "set", Short: "选择 GUI 托盘或后台模式"}
	for _, mode := range []string{"gui", "daemon"} {
		set.AddCommand(&cobra.Command{Use: mode, Short: mode, Args: cobra.NoArgs, RunE: run([]string{"autostart", "set", mode})})
	}
	startup.AddCommand(set)
	group.AddCommand(path, startup)
	return group
}
func safeManagerCode(code string) string {
	switch code {
	case "unsupported", "invalid_arguments", "invalid_directory", "path_read_failed", "path_write_failed", "path_verify_failed", "integration_lock_failed", "scheduled_task_query_failed", "scheduled_task_conflict", "scheduled_task_verify_failed", "scheduled_task_operation_failed", "scheduled_task_response_invalid", "unsupported_release_script_host", "autostart_target_missing":
		return code
	default:
		return "integration_failed"
	}
}
