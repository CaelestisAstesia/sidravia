package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
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
			options, err := parseAuthStart(args)
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

func parseAuthStart(args []string) (authStartOptions, error) {
	var options authStartOptions
	var profileSet, usernameSet, sessionSet, configSet, passwordStdinSet bool

	for index := 0; index < len(args); {
		flag, value, hasValue := strings.Cut(args[index], "=")
		if flag == "--interface-id" || flag == "--local-ipv4" {
			if !hasValue {
				if index+1 >= len(args) {
					return authStartOptions{}, errors.New("网卡绑定选项需要值")
				}
				value = args[index+1]
				index++
			}
			if flag == "--interface-id" {
				if options.binding.interfaceSet {
					return authStartOptions{}, errors.New("重复的网卡绑定选项")
				}
				options.binding.interfaceSet = true
				options.binding.interfaceID = value
			} else {
				if options.binding.addressSet {
					return authStartOptions{}, errors.New("重复的网卡绑定选项")
				}
				options.binding.addressSet = true
				options.binding.localIPv4 = value
			}
			index++
			continue
		}
		if flag == "--protocol-override-file" {
			return authStartOptions{}, errors.New("一次性认证不支持协议覆写，请先保存到连接配置")
		}
		switch args[index] {
		case "--profile":
			if profileSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.profileID = args[index+1]
			profileSet = true
			index += 2
		case "--username":
			if usernameSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.username = args[index+1]
			usernameSet = true
			index += 2
		case "--password-stdin":
			if passwordStdinSet {
				return authStartOptions{}, errCommandUsage
			}
			options.passwordStdin = true
			passwordStdinSet = true
			index++
		case "--session":
			if sessionSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.sessionID = args[index+1]
			sessionSet = true
			index += 2
		case "--config":
			if configSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return authStartOptions{}, errCommandUsage
			}
			options.configurationID = args[index+1]
			configSet = true
			index += 2
		default:
			return authStartOptions{}, errCommandUsage
		}
	}

	if _, err := options.binding.policy(false); err != nil {
		return authStartOptions{}, err
	}
	if (sessionSet || configSet) && options.binding.supplied() {
		return authStartOptions{}, errors.New("网卡绑定选项不能与 --session 或 --config 同时使用")
	}
	if sessionSet || configSet {
		if sessionSet && configSet || profileSet || usernameSet || passwordStdinSet {
			return authStartOptions{}, errCommandUsage
		}
		return options, nil
	}
	if !profileSet || !usernameSet {
		return authStartOptions{}, errCommandUsage
	}
	return options, nil
}

func parseSessionID(args []string) (string, error) {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return "", errCommandUsage
	}
	return args[0], nil
}
