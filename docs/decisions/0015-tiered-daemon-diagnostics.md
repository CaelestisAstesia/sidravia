# ADR 0015：分层 daemon 诊断

**状态：** Accepted

## 背景

ADR 0013 建立了安全的进程与 IPC 边界日志，但缺少面向认证运营的分层诊断：操作者
需要一条按时间顺序的人类运营轨迹（Session 创建、每次公开状态变更、所选机构与
账号、认证建立、重试与阻塞），需要在 Debug 看到控制流决策（命令、协议运行
代际、阶段边界、重试调度），也需要在显式敏感的 Trace 看到每个 D520 UDP 报文
的完整字节用于现场对照。直接复用 coalescing 的 `RevisionEvents` 流会丢失中间
revision，把 `log/slog` 类型放进 Session/Supervisor/D520 会破坏领域边界，而完整
报文默认输出会泄漏账号与认证材料。

## 决定

daemon 诊断分为三级，级别由 `SIDRAVIA_LOG_LEVEL` 选择（见 ADR 0013）：

- **Info** 是按时间顺序的人类运营轨迹：daemon 运行启动、接受的网絡快照与正常
  停止；Session 创建与每个已提交的公开 Session 状态/revision；所选 Profile、
  完整账号名、友好接口名与所选 IPv4；认证建立、等待/重试/阻塞/停止/暂停状态；
  以及出现时的稳定原因、失败与建议码和重试时间。
- **Debug** 包含 Info 加上不含报文字节的生命周期决策：IPC 连接建立、接受的
  方法完成、拒绝码与响应失败阶段；网络快照 revision/count；Session 命令、
  协议运行代际、D520 阶段开始/结束、重试调度与稳定失败分类；以及诊断控制流
  所需的脱敏稳定标识与数值/时序事实。
- **Trace** 包含 Debug 加上每个 D520 UDP 数据报的一条记录：固定事件与固定中文
  消息；`session_id`、稳定 `phase`、`direction`（`tx` 或 `rx`）、字节 `length`；
  完整小写 `datagram_hex`，每字节恰好两个 hex 字符。

Trace 在真实 socket 写入前记录 TX，在每次完整读取后、分类前记录 RX，包括被
忽略的过期或无效数据报。稳定阶段为 `challenge`、`login`、`bootstrap_ka1`、
`bootstrap_ka2`、`keepalive_ka1`、`keepalive_ka2` 与 `logout`。阶段边界
`begin`/`end` 在 Debug 记录。

## 隐私边界

Debug 永不包含数据报、密码、token、request ID、payload、原始包装 error、peer
提供的 error 消息、Credential ID、MAC、DNS/DHCP 值或不透明协议覆盖。Info 与
Debug 允许 `account_name`、`institution_display_name`、`selected_interface_name`
和 `selected_ipv4`；机器 `InterfaceID`、MAC、DNS、DHCP、网关与主机名仍不进入
日志。Trace 显式敏感：完整报文可能含账号与认证材料。启用 Trace 时在任何数据报
之前发一条 Warn 事件 `trace_logging_sensitive`。Trace 永不默认启用、永不自动
持久化，不提交捕获输出或报文 fixture。

## 架构

- `log/slog` 限制在进程组合根与 IPC server。
- 在 protocol/session 边界定义窄的、传输中立的诊断值/接口类型：
  `protocol.AuthenticationProtocolDiagnostics`（`PhaseEvent`、`DatagramEvent`）
  与 `session.Diagnostics`（`SessionSnapshot`、`SessionCommand`、
  `ProtocolRunGeneration`、`RetryScheduled`）。没有 slog 类型进入 Session、
  Supervisor 或 D520。
- 组合根注入一个生产 adapter 到 Supervisor/Session 和 D520 运行创建。生产
  `sessionDiagnostics` 与 `protocolDiagnosticsAdapter` 选择允许字段并写固定中文
  消息；sink 无返回值，不能改变 Session、协议、重试、IPC 响应、关闭顺序或返回
  值。
- Nil/禁用诊断使用显式 no-op 实现（`NoopDiagnostics`、
  `NoopAuthenticationProtocolDiagnostics`），而不是分散的 nil 检查。
- D520 的 UDP exchange 接收稳定 phase 与诊断 sink；它不从报文内容派生字段。
  `ProtocolDiagnosticsFactory` 把每个协议 sink 绑定到拥有该运行的 Session，使
  Trace 数据报记录包含 `session_id` 而协议运行本身永不知道 SessionID。
- Session 的 `SessionSnapshot` 在每次已提交 revision 时直接调用，不经过
  coalescing 的 `RevisionEvents` 流，因此中间公开 revision 不会丢失。
- 诊断错误永不改变协议、重试、Session 状态、IPC 响应、关闭顺序或返回值。

## 结果

- 操作者获得完整且分层的运营可见性：Info 是人类时间线，Debug 是控制流，Trace
  是逐字节现场对照，且 Trace 的敏感性被显式标记。
- 领域核心边界保持：Session、Supervisor 和 D520 不导入 `log/slog`，只依赖窄
  诊断接口；生产 adapter 在组合根注入。
- 每个 Session revision 都被观察，不因事件 coalescing 而丢失；诊断永不改变行为
  或返回值。
- 本切片不新增日志文件、轮转、日志 IPC/CLI 命令、终端样式或报文 fixture。
- Windows 原生与校园现场尚未重新验证这些诊断。
