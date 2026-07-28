# ADR 0013：安全的 daemon 运行日志

**状态：** Accepted

## 背景

`sidraviad` 首版只有一次未结构化的 package-global `log.Printf` 调用和一条
直接写到 stderr 的原始错误行。它们既没有稳定的事件码，也无法保证秘密或诊断
原因不进入日志。随着 CLI 命令树、Session 列表和 Profile 列表接近首版可用，
daemon 需要一个可观察但安全的运行日志边界：人类能在不泄漏秘密的前提下知道
daemon 启动、运行、网络快照和 IPC 传输发生了什么，而脚本和未来 GUI 只依赖
稳定机器事件码。分层诊断（Info/Debug/Trace）的完整契约见
[ADR 0015](0015-tiered-daemon-diagnostics.md)。

## 决定

daemon 使用标准库 `log/slog`，在 `cmd/sidraviad` 构造唯一生产
`*slog.Logger`，使用 `TextHandler`、stderr。logger 显式传入生产装配、
`composedRuntime` 和 IPC Server，不修改 package-global default logger。
核心 Session、Supervisor、D520、持久化和配置包不导入 `log/slog`。

`SIDRAVIA_LOG_LEVEL` 选择运行日志级别，精确接受小写值 `info`、`debug` 和
`trace`；未设置表示 `info`。`trace` 是低于 `debug` 的自定义级别，启用完整
D520 数据报日志。非法值导致 daemon 构造失败，且不回显该值。启用 `trace` 时，
daemon 在任何数据报之前发一条 Warn 事件 `trace_logging_sensitive`，提醒完整
报文可能含账号与认证材料。

每条运行日志都带 `level`、`msg`（固定简体中文摘要）和 `event`（稳定
snake_case 机器码），且只携带该事件允许的安全属性：

- `daemon_start_failed`（Error）：无属性；
- `daemon_runtime_started`（Info）：`product_version`、`build_id`、`pid`；
- `daemon_runtime_failed`（Error）：无属性；
- `daemon_runtime_stopped`（Info）：无属性；
- `network_snapshot_applied`（Info）：`revision`、`interface_count`；
- `ipc_upgrade_failed`（Warn）：无属性；
- `ipc_connection_opened`（Debug）：无属性；
- `ipc_request_completed`（Debug）：`method`；
- `ipc_request_rejected`（Warn）：`method`、`error_code`；
- `ipc_response_failed`（Warn）：`stage`。

`method` 只接受当前 IPC 契约方法，其他值归一化为 `unknown`；`error_code`
只接受当前契约错误码，其他值归一化为 `internal_error`；`stage` 只接受
`encode` 和 `write`。固定中文消息绝不由 error、请求或响应构造。

进程边界把 fatal reporting 与 `os.Exit` 分离：构造失败只发一条
`daemon_start_failed`，运行失败只发一条 `daemon_runtime_failed`，两者都不含
返回的 error 或原因，但仍把原始 error 保留给调用方用于所有权和测试。正常启动
记 `daemon_runtime_started`，正常完成记 `daemon_runtime_stopped`，失败运行不
再记正常停止事件。

`network_snapshot_applied` 只在 Application 接受快照后记录，只含 revision 和
`len(snapshot.Interfaces())`，不枚举接口。成功 IPC 请求完成与连接建立从 Info
移到 Debug；拒绝与响应失败保留 Warn 严重度。Session 与协议诊断事件的等级与
隐私契约以 ADR 0015 为准。日志是观察层，不改变 IPC 响应、错误码、生命周期
取消、goroutine 所有权或退出结果。

## 隐私边界

Info 与 Debug 记录 operator-significant 的公开标识：`account_name`（完整
账号名）、`institution_display_name`、`selected_interface_name`（友好名称）
和 `selected_ipv4`。机器 `InterfaceID`、MAC、DNS、DHCP、网关与主机名仍不
进入日志。

日志永不记录：`error.Error()` 或包装的诊断原因；请求/响应字节、JSON 或报文；
request ID；Authorization 头、token、endpoint URL 或运行信息路径；密码或
凭据 ID；Profile JSON 或协议上下文；网卡 ID、MAC、DNS、DHCP、网关或主机名；
peer 提供的任意 method/error/stage 字符串。Trace 数据报记录是唯一允许包含
完整账号与认证派生字节的位置，且仅在显式启用 Trace 时出现。

## 结果

- daemon 运行日志可观察且安全：稳定事件码和固定中文消息供人类阅读，安全属性
  白名单和归一化保证秘密、诊断原因和 peer 任意字符串不进入日志。
- 进程边界错误只记录一次，原始原因仍由错误传播保留，日志不替代错误所有权。
- 分层诊断（Info/Debug/Trace）与完整 D520 数据报记录由 ADR 0015 定义，本 ADR
  只覆盖进程与 IPC 边界。
- 本切片不写日志文件、不做轮转、不暴露日志 IPC/CLI 命令、不加终端样式。
- Windows 原生和校园现场尚未重新验证这些日志。
