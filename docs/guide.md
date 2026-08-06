# Sidravia 用户手册

## 支持范围与命令记法

Sidravia 的首版产品是 Windows amd64 portable 包。Linux/WSL 只提供源码运行时基线，
不提供首版包；macOS 明确 Unsupported。Windows portable 包中命令写作 `sidravia.exe`；
Linux/WSL 从源码构建后对应为 `sidravia`。以下 Windows 示例在包根目录的 PowerShell
中运行，`<...>` 表示自行替换且不要把密码放进命令行。

当前源码已有完整 Windows/campus field evidence，但这不等同于 signing、Release 或 hosted
readiness；本手册不承诺 Installer、updater、GUI、Windows Service、Linux 首版包或发布日期。

## 五分钟 Windows portable 起步

从可信来源取得 zip 和随附的 `SHA256SUMS.txt`，在解压前核对外部 SHA-256：

```powershell
Get-FileHash .\sidravia-v<version>-windows-amd64-portable.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
```

将 zip 全部解压到固定、当前用户可写的目录，例如
`%LOCALAPPDATA%\Programs\Sidravia`；不要直接在 zip 内运行。进入解压目录后执行：

```powershell
.\sidravia.exe daemon start
.\sidravia.exe daemon status
.\sidravia.exe profile list
.\sidravia.exe auth start --profile jlu --username '<账号>'
.\sidravia.exe auth status <session-id>
# 不再需要时可选：
.\sidravia.exe daemon stop
```

`auth start` 只返回初始 Snapshot，认证仍可能在 daemon 中继续。复制它返回的 Session ID
再查询 `auth status`；不知道 ID 时运行 `auth list`。

## daemon、Profile、Configuration 与 Session

daemon 是本机后台进程；`daemon status` 严格只读，不会启动 daemon、创建目录或轮转日志。
可使用 `daemon start`、`daemon status`、`daemon restart --log-level debug` 和 `daemon stop`。
若 runtime 信息存在但无法连接，先运行 `daemon status`；持续失败时运行 `daemon restart`。
停止或重启结果不确定时，也先用 `daemon status` 确认。

Profile 是随版本提供的机构参数，位于 `<exe-dir>\institution-profiles\`；`profile list`
显示已加载 Profile。它不含账号或密码。临时调试 Profile 可放在此处，但更新或重新解压不会
保留它；正式新增机构应提交上游。

Configuration 是持久认证配置，拥有账号、密码和自动化选项：

```powershell
.\sidravia.exe config list
.\sidravia.exe config show <configuration-id>
.\sidravia.exe config create
.\sidravia.exe config update <configuration-id>
.\sidravia.exe config set-password <configuration-id>
.\sidravia.exe config remove <configuration-id>
```

Session 是一次认证或从 Configuration 启动后的运行实体。日常查看和控制：

```powershell
.\sidravia.exe auth list
.\sidravia.exe auth status <session-id>
.\sidravia.exe auth start --session <session-id>
.\sidravia.exe auth restart <session-id>
.\sidravia.exe auth stop <session-id>
.\sidravia.exe auth remove <session-id>
```

每个 daemon 同时只允许一个活动认证 Session。daemon 重启后不恢复旧 Session ID。

## 认证与自动化

先退出其他 Dr.COM 客户端。一次性认证不会持久保存密码：

```powershell
.\sidravia.exe auth start --profile jlu --username '<账号>'
# 非交互输入密码时：
<命令生成密码> | .\sidravia.exe auth start --profile jlu --username '<账号>' --password-stdin
```

不要把密码写进 argv、Profile、截图或日志。持久 Configuration 可交互创建，或用完整参数创建：

```powershell
<命令生成密码> | .\sidravia.exe config create --id campus --profile jlu --username '<账号>' --password-stdin
.\sidravia.exe auth start --config campus
```

`AutoLogin` 最多只允许一份 Configuration 启用；daemon 的每个 generation 在首次网络
Snapshot 后评估一次。`AutoReconnect` 决定 retained Session 在失败或网络中断后是否自动
重连。创建时 AutoLogin 默认关闭、AutoReconnect 默认开启：

```powershell
.\sidravia.exe config update campus --auto-login true
.\sidravia.exe config update campus --auto-reconnect false
```

完整选项和交互方式见 `sidravia.exe help config create` 与
`sidravia.exe help config update`。

## Windows 集成、移动与删除

可选用户态集成不是 Installer。它使用系统 Windows PowerShell 5.1，加入当前用户 PATH，
并建立当前用户登录任务 `SidraviaDaemon`；不要求 PowerShell 7：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1 -LogLevel debug
```

