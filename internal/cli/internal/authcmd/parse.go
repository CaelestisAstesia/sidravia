package authcmd

import (
	"errors"
	"strings"
)

var errUsage = errors.New("invalid auth start arguments")

func ParseStart(args []string) (StartOptions, error) {
	var options StartOptions
	var profileSet, usernameSet, sessionSet, configSet, passwordStdinSet bool

	for index := 0; index < len(args); {
		flag, value, hasValue := strings.Cut(args[index], "=")
		if flag == "--interface-id" || flag == "--local-ipv4" {
			if !hasValue {
				if index+1 >= len(args) {
					return StartOptions{}, errors.New("网卡绑定选项需要值")
				}
				value = args[index+1]
				index++
			}
			if flag == "--interface-id" {
				if options.Binding.InterfaceSet {
					return StartOptions{}, errors.New("重复的网卡绑定选项")
				}
				options.Binding.InterfaceSet = true
				options.Binding.InterfaceID = value
			} else {
				if options.Binding.AddressSet {
					return StartOptions{}, errors.New("重复的网卡绑定选项")
				}
				options.Binding.AddressSet = true
				options.Binding.LocalIPv4 = value
			}
			index++
			continue
		}
		if flag == "--protocol-override-file" {
			return StartOptions{}, errors.New("一次性认证不支持协议覆写，请先保存到连接配置")
		}
		switch args[index] {
		case "--profile":
			if profileSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return StartOptions{}, errUsage
			}
			options.ProfileID = args[index+1]
			profileSet = true
			index += 2
		case "--username":
			if usernameSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return StartOptions{}, errUsage
			}
			options.Username = args[index+1]
			usernameSet = true
			index += 2
		case "--password-stdin":
			if passwordStdinSet {
				return StartOptions{}, errUsage
			}
			options.PasswordStdin = true
			passwordStdinSet = true
			index++
		case "--session":
			if sessionSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return StartOptions{}, errUsage
			}
			options.SessionID = args[index+1]
			sessionSet = true
			index += 2
		case "--config":
			if configSet || index+1 >= len(args) || args[index+1] == "" || strings.HasPrefix(args[index+1], "-") {
				return StartOptions{}, errUsage
			}
			options.ConfigurationID = args[index+1]
			configSet = true
			index += 2
		default:
			return StartOptions{}, errUsage
		}
	}

	if _, err := options.Binding.Policy(false); err != nil {
		return StartOptions{}, err
	}
	if (sessionSet || configSet) && options.Binding.Supplied() {
		return StartOptions{}, errors.New("网卡绑定选项不能与 --session 或 --config 同时使用")
	}
	if sessionSet || configSet {
		if sessionSet && configSet || profileSet || usernameSet || passwordStdinSet {
			return StartOptions{}, errUsage
		}
		return options, nil
	}
	if !profileSet || !usernameSet {
		return StartOptions{}, errUsage
	}
	return options, nil
}
