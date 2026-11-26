package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
	"sidravia/internal/background/daemonctl"
	"sidravia/internal/background/serviceswitch"
	"sidravia/internal/installation/shellintegration"
	"sidravia/internal/installation/userservice"
)

type integrationOperationError struct {
	message string
	cause   error
}

func (e *integrationOperationError) Error() string { return e.message }
func (e *integrationOperationError) Unwrap() error { return e.cause }
func integrationFailure(message string, cause error) error {
	return &integrationOperationError{message: message, cause: cause}
}

func newIntegrationCommand(out io.Writer, invoke func(context.Context, []string) ([]byte, error), identities ...daemonctl.Identity) *cobra.Command {
	group := &cobra.Command{Use: "integration", Short: "管理当前用户的系统集成"}
	run := func(args []string) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			data, processErr := invoke(cmd.Context(), args)
			if len(data) == 0 {
				if processErr != nil {
					return processErr
				}
				return errors.New("维护工具返回了空结果。")
			}
			var r shellintegration.Result
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
	group.AddCommand(&cobra.Command{Use: "status", Short: "查询 Windows 实际系统集成状态（JSON）", Args: cobra.NoArgs, RunE: run([]string{"status"})})
	path := &cobra.Command{Use: "path", Short: "管理 Windows 当前用户 PATH"}
	for _, op := range []string{"enable", "disable"} {
		path.AddCommand(&cobra.Command{Use: op, Short: op + " Windows 用户 PATH", Args: cobra.NoArgs, RunE: run([]string{"path", op})})
	}
	startup := &cobra.Command{Use: "autostart", Short: "管理 Windows 登录时的启动模式"}
	startup.AddCommand(&cobra.Command{Use: "disable", Short: "关闭 Windows 登录自启", Args: cobra.NoArgs, RunE: run([]string{"autostart", "disable"})})
	set := &cobra.Command{Use: "set", Short: "选择 Windows GUI 托盘或后台模式"}
	for _, mode := range []string{"gui", "daemon"} {
		set.AddCommand(&cobra.Command{Use: mode, Short: "设置 Windows 登录启动模式", Args: cobra.NoArgs, RunE: run([]string{"autostart", "set", mode})})
	}
	startup.AddCommand(set)
	group.AddCommand(path, startup)
	if runtime.GOOS == "linux" && len(identities) == 1 {
		service := &cobra.Command{Use: "service", Short: "管理 systemd 用户服务"}
		for _, action := range []string{"status", "register", "replace", "unregister", "start", "stop", "restart", "enable", "disable", "linger-on", "linger-off", "takeover", "release", "recover", "discard", "discard-damaged"} {
			var owner int
			var interrupt bool
			changesMode := action == "takeover" || action == "release" || action == "recover" || action == "discard" || action == "discard-damaged"
			command := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
				var state userservice.ServiceState
				var err error
				if changesMode {
					state, err = serviceswitch.SwitchService(cmd.Context(), identities[0], action, owner, interrupt)
				} else {
					state, err = serviceswitch.ServiceOperation(cmd.Context(), identities[0], action)
				}
				if outputErr := json.NewEncoder(out).Encode(state); outputErr != nil {
					return outputErr
				}
				if err != nil {
					message := "用户服务操作失败，请刷新并检查实际状态。"
					switch {
					case errors.Is(err, userservice.ErrServiceJournalDamaged):
						message = "切换记录已损坏，无法自动恢复。确认放弃后运行 sidraviactl integration service discard-damaged --yes 清除记录。"
					case errors.Is(err, userservice.ErrServiceJournal):
						message = "无法读取或确认切换记录，请检查状态和用户服务目录后重试。"
					case errors.Is(err, userservice.ErrServiceRecovery):
						message = "后台管理切换需要恢复，请运行 sidraviactl integration service recover。"
					case errors.Is(err, userservice.ErrServiceStop):
						message = "后台未正常退出。请检查用户服务的实际状态和日志。"
					case errors.Is(err, userservice.ErrServiceConflict), errors.Is(err, daemonctl.ErrModeConflict):
						message = "用户服务状态或运行身份冲突，请检查原后台状态后再重试。"
					}
					return wrapCommandOperation(integrationFailure(message, err))
				}
				return nil
			}}
			if action == "discard" || action == "discard-damaged" {
				confirmed := false
				run := command.RunE
				if action == "discard" {
					command.Short = "放弃中断的安装更换并解除登记"
				} else {
					command.Short = "清除损坏的切换记录并保留当前登记"
				}
				command.Flags().BoolVar(&confirmed, "yes", false, "确认放弃恢复")
				command.RunE = func(cmd *cobra.Command, args []string) error {
					if !confirmed {
						return wrapCommandOperation(integrationFailure("请用 --yes 确认放弃恢复。", nil))
					}
					return run(cmd, args)
				}
			}
			if action == "takeover" || action == "release" {
				command.Flags().BoolVar(&interrupt, "interrupt-session", false, "确认中断一次性认证")
			}
			if action == "release" {
				command.Flags().IntVar(&owner, "owner-pid", 0, "接管后台的 GUI 进程")
			}
			service.AddCommand(command)
		}
		group.AddCommand(service)
	}
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