脚本重复运行安全，且不会删除 Configuration、凭据、Profile 或日志。撤销：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
```

移动或重命名解压目录前，先运行 uninstall；移动后从新目录重新运行 install。彻底删除前，
先 `sidravia.exe daemon stop`，再 uninstall，最后删除整个解压目录；这会删除 portable
目录中的 `config\`、`runtime\` 和 `logs\` 数据。当前没有 Windows Service。

安装模式的 Configuration 位于 `%APPDATA%\Sidravia`，runtime 和日志位于
`%LOCALAPPDATA%\Sidravia`；portable 模式则都在解压目录。增删 `sidravia.portable` marker
前必须先停止 daemon。

## 排障与恢复

- daemon 未运行或无法连接：先运行 `sidravia.exe daemon status`，持续失败运行
  `sidravia.exe daemon restart`，然后检查日志。
- `session_not_found`：运行 `sidravia.exe auth list` 并使用实际 Session ID。
- 活动 Session conflict：先 `auth stop <session-id>` 或 `auth remove <session-id>`；状态冲突则
  `auth status <session-id>`。`blocked_by_error` 状态也应先查看 status。
- `configuration_not_found`：运行 `sidravia.exe config list`；`configuration_conflict`：列出
  现有配置后使用 `sidravia.exe help config update`。删除配置时非交互必须附加 `--yes`。
- JLU D520 Profile 使用固定本地端口 `61440`；端口绑定失败不会改用临时端口。停止占用该端口的
  程序后重试。
- 日志在当前模式的 `sidraviad.log`；Windows portable 路径为
  `logs\sidraviad.log`，只有一个轮转文件 `sidraviad.log.1`。

### Mihomo/Clash-family TUN

若 TUN 捕获校园私有目的地址，可停止 TUN，或在代理配置的
`tun.route-exclude-address` 排除 RFC 1918 范围：`10.0.0.0/8`、`172.16.0.0/12`、
`192.168.0.0/16`。不要把临时 `/32` 系统路由当作产品方案；Sidravia 不修改系统 route。

## 日志、凭据与安全分享

`SIDRAVIA_LOG_LEVEL` 可设为 `info`、`debug` 或 `trace`。Info/Debug 不包含密码、token、
Profile JSON、MAC、DNS/DHCP、网关、接口 ID、报文字节或原始错误；`trace` 会记录完整 D520
报文和潜在认证材料，必须按秘密处理。

求助时只分享非秘密 Snapshot、稳定错误码、包校验和、平台和网络条件。不要公开密码、token、
完整日志、抓包、Profile JSON、路由表或认证派生字段。

## FAQ 与限制

Linux/WSL 可从源码构建运行，但不提供首版包，也不能替代 Windows 原生和校园现场结果；
macOS 不支持。当前官方 Profile 是 `jlu`。没有 GUI、Windows Service、自动更新、Installer、
发布签名或日期承诺。

## field-validation 测试者附录

field-validation zip 比普通 portable 包多出测试脚本，不应作为普通用户路径。测试者用
PowerShell 7 运行：

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File .\scripts\field-test.ps1
```

它会先校验包内容，在带随机标记的 portable 沙箱内运行；可选择 `-SkipCampus`、
`-SkipIntegration`、`-SkipNetworkTransition` 或 `-Profile jlu`。正式 install/uninstall 仍由
系统 Windows PowerShell 5.1 执行。脚本不接收账号命令行参数，密码以隐藏输入并仅在 stdin
边界传递；保留的 JSON 报告不含账号、密码、MAC、token、原始日志、Profile JSON、抓包或
路由表。它不修改 VPN/TUN、路由、网卡或热点，也不证明签名、Release 或 hosted readiness。
