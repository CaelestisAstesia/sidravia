# ADR 0004：首版只允许一个活动 Session

状态：Accepted

## 决定

产品可以保存多个配置，也可以保留多个带 ID 的已停止 Session。Supervisor 在首版最多允许一个 Session 处于活动状态。

## 原因

一个活动 Session 已经覆盖个人 Windows 使用场景。未来多会话的主要复杂度来自调度、抢占和网络资源冲突，而不是 Session 本身。

## 结果

Supervisor 负责拒绝第二个活动 Session。它仍可保存和查询已经停止的 Session。

未来如果需要多会话，只扩展 Supervisor 的调度策略。Session、配置和 IPC 标识不应因此重写。
