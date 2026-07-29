# Sidravia

Sidravia 是一个目前首先在 Windows 上交付、并为 Linux/macOS 保留平台边界的
Dr.COM 网络认证客户端。产品由短生命周期 CLI `sidravia` 和长期运行的本地 daemon
`sidraviad` 组成：

```text
sidravia CLI -> loopback WebSocket IPC -> sidraviad -> Dr.COM -> network
```

## 当前状态

`v0.1.0-alpha.1` 是面向 Windows amd64 的首个 Alpha。仓库已经实现并自动验证：

- CLI/daemon 状态通信骨架；
- `config list/show/create/update/set-password/remove` 持久认证配置、`auth start/status/stop/list/restart/remove`、retained Session ensure-running、`profile list`、Windows 隐藏密码输入和
  `--password-stdin`；
- typed 一次性 Session 启动、停止、查询和安全列表，以及机构 Profile 安全摘要列表；
- Session、Supervisor、网络快照分发与单活动 Session 规则；
- Dr.COM 5.2.0(D) 报文、Factory 和阻塞式认证 Run；
- 本地可编辑机构 Profile 的严格一次性加载；
- 生产 daemon 的 D520 注册、Profile 加载、Windows Environment Observer、typed IPC
  和统一生命周期；
- Windows JSON 文件 ACL 与原子持久化基础；
- 安全结构化 daemon 运行日志（stderr TextHandler、稳定事件码、固定简体中文消息
  和安全属性白名单）。
- `daemon status/start/stop/restart` 生命周期命令；生产 status 接入严格只读探测，
  Windows 后台子进程可用 `--log-level info|debug|trace` 指定确定的日志级别。
- retained Session 的继续运行、重启和停止后删除；CLI 后台日志落盘、统一分层中文
  Help，以及不泄漏控制字符的轻量终端呈现。

提交 `508197d` 已在 Windows 11 与吉林大学校园网完成首次现场验证：原生 CLI/daemon
选择物理以太网，完成 D520 登录、持续心跳和主动 Logout。Clash TUN 在场但未被选中。
这是单台机器、单个网络环境的 Alpha 证据，不代表已经覆盖所有 Windows 版本、网卡或
校园网络变体。机构 Profile 仍由用户放入本地配置目录，仓库不包含个人配置或凭据。

长期进度见 [产品路线图](docs/roadmap.md)，模块关系见
[当前架构](docs/architecture.md)。

当前代码通过共享的 `internal/productlayout` 解析两种显式运行目录模式。默认安装版
仍使用操作系统用户目录：配置位于 `os.UserConfigDir()/Sidravia`，运行信息与后台日志
位于 `os.UserCacheDir()/Sidravia`。在可执行文件同目录放置空标记文件
`sidravia.portable` 即启用便携版，此时配置、凭据与机构 Profile 位于
`<exe-dir>/config`，运行信息位于 `<exe-dir>/runtime`，后台日志位于
`<exe-dir>/logs`；便携版不依赖 AppData。CLI 与 daemon 每次启动各自解析同一标记，
用户增删标记前必须先停止 daemon。

## 运行日志

直接运行 `sidraviad.exe` 时，结构化运行日志写到 stderr，使用标准库 `log/slog` 的
TextHandler、默认 Info 级别。由 Windows CLI 后台启动时，stdout 和 stderr 都写入
`%LOCALAPPDATA%\Sidravia\logs\sidraviad.log`；启动前达到 10 MiB 会轮转为唯一备份
`sidraviad.log.1`，之后创建新的当前文件。`SIDRAVIA_LOG_LEVEL` 可设置为 `info`、
`debug` 或 `trace`；`trace` 启用完整 D520 数据报日志（含账号与认证材料，显式敏感）。
每条日志带稳定 `event` 码、固定简体中文 `msg` 和该事件允许的安全属性，
例如：

