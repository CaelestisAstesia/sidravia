# Linux 便携版

本页适用于 `0.5.0-alpha.1`。下载与版本变化见[发行说明](https://github.com/CaelestisAstesia/sidravia/releases/tag/v0.5.0-alpha.1)。

## 运行环境

GUI 便携版初始运行环境为 **Fedora 45 x86_64、KDE Plasma / Wayland**。运行时需要以下系统库。

Fedora 安装 GUI 运行依赖：

```bash
sudo dnf install gtk3 libayatana-appindicator-gtk3 webkit2gtk4.1
```

GTK 3、GLib、glibc、libstdc++、Wayland 等系统库也需可用。中文界面使用系统字体；缺字时可安装 `google-noto-sans-cjk-vf-fonts`。随包只携带诊断文本使用的 Source Code Pro。

核心便携包不含 Flutter GUI、托盘或公告窗口，不需要上述 GUI 库。Linux ARM64 当前仅提供核心便携包，尚未经 ARM64 实机验证。

## 解压与启动

完整解压到当前用户可写的固定目录，在包根目录运行：

```bash
./sidravia_gui
```

保留 `sidravia.portable`、`gui-runtime/`、机构资源和配套程序。`gui-runtime/` 含实际 Flutter 程序、`lib/` 与 `data/`；应始终通过包根入口启动，不能绕过启动器直接运行内部 GUI。启动器会先检查更新状态，再启动 GUI。

无法启动时可用 `ldd ./gui-runtime/sidravia_gui` 查找缺失系统库。不要以 root 运行 GUI，也不要在不互信的账户间共用可写便携目录。仅解压或打开 GUI 不会登记自启。

首次连接见[快速开始](quick-start.md)。核心包从 `./sidraviactl daemon start` 开始，后续配置与连接步骤见[命令行指南](advance.md)。

## 数据与系统集成

默认数据位于本副本的 `config/`、`runtime/` 和 `logs/`，GUI 设置为 `config/gui-settings.json`。指定 `SIDRAVIA_NAMESPACE` 的隔离实例使用 `namespaces/<namespace>/`。密码以明文保存，权限和备份要求见[数据与隐私](privacy.md)。

GUI 设置支持登录桌面会话后启动 GUI 或仅后台，两种登记互斥。自启文件位于 `$XDG_CONFIG_HOME/autostart`，未设置或不是绝对路径时使用 `~/.config/autostart`。GUI 模式启动到托盘；没有托盘服务时保留主窗口。仅后台模式运行本包的 `sidraviactl daemon start --log-level info`。需要登录后自动认证时，另行开启自动登录。

改变自启设置只影响未来启动，不会接管或停止现有后台。从仅后台切回 GUI 时，先用本包的 `./sidraviactl daemon stop` 正常停止后台。

GUI 的用户 PATH 设置在 `~/.local/bin` 创建本安装的 `sidraviactl`、`sidraviad`、`sidraviaupdater` 和 `sidravia_gui` 精确符号链接；不覆盖无关文件，不修改 shell 配置，关闭时只移除本安装的链接。已有终端不会更新环境，目录不在 PATH 或有前序同名命令时会提示。namespace 取自调用进程的 `SIDRAVIA_NAMESPACE`。

其他安装的自启或 PATH 登记冲突会保留并报错，不会静默接管。核心包没有 GUI 设置入口，CTL 的 `integration` 命令仍仅面向 Windows。

## 更新、移动和移除

移动或删除前，在旧位置关闭已启用的自启及 PATH 登记，正常退出 GUI 与后台，再操作整个目录；移动后按需重新登记。

手动升级时，完整解压新版到新目录，在两端都退出后只转移需要保留的私有数据，不能覆盖正在运行的目录。配置迁移步骤见[使用指南](guide.md#更新移动与卸载)。

GUI 包使用 `-linux-amd64-gui-portable.tar.gz`，核心包使用 `-core-portable.tar.gz`。更新器按当前包型选择更新。自动更新的使用条件、命令和故障处理见[更新检查与下载](advance.md#更新检查与下载)。

其他发行版、桌面环境和 X11 尚待验证。通知、登录自启和校园认证请在实际使用环境中检查。
