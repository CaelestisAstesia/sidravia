# Sidravia 当前架构

本文是当前系统结构、状态所有权和稳定运行语义的公开投影。长期取舍见
[架构决策](decisions/README.md)，当前进度和现场证据分别见[产品路线图](roadmap.md)与
[验收证据](evidence/README.md)。

## 产品与进程

Sidravia 首先是 Windows CLI/daemon 产品：

- `sidravia` 是短生命周期 CLI，只收集用户意图、调用 typed IPC 并呈现结果；
- `sidraviad` 是长期运行 daemon，独占配置、秘密、Environment、Session 和协议运行；
- 两个进程只通过本机回环 WebSocket 通信；
- 未来 GUI 只能复用 typed IPC，不能导入 daemon 内部包或直接读取其文件。

CLI 退出不终止 daemon 持有的 Session。daemon 重启会结束运行期 `SessionID`，但不会删除
持久 `ConfigurationID`。首版最多一个活动 Session。

## 依赖方向与边界

```text
cmd/sidravia
  -> internal/cli
  -> internal/ipc/client + internal/ipc/contract

cmd/sidraviad
  -> internal/ipc/server
  -> internal/daemon/app
  -> configuration / environment / supervisor / protocol registry
  -> session
  -> protocol Run
```

稳定规则：

1. `cmd` 只组合进程，不承载领域规则。
2. CLI 不读取 daemon 配置、runtime 文件或内部状态。
3. IPC server 只经窄 Handler 调用 daemon app。
4. daemon app 只拥有跨模块用例顺序，不复制模块状态。
5. Supervisor 创建和管理 Session；Session 不读取 Supervisor 状态。
6. Session 只依赖协议契约、不可变运行定义和 typed Environment 事实。
7. 平台专用实现留在能力附近，平台无关核心不复制。

模块发送意图并读取完整快照，不共享可任意修改状态。接口只用于真实替换点、平台边界和
跨模块契约。

## 运行与分发边界

`internal/productlayout` 是 CLI 与 daemon 唯一的运行目录 resolver。官方机构 Profile
是随版本发布的程序内容，两种模式均位于 `<exe-dir>/institution-profiles/` 并原地读取。

安装版把秘密 `configurations.json` 放在 OS 用户配置目录，把 runtime 信息和后台日志放在
OS 用户缓存目录。便携版由同目录普通 `sidravia.portable` marker 启用：

```text
<exe-dir>/
  sidravia
  sidraviad
  sidravia.portable
  institution-profiles/
    jlu.json
  config/
    configurations.json
  runtime/
    runtime.json
  logs/
    sidraviad.log
    sidraviad.log.1
```

resolver 不读取当前工作目录，不按可写性猜模式，不扫描另一模式，也不自动复制、迁移或
回退。`daemon status` 的路径解析严格只读。

`tools/build` 是唯一规范构建入口。普通与便携 Windows amd64 包复用同一对二进制；两个
zip 都携带官方 Profile 与 `scripts/install.ps1` / `scripts/uninstall.ps1`，仅便携包额外
携带 marker。构建工具不拥有签名、tag、上传或 Release 权限。

PowerShell 脚本是 Windows 用户态部署单元：安装脚本管理用户 PATH 和登录任务
`SidraviaDaemon`；卸载脚本只撤销这两项，永不删除数据。脚本模式无关、幂等并在前后验证
状态。Go CLI 不提供 install/uninstall 命令。

## 状态所有权

| 状态 | 唯一所有者 | 其他模块如何使用 |
|---|---|---|
| 持久 Configuration 与唯一私有密码 | Authentication Configuration Store | daemon app 读取聚合；IPC/CLI 只见公开投影 |
| 当前主机与网络事实 | Environment Detector | daemon app 转交 typed Snapshot |
| 最新网络 Snapshot 与分发顺序 | Supervisor | 新旧 Session 获得同一 revision |
| Session 集合、准入和 retained 生命周期 | Supervisor | daemon app 按 SessionID 委托 |
| 单次认证意图、协议 Run、重试和 Snapshot | Session | Supervisor 读取 revision |
| 跨模块用例顺序和 Configuration–Session 关联 | daemon app | IPC Handler 调用公开用例 |
| 单个 IPC 连接的发送队列 | 该 IPC 连接 | 慢客户端不阻塞 Application |

