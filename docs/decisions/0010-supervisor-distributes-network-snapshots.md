# ADR 0010：Supervisor 保存并分发最新系统网络快照

状态：Accepted

## 决定

Environment Detector 是当前系统网络事实的权威来源。daemon app 接收 Detector 产生的
typed `environment.Snapshot`，再把它交给 Supervisor。

Supervisor 保存自己已经接受的最新网络快照，并把该快照送给所有已存在的 Session。
Supervisor 创建新 Session 时，必须先把最新快照送入该 Session，再返回初始公开
Snapshot。Session 继续独立选择绑定、决定是否启动或取消协议 Run，并拥有所有公开认证
状态。

网络 revision 的处理规则：

- revision 大于当前值时，Supervisor 保存并分发新快照；
- revision 小于当前值时，Supervisor 忽略旧快照；
- revision 等于当前值时，Supervisor 重放自己保存的权威快照，不接受同 revision 的
  不同内容替换它。

分发失败不回滚 Supervisor 保存的最新快照，也不回滚已经成功接收快照的 Session。调用者
可以用相同 revision 重试；Session 已有的 revision 门会让重放保持幂等。

## 原因

Session 已经拥有绑定选择、网络变化后的协议取消和重新认证逻辑，但新建 Session 当前
无法获得 daemon 已经观察到的网络事实，因此会永久停在 `waiting_for_network`。

Environment Detector 不应管理 Session 集合，daemon app 也不应复制 Session 的绑定状态。
Supervisor 已经是 Session 集合和创建顺序的唯一所有者，因此它是保证“现有 Session
收到更新、新 Session 不错过最近快照”的最小稳定边界。

保存最新快照后再分发，可以让新 Session、失败重试和并发 Start/Apply 收敛到同一份事实。
允许相同 revision 重放，则避免一次调用 context 取消或单个 Session 关闭后永久丢失该
revision。

## 结果

- Supervisor 不探测网卡，不读取 Windows API，也不解释 Profile 或协议字段。
- daemon app 只转交 typed 快照；普通 IPC 一次性启动请求仍不接受任意环境参数。
- Supervisor 不创建网络观察 goroutine。真实 Detector 的生命周期和调用循环属于后续
  生产装配。
- 每个 Session 仍是其绑定选择、协议 Run 和认证 Snapshot 的唯一所有者。
- 后续真实 Windows Detector 或测试装配只需调用同一个
  `ApplySystemNetworkSnapshot` 应用边界。
