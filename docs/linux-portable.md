# Linux 便携版

本文介绍当前开发版。[0.5.0-alpha.2 对应文档](https://github.com/CaelestisAstesia/sidravia/blob/v0.5.0-alpha.2/docs/linux-portable.md)。

从[发行页面](https://github.com/CaelestisAstesia/sidravia/releases)选择与系统架构和用途匹配的包。只有发行说明列出的功能适用于对应下载包。

## 运行环境

GUI 便携版初始运行环境为 **Fedora 45 x86_64、KDE Plasma / Wayland**。运行时需要以下系统库。

Fedora 安装 GUI 运行依赖：

```bash
sudo dnf install gtk3 libayatana-appindicator-gtk3 webkit2gtk4.1
```

GTK 3、GLib、glibc、libstdc++、Wayland 等系统库也需可用。中文界面使用系统字体；缺字时可安装 `google-noto-sans-cjk-vf-fonts`。随包只携带诊断文本使用的 Source Code Pro。

核心便携包不含 Flutter GUI、托盘或公告窗口，不需要上述 GUI 库。Linux ARM64 当前仅提供核心便携包，尚未经 ARM64 实机验证。

## 解压与启动

完整解压到当前用户可写的固定目录。以下命令可解压 `.tar.gz` 或 `.tar.xz`，把变量值改成实际下载文件名：

```sh
archive='sidravia-v0.5.0-alpha.2-linux-amd64-gui-portable.tar.gz'
mkdir -p "$HOME/Applications"
tar -xf "$archive" -C "$HOME/Applications"
```

进入解压出的包根目录，运行：

```bash
./sidravia_gui
```

保留 `sidravia.portable`、`gui-runtime/`、机构资源和配套程序。`gui-runtime/` 含实际 Flutter 程序、`lib/` 与 `data/`；应始终通过包根入口启动，不能绕过启动器直接运行内部 GUI。启动器会先检查更新状态，再启动 GUI。

无法启动时可用 `ldd ./gui-runtime/sidravia_gui` 查找缺失系统库。不要以 root 运行 GUI，也不要在不互信的账户间共用可写便携目录。仅解压或打开 GUI 不会登记自启。

首次连接时，打开「设置 → 连接配置」，选择机构，填写账号与密码，保存并连接。核心包先从包根运行 `./sidraviactl daemon start`，再使用 `./sidraviactl config create` 创建配置，并运行 `./sidraviactl auth start --config <配置 ID>`。详细选项见[快速开始](quick-start.md)和[命令行指南](advance.md)。随包说明中的扩展阅读链接需要联网。

## 数据与系统集成

默认数据位于本副本的 `config/`、`runtime/` 和 `logs/`，GUI 设置为 `config/gui-settings.json`。指定 `SIDRAVIA_NAMESPACE` 的隔离实例使用 `namespaces/<namespace>/`。密码以明文保存，权限和备份要求见[数据与隐私](privacy.md)。

「自动启动」可选关闭、仅后台或图形界面和后台。是否在 GUI 启动时隐藏到托盘是独立设置。桌面自启文件位于 `$XDG_CONFIG_HOME/autostart`，未设置或不是绝对路径时使用 `~/.config/autostart`。应用管理模式的「仅后台」会运行本包的 `sidraviactl daemon start --log-level info`，systemd 模式由用户服务启动后台。需要后台启动后自动认证时，另行开启「启动时自动登录」。

改变自启设置只影响未来启动，不会接管或停止现有后台。从仅后台切回 GUI 时，先用本包的 `./sidraviactl daemon stop` 正常停止后台。

GUI 的用户 PATH 设置在 `~/.local/bin` 创建本安装的 `sidraviactl`、`sidraviad`、`sidraviaupdater` 和 `sidravia_gui` 精确符号链接；不覆盖无关文件，不修改 shell 配置，关闭时只移除本安装的链接。已有终端不会更新环境，目录不在 PATH 或有前序同名命令时会提示。namespace 取自调用进程的 `SIDRAVIA_NAMESPACE`。

其他安装的自启或 PATH 登记冲突会保留并报错，不会静默接管。核心包没有 GUI 设置入口。Linux 的 systemd 用户服务可以通过 `sidraviactl integration service` 管理。

## 更新、移动和移除

移动或删除前，在旧位置关闭已启用的桌面自启及 PATH 登记，并正常退出 GUI。使用 [systemd 用户服务](#systemd-用户服务) 时，按下节步骤停止服务并在移动后更新登记。确定移除时，在删除目录前运行 `./sidraviactl integration service unregister`。应用管理模式下还需确认后台已结束。

手动升级时，完整解压新版到新目录，在两端都退出后只转移需要保留的私有数据，不能覆盖正在运行的目录。配置迁移步骤见[使用指南](guide.md#更新移动与卸载)。

GUI 包的名称含 `-linux-amd64-gui-portable`，仅核心包以 `-core-portable` 区分。归档扩展名以该版本发行页为准。更新器按当前包型选择更新，见[更新检查与下载](advance.md#更新检查与下载)。

其他发行版、桌面环境和 X11 尚待验证。通知、登录自启和校园认证请在实际使用环境中检查。

## systemd 用户服务

运行所需命令包括 `/usr/bin/getent`、`/usr/bin/systemctl` 和
`/usr/bin/loginctl`。服务由当前用户管理，每个用户登记一个 Sidravia 安装。
后台未运行时，可用目标目录内的 `sidraviactl` 登记并启动：

```sh
./sidraviactl integration service register
./sidraviactl integration service start
./sidraviactl integration service status
```

同一安装正在运行时，用 `takeover` 切换到 systemd，用 `release` 切回应用管理。
这两个命令会按原已保存配置重新认证，原先手动断开则保持断开。
一次性认证需加 `--interrupt-session` 确认中断，完成后可重新输入。
切换中断时，从同一安装运行 `./sidraviactl integration service recover` 恢复原管理方式。
普通后台重启仍沿用配置的自动登录设置。

用 `enable` 和 `disable` 开关用户服务自启，用 `stop` 停止当前后台。
`linger-on` 和 `linger-off` 是单独的账户设置，会影响该用户的其他 systemd 服务。
启用 linger 前请确认主目录、便携目录和网络能在登录前访问。
遇到系统权限拒绝时，请在可授权的桌面会话操作或请管理员使用 `loginctl`。

手动移动目录或更换安装前，退出 GUI，再从旧目录运行 `./sidraviactl integration service stop`。
移动整个目录，或先按[便携版更新步骤](guide.md#更新移动与卸载)把需要保留的配置复制到新安装。
从目标目录运行 `./sidraviactl integration service replace` 更新登记，需要继续运行时再执行
`./sidraviactl integration service start`。
更换中断后可从任一参与安装运行 `sidraviactl integration service recover`，恢复原登记和原启用状态。若旧安装已移除，可用 `sidraviactl integration service discard --yes` 停止后台、解除登记并清除中断记录。各安装的数据会保留。

如果状态提示切换记录损坏，在「连接与诊断 → 高级排障」清除损坏的切换记录，或运行 `sidraviactl integration service discard-damaged --yes`。这会保留当前后台、登记和账号配置。清除后可按实际状态继续管理或解除登记。

桌面自启和用户 PATH 操作使用目录锁。若旧版本留下 `.sidravia-integration.lock`，可先退出所有旧版 GUI，再删除自己确认由旧版 Sidravia 生成的锁文件。该文件可能位于对应用户配置目录的 `autostart` 或 `~/.local/bin`。
`unregister` 停止服务、撤销自启及登记，保留账号配置和 linger。
可从仍可用的安装执行解除登记。若已经删除唯一一份程序，先用
`systemctl --user cat sidravia.service` 核对实际 unit 属于已移除的 Sidravia 安装，
再用 `systemctl --user disable --now sidravia.service` 停止并解除自启。
确认文件归属后，删除账号主目录 `.config/systemd/user/sidravia.service`，
运行 `systemctl --user daemon-reload`，通过 `systemctl --user show -p LoadState,ActiveState sidravia.service`
确认已移除。账号数据与日志仍在原便携目录，由用户决定是否删除。

如需降级，先取得并核对原发行的完整旧包，将它解压到独立目录。
如果目标版本支持用户服务，从旧包目录执行 `./sidraviactl integration service replace`，再确认旧版的版本和运行状态。目标版本缺少用户服务命令时（例如 0.5.0-alpha.2），先从仍可用的当前安装运行 `./sidraviactl integration service unregister`，停止并解除当前托管，再通过旧包入口启动。
各目录保留各自的数据。如果需要继续使用已有配置，请先确认旧版兼容，再自行选择要迁移的文件。
