package configcmd

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"sidravia/internal/background/daemonctl"
	"sidravia/internal/cli/internal/bindingargs"
	"sidravia/internal/cli/internal/daemonconn"
	"sidravia/internal/cli/internal/terminal"
	"sidravia/internal/connection/credential/input"
	"sidravia/internal/ipc/contract"
)

const insecureStorageWarning = "警告：可访问便携目录的用户可能读取或修改认证配置和密码、daemon 运行 token，以及敏感 Trace 日志。\n"

type CreateOptions struct {
	Binding                                  bindingargs.Flags
	ID, Name, Profile, Username              string
	ProtocolOverrideFile                     string
	ProtocolOverrideFileSet                  bool
	PasswordStdin, AllowInsecure             bool
	AutoLogin, AutoReconnect                 bool
	AutoLoginExplicit, AutoReconnectExplicit bool
}
type UpdateOptions struct {
	Binding                  bindingargs.Flags
	AllowInsecure            bool
	ID                       string
	ProtocolOverrideFile     string
	ProtocolOverrideFileSet  bool
	ClearProtocolOverride    bool
	Name, Profile, Username  *string
	AutoLogin, AutoReconnect *bool
}

type PasswordOptions struct {
	ID                           string
	PasswordStdin, AllowInsecure bool
}

type dependencies struct {
	connection daemonconn.Connection
	terminal.IO
}

func commandDependencies(connection daemonconn.Connection) dependencies {
	return dependencies{connection: connection, IO: terminal.StandardIO()}
}

func List(identity daemonctl.Identity) error {
	return runConfigList(commandDependencies(daemonconn.Existing(identity)))
}
func Show(identity daemonctl.Identity, id string) error {
	return runConfigShow(id, commandDependencies(daemonconn.Existing(identity)))
}

func Create(identity daemonctl.Identity, options CreateOptions) error {
	return runConfigCreate(options, commandDependencies(daemonconn.MayStart(identity)))
}
func Update(identity daemonctl.Identity, options UpdateOptions) error {
	return runConfigUpdate(options, commandDependencies(daemonconn.MayStart(identity)))
}
func SetPassword(identity daemonctl.Identity, options PasswordOptions) error {
	return runConfigSetPassword(options, commandDependencies(daemonconn.MayStart(identity)))
}
func Remove(identity daemonctl.Identity, id string, yes, allow bool) error {
	return runConfigRemove(id, yes, commandDependencies(daemonconn.MayStart(identity)), allow)
}

func callConfiguration(connection daemonconn.Client, method string, payload any) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, terminal.Failure("编码配置请求", err)
	}
	// Clear the CLI-owned wire bytes after the synchronous call on all exits.
	// This does not erase immutable password strings or transport copies.
	defer clear(data)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := connection.Call(ctx, method, data)
	if err != nil {
		return nil, terminal.Failure("调用配置操作", err)
	}
	if !response.OK {
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		if code == contract.ErrorCodeInsecureStorageConfirmationRequired {
			return nil, errors.New("存储未受保护；请确认警告后使用 --allow-insecure-storage")
		}
		return nil, errors.New(terminal.IPCErrorText(code))
	}
	return response.Result, nil
}