```text
time=2026-07-27T... level=INFO msg=守护进程运行已启动 event=daemon_runtime_started product_version=... build_id=... pid=...
time=2026-07-27T... level=INFO msg=IPC 请求已完成 event=ipc_request_completed method=daemon.status
```

Info/Debug 永不包含密码、token、凭据、Profile JSON、MAC、DNS/DHCP、网卡 ID、
请求/响应字节或原始 error；它们包含完整账号名、友好接口名和所选 IPv4。Trace
数据报记录是唯一含完整报文字节的位置，且仅在显式启用 Trace 时出现。后台 Trace
字节会持久化到上述当前文件或单备份，分享前必须按敏感材料处理。直接前台运行时仍可
自行重定向 stderr。当前不提供日志 IPC、`daemon logs` 或实时 tail。

## CLI 呈现

`sidravia` CLI 的帮助、daemon 状态、Session 详情/列表、Profile 列表、密码提示和错误
消息统一使用简体中文，并在括号内保留稳定英文机器码以便识别状态或失败。终端着色由
`internal/cli` 的呈现边界拥有（见 [ADR 0014](docs/decisions/0014-cli-presentation.md)）：
只在真实交互终端启用，重定向或管道输出始终是纯文本，`NO_COLOR` 始终禁用着色，
`CLICOLOR_FORCE` 无法在重定向时重新启用颜色。每个来自 daemon 的动态值在写入前都清理
控制字符，因此无法注入 ANSI 序列或新输出行。

根命令、资源组、普通 `help <path>`、`-h` 和 `--help` 共享同一份分层中文规格。当前
daemon 命令为 `status/start/stop/restart`；Session 命令为
`auth list/start/status/stop/restart/remove`；Profile 摘要使用 `profile list`。
`daemon status` 严格只读，不会为了查询而启动 daemon。

```powershell
.\sidravia.exe
.\sidravia.exe help daemon
.\sidravia.exe auth --help
.\sidravia.exe help auth start
```

```text
守护进程：运行中（running） | 版本：1.0.0 | 构建：build-1 | PID：42
```

```text
会话：session-1
状态：已认证（authenticated）
机构：吉林大学（JLU）
协议：drcom-5.2.0-d
账号：2024012345
更新时间：2026-07-27T02:25:38+08:00
```

交互式 `auth start` 在 stderr 显示 `密码： ` 提示并关闭回显；非交互式调用必须显式
使用 `--password-stdin`。密码永不进入命令行、错误、普通输出或日志。

当前大版本保持 Cobra + termenv 的轻量逐行交互，不引入全屏 TUI。持久配置入口需要的
标题、字段、选择和确认会复用现有呈现与密码输入边界，同时为脚本保留完整的非交互参数
形式；详见 [ADR 0021](docs/decisions/0021-lightweight-line-oriented-cli.md)。

## Windows Alpha 使用

安装、创建本地 Profile、启动认证与停止认证见
[Windows Alpha 快速开始](docs/getting-started-windows.md)。本版本仍有明确限制：
没有 GUI、安装器、Windows Service、自动更新、自动连接或多活动 Session；
它适合愿意使用 PowerShell 并能自行保留原网络客户端作为回退的测试者。

本轮 operator correction 的代码与自动验证已经完成，但修正后的 status/help/后台日志
尚未进行最小 Windows 原生复核。
停止与重启锁定最初探测到的精确 daemon generation；Linux（包括 WSL）已实现 daemon
host、基本 Environment Observer、CLI 进程控制与 termios 密码输入作为运行时基线
（[ADR 0025](docs/decisions/0025-linux-wsl-platform-baseline.md)），可在 WSL 中完成
真实生命周期 smoke；macOS 仍不受支持。Session ensure/restart/remove 已进入当前源码。
接下来的当前大版本顺序是：网络诊断与选择修正，最后统一修订文本和文档。WSS、IPC 长连接
强化和 GUI 放在下一大版本；见
[ADR 0020](docs/decisions/0020-explicit-installed-and-portable-layouts.md) 与
[ADR 0022](docs/decisions/0022-credentials-before-ipc-transport-hardening.md)。

