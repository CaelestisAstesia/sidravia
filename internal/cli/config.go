package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"sidravia/internal/clientbootstrap"
	"sidravia/internal/ipc/contract"
)

const insecureStorageWarning = "警告：可访问便携目录的用户可能读取或修改认证配置和密码、daemon 运行 token，以及敏感 Trace 日志。\n"

type configCreateOptions struct {
	id, name, profile, username              string
	passwordStdin, allowInsecure             bool
	autoLogin, autoReconnect                 bool
	autoLoginExplicit, autoReconnectExplicit bool
}
type configUpdateOptions struct {
	allowInsecure            bool
	id                       string
	name, profile, username  *string
	autoLogin, autoReconnect *bool
}
type configPasswordOptions struct {
	id                           string
	passwordStdin, allowInsecure bool
}

func newConfigCommand(deps commandDependencies) *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "管理认证配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return wrapCommandOperation(renderHelpCompletion(cmd)) }}
	root.AddCommand(newListCommand("list", "列出认证配置", deps.configList))
	root.AddCommand(&cobra.Command{Use: "show <configuration-id>", Short: "显示认证配置", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return wrapCommandOperation(deps.configShow(args[0])) }})

	var create configCreateOptions
	createCommand := &cobra.Command{Use: "create", Short: "创建认证配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		create.autoLoginExplicit = cmd.Flags().Changed("auto-login")
		create.autoReconnectExplicit = cmd.Flags().Changed("auto-reconnect")
		return wrapCommandOperation(deps.configCreate(create))
	}}
	createCommand.Flags().StringVar(&create.id, "id", "", "配置 ID")
	createCommand.Flags().StringVar(&create.name, "name", "", "显示名称")
	createCommand.Flags().StringVar(&create.profile, "profile", "", "Profile ID")
	createCommand.Flags().StringVar(&create.username, "username", "", "账号")
	createCommand.Flags().BoolVar(&create.passwordStdin, "password-stdin", false, "从 stdin 读取密码")
	createCommand.Flags().BoolVar(&create.allowInsecure, "allow-insecure-storage", false, "允许未保护存储")
	createCommand.Flags().BoolVar(&create.autoLogin, "auto-login", false, "自动登录")
	createCommand.Flags().BoolVar(&create.autoReconnect, "auto-reconnect", true, "自动重连")

	var update configUpdateOptions
	updateCommand := &cobra.Command{Use: "update <configuration-id>", Short: "更新认证配置", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		update.id = args[0]
		if cmd.Flags().Changed("name") {
			value, _ := cmd.Flags().GetString("name")
			update.name = &value
		}
		if cmd.Flags().Changed("profile") {
			value, _ := cmd.Flags().GetString("profile")
			update.profile = &value
		}
		if cmd.Flags().Changed("username") {
			value, _ := cmd.Flags().GetString("username")
			update.username = &value
		}
		if cmd.Flags().Changed("auto-login") {
			value, _ := cmd.Flags().GetString("auto-login")
			if value == "true" {
				b := true
				update.autoLogin = &b
			} else if value == "false" {
				b := false
				update.autoLogin = &b
			} else {
				return errors.New("--auto-login 需要 true 或 false")
			}
		}
		if cmd.Flags().Changed("auto-reconnect") {
			value, _ := cmd.Flags().GetString("auto-reconnect")
			if value == "true" {
				b := true
				update.autoReconnect = &b
			} else if value == "false" {
				b := false
				update.autoReconnect = &b
			} else {
				return errors.New("--auto-reconnect 需要 true 或 false")
			}
		}
		if update.name == nil && update.profile == nil && update.username == nil && update.autoLogin == nil && update.autoReconnect == nil {
			// A bare update is completed by the interactive path after it has
			// fetched the current values.
		}
		return wrapCommandOperation(deps.configUpdate(update))
	}}
	updateCommand.Flags().BoolVar(&update.allowInsecure, "allow-insecure-storage", false, "允许未保护存储")
	updateCommand.Flags().String("name", "", "显示名称")
	updateCommand.Flags().String("profile", "", "Profile ID")
	updateCommand.Flags().String("username", "", "账号")
	updateCommand.Flags().String("auto-login", "", "自动登录（true 或 false）")
	updateCommand.Flags().String("auto-reconnect", "", "自动重连（true 或 false）")

	var password configPasswordOptions
	passwordCommand := &cobra.Command{Use: "set-password <configuration-id>", Short: "更新认证密码", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		password.id = args[0]
		return wrapCommandOperation(deps.configSetPassword(password))
	}}
	passwordCommand.Flags().BoolVar(&password.passwordStdin, "password-stdin", false, "从 stdin 读取密码")
	passwordCommand.Flags().BoolVar(&password.allowInsecure, "allow-insecure-storage", false, "允许未保护存储")

	var yes, allowRemove bool
	removeCommand := &cobra.Command{Use: "remove <configuration-id>", Short: "删除认证配置", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return wrapCommandOperation(deps.configRemove(args[0], yes, allowRemove))
	}}
	removeCommand.Flags().BoolVar(&allowRemove, "allow-insecure-storage", false, "允许未保护存储")
	removeCommand.Flags().BoolVar(&yes, "yes", false, "确认删除")
	root.AddCommand(createCommand, updateCommand, passwordCommand, removeCommand)
	return root
}

