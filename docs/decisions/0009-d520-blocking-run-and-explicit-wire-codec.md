# ADR 0009：D520 使用阻塞式 Run 和显式线级 codec

状态：Accepted

## 决定

Dr.COM 5.2.0(D) 的一次认证执行由一个阻塞式
`AuthenticationProtocolRun.Execute` 完成。Run 独占一个 UDP socket，并按确定顺序执行：

```text
Challenge
-> Login（仅 server busy 在 Run 内做有界短重试）
-> KA1 + KA2 1-1-3
-> AuthenticationEstablished
-> 周期性 KA1 + KA2 1-3
-> best-effort Logout
```

Session 继续拥有产品层意图、状态、网络变化、取消、重连调度、公开 Snapshot 和失败后的
处理。D520 不建立第二套接收 Session 命令的通用事件状态机。D520 可以用私有 phase
记录诊断位置，也可以把固定 KA2 序列表示成私有步骤表，但 phase 不负责产品状态迁移。

D520 的 wire codec 保留每种语义请求和每个预期响应的命名 builder/parser。它可以共享
严格长度、opcode、固定字段、文本编码和密码学 helper；Login 成功与拒绝由同一个
Login response parser 区分。实现不得建立反射式通用报文 schema，也不得根据收到的
任意 UDP 数据启发式猜测协议阶段。

长 Login builder 应按协议布局区域拆分，并优先使用固定长度报文类型、明确偏移和经过
预验证的 wire 输入。运行时的 salt、Auth Info、KA2 Tail、serial、timestamp 和随机
tail 仍由单次 Execute 的私有状态持有，不得进入 Profile、IPC 或包级可变状态。

## 原因

D520 是单 socket、单请求、单响应的串行协议。它只有 Login busy 短重试、固定 bootstrap
序列、周期性 heartbeat 和取消清理几个分支。通用启发式状态机会引入无效转移、错序包
解释和第二套生命周期所有权，却不能改善当前真实行为。

Session 已经是 Sidravia 的认证状态机。让 D520 再接收激活、暂停、重启等命令，会模糊
重试、停止和公开状态的唯一所有者。

每类报文具有不同的必填字段、长度和上下文约束。特别是 KA1 与 KA2 都可能使用
`0x07` opcode，KA2 还必须验证预期 serial 和 type。一个按 opcode 自动分派的通用
parser 无法安全表达这些约束。

另一方面，旧实现中数百行连续字节追加、运行时随机值生成和流程状态混在同一函数，难以
对照抓包。按线级区域拆分、保留明确偏移和预先编码稳定输入，可以改善可读性而不隐藏协议。

## 结果

- D520 Run 自身不创建长期 heartbeat goroutine；Session 调用 Run 的执行 goroutine
  仍由 Session 取消和等待。
- 每个阻塞 exchange 先设置绝对 socket deadline，再安装短生命周期的 context 取消
  回调，通过把 deadline 推到当前时间唤醒 UDP I/O。exchange 结束时必须停止回调；如果
  回调已经开始则等待它完成，之后才能重设 deadline，避免旧回调覆盖下一次交换或清理。
  实现不使用周期性 deadline 轮询，也不在 Logout 前关闭 socket。
- best-effort Logout 使用独立的短超时；它的失败不得覆盖原始协议失败或阻塞 Session
  停止。该超时不得从已经取消的执行 context 派生。
- Run 只能在 Login 和首次 KA1 + KA2 `1-1-3` 全部完成后调用一次
  `AuthenticationEstablished`。
- 每个 exchange 使用不会因忽略报文而延长的绝对 deadline。完整且能证明属于已经发送
  过的旧 KA1/KA2 交换的响应可以忽略，包括期待 KA1 时迟到的完整 KA2、期待 KA2 时
  迟到的完整 KA1，以及 KA2 的旧 serial/type。无法安全识别、结构畸形或不可能来自旧
  交换的响应必须返回协议失败，不得推动流程。
- 如果未来真实抓包证明服务器主动推送、允许多个未完成请求或要求可恢复的多路径流程，
  可以用新 ADR 替代本决定；首版不得为这些尚未观察到的行为预建框架。