func runConfigList(deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		var result contract.ConfigurationListResult
		cursors := make(map[string]bool)
		cursor := ""
		for {
			raw, err := callConfiguration(connection, contract.MethodConfigurationList, contract.ConfigurationListPayload{Cursor: cursor})
			if err != nil {
				return err
			}
			page, err := decodeConfigurationList(raw)
			if err != nil {
				return terminal.Failure("解码配置列表", err)
			}
			result.StorageProtection = page.StorageProtection
			result.Configurations = append(result.Configurations, page.Configurations...)
			cursor = page.NextCursor
			if cursor == "" {
				return terminal.WriteConfigurationList(deps.Stdout, result)
			}
			if cursors[cursor] {
				return errors.New("配置列表分页结果无效")
			}
			cursors[cursor] = true
		}
	})
}
func runConfigShow(id string, deps dependencies) error {
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		raw, err := callConfiguration(connection, contract.MethodConfigurationGet, contract.ConfigurationIDPayload{ConfigurationID: id})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return terminal.Failure("解码配置", err)
		}
		return terminal.WriteConfiguration(deps.Stdout, result)
	})
}
func runConfigCreate(options CreateOptions, deps dependencies) error {
	if options.Username != "" {
		if err := input.ValidateNewText(options.Username); err != nil {
			return err
		}
	}
	protocolOverride, err := loadProtocolContextOverride(options.ProtocolOverrideFile, options.ProtocolOverrideFileSet)
	if err != nil {
		return err
	}
	policy, err := options.Binding.Policy(false)
	if err != nil {
		return err
	}
	interactive := deps.InputIsConsole != nil && deps.InputIsConsole(deps.Stdin)
	if !interactive && (options.ID == "" || options.Profile == "" || options.Username == "" || !options.PasswordStdin) {
		return errors.New("非交互式创建需要 --id、--profile、--username 和 --password-stdin")
	}
	if options.AllowInsecure {
		if err := terminal.Write(deps.Stderr, insecureStorageWarning); err != nil {
			return terminal.Failure("写入存储警告", err)
		}
	}
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		var err error
		if interactive {
			if options.ID == "" {
				options.ID, err = terminal.ReadPrompt(deps.Stdin, deps.Stderr, "配置 ID：")
			}
			if err == nil && options.Profile == "" {
				options.Profile, err = selectProfile(deps, connection)
			}
			if err == nil && options.Username == "" {
				options.Username, err = terminal.ReadCredential(deps.Stdin, deps.Stderr, "账号：")
			}
			if err == nil && !options.AutoLoginExplicit {
				options.AutoLogin, err = terminal.Confirm(deps.Stdin, deps.Stderr, "自动登录？[y/N] ")
			}
			if err == nil && !options.AutoReconnectExplicit {
				options.AutoReconnect, err = terminal.ReadBool(deps.Stdin, deps.Stderr, "自动重连？[Y/n] ", true)
			}
		}
		if err != nil {
			return err
		}
		if options.ID == "" || options.Profile == "" || options.Username == "" {
			return errors.New("配置输入不能为空")
		}
		if err := input.ValidateNewText(options.Username); err != nil {
			return err
		}
		if interactive && !options.Binding.Supplied() {
			selected, err := selectNetworkBinding(deps, connection)
			if err != nil {
				return err
			}
			policy = &selected
		}
		password, err := deps.ValidatedPassword(options.PasswordStdin)
		if err != nil {
			return err
		}
		if interactive && !options.AllowInsecure {
			raw, listErr := callConfiguration(connection, contract.MethodConfigurationList, struct{}{})
			if listErr != nil {
				return listErr
			}
			list, decodeErr := decodeConfigurationList(raw)
			if decodeErr != nil {
				return terminal.Failure("解码配置列表", decodeErr)
			}
			if list.StorageProtection == "unprotected" {
				if err := terminal.Write(deps.Stderr, insecureStorageWarning); err != nil {
					return terminal.Failure("写入存储警告", err)
				}
				options.AllowInsecure, err = terminal.Confirm(deps.Stdin, deps.Stderr, "仍要保存？[y/N] ")
				if err != nil || !options.AllowInsecure {
					if err != nil {
						return err
					}
					return errors.New("操作已取消")
				}
			}
		}
		raw, err := callConfiguration(connection, contract.MethodConfigurationCreate, contract.ConfigurationCreatePayload{
			NetworkBindingPolicy: *policy, ConfigurationID: options.ID, DisplayName: options.Name, InstitutionProfileID: options.Profile,
			Username: options.Username, Password: password, AllowInsecureStorage: options.AllowInsecure,
			AutoLogin: options.AutoLogin, AutoReconnect: options.AutoReconnect,
			ProtocolContextOverride: protocolOverride,
		})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return terminal.Failure("解码配置", err)
		}
		return terminal.WriteConfiguration(deps.Stdout, result)
	})
}
func runConfigUpdate(options UpdateOptions, deps dependencies) error {
	if options.Username != nil {
		if err := input.ValidateNewText(*options.Username); err != nil {
			return err
		}
	}
	if options.ClearProtocolOverride && options.ProtocolOverrideFileSet {
		return errors.New("协议上下文 Override 文件与清除选项不能同时使用")
	}
	protocolOverride, err := loadProtocolContextOverride(options.ProtocolOverrideFile, options.ProtocolOverrideFileSet)
	if err != nil {
		return err
	}
	if options.ClearProtocolOverride {
		protocolOverride = json.RawMessage("null")
	}
	policy, err := options.Binding.Policy(true)
	if err != nil {
		return err
	}
	if options.AllowInsecure {
		if err := terminal.Write(deps.Stderr, insecureStorageWarning); err != nil {
			return terminal.Failure("写入存储警告", err)
		}
	}
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		// Bare updates prompt only after the persisted record is read and validated.
		if options.Name == nil && options.Profile == nil && options.Username == nil && options.AutoLogin == nil && options.AutoReconnect == nil && policy == nil && !options.ProtocolOverrideFileSet && !options.ClearProtocolOverride {
			if deps.InputIsConsole == nil || !deps.InputIsConsole(deps.Stdin) {
				return errors.New("非交互式更新需要至少一个更新选项")
			}
			raw, err := callConfiguration(connection, contract.MethodConfigurationGet, contract.ConfigurationIDPayload{ConfigurationID: options.ID})
			if err != nil {
				return err
			}
			current, err := decodeConfiguration(raw)
			if err != nil {
				return terminal.Failure("解码配置", err)
			}
			name, err := terminal.ReadPrompt(deps.Stdin, deps.Stderr, "显示名称（空=保留，-=清除）：")
			if err != nil {
				return err
			}
			if name == "-" {
				empty := ""
				options.Name = &empty
			} else if name != "" && name != current.DisplayName {
				options.Name = &name
			}
			if options.Name == nil {
				return errors.New("没有要更新的字段")
			}
		}
		raw, err := callConfiguration(connection, contract.MethodConfigurationUpdate, contract.ConfigurationUpdatePayload{
			NetworkBindingPolicy: policy, ConfigurationID: options.ID, DisplayName: options.Name, InstitutionProfileID: options.Profile, Username: options.Username, AllowInsecureStorage: options.AllowInsecure,
			AutoLogin: options.AutoLogin, AutoReconnect: options.AutoReconnect, ProtocolContextOverride: protocolOverride,
		})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return terminal.Failure("解码配置", err)
		}
		if err := terminal.WriteConfiguration(deps.Stdout, result); err != nil {
			return err
		}
		return terminal.Write(deps.Stdout, "认证参数变更已清理关联旧会话；下次连接使用新配置。\n")
	})
}
func runConfigSetPassword(options PasswordOptions, deps dependencies) error {
	interactive := deps.InputIsConsole != nil && deps.InputIsConsole(deps.Stdin)
	if !options.PasswordStdin && !interactive {
		return errors.New("非交互式设置密码需要 --password-stdin")
	}
	if options.AllowInsecure {
		if err := terminal.Write(deps.Stderr, insecureStorageWarning); err != nil {
			return terminal.Failure("写入存储警告", err)
		}
	}
	password, err := deps.ValidatedPassword(options.PasswordStdin)
	if err != nil {
		return err
	}
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		raw, err := callConfiguration(connection, contract.MethodConfigurationSetPassword, contract.ConfigurationSetPasswordPayload{ConfigurationID: options.ID, Password: password, AllowInsecureStorage: options.AllowInsecure})
		if err != nil {
			return err
		}
		result, err := decodeConfiguration(raw)
		if err != nil {
			return terminal.Failure("解码配置", err)
		}
		if err := terminal.WriteConfiguration(deps.Stdout, result); err != nil {
			return err
		}
		return terminal.Write(deps.Stdout, "认证参数变更已清理关联旧会话；下次连接使用新配置。\n")
	})
}
func runConfigRemove(id string, yes bool, deps dependencies, allow ...bool) error {
	permitted := len(allow) > 0 && allow[0]
	if permitted {
		if err := terminal.Write(deps.Stderr, insecureStorageWarning); err != nil {
			return terminal.Failure("写入存储警告", err)
		}
	}
	if !yes && (deps.InputIsConsole == nil || !deps.InputIsConsole(deps.Stdin)) {
		return errors.New("非交互式删除需要 --yes")
	}
	return daemonconn.With(deps.connection, func(connection daemonconn.Client) error {
		raw, err := callConfiguration(connection, contract.MethodConfigurationGet, contract.ConfigurationIDPayload{ConfigurationID: id})
		if err != nil {
			return err
		}
		current, err := decodeConfiguration(raw)
		if err != nil {
			return terminal.Failure("解码配置", err)
		}
		if !yes {
			prompt := "确认删除配置 “" + terminal.Sanitize(id) + "”？关联 Session、认证配置和已保存密码将被删除。[y/N] "
			confirmed, err := terminal.Confirm(deps.Stdin, deps.Stderr, prompt)
			if err != nil {
				return err
			}
			if !confirmed {
				return errors.New("操作已取消")
			}
		}
		raw, err = callConfiguration(connection, contract.MethodConfigurationRemove, contract.ConfigurationRemovePayload{
			ConfigurationID: id, ExpectedVersion: current.Version, AllowInsecureStorage: permitted,
		})
		if err != nil {
			return err
		}
		result, err := contract.DecodeConfigurationRemoveResult(raw, id)
		if err != nil {
			return terminal.Failure("解码配置删除结果", err)
		}
		return terminal.Write(deps.Stdout, "配置已删除："+terminal.Sanitize(result.ConfigurationID)+"\n")
	})
}

func decodeConfiguration(data []byte) (contract.ConfigurationResult, error) {
	return contract.DecodeConfigurationResult(data)
}
func decodeConfigurationList(data []byte) (contract.ConfigurationListResult, error) {
	return contract.DecodeConfigurationListResult(data)
}
