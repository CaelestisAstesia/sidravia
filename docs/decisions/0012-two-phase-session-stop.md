# ADR 0012：Session 停止分为两阶段

**状态：** Accepted

## 背景

Dr.COM 的主动停止不是一个瞬时的本地状态赋值。当前协议 Run 收到取消后，仍可能在独立
的有界时间内完成 Challenge、Logout 和 ACK 交换。旧行为在请求取消后立即把 Session
公开为 `suspended`，Supervisor 也立即释放单活动准入槽；此时旧 Run 可能仍持有 socket
并执行退出，而新的认证 Run 已经被允许启动。

等待整个协议清理再返回 Stop 又会让 CLI 和 IPC 请求承担协议超时，并掩盖“请求已经被
接受”和“资源已经退出”是两个不同事实。

## 决定

首次接受 Stop 时，Session 立即把意图改为
`suspend_authentication`，发布公开状态 `stopping`，并向活动 Run 请求
`terminate_with_best_effort_logout`。Stop 返回这个 Snapshot，不等待协议清理。

活动 Run 退出后，Session 才发布 `suspended`。没有活动 Run 的 Session 也先发布
`stopping`，再通过私有队列事件发布 `suspended`，从而保持统一的可观察顺序。

`stopping` 是权威且不可被 Activate、Restart、网络变化、异步回调或运行定义替换覆盖的
过渡状态。重复 Stop 在 `stopping` 和 `suspended` 中均为幂等操作，不增加 revision，
也不请求第二次清理。

Supervisor 在看到 Session 的权威 `suspended` revision 前一直保留单活动准入槽。只有
随后才允许新 Session 或 Restart。

D520 清理成功时仍返回正常取消结果；清理失败时返回稳定的内部 Run failure。Session
保存该诊断但仍完成本地 suspension。该诊断不进入当前公开 Snapshot、IPC 或 CLI。

daemon shutdown 继续使用同一取消和等待边界，但 Session 关闭准入后不额外发布终态。

## 结果

- Stop 请求保持快速，调用方可以明确观察 `stopping -> suspended`。
- 新旧协议 Run 不会在退出窗口内因过早释放准入而重叠。
- 脚本若需要确认资源已经退出，必须在 Stop 成功后查询 Snapshot 直到
  `suspended`，不能只依赖 Stop 命令退出码。
- 协议清理失败不会使本地 Session 永久卡在 `stopping`，也不会自动触发认证重试。
- 公开清理诊断与结构化日志呈现留给后续独立设计，不扩大本次 IPC 契约。