Configuration 自己拥有 `AutoLogin` 与 `AutoReconnect`。最多一份 Configuration 启用
`AutoLogin`；它在每个 daemon generation 的首个已接受 Environment Snapshot 后评估一次。
`AutoReconnect` 冻结进 retained Session 的不可变运行定义。一次性认证始终自动重连；
显式 stop/remove/restart 保持权威。不存在全局 Settings owner 或持续 desired-state
controller。

## Configuration 与认证链

schema 3 是 Configuration 唯一编码格式。严格 schema 2 输入仍可读取，使用
`AutoLogin=false`、`AutoReconnect=true` 默认值；仅打开旧文档不触发重写。Store 先构造并
验证完整候选，再原子替换文件，最后提交内存状态。

一次性输入与持久 `ConfigurationID` 汇入同一运行链：

```text
typed request
  -> daemon app 解析 Configuration / Profile / Environment
  -> Supervisor 执行单活动准入并创建或复用 Session
  -> Session 选择绑定并拥有协议 Run
  -> IPC 返回或发布带 revision 的最新完整 Snapshot
```

更新 Configuration 不暗中替换既有 Session 的不可变运行定义。每个 Configuration 在一个
daemon generation 内最多关联一个 retained Session。

## Session、Supervisor 与 Environment

Supervisor 保存最新 Environment Snapshot。新 Session 返回初始 Snapshot 前必须先得到它；
旧 revision 被忽略，相同 revision 可重放权威内容。Supervisor 不解释接口事实，也不替
Session 选择绑定。

Session 拥有协议执行、取消、重试、公开状态与 Snapshot。停止采用两阶段：
`stopping -> suspended`。Supervisor 在 `suspended` 前不释放活动准入。ensure、restart、
remove 由 Supervisor 按 ID 串行；remove 成功时 actor、协议、revision forwarder 与集合成员
均已退出。

自动网络策略只选择 Up、有 IPv4、hardware-backed、有 physical connector 且非
filter/endpoint 的接口，不依赖显示名称黑名单。Linux/WSL Observer 保守分类虚拟接口；
WSL 基线不证明 Windows 或校园行为。

## IPC 与呈现

IPC 使用回环 WebSocket、随机 token 与精确 BuildID。每个已接受 Request 恰有一个同 ID
Response；Event 携带资源 ID、单调 revision 与最新完整公开 Snapshot。客户端重连后重新
查询权威 Snapshot；慢客户端持续跟不上时关闭连接，不阻塞 daemon app。

CLI presentation 只消费 DTO 与稳定错误码，负责安全中文、终端能力和动态文本清理，不拥有
daemon 发现、IPC、Session、Configuration 或协议行为。密码和内部错误链不得进入公开 DTO、
Snapshot、日志或错误文本。

诊断也是只观察不控制的适配器。Info/Debug 使用字段白名单；Trace 才允许完整 D520 数据报，
并明确标记敏感。后台 daemon 日志由 launcher 放在当前运行目录并保留唯一轮转备份。

## D520 边界

真实协议位于 `internal/daemon/authentication/protocol/drcom/d520`，稳定协议 ID 是
`drcom-5.2.0-d`。Session 是产品认证状态机；一次 D520 Run 只拥有一次阻塞线级执行。

每次 Run 独占一个 IPv4 UDP socket：

- 绑定 Session 已选 IPv4；
- 本地端口由 Profile 的 fixed/system-assigned `localPort` 决定；
- fixed bind 失败不回退；
- socket connect 到 Profile 远端 endpoint；
- D520 不重新选择接口、不修改系统 route；
- Run 退出后由 Session 决定是否重试。

```text
Challenge -> Login -> bootstrap KA -> authenticated heartbeat -> best-effort Logout
```

Mihomo/Clash-family TUN 捕获私有校园目的地址的现场问题属于环境配置。停止 TUN 或用
`tun.route-exclude-address` 排除 RFC 1918 范围后，Challenge 与产品认证恢复。这个结论不
批准产品路由修复，也不允许把临时 /32 路由写成产品行为。

## 平台与证据边界

Windows 是首版发布平台。Linux/WSL 共享 domain core、IPC、Configuration、运行目录与日志
契约，只在 host、进程控制、终端密码和基本 Observer 上使用 Linux 实现；不发布 Linux
产品包。macOS 明确 Unsupported。

代码、自动验证、Windows-native、WSL/Linux-native、校园网络和 Release readiness 是独立
证据状态。交叉编译、mock 和 WSL 不能证明 Windows 或校园行为。
