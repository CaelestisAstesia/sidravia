# Fedora RPM

本页适用于 `0.5.0-alpha.2`，运行环境为 **Fedora 45 x86_64、KDE Plasma / Wayland**。软件包由项目提供，下载见[发行页面](https://github.com/CaelestisAstesia/sidravia/releases/tag/v0.5.0-alpha.2)。

## 选择与安装

| 软件包 | 内容 |
| --- | --- |
| `sidravia` | GUI、CTL、后台和机构资源 |
| `sidravia-core` | CTL、后台和机构资源 |

两个包共用安装路径，不能同时安装。核心包不依赖 GTK、WebKitGTK 等图形库。切换包型时，先退出程序并备份配置，卸载原包后再安装另一包。两种包使用相同的用户数据位置。

ARM64 RPM 暂不提供。ARM64 用户可选择[核心便携包](../README.md#选择下载包)。

下载对应 `.rpm` 与校验清单，按[快速开始](quick-start.md#校验下载)核对哈希。在下载目录执行，尖括号部分替换为实际文件名：

```bash
sudo dnf install ./<下载的RPM文件名>.rpm
```

安装失败时，保留 `dnf` 的错误提示并查看发行说明。

GUI 包安装后从应用菜单打开 Sidravia，首次连接见[快速开始](quick-start.md)。命令行可直接使用 `sidraviactl`，无需另外加入 PATH。

## 程序与依赖

程序位于 `/usr/lib64/sidravia`，核心包提供 `/usr/bin/sidraviactl` 与 `/usr/bin/sidraviad`。GUI 包另带界面、应用菜单和图标。

GUI 入口为 `/usr/bin/sidravia_gui`，Flutter 运行库和资源随程序安装。

GUI 依赖由系统 RPM 提供，包括 GTK 3、WebKitGTK 4.1 和 Ayatana AppIndicator。中文使用系统字体，缺字时可安装 `google-noto-sans-cjk-vf-fonts`；随包只携带诊断用 Source Code Pro 及相应许可。

RPM 不包含便携更新器。`sidraviactl update version` 查询已安装 CLI 身份，其他更新操作提示使用包管理器；GUI 不再创建冗余的 `~/.local/bin` 链接。

## 配置与登录自启

账号配置和 GUI 设置默认位于 `~/.config/Sidravia`，后台运行信息与日志位于 `~/.cache/Sidravia`。绝对的 `XDG_CONFIG_HOME`、`XDG_CACHE_HOME` 可分别改变这两个根目录。namespace 路径及明文密码保护说明见[数据与隐私](privacy.md#linux)。RPM 不自动迁移便携版数据。

安装、升级或移除不会启用自启、安装常驻系统服务或自动启动用户程序。GUI 用户可自行选择登录桌面会话后启动 GUI 或仅后台；自动认证需另行开启自动登录。

RPM 的桌面自启登记在升级后继续使用。卸载时删除本安装创建的桌面自启项，重新安装后可按需再启用。自定义 XDG 目录里的旧版登记若尚未被识别，可在原桌面自启设置中关闭。

## 升级与卸载

升级时下载新版 RPM，使用同样的 `dnf install ./<文件名>.rpm` 命令；包名与架构需匹配当前安装。卸载前确认需要保留的配置已备份，再使用系统软件管理器移除，或用 `sudo dnf remove <实际包名>`；GUI 包名为 `sidravia`，核心包名为 `sidravia-core`。

事务会请求本安装的 GUI、会话与后台正常退出；未保存的配置草稿或无法响应的程序会阻止维护，先保存修改、正常退出后重试。脚本不使用 SIGKILL，也不停止独立便携副本。事务锁生效期间拒绝从本安装启动新进程。升级后，RPM 会验证包完整性，恢复原先运行的已登记 systemd 用户服务，并确认同一后台进程已能通过产品 IPC 响应。如果后台未能恢复，可用 `journalctl -t sidravia-rpm` 查看诊断。

RPM 卸载后保留用户配置与日志。需要清理时，先按[数据位置](privacy.md#linux)核对并备份。不要手工替换 `/usr/lib64/sidravia` 中的文件，更新和修复继续由包管理器负责。

## 开发与验证

构建与隔离验收入口见[开发工具说明](../tools/developer/README.md#local-unsigned-fedora-rpm-x86_64)。

## systemd 用户服务

以普通用户使用 `sidraviactl integration service` 管理登记、启停、自启和 linger。
RPM 与便携安装共用一个登记。通过目标安装的 `sidraviactl integration service replace`
更换登记，两边配置各自保留。

移除 RPM 时，包维护程序会停止该 RPM 的后台，并在移除完成后按登记用户权限解除其用户服务登记。
便携登记、账号数据和 linger 保留。也可以在移除前运行 `sidraviactl integration service unregister` 显式解绑。
若升级后后台没有恢复，或自动清理失败，可用 `journalctl -t sidravia-rpm` 查看发生在哪个账号。
运行中的 RPM 用户服务升级由包维护脚本协调。
请通过 `sidraviactl daemon status` 确认更新后的版本和运行状态。

需要降级时，确认旧 RPM 对应同一包名及架构，并检查配置兼容性。
通过包管理器安装已验证的旧包，例如 `sudo dnf downgrade ./<旧包文件>.rpm`，完成后查询后台版本与状态。

如果 RPM 已经移除，且没有其他 Sidravia 安装可用，可以通过系统工具
[解除失效登记](linux-portable.md#systemd-用户服务)。已有 linger 设置可用
`loginctl disable-linger "$(id -un)"` 单独关闭。这个操作影响该账号的全部用户服务。
