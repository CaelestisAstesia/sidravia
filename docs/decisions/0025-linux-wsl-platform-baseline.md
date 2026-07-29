# ADR 0025：Linux/WSL 平台基线

状态：Accepted

## 背景

首版产品在 Windows 上完成首个真实纵向链路与校园现场验证。Linux（包括 WSL）此前
只保留可编译的平台边界：daemon host、Environment Observer、CLI 进程控制和交互式
密码输入在非 Windows 上都返回固定 Unsupported。为了让 Sidravia 在 Linux/WSL 上成为
真实可运行的开发与运行时基线，需要一个不分叉 domain core、不改变公开
CLI/IPC/domain 契约的 Linux 实现。

## 决定

Linux 与 Windows 共享同一 domain core、IPC 契约、Authentication Configuration 聚合、
显式安装版/便携版运行目录布局和后台日志契约。Linux 只新增平台边界实现，不新增公开
接口、不改协议、不改自动绑定选择规则、不改 Windows 代码。`golang.org/x/sys` 已是直接
依赖，不新增依赖。

### 单实例与进程生命周期

Linux daemon host 用 `flock` 在 `RuntimeInfoPath + ".lock"` 上持有非阻塞排他锁来保证
同用户单实例，替代 Windows 的命名 mutex。锁文件父目录以 0700 创建，锁文件以
`O_CREAT|O_RDWR|O_CLOEXEC|O_NOFOLLOW` 打开、`Fchmod` 加固为 0600、
`Flock(LOCK_EX|LOCK_NB)` 持有整个 host 生命期。已持有锁返回固定“already running”
错误且不含路径；退出后锁文件保留为零长度 owner-only 协调 inode，不 unlink，以避免
inode 替换竞态。runtime info 仍只在存储 PID 和 token 仍匹配时删除，listener 只监听
`127.0.0.1:0`，停止由父取消、`os.Interrupt` 或 `SIGTERM` 触发现有有界 graceful HTTP
shutdown。

CLI 用 `syscall.SysProcAttr{Setsid: true}` 启动同目录兄弟 `sidraviad`，子进程不共享
CLI 终端会话；stdin 为 nil，stdout/stderr 都指向运行目录解析出的 `sidraviad.log`，
环境用现有确定性 `daemonEnvForLevel`。成功 start 后调用 `Process.Release`，因为 CLI
不拥有后续 Wait；父进程只关闭自己的日志句柄。`daemon status` 仍严格只读，不创建目录、
日志、锁或进程。

### 交互式密码输入

Linux 用 `unix.IoctlGetTermios(TCGETS)` 和 `unix.IoctlSetTermios(TCSETS, ...)` 实现
隐藏输入：接受 `*os.File` 终端，写 `密码： ` 提示到 stderr，保存完整 termios 状态，
只清除 `ECHO`，通过现有 `readPasswordLine` 读取，写一个换行，再恢复原状态。读取、
换行和恢复失败合并，使 `errors.Is` 能观察每个原因。非文件或非终端输入返回固定中文
指令改用 `--password-stdin`，不读取密码字节、不回显秘密。4096 字节上限、stdin 模式、
argv 禁止与 Windows 实现不变。

### 主机信息与基本 Environment Observer

Linux host information 从 `os.Hostname` 读非空主机名，从 `unix.Uname` 转 NUL 终止的
内核版本，返回 `OperatingSystemFamily == "Linux"`、非空内核版本和 `runtime.GOARCH`，
空主机名或版本被拒绝。Linux Environment Observer 用 `net.Interfaces` 和每个接口的
`Addrs` 采集，不 shell out、不启动 goroutine，用现有平台中立 `systemObserver` 以
两秒间隔轮询。

Observer 故意保守：省略 loopback；用内核接口名作为稳定 `InterfaceID` 和当前
`DisplayName`；只保留有效、非 unspecified、非 loopback、非 multicast 的 IPv4 unicast
及真实前缀长度，并排序去重；Up/Down 来自 `net.FlagUp`；只有
`/sys/class/net/<name>/device` 存在才标记 hardware-backed，`PhysicalConnectorPresent`
等于该保守事实；hardware 且 `/sys/class/net/<name>/wireless` 为 wireless，hardware 且
Ethernet 长度 MAC 为 wired，其余 unknown；非 hardware-backed 接口
`EndpointInterface=true`，不用适配器名黑名单；`FilterInterface=false`，assignment
method unknown，gateway、DNS、DHCP 为空。sysfs device/wireless 路径不存在是普通 false
事实，其他 stat 错误和 `Interfaces`/`Addrs` 错误停止采集并保留原因。

因此 WSL、容器、隧道等虚拟接口可能出现在 Snapshot 中，但 `EndpointInterface=true` 且
`HardwareBacked=false`，不能通过现有自动物理绑定谓词。该谓词和 route-aware 网络选择
不在本基线修改。

### 平台边界

macOS 和其他非 Linux/非 Windows 目标继续返回固定 Unsupported。Windows 生产代码、公开
IPC/domain 契约、自动绑定选择规则和构建/打包工作流不变。

## 结果

- Linux/WSL 成为真实支持的开发与运行时基线，可在 WSL 中完成 native Go 构建、安装版/
  便携版布局、daemon 生命周期、IPC、Configuration 和日志的真实 smoke。这不声明 WSL
  能认证 Windows 校园接口，也不声明校园现场验证。
- 代码完成与自动验证（聚焦测试、race、三平台编译、WSL/Linux-native portable lifecycle、
  公开 verifier）与 Windows-native 验证和校园验证分开报告。
- route-aware 网络选择（面向认证服务器目标地址的路由/源 IP、interface metric、
  `/proc/net/route`、`resolv.conf`、DHCP 发现）、Linux 包/服务管理、WSS 和 GUI 仍属
  后续切片。
- macOS 仍 Unsupported；不为本基线引入通用 RPC、通用日志框架、抽象终端接口或测试接口。
