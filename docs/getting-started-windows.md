# Windows Alpha 使用指南

本文适用于 `v0.1.0-alpha.1` 的 Windows amd64 压缩包。这个版本已经在一台
Windows 11 机器上完成吉林大学 Dr.COM 5.2.0(D) 的认证、保活和注销现场验证，但仍是
Alpha：二进制未签名，不提供 GUI、Windows Service、自动更新或通用机构配置。

## 下载与校验

从 GitHub Release 同时下载：

- `sidravia-v0.1.0-alpha.1-windows-amd64.zip`
- `SHA256SUMS.txt`

在 PowerShell 7 中比较压缩包哈希：

```powershell
(Get-FileHash .\sidravia-v0.1.0-alpha.1-windows-amd64.zip -Algorithm SHA256).Hash.ToLower()
Get-Content .\SHA256SUMS.txt
```

两者必须一致。解压后目录包含：

- `sidravia.exe`：短生命周期命令行客户端；
- `sidraviad.exe`：本地长期运行 daemon；
- `SHA256SUMS`：两个 PE 文件的 SHA-256；
- `README.md`、`GETTING-STARTED.md` 和 `LICENSE`。

Windows SmartScreen 可能提示这是未签名应用。只有从项目 GitHub Release 下载且哈希
匹配时才继续。

当前源码构建通过同目录的 `sidravia.portable` 标记选择运行目录模式。标记不存在时为
安装版：Profile 位于 `%APPDATA%\Sidravia`，运行信息和后台日志位于
`%LOCALAPPDATA%\Sidravia`。在 `sidravia.exe` 同目录放置空 `sidravia.portable` 文件
即启用便携版：Profile、凭据和机构 Profile 位于 `<解压目录>\config`，运行信息位于
`<解压目录>\runtime`，后台日志位于 `<解压目录>\logs`，全部随目录移动且不依赖
AppData。CLI 与 daemon 每次启动各自解析同一标记，增删标记前必须先停止 daemon。
该能力已进入当前源码，但尚未包含在已发布的 `v0.1.0-alpha.1` Release 中。

## 安装吉林大学 Profile

Profile 是不含账号和密码的本地机构配置。daemon 只在启动时加载一次。

在 PowerShell 7 中执行：

```powershell
$ProfileDir = Join-Path $env:APPDATA 'Sidravia\institution-profiles'
New-Item -ItemType Directory -Force -Path $ProfileDir | Out-Null
$ProfilePath = Join-Path $ProfileDir 'jlu.json'
```

创建 `jlu.json`：

```powershell
@'
{
  "schemaVersion": 1,
  "institutionProfileId": "jlu",
  "displayName": "吉林大学",
  "authenticationProtocolId": "drcom-5.2.0-d",
  "institutionProtocolConfiguration": {
    "serverAddress": "10.100.61.3",
    "serverPort": 61440,
    "authVersionHex": "2c00",
    "keepAliveVersionHex": "dc02",
    "controlCheckStatusHex": "20",
    "ipdogHex": "01",
    "adapterNumberHex": "01",
    "osInfoHex": "940000000600000000000000280a000002000000",
    "challengePaddingHex": "000000000000000000000000000000",
    "challengeTimeout": "3s",
    "loginTimeout": "5s",
    "keepaliveTimeout": "3s",
    "logoutTimeout": "1s",
    "heartbeatInterval": "20s",
    "busyMaxAttempts": 3,
    "busyBackoffMin": "1s",
    "busyBackoffMax": "2s"
  }
}
'@ | Set-Content -LiteralPath $ProfilePath -Encoding utf8NoBOM
```

不要把用户名或密码写入 Profile。其他学校不能直接复用这些服务器和 wire 参数；它们
需要独立确认的机构 Profile。

## 启动和认证

先完全退出其他 Dr.COM 客户端，避免同一账号同时维持多个认证会话。

打开 PowerShell 7 并进入解压目录。当前源码构建默认由 CLI 后台启动 daemon：

```powershell
.\sidravia.exe daemon start
```

PowerShell 会立即恢复提示符，daemon 不再持续向前端终端刷日志。后台 stdout/stderr
写入 `%LOCALAPPDATA%\Sidravia\logs\sidraviad.log`；启动前达到 10 MiB 时，当前文件
轮转为唯一 `sidraviad.log.1` 备份。

需要前台诊断时，可以直接运行 daemon；此模式继续把日志写到 stderr：

```powershell
.\sidraviad.exe
```

`SIDRAVIA_LOG_LEVEL` 可设置为 `info`（默认）、`debug` 或 `trace`；`trace` 启用
完整 D520 数据报日志，可能含账号与认证材料，显式敏感。Info/Debug 日志包含完整
账号名、友好接口名与所选 IPv4，但不含密码、token、凭据、Profile JSON、MAC、
DNS/DHCP、网卡 ID 或原始 error；Trace 数据报是唯一含完整报文字节的位置。后台启用
Trace 后，这些敏感字节可能保留在当前日志或 `.1` 备份中。Sidravia 不提供日志 IPC、
`daemon logs` 或实时 tail。

检查已发布版本：

```powershell
.\sidravia.exe status
```

上面是已发布 `v0.1.0-alpha.1` 二进制的命令。当前源码构建已经迁移到资源命令树，应改用：

```powershell
.\sidravia.exe daemon status
```

