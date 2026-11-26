package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"sidravia/internal/cli/internal/configcmd"
)

func newConfigCommand(deps commandDependencies) *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "管理认证配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return wrapCommandOperation(renderHelpCompletion(cmd)) }}
	root.AddCommand(newNoArgsCommand("list", "列出认证配置", deps.configList))
	root.AddCommand(&cobra.Command{Use: "show <configuration-id>", Short: "显示认证配置", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return wrapCommandOperation(deps.configShow(args[0])) }})

	var create configcmd.CreateOptions
	createCommand := &cobra.Command{Use: "create", Short: "创建认证配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		create.ProtocolOverrideFileSet = cmd.Flags().Changed("protocol-override-file")
		if create.ProtocolOverrideFileSet && (create.ProtocolOverrideFile == "" || strings.ContainsRune(create.ProtocolOverrideFile, 0)) {
			return errCommandUsage
		}
		create.Binding.InterfaceSet = cmd.Flags().Changed("interface-id")
		create.Binding.AddressSet = cmd.Flags().Changed("local-ipv4")
		if _, err := create.Binding.Policy(false); err != nil {
			return err
		}
		create.AutoLoginExplicit = cmd.Flags().Changed("auto-login")
		create.AutoReconnectExplicit = cmd.Flags().Changed("auto-reconnect")
		return wrapCommandOperation(deps.configCreate(create))
	}}
	createCommand.Flags().StringVar(&create.Binding.InterfaceID, "interface-id", "", "真实网卡 ID（须同时提供 --local-ipv4）")
	createCommand.Flags().StringVar(&create.Binding.LocalIPv4, "local-ipv4", "", "本地 IPv4（须同时提供 --interface-id）")
	createCommand.Flags().StringVar(&create.ID, "id", "", "配置 ID")
	createCommand.Flags().StringVar(&create.Name, "name", "", "显示名称")
	createCommand.Flags().StringVar(&create.Profile, "profile", "", "Profile ID")
	createCommand.Flags().StringVar(&create.Username, "username", "", "账号")
	createCommand.Flags().BoolVar(&create.PasswordStdin, "password-stdin", false, "从 stdin 读取密码")
	createCommand.Flags().BoolVar(&create.AllowInsecure, "allow-insecure-storage", false, "允许未保护存储")
	createCommand.Flags().BoolVar(&create.AutoLogin, "auto-login", false, "自动登录")
	createCommand.Flags().BoolVar(&create.AutoReconnect, "auto-reconnect", true, "自动重连")
	addProtocolOverrideFileFlag(createCommand.Flags(), &create.ProtocolOverrideFile, &create.ProtocolOverrideFileSet)

	var update configcmd.UpdateOptions
	updateCommand := &cobra.Command{Use: "update <configuration-id>", Short: "更新认证配置", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		update.ID = args[0]
		update.ProtocolOverrideFileSet = cmd.Flags().Changed("protocol-override-file")
		update.ClearProtocolOverride = cmd.Flags().Changed("clear-protocol-override") && update.ClearProtocolOverride
		if cmd.Flags().Changed("clear-protocol-override") && !update.ClearProtocolOverride || update.ProtocolOverrideFileSet && (update.ProtocolOverrideFile == "" || strings.ContainsRune(update.ProtocolOverrideFile, 0)) || update.ClearProtocolOverride && update.ProtocolOverrideFileSet {
			return errCommandUsage
		}
		update.Binding.InterfaceSet = cmd.Flags().Changed("interface-id")
		update.Binding.AddressSet = cmd.Flags().Changed("local-ipv4")
		if _, err := update.Binding.Policy(true); err != nil {
			return err
		}
		if cmd.Flags().Changed("name") {
			value, _ := cmd.Flags().GetString("name")
			update.Name = &value
		}
		if cmd.Flags().Changed("profile") {
			value, _ := cmd.Flags().GetString("profile")
			update.Profile = &value
		}
		if cmd.Flags().Changed("username") {
			value, _ := cmd.Flags().GetString("username")
			update.Username = &value
		}
		if cmd.Flags().Changed("auto-login") {
			value, _ := cmd.Flags().GetString("auto-login")
			if value == "true" {
				b := true
				update.AutoLogin = &b
			} else if value == "false" {
				b := false
				update.AutoLogin = &b
			} else {
				return errors.New("--auto-login 需要 true 或 false")
			}
		}
		if cmd.Flags().Changed("auto-reconnect") {
			value, _ := cmd.Flags().GetString("auto-reconnect")
			if value == "true" {
				b := true
				update.AutoReconnect = &b
			} else if value == "false" {
				b := false
				update.AutoReconnect = &b
			} else {
				return errors.New("--auto-reconnect 需要 true 或 false")
			}
		}
		return wrapCommandOperation(deps.configUpdate(update))
	}}
	updateCommand.Flags().StringVar(&update.Binding.InterfaceID, "interface-id", "", "真实网卡 ID（须同时提供 --local-ipv4）")
	updateCommand.Flags().StringVar(&update.Binding.LocalIPv4, "local-ipv4", "", "本地 IPv4（须同时提供 --interface-id）")
	updateCommand.Flags().BoolVar(&update.Binding.Automatic, "automatic-binding", false, "恢复自动绑定")
	updateCommand.Flags().BoolVar(&update.AllowInsecure, "allow-insecure-storage", false, "允许未保护存储")
	updateCommand.Flags().String("name", "", "显示名称")
	updateCommand.Flags().String("profile", "", "Profile ID")
	updateCommand.Flags().String("username", "", "账号")
	updateCommand.Flags().String("auto-login", "", "自动登录（true 或 false）")
	updateCommand.Flags().String("auto-reconnect", "", "自动重连（true 或 false）")
	addProtocolOverrideFileFlag(updateCommand.Flags(), &update.ProtocolOverrideFile, &update.ProtocolOverrideFileSet)
	updateCommand.Flags().BoolVar(&update.ClearProtocolOverride, "clear-protocol-override", false, "清除已保存的协议上下文 Override；保存后结束旧 Session，不会自动连接")

	var password configcmd.PasswordOptions
	passwordCommand := &cobra.Command{Use: "set-password <configuration-id>", Short: "更新认证密码", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		password.ID = args[0]
		return wrapCommandOperation(deps.configSetPassword(password))
	}}
	passwordCommand.Flags().BoolVar(&password.PasswordStdin, "password-stdin", false, "从 stdin 读取密码")
	passwordCommand.Flags().BoolVar(&password.AllowInsecure, "allow-insecure-storage", false, "允许未保护存储")

	var yes, allowRemove bool
	removeCommand := &cobra.Command{Use: "remove <configuration-id>", Short: "删除认证配置", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return wrapCommandOperation(deps.configRemove(args[0], yes, allowRemove))
	}}
	removeCommand.Flags().BoolVar(&allowRemove, "allow-insecure-storage", false, "允许未保护存储")
	removeCommand.Flags().BoolVar(&yes, "yes", false, "确认删除")
	root.AddCommand(createCommand, updateCommand, passwordCommand, removeCommand)
	return root
}