## 构建

需要 Go 1.26.4：

```bash
go build -o build/sidravia ./cmd/sidravia
go build -o build/sidraviad ./cmd/sidraviad
```

Windows amd64 交叉构建：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/sidravia.exe ./cmd/sidravia
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/sidraviad.exe ./cmd/sidraviad
```

这些手工命令只用于本地开发，不负责版本注入、可复现 zip、便携 marker 或校验清单；不应把
手工 `build/` 输出描述成规范发布包。

### 可复现打包

Sidravia 使用唯一的标准库 Go 构建工具生成可复现的 Windows amd64 包。从仓库根目录调用
（需要 Go 1.26.4）：

```bash
go run ./tools/build \
  --version <MAJOR.MINOR.PATCH[-prerelease]> \
  --build-id <stable-build-id> \
  --output <new-output-directory> \
  [--go <go-executable>]
```

`--version` 不带前导 `v`，`--build-id` 由调用方提供（例如 commit hash），`--output` 必须
原先不存在且其父目录已存在。工具固定使用 `GOOS=windows GOARCH=amd64 CGO_ENABLED=0
GOAMD64=v1`、`-trimpath`、`-buildvcs=false`、只读 module 模式和空 Go linker build ID；
daemon 的 `main.ProductVersion` 与 `main.BuildID` 通过 linker flags 注入。一次调用只编译
`sidravia.exe` 与 `sidraviad.exe` 各一次，普通包与便携包复用同一对二进制。

输出目录精确只有三个文件：

- `sidravia-v<version>-windows-amd64.zip`：默认安装版，不含 `sidravia.portable`，运行时
  使用 `%APPDATA%\Sidravia` 与 `%LOCALAPPDATA%\Sidravia`；
- `sidravia-v<version>-windows-amd64-portable.zip`：便携版，额外含空 marker
  `sidravia.portable`，运行时使用解压目录下的 `config`、`runtime` 和 `logs`；
- `SHA256SUMS.txt`：两个 zip 的 SHA-256，小写 hex、两个空格、按文件名字典序。

两个 zip 内都包含 `BUILD-INFO.txt`、`GETTING-STARTED.md`、`LICENSE`、`README.md`、内部
`SHA256SUMS` 和两个 PE 文件；便携 zip 额外含 `sidravia.portable`。相同源码、Go 1.26.4、
版本与 BuildID 的重复构建得到逐字节相同的二进制、zip 和校验文件。工具不运行 Git、不
签名、不上传、不创建 tag 或 GitHub Release。详见 ADR 0023。

GitHub Actions 中已提交 `Verify`（push/pull request 运行公共 verifier）与 `Package`
（手动 `workflow_dispatch` 调用同一构建工具并上传三个文件作为临时 artifact）两个工作流；
它们的具体运行与签名/Release 状态见[产品路线图](docs/roadmap.md)。

## 验证

运行完整的公开仓库检查：

```bash
python3 tools/developer/verify_repository.py --scope all
```

它检查 Go 格式、测试、vet、Windows 交叉构建，以及 Python mock/验收工具测试。
涉及本地 UDP 或 HTTP 测试时，运行环境必须允许回环监听。

## 文档

- [文档入口](docs/README.md)
- [当前架构](docs/architecture.md)
- [工程实践](docs/engineering.md)
- [产品路线图](docs/roadmap.md)
- [架构决策](docs/decisions/README.md)
- [Dr.COM 5.2.0(D) 协议规范](docs/protocols/drcom-5.2.0-d.md)
- [验收证据](docs/evidence/README.md)
- [Windows Alpha 快速开始](docs/getting-started-windows.md)
- [Linux/WSL 快速开始](docs/getting-started-linux.md)

## License

Sidravia 使用 [GNU Affero General Public License v3.0](LICENSE)。