当前开发构建执行旧的顶层 `status` 不会连接或启动 daemon，而会返回
`命令已迁移，请使用 sidravia daemon status`。不要把新命令用于旧 Alpha 二进制，也
不要把旧命令当作当前构建的兼容别名。

当前源码构建还提供：

```powershell
.\sidravia.exe daemon start --log-level info
.\sidravia.exe daemon stop
.\sidravia.exe daemon restart --log-level debug
```

`daemon status` 严格只读。`start` 和 `restart` 的 `--log-level` 接受 `info`、
`debug`、`trace`，省略时确定使用 `info`；CLI 会移除继承环境中大小写不同的重复
`SIDRAVIA_LOG_LEVEL`，再为 Windows 子进程设置唯一值。stop/restart 始终针对命令首次
探测到的精确 daemon generation，不会因运行信息被替换而停止新的 generation。
生产 `daemon status` 已接入严格只读 probe；缺少运行信息时只显示 stopped，不启动进程
也不创建或轮转日志。这组修正已有代码与自动验证，但尚未重新完成 Windows 原生验证。

可以用 bare command、`help <path>`、`-h` 或 `--help` 查看同一份分层中文帮助：

```powershell
.\sidravia.exe
.\sidravia.exe help daemon
.\sidravia.exe help auth start
.\sidravia.exe auth start --help
```

当前源码构建还可以确认 daemon 实际加载的机构 Profile：

```powershell
.\sidravia.exe profile list
```

该命令只显示 Profile ID、名称和协议，不显示协议配置或凭据。

预期包含：

```text
守护进程：运行中（running） | 版本：0.1.0-alpha.1 | 构建：<build-id> | PID：<PID>
```

启动一次性 Session：

```powershell
.\sidravia.exe auth start --profile jlu --username '<你的账号>'
```

密码只在交互式 `密码：` 提示中输入，不会回显。不要把密码放进命令行、脚本、
Profile、截图或日志。非交互式调用必须显式使用 `--password-stdin`。

`auth start` 会立即返回初始 Snapshot，不会等待认证完成。记下输出中的 Session ID，
例如：

```powershell
$SessionID = 'session-1'
.\sidravia.exe auth status $SessionID
```

当前源码构建也可以列出当前 daemon 进程仍保留的全部 Session：

```powershell
.\sidravia.exe auth list
```

列表包含完整账号名；daemon 重启后不会恢复旧 Session。

当前源码还支持在不重新读取密码的情况下管理 retained Session：

```powershell
.\sidravia.exe auth start --session $SessionID
.\sidravia.exe auth restart $SessionID
.\sidravia.exe auth remove $SessionID
```

ensure 只保证同一 Session 正在运行；restart 是明确重启；remove 会等待在线 Session
完成停止与清理后再删除。这些行为已有代码与自动验证设计，但尚未完成 Windows 原生或
新增校园现场验证。

认证成功时状态为：

```text
状态：已认证（authenticated）
```

同时核对 `网络：` 是实际校园物理网卡和预期 IPv4，而不是 VPN、TUN、虚拟交换机或
其他软件接口。

## 停止与退出

停止当前 Session：

```powershell
.\sidravia.exe auth stop $SessionID
```

已发布的 `v0.1.0-alpha.1` 会直接返回 `suspended`。包含两阶段停止语义的后续开发构建
会先立即返回：

```text
状态：正在停止（stopping）
```

`stopping` 表示 daemon 正在执行有界的尽力 Logout，并且单活动 Session 槽仍被占用。
无论 Stop 命令直接返回 `suspended` 还是先返回 `stopping`，都继续查询，直到状态为
`suspended`：

```powershell
do {
  Start-Sleep -Milliseconds 250
  $Status = .\sidravia.exe auth status $SessionID
  $Status
} until ($Status -match '(?m)^状态：已暂停（suspended）$')
```

此时协议 Run 已经退出，随后再次查询应保持 `suspended`。最后执行
`.\sidravia.exe daemon stop` 关闭后台 daemon。只有直接前台诊断时才在该窗口按一次
`Ctrl+C`。

首个 Alpha 同一 daemon 进程只允许一个活动 Session。进程重启后不会恢复旧 Session
ID。不要用强杀进程、反复启动第二个 Session 或同时运行其他认证客户端代替正常 Stop。

## Alpha 限制

- 只发布 Windows amd64 二进制；其他平台尚不受支持。
- 只对吉林大学的一台真实 Windows 11 机器完成过校园现场验证。
- 本地 Profile 需要手动创建，尚无引导式配置界面。
- 尚无持久认证配置、自动登录或 Windows Service。
- 尚无 WSS、持久 Configuration/Credentials 或 Linux/macOS daemon 进程控制；
  Session ensure/restart/remove 已进入当前源码，但仍待 Windows 原生复核。
- 简体中文 CLI 呈现、`NO_COLOR` 和重定向安全着色已加入；route-aware 多 IPv4 选择和
  自动化仍未完成。结构化 daemon 日志与 CLI 呈现的 Windows 原生尚未重新验证。
- 官方客户端曾出现 346 字节 Login 样本，但其扩展和长度是否可变仍未解决；Sidravia
  继续发送已经被真实服务器接受的 330 字节 Login。

遇到失败时保留 CLI 的非秘密 Snapshot、所选网卡和稳定错误码。不要公开原始抓包、
完整账号、MAC、IPC 运行信息内容或任何认证派生字段。
