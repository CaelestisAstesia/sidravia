# Sidravia Linux/WSL 快速开始

本文说明在 Linux（包括 WSL）上从源码构建并运行 Sidravia daemon 基线的方式。Linux 基线
与 Windows 共享同一 domain core、IPC 契约、Authentication Configuration 聚合、运行目录
布局和日志契约；它是一个真实可运行的开发与运行时基线，**不声明能在 WSL 中认证 Windows
校园接口**，也不替代 Windows-native 与校园现场验证。macOS 仍返回 Unsupported。

## 前置条件

- Go 1.26.4（与 Windows 构建一致）。
- 允许回环监听与本地进程启动的普通用户环境。

## 原生构建

从仓库根目录构建两个二进制：

```bash
go build -trimpath -buildvcs=false -o build/sidravia ./cmd/sidravia
go build -trimpath -buildvcs=false \
  -ldflags '-X main.ProductVersion=0.1.0-dev -X main.BuildID=linux-dev' \
  -o build/sidraviad ./cmd/sidraviad
```

`main.ProductVersion` 与 `main.BuildID` 只注入 `sidraviad`。CLI 从当前 daemon 发布的
owner-only `runtime.json` 读取 endpoint、token 和 BuildID，再把该 BuildID 放入 IPC
连接头；`sidravia` 本身没有独立的 BuildID linker 变量。

## 安装版与便携版布局

Sidravia 通过 `internal/productlayout` 解析两种显式运行目录模式，CLI 与 daemon 各自解析
同一标记：

- **安装版（默认）**：认证配置与机构 Profile 位于 `os.UserConfigDir()/Sidravia`，运行
  信息与后台日志位于 `os.UserCacheDir()/Sidravia`。在 Linux 上通常对应
  `~/.config/Sidravia` 与 `~/.cache/Sidravia`。
- **便携版**：在可执行文件同目录放置空标记文件 `sidravia.portable` 即启用。此时配置位于
  `<exe-dir>/config`，运行信息位于 `<exe-dir>/runtime`，日志位于 `<exe-dir>/logs`。

便携版布局：

```text
<exe-dir>/
  sidravia
  sidraviad
  sidravia.portable
  config/
    configurations.json
    institution-profiles/
  runtime/
    runtime.json
    runtime.json.lock
  logs/
    sidraviad.log
```

增删标记前必须先停止 daemon，避免两个进程在同一时刻使用不同模式。`daemon status` 严格
只读，只解析路径并读取运行信息，不创建目录、日志、锁或进程。

## Profile 放置

机构 Profile 是本地可编辑文件。安装版放在
`~/.config/Sidravia/institution-profiles/<InstitutionProfileID>.json`，便携版放在
`<exe-dir>/config/institution-profiles/<InstitutionProfileID>.json`。ID 使用简短稳定标识
（例如 `jlu`）。daemon 只在启动时加载目录，本地编辑后重启 daemon 生效。仓库不包含个人
Profile 或凭据。

## daemon 生命周期

```bash
./sidravia daemon status      # 严格只读探测
./sidravia daemon start       # 启动同目录 sidraviad
./sidravia daemon restart     # 停止当前 generation 再启动新 daemon
./sidravia daemon stop        # 向当前 generation 发送 daemon.stop
```

CLI 用 `setsid` 启动兄弟 `sidraviad`，子进程不共享 CLI 终端会话；stdin 为 nil，
stdout/stderr 都写入运行目录中的 `sidraviad.log`。daemon 用 `flock` 在
`runtime.json.lock` 上持有非阻塞排他锁保证同用户单实例；已持有锁返回固定“already
running”错误。退出后锁文件保留为零长度 owner-only 协调 inode。

## 配置与认证命令

```bash
./sidravia config list
./sidravia config create
./sidravia config set-password <configuration-id>
./sidravia auth start --config <configuration-id>
./sidravia auth status <session-id>
./sidravia auth stop <session-id>
./sidravia profile list
```

交互式 `auth start` 在真实终端显示 `密码： ` 提示并关闭回显；Linux 使用 termios
`TCGETS`/`TCSETS` 保存状态、清除 `ECHO`、读取一行、恢复原状态。非交互调用必须显式使用
`--password-stdin`。密码永不进入命令行、错误、普通输出或日志。

## 日志

直接运行 `sidraviad` 时结构化运行日志写到 stderr（`log/slog` TextHandler，默认 Info）。
由 CLI 后台启动时 stdout/stderr 都写入 `sidraviad.log`，启动前达到 10 MiB 轮转为唯一
`.1` 备份。`SIDRAVIA_LOG_LEVEL` 可设为 `info`、`debug` 或 `trace`；`trace` 启用完整 D520
数据报日志（含账号与认证材料，显式敏感）。Info/Debug 永不包含密码、token、凭据、MAC、
DNS/DHCP、网关、网卡 ID、请求/响应字节或原始 error。

## 权限与文件模式

便携版与安装版的运行目录、runtime 文件和锁文件都使用 owner-only 权限：运行目录 0700，
`runtime.json` 与 `runtime.json.lock` 0600，`sidraviad.log` 0600。便携版在文件系统明确
不支持所需权限模型时可进入可观察的 `unprotected` 状态，但普通 IO 或拒绝访问错误不降级；
在 unprotected 状态新增或替换密码需要逐次显式授权（交互确认或非交互
`--allow-insecure-storage`）。

## WSL 限制与状态标签

- WSL 中的虚拟接口（如 `veth*`、Hyper-V 内部接口）会出现在网络 Snapshot 中，但被分类为
  `EndpointInterface=true` 且 `HardwareBacked=false`，**不能**通过现有自动物理绑定谓词。
  本基线不实现面向认证服务器目标地址的路由、interface metric、`/proc/net/route`、
  `resolv.conf` 或 DHCP 发现。
- 因此 WSL 中 `auth start` 通常无法选择可用绑定；这是已知的基线范围，不代表代码缺陷。
- `daemon status` 输出的状态标签：`守护进程：运行中（running）` 或 `守护进程：已停止
  （stopped）`；状态未知时返回固定安全错误，不清理运行信息。
- 本基线不提供 Linux 包、service manager 单元、WSS、GUI 或 Windows-native 重跑；这些属
  后续切片。
