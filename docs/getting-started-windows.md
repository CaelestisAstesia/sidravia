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
- `institution-profiles\jlu.json`：官方吉林大学 Profile（非秘密；便携包位于
  `config\institution-profiles\jlu.json`）；
- `SHA256SUMS`：两个 PE 文件的 SHA-256；
- `README.md`、`GETTING-STARTED.md` 和 `LICENSE`。

Windows SmartScreen 可能提示这是未签名应用。只有从项目 GitHub Release 下载且哈希
匹配时才继续。

当前源码构建通过同目录的 `sidravia.portable` 标记选择运行目录模式。标记不存在时为
安装版：Profile 位于 `%APPDATA%\Sidravia`，运行信息和后台日志位于
`%LOCALAPPDATA%\Sidravia`。在 `sidravia.exe` 同目录放置空 `sidravia.portable` 文件
即启用便携版：Authentication Configuration（含其唯一私有密码）和机构 Profile 位于
`<解压目录>\config`，运行信息位于
`<解压目录>\runtime`，后台日志位于 `<解压目录>\logs`，全部随目录移动且不依赖
AppData。CLI 与 daemon 每次启动各自解析同一标记，增删标记前必须先停止 daemon。
该能力已进入当前源码，但尚未包含在已发布的 `v0.1.0-alpha.1` Release 中。

从当前源码用 `tools/build` 构建工具生成的包与已发布的 `v0.1.0-alpha.1` Release 是不同
产物，二者状态分开。工具为指定版本生成两个 zip 和一个外部校验文件：
`sidravia-v<version>-windows-amd64.zip`（安装版，不含 marker）、
`sidravia-v<version>-windows-amd64-portable.zip`（便携版，含空 `sidravia.portable`
marker）与 `SHA256SUMS.txt`（两个 zip 的 SHA-256）。两个 zip 共享同一对二进制，仅运行
模式元数据与 marker 不同。

## 安装吉林大学 Profile

Profile 是不含账号和密码的本地机构配置，daemon 只在启动时加载一次。当前源码构建
的包已随附官方吉林大学 Profile：安装版 zip 根目录含 `institution-profiles\jlu.json`；
便携版 zip 已在 `config\institution-profiles\jlu.json` 预置，解压即用。

安装版（默认，无 `sidravia.portable` marker）需要把随包附带的 Profile 复制到
`%APPDATA%\Sidravia\institution-profiles`。在 PowerShell 7 中、从解压目录执行：

```powershell
$ProfileDir = Join-Path $env:APPDATA 'Sidravia\institution-profiles'
$ProfilePath = Join-Path $ProfileDir 'jlu.json'
New-Item -ItemType Directory -Force -Path $ProfileDir | Out-Null
if (Test-Path -LiteralPath $ProfilePath) {
    throw "已存在 $ProfilePath；请先人工核对再决定是否覆盖。"
}
Copy-Item -LiteralPath '.\institution-profiles\jlu.json' -Destination $ProfilePath
```

便携版（解压目录含 `sidravia.portable` marker）无需复制：
`<解压目录>\config\institution-profiles\jlu.json` 已在包内就位，daemon 直接读取。

本地编辑与自定义 Profile：

- daemon 只在启动时加载一次 Profile，从不写入或覆盖它们；
- 修改 Profile 文件后重启 daemon 才生效；
- 重新复制或重新解压不得覆盖已存在的 Profile（上面的 `Test-Path` 守卫会阻止）；
- 不要把用户名或密码写入 Profile。其他学校不能直接复用吉林大学的服务器和 wire
  参数；它们需要独立确认的机构 Profile，可放入同一目录。

## 启动和认证

当前源码若显示
`sidraviad 在就绪前退出；请检查 daemon 日志`，表示 Windows CLI 观察到自己创建的精确
子进程已经退出，并在最后一次 typed readiness 探测后仍未就绪。CLI 不显示私有 Wait
cause；安装版检查 `%LOCALAPPDATA%\Sidravia\logs\sidraviad.log`，便携版检查
`<解压目录>\logs\sidraviad.log` 中由子进程写入的诊断。