func configList(identity clientbootstrap.Identity) error {
	return runConfigList(defaultReadOnlyAuthDependencies(identity))
}
func configShow(identity clientbootstrap.Identity, id string) error {
	return runConfigShow(id, defaultReadOnlyAuthDependencies(identity))
}

func defaultReadOnlyAuthDependencies(identity clientbootstrap.Identity) authDependencies {
	deps := defaultAuthDependencies(identity)
	deps.connection = defaultReadOnlyDaemonConnectionDependencies(identity)
	return deps
}
func configCreate(identity clientbootstrap.Identity, options configCreateOptions) error {
	return runConfigCreate(options, defaultAuthDependencies(identity))
}
func configUpdate(identity clientbootstrap.Identity, options configUpdateOptions) error {
	return runConfigUpdate(options, defaultAuthDependencies(identity))
}
func configSetPassword(identity clientbootstrap.Identity, options configPasswordOptions) error {
	return runConfigSetPassword(options, defaultAuthDependencies(identity))
}
func configRemove(identity clientbootstrap.Identity, id string, yes, allow bool) error {
	return runConfigRemove(id, yes, defaultAuthDependencies(identity), allow)
}

func callConfiguration(deps authDependencies, connection daemonClient, method string, payload any) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, wrapSafeOperation("编码配置请求", err)
	}
	// Clear the CLI-owned wire bytes after the synchronous call on all exits.
	// This does not erase immutable password strings or transport copies.
	defer clear(data)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := connection.Call(ctx, method, data)
	if err != nil {
		return nil, wrapSafeOperation("调用配置操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		if code == contract.ErrorCodeInsecureStorageConfirmationRequired {
			return nil, errors.New("存储未受保护；请确认警告后使用 --allow-insecure-storage")
		}
		return nil, errors.New(ipcErrorText(code))
	}
	return response.Result, nil
}

