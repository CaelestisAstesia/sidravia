# Sidravia 架构决策记录

ADR 只解释会长期影响产品边界、且未来可能被重新质疑的决定。普通文件名、私有 API 和局部实现不建立 ADR。

| ADR | 决定 | 状态 |
|---|---|---|
| [0001](0001-loopback-websocket-ipc.md) | 本机回环 WebSocket IPC | Accepted |
| [0002](0002-configuration-credential-separation.md) | 配置与凭据分离 | Accepted |
| [0003](0003-session-identity-lifetime.md) | SessionID 仅属于 daemon 运行期 | Accepted |
| [0004](0004-single-active-session.md) | 首版最多一个活动 Session | Accepted |
| [0005](0005-snapshot-event-model.md) | revision + 最新完整 Snapshot | Accepted |
| [0006](0006-windows-credential-json-acl.md) | 首版使用当前用户 ACL 保护凭据 JSON | Accepted |
| [0007](0007-websocket-and-windows-system-dependencies.md) | WebSocket 与 Windows 系统依赖 | Accepted |
| [0008](0008-permanent-one-shot-session-start.md) | 永久保留 typed 一次性 Session 启动 | Accepted |
| [0009](0009-d520-blocking-run-and-explicit-wire-codec.md) | D520 使用阻塞式 Run 和显式线级 codec | Accepted |
| [0010](0010-supervisor-distributes-network-snapshots.md) | Supervisor 保存并分发最新系统网络快照 | Accepted |
| [0011](0011-daemon-composition-owns-runtime-lifecycle.md) | daemon 组合根统一拥有生产运行期 | Accepted |
| [0012](0012-two-phase-session-stop.md) | Session 停止分为 stopping 和 suspended 两阶段 | Accepted |
| [0013](0013-safe-operational-logging.md) | 安全的 daemon 运行日志 | Accepted |
| [0014](0014-cli-presentation.md) | CLI 呈现边界 | Accepted |
| [0015](0015-tiered-daemon-diagnostics.md) | 分层 daemon 诊断 | Accepted |
| [0016](0016-daemon-lifecycle.md) | daemon 生命周期与精确 generation 控制 | Accepted |
| [0017](0017-retained-session-lifecycle.md) | retained Session 生命周期控制 | Accepted |
| [0018](0018-unified-cli-help.md) | 统一、分层且可导航的 CLI 帮助 | Accepted |
| [0019](0019-background-daemon-log-files.md) | Windows 后台 daemon 日志文件 | Accepted |

修改 Accepted ADR 时，新建替代 ADR，不静默重写历史理由。