先完全退出其他 Dr.COM 客户端，避免同一账号同时维持多个认证会话。

打开 PowerShell 7 并进入解压目录。当前源码构建默认由 CLI 后台启动 daemon：

```powershell
.\sidravia.exe daemon start
```

PowerShell 会立即恢复提示符，daemon 不再持续向前端终端刷日志。安装版的后台
stdout/stderr 写入 `%LOCALAPPDATA%\Sidravia\logs\sidraviad.log`；便携版写入
`<解压目录>\logs\sidraviad.log`。两种模式都在启动前达到 10 MiB 时把当前文件轮转为
唯一 `sidraviad.log.1` 备份。

需要前台诊断时，可以直接运行 daemon；此模式继续把日志写到 stderr：

```powershell
.\sidraviad.exe
```

`SIDRAVIA_LOG_LEVEL` 可设置为 `info`（默认）、`debug` 或 `trace`。Info 包含 daemon
生命周期、网络快照应用和 Session Snapshot；Debug 增加 IPC 连接/完成、Session 命令、
协议运行代际、重试调度和阶段边界；Trace 增加完整 D520 数据报 hex，可能含账号与认证
材料，启用时先写 `trace_logging_sensitive`。Info/Debug 允许完整账号名、机构显示名、
友好接口名与所选 IPv4，但不含密码、token、Profile JSON、MAC、DNS/DHCP、网关、
InterfaceID、请求/响应字节或原始 error。后台启用 Trace 后，这些敏感字节会保留在当前
日志或唯一 `.1` 备份中。Sidravia 不提供日志 IPC、`daemon logs` 或实时 tail。

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

当前源码也可持久保存认证配置（密码从 stdin 读取，不进入 argv）：

```powershell
'<你的密码>' | .\sidravia.exe config create --id campus --profile jlu --username '<你的账号>' --name '校园网' --password-stdin
.\sidravia.exe config list
.\sidravia.exe config show campus
.\sidravia.exe auth start --config campus
```

安装版要求当前用户权限保护。便携版仅在文件系统明确不支持所需保护模型时进入
`unprotected`，并发出固定警告；此时 create/set-password 还需显式
`--allow-insecure-storage`。同目录访问者可能读取或修改配置和密码、daemon runtime
token 与敏感 Trace 日志。当前使用明文 JSON，不提供 DPAPI、Keyring 或旧双文件迁移。

当前源码已修复 Windows 安全配置目录的继承 DACL：配置根目录的 owner/LocalSystem ACE 带
对象/容器继承标志，传播到既有和未来 `institution-profiles` 子树；秘密文件 ACE 保持不可
继承。catalog 在 Profile 加载之前打开，使 daemon 重启能修复旧版本留下的“子目录空继承
DACL”状态。该修复已完成代码与自动验证，Windows 原生与校园现场复核仍为独立证据；详见
ADR 0026。

当前源码还把 D520 `localPort` 与 `serverPort` 分开建模；上述 JLU Profile 对两者都选择
fixed `61440`，固定端口绑定失败不会降级为系统分配端口。该改动与 Windows launcher
早退报告均已完成代码和自动验证，但尚未完成组合 Windows-native 与校园热点复核。

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
- 官方吉林大学 Profile 随包附带；本地自定义 Profile 仍需手动创建，尚无引导式配置界面。
- 尚无自动登录或 Windows Service。
- 尚无 WSS、Linux/macOS daemon 进程控制；
  Session ensure/restart/remove 已进入当前源码，但仍待 Windows 原生复核。
- 简体中文 CLI 呈现、`NO_COLOR` 和重定向安全着色已加入；route-aware 多 IPv4 选择和
  自动化仍未完成。结构化 daemon 日志与 CLI 呈现的 Windows 原生尚未重新验证。
- 官方客户端曾出现 346 字节 Login 样本，但其扩展和长度是否可变仍未解决；Sidravia
  继续发送已经被真实服务器接受的 330 字节 Login。

遇到失败时保留 CLI 的非秘密 Snapshot、所选网卡和稳定错误码。不要公开原始抓包、
完整账号、MAC、IPC 运行信息内容或任何认证派生字段。