func runConfigList(deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationList, struct{}{})
		if err != nil {
			return err
		}
		result, err := decodeConfigurationList(raw)
		if err != nil {
			return wrapSafeOperation("解码配置列表", err)
		}
		return writeConfigurationList(deps.stdout, result)
	})
}
func runConfigShow(id string, deps authDependencies) error {
	return withAuthClient(deps, func(connection daemonClient) error {
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationGet, contract.ConfigurationIDPayload{ConfigurationID: id})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return wrapSafeOperation("解码配置", err)
		}
		return writeConfiguration(deps.stdout, result)
	})
}
func runConfigCreate(options configCreateOptions, deps authDependencies) error {
	interactive := deps.inputIsConsole != nil && deps.inputIsConsole(deps.stdin)
	if !interactive && (options.id == "" || options.profile == "" || options.username == "" || !options.passwordStdin) {
		return errors.New("非交互式创建需要 --id、--profile、--username 和 --password-stdin")
	}
	if options.allowInsecure {
		if err := writeAll(deps.stderr, insecureStorageWarning); err != nil {
			return wrapSafeOperation("写入存储警告", err)
		}
	}
	return withAuthClient(deps, func(connection daemonClient) error {
		var err error
		if interactive {
			if options.id == "" {
				options.id, err = readPromptLine(deps.stdin, deps.stderr, "配置 ID：")
			}
			if err == nil && options.profile == "" {
				options.profile, err = selectProfile(deps, connection)
			}
			if err == nil && options.username == "" {
				options.username, err = readPromptLine(deps.stdin, deps.stderr, "账号：")
			}
			if err == nil && !options.autoLoginExplicit {
				options.autoLogin, err = readConfirmation(deps.stdin, deps.stderr, "自动登录？[y/N] ")
			}
			if err == nil && !options.autoReconnectExplicit {
				options.autoReconnect, err = readBooleanPrompt(deps.stdin, deps.stderr, "自动重连？[Y/n] ", true)
			}
		}
		if err != nil {
			return err
		}
		if options.id == "" || options.profile == "" || options.username == "" {
			return errors.New("配置输入不能为空")
		}
		var password string
		if options.passwordStdin {
			password, err = deps.readStdinPassword(deps.stdin)
		} else {
			password, err = deps.readInteractivePassword(deps.stdin, deps.stderr)
		}
		if err != nil {
			return err
		}
		if interactive && !options.allowInsecure {
			raw, listErr := callConfiguration(deps, connection, contract.MethodConfigurationList, struct{}{})
			if listErr != nil {
				return listErr
			}
			list, decodeErr := decodeConfigurationList(raw)
			if decodeErr != nil {
				return wrapSafeOperation("解码配置列表", decodeErr)
			}
			if list.StorageProtection == "unprotected" {
				if err := writeAll(deps.stderr, insecureStorageWarning); err != nil {
					return wrapSafeOperation("写入存储警告", err)
				}
				options.allowInsecure, err = readConfirmation(deps.stdin, deps.stderr, "仍要保存？[y/N] ")
				if err != nil || !options.allowInsecure {
					if err != nil {
						return err
					}
					return errors.New("操作已取消")
				}
			}
		}
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationCreate, contract.ConfigurationCreatePayload{
			ConfigurationID: options.id, DisplayName: options.name, InstitutionProfileID: options.profile,
			Username: options.username, Password: password, AllowInsecureStorage: options.allowInsecure,
			AutoLogin: options.autoLogin, AutoReconnect: options.autoReconnect,
		})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return wrapSafeOperation("解码配置", err)
		}
		return writeConfiguration(deps.stdout, result)
	})
}
func runConfigUpdate(options configUpdateOptions, deps authDependencies) error {
	if options.allowInsecure {
		if err := writeAll(deps.stderr, insecureStorageWarning); err != nil {
			return wrapSafeOperation("写入存储警告", err)
		}
	}
	return withAuthClient(deps, func(connection daemonClient) error {
		if options.name == nil && options.profile == nil && options.username == nil && options.autoLogin == nil && options.autoReconnect == nil {
			if deps.inputIsConsole == nil || !deps.inputIsConsole(deps.stdin) {
				return errors.New("非交互式更新需要至少一个更新选项")
			}
			raw, err := callConfiguration(deps, connection, contract.MethodConfigurationGet, contract.ConfigurationIDPayload{ConfigurationID: options.id})
			if err != nil {
				return err
			}
			current, err := decodeConfiguration(raw)
			if err != nil {
				return wrapSafeOperation("解码配置", err)
			}
			name, err := readPromptLine(deps.stdin, deps.stderr, "显示名称（空=保留，-=清除）：")
			if err != nil {
				return err
			}
			if name == "-" {
				empty := ""
				options.name = &empty
			} else if name != "" && name != current.DisplayName {
				options.name = &name
			}
			if options.name == nil {
				return errors.New("没有要更新的字段")
			}
		}
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationUpdate, contract.ConfigurationUpdatePayload{
			ConfigurationID: options.id, DisplayName: options.name, InstitutionProfileID: options.profile, Username: options.username, AllowInsecureStorage: options.allowInsecure,
			AutoLogin: options.autoLogin, AutoReconnect: options.autoReconnect,
		})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return wrapSafeOperation("解码配置", err)
		}
		if err := writeConfiguration(deps.stdout, result); err != nil {
			return err
		}
		return writeAll(deps.stdout, "认证参数变更已清理关联旧会话；下次连接使用新配置。\n")
	})
}
func runConfigSetPassword(options configPasswordOptions, deps authDependencies) error {
	interactive := deps.inputIsConsole != nil && deps.inputIsConsole(deps.stdin)
	if !options.passwordStdin && !interactive {
		return errors.New("非交互式设置密码需要 --password-stdin")
	}
	if options.allowInsecure {
		if err := writeAll(deps.stderr, insecureStorageWarning); err != nil {
			return wrapSafeOperation("写入存储警告", err)
		}
	}
	var password string
	var err error
	if options.passwordStdin {
		password, err = deps.readStdinPassword(deps.stdin)
	} else {
		password, err = deps.readInteractivePassword(deps.stdin, deps.stderr)
	}
	if err != nil {
		return err
	}
	return withAuthClient(deps, func(connection daemonClient) error {
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationSetPassword, contract.ConfigurationSetPasswordPayload{ConfigurationID: options.id, Password: password, AllowInsecureStorage: options.allowInsecure})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return wrapSafeOperation("解码配置", err)
		}
		if err := writeConfiguration(deps.stdout, result); err != nil {
			return err
		}
		return writeAll(deps.stdout, "认证参数变更已清理关联旧会话；下次连接使用新配置。\n")
	})
}
func runConfigRemove(id string, yes bool, deps authDependencies, allow ...bool) error {
	permitted := len(allow) > 0 && allow[0]
	if permitted {
		if err := writeAll(deps.stderr, insecureStorageWarning); err != nil {
			return wrapSafeOperation("写入存储警告", err)
		}
	}
	if !yes {
		if deps.inputIsConsole == nil || !deps.inputIsConsole(deps.stdin) {
			return errors.New("非交互式删除需要 --yes")
		}
		prompt := "确认删除配置 “" + sanitizeDynamicText(id) + "”？关联 Session、认证配置和已保存密码将被删除。[y/N] "
		confirmed, err := readConfirmation(deps.stdin, deps.stderr, prompt)
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("操作已取消")
		}
	}
	return withAuthClient(deps, func(connection daemonClient) error {
		raw, err := callConfiguration(deps, connection, contract.MethodConfigurationRemove, contract.ConfigurationRemovePayload{ConfigurationID: id, AllowInsecureStorage: permitted})
		if err != nil {
			return err
		}
		var result contract.ConfigurationRemoveResult
		if err := decodeStrictCLI(raw, &result); err != nil || result.ConfigurationID == "" || result.Status != "removed" {
			return wrapSafeOperation("解码配置删除结果", err)
		}
		return writeAll(deps.stdout, "配置已删除："+sanitizeDynamicText(result.ConfigurationID)+"\n")
	})
}

