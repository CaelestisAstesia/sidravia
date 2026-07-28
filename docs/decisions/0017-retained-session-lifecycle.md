# ADR 0017：retained Session 生命周期控制

**状态：** Accepted

## 决定

当前 daemon 进程内保留的 Session 可以按同一 SessionID 执行 ensure-running、restart
和 remove。Supervisor 是这些原子决策、单活动准入和每个 Session 生命周期串行化的唯一
所有者；CLI、IPC 与 Application 不拼接 get/stop/start。

ensure-running 对活动公开状态无扰动；对 `suspended` 使用 `Activate`。restart 是明确
动作，对所有公开状态使用 `Restart`。两者遇到 `stopping` 时保留同一 Session 的准入槽
并等待权威 `suspended` revision，绝不覆盖停止边界。

remove 对在线 Session 执行 stop、等待清理退出、shutdown、等待 revision forwarder
退出并从 Supervisor 删除。成功返回时 `session.get` 与 `session.list` 已不可观察该 ID。

SessionID、一次性密码与 runtime definition 都只存在于当前 daemon 进程内。ensure 与
restart 不读取、不替换、不持久化密码。

## 结果

同 ID 生命周期操作不会重叠 actor command；等待取消会释放预留且不留下延迟启动。
WSS、持久 Configuration/Credentials、GUI 与多活动 Session 不由本决定实现。
