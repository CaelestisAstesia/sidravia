# 数据与隐私

## 账号与密码

账号、密码和连接设置保存在本地 `configurations.json`。**密码以明文保存，没有加密。**

Sidravia 使用文件权限保护账号配置：Windows 限制到当前用户与 LocalSystem，Linux 使用用户私有权限。这不能阻止以当前用户或更高权限运行的程序读取密码。

无法建立所需保护时，程序会提示并要求确认后再保存。选择继续保存不会加密密码，也不会自动修复权限。

配置文件及其备份、复制或同步副本均含密码，请妥善保管，不要公开上传。CTL 交互式输入密码时不会回显；脚本可通过 `--password-stdin` 输入，不要把密码直接写进命令行参数或公开脚本。

兼容的旧格式账号配置在首次保存为新格式前，会在同一数据目录生成 `.sidravia-migration-*.json` 恢复副本；其中保留原文件字节，也包含原账号和明文密码，请按原配置文件保管。0.5 安装器也会在覆盖前保存同类副本，并用 `.sidravia-upgrade-*.txt` 标记来源/目标版本及副本名；完整清除会一并处理受管副本。旧 GUI 设置首次保存前也会生成 `.sidravia-gui-migration-*.json`，只包含界面偏好。

## 数据位置

### Windows

下表列出 Windows 安装版与便携版的主要数据位置。`<程序目录>` 指程序文件所在的文件夹；开发和测试使用的数据位置另行隔离。

| 数据 | 安装版 | 便携版 |
| --- | --- | --- |
| 账号配置 | `%APPDATA%\Sidravia\configurations.json` | `<程序目录>\config\configurations.json` |
| GUI 设置 | `%APPDATA%\Sidravia\gui-settings.json` | `<程序目录>\config\gui-settings.json` |
| 后台运行信息 | `%LOCALAPPDATA%\Sidravia\runtime.json` | `<程序目录>\runtime\runtime.json` |
| 后台日志 | `%LOCALAPPDATA%\Sidravia\logs\` | `<程序目录>\logs\` |

`institution-profiles` 是随版本提供的机构参数，不含用户账号密码；GUI 设置文件也不保存密码。不要用旧版机构配置替换新版随包资源。

程序目录中的 `sidravia.portable` 文件决定是否采用便携布局。添加或删除它只会改变数据位置，不会迁移已有数据。Linux 核心便携包也将配置、运行信息和日志保存在程序目录下。

学校公告使用的 WebView2 缓存，在两种 Windows 布局下均位于 `%LOCALAPPDATA%\Sidravia\announcement-webview\production\`；删除便携目录不会清理这部分缓存；0.5 安装器明确选择完整清除时会处理已确认归属的共享缓存。隔离实例使用同一父目录下的 `ns-<小写命名空间>`。

### Linux

| 数据 | RPM／非便携布局 | 便携布局 |
| --- | --- | --- |
| 账号配置 | `~/.config/Sidravia/configurations.json` | `<程序目录>/config/configurations.json` |
| GUI 设置 | `~/.config/Sidravia/gui-settings.json` | `<程序目录>/config/gui-settings.json` |
| 后台运行信息 | `~/.cache/Sidravia/runtime.json` | `<程序目录>/runtime/runtime.json` |
| 后台日志 | `~/.cache/Sidravia/logs/` | `<程序目录>/logs/` |

`XDG_CONFIG_HOME` 和 `XDG_CACHE_HOME` 为绝对路径时，分别替换上表的 `~/.config` 与 `~/.cache`；空值或相对值使用默认路径。GUI 与核心共享同一布局，RPM 不自动导入便携配置。Linux 公告使用临时 WebKit 会话，不创建 Windows WebView2 的共享持久缓存。

指定 `SIDRAVIA_NAMESPACE` 的隔离实例中，非便携布局的配置、GUI 设置、运行信息和日志集中到 `<配置根>/Sidravia/namespaces/<namespace>/`；便携布局使用 `<程序目录>/namespaces/<namespace>/`。这些隔离配置也可能含明文密码。

更新检查及下载还会使用受保护的用户缓存保存候选包、签名证据和操作回执；这些缓存位于程序目录之外。不要在更新仍进行或结果不明时删除这些记录，应先用 `update status` 核对。

## 网络通信

- **认证通信**：D520 按协议向认证服务器发送账号、认证数据及主机和网络信息。这类通信不能视为加密传输。
- **本机通信**：GUI、CTL 与后台通过本机回环 WebSocket 通信，用于控制认证和查询状态。
- **更新**：手动检查、下载或应用更新会访问 GitHub Releases 与其资产下载服务，以获取版本、软件包及签名证明；这些操作不需要上传账号配置或校园认证凭据。
- **学校公告**：公告窗口加载学校原网页，学校网站会收到相应网页请求。当前吉林大学公告地址使用 HTTP，网页通信未受 HTTPS 保护。

## 日志与诊断

TRACE 会记录完整认证协议报文，可能包含账号和认证信息，仅在排障需要时开启。排障结束后恢复原日志等级，并重启核心使其生效。降低日志等级不会删除此前已生成的 TRACE 日志。

GUI「运行状态快照」显示并复制界面已接收的公共状态，排除密码、IPC 授权令牌、认证密钥和原始认证报文。账号、IP、网卡名称、地址和各种标识等非秘密字段保留原值，没有身份脱敏。快照基于已有界面数据，无法恢复后端未提供的内部字段。CTL 的 `diagnostics export` 继续提供原有的脱敏摘要。分享前请检查快照、日志、截图和其他附加材料，不要直接上传整个程序目录或数据目录。

协议上报覆写文件可能包含主机名、MAC 和 IP 等信息，分享前也应检查并遮蔽不希望公开的内容。

导出的诊断文件、手动复制的日志和配置备份由你自行保管；程序卸载或日志轮转不会自动清理这些副本。

## 备份与卸载

备份前，请正常退出 GUI 和后台。Linux 使用 systemd 托管时，先停止用户服务。便携版在包根目录运行 `./sidraviactl integration service stop`，RPM 安装后运行 `sidraviactl integration service stop`。需要保留账号与连接设置时，备份 `configurations.json`。需要保留外观等 GUI 设置时，同时备份 `gui-settings.json`。备份含明文密码，请按账号凭据妥善保管。

无需为恢复配置备份 `runtime.json` 等临时运行信息，也不要用旧版运行记录替换新版文件。预发布版本之间可能存在不兼容改动，恢复或迁移前请查看目标版本的发行说明。

Windows 安装版卸载默认保留数据，请在删除数据的询问中选择「否」。0.5 安装器的完整清除还包括受管命名空间配置、迁移/升级副本、指定日志、崩溃遗留的严格命名配置临时文件、共享公告 WebView2 缓存及产品通知状态；旧版安装器的清理范围以该版本说明为准。请先退出当前账户全部实例。外部备份/诊断导出、用户机构覆盖、未知文件、其他账户、便携目录内数据和系统 WebView2 Runtime 保留，不清除 Windows 通知数据库或系统审计历史。

清理中断或遇到未知文件时会报告未完成，保留程序供重试；已删除的数据需要从备份恢复。数据目录内 `.cleanup-incomplete` 仅表示固定范围清理未完成，不含凭据、操作目标或逐项日志，核查完成后移除。

RPM 卸载保留当前用户的数据，不遍历用户家目录。

删除便携目录会同时删除其中的账号配置、GUI 设置和日志。先备份需要保留的数据，按[更新、移动与卸载](guide.md#更新移动与卸载)处理相应的系统集成。Windows 维护命令见 [Windows 维护工具](manager.md)。