func decodeStrictCLI(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("结果包含多余数据")
	}
	return nil
}
func decodeConfiguration(data []byte) (contract.ConfigurationResult, error) {
	var result contract.ConfigurationResult
	if err := decodeStrictCLI(data, &result); err != nil {
		return result, err
	}
	if result.ConfigurationID == "" || result.InstitutionProfileID == "" || result.AuthenticationProtocolID == "" || result.Username == "" || !result.CredentialStored || result.StorageProtection != "protected" && result.StorageProtection != "unprotected" {
		return contract.ConfigurationResult{}, fmt.Errorf("配置结果无效")
	}
	return result, nil
}
func decodeConfigurationList(data []byte) (contract.ConfigurationListResult, error) {
	var result contract.ConfigurationListResult
	if err := decodeStrictCLI(data, &result); err != nil {
		return result, err
	}
	if result.StorageProtection != "protected" && result.StorageProtection != "unprotected" || result.Configurations == nil {
		return result, fmt.Errorf("配置列表无效")
	}
	for _, value := range result.Configurations {
		if _, err := decodeConfigurationMust(value); err != nil {
			return result, err
		}
	}
	return result, nil
}
func decodeConfigurationMust(value contract.ConfigurationResult) (contract.ConfigurationResult, error) {
	data, _ := json.Marshal(value)
	return decodeConfiguration(data)
}
