# ADR 0029：Configuration 拥有自动登录与重连

**状态：** Accepted

## 决定

Windows 进程登录启动、Configuration 自动登录与 Session 自动重连是三个
不同行为。每份持久 Authentication Configuration 自己拥有 `AutoLogin` 和
`AutoReconnect`，不建立全局 Settings 所有者。

- 最多一份 Configuration 可以启用 `AutoLogin`；
- daemon 每个 generation 在首个已接受 Environment Snapshot 后只评估一次；
- `AutoReconnect` 冻结进 retained Session 的不可变 `RuntimeDefinition`；
- 一次性认证始终自动重连；
- 显式 stop/remove/restart 始终优先。

Configuration catalog 从 schema 2 演进到 schema 3。schema 2 严格解码，默认
`AutoLogin=false` 与 `AutoReconnect=true`；schema 3 要求两个布尔字段，且是唯一
写出格式。仅打开有效 schema 2 文档不会重写它。

本 ADR 只取代 ADR 0024 中“自动连接与 Settings 尚未接入”的延后陈述；
ADR 0024 的聚合、秘密与便携安全边界不变。
