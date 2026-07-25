# ADR 0008：保留一次性 Session 启动

状态：Accepted

## 决定

Sidravia 永久支持不保存 Configuration 和 Credential 的一次性连接。

一次性连接通过 typed IPC payload 提交必要的非秘密参数和秘密。daemon app 验证请求并把它转换为内部 `RunDefinition`。未来按 `ConfigurationID` 启动时，daemon app 从 Configuration、Credentials 和 Environment 解析出同一种 `RunDefinition`。

IPC server 只负责连接鉴权、typed payload 解码、调用 daemon app 的窄 Handler 和 Response 编码。它不直接操作 Configuration Catalog、Credentials Store、Environment Detector、Supervisor、Session 或协议实现。

## 原因

最早实现 Configuration、Credentials 和 Environment 的完整管理入口，会推迟对 Session 生命周期、协议取消、保活和 Dr.COM 行为这些高风险核心的验证。

一次性连接允许产品先贯通：

```text
CLI -> IPC -> daemon app -> Supervisor -> Session -> Protocol
```

它本身也是合理的长期产品能力，而不是未来必须删除的临时兼容层。

## 结果

- 下一阶段先实现最小 Session 运行链和协议契约，再实现 Dr.COM mock 纵向链路。
- Configuration、Credentials 和真实 Environment 输入随后接入同一个 `RunDefinition` 边界。
- 一次性秘密不得进入命令行参数、日志、公开 Snapshot 或 Response，也不得由 daemon 持久化。
- CLI 应通过交互输入或 stdin 接收一次性秘密；具体命令语法由后续产品计划确认。
- IPC contract 不接受任意 `map[string]any` 参数袋。一次性启动仍使用明确 typed payload。
- mock 所需的环境事实优先由测试装配提供，不把任意环境覆盖暴露成普通用户接口。
