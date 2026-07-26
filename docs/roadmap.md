# Sidravia 产品路线图

本文记录长期阶段和当前进度。我们分别报告“代码已经写完”“自动测试已经通过”和“Windows 现场已经验证”，不得把它们合并成一个“完成”。

## 当前结论

后端模块已经完成职责重整，并通过独立 Review。

`sidravia status`、host 运行信息清理和验收脚本安全清理已经通过独立 Review。
完整 Go 测试、vet、格式检查和两个 Windows amd64 交叉编译均已通过。Windows 原生
双进程现场尚未验证；它被明确保留为人类验收项，不再阻塞纯代码阶段。阶段 2 因此
按“代码和自动验证完成、Windows 现场待验证”条件关闭。D520 Factory/Run 的代码与自动
验证也已完成，真实校园协议正确性仍待现场验证。typed 系统网络快照已经贯通 daemon
app、Supervisor 和新旧 Session；应用层名称已在生产装配前收敛为 `Application` 和
`AuthenticationResolver`。真实 Windows Environment Detector 已实现 host facts 和
两秒轮询的网络快照，代码与自动验证完成；它现已接入生产 daemon，Windows 原生网卡
事实仍待验证。
本地机构 Profile 加载器也已完成：它从用户配置目录严格、全有或全无地加载版本化
`<InstitutionProfileID>.json`，并在进入 Catalog 前交给已注册协议 Factory 验证。
生产 `sidraviad` 现已注册 D520、加载 Profile、打开 Configuration/Credential 存储、
接入真实 Environment Observer、typed IPC 与统一生命周期；代码、race 检查和完整 Go
verifier 已通过。首套 `auth start/status/stop` CLI、Windows 隐藏密码输入、
`--password-stdin` 和共享 Session Snapshot 输出也已完成并通过聚焦测试、race 与
Windows amd64 交叉编译。

首轮纵向链路只读 Review 在 `cbfdfa5` 上完成，race 探针和公开 Go verifier 均通过，
但结论为 **NO-GO**：Windows Observer 会把常见虚拟 Ethernet 当作 wired，自动选择器
又允许最新候选优先，因此 WSL、Hyper-V、Docker 或软件 VPN 接口可能取代校园物理
网卡。进入 Windows/校园现场前必须先让 Snapshot 保留系统的接口分类，并让自动模式只
选择真实硬件、存在物理适配器且非 filter/endpoint 的接口。其他已确认 P2/P3 进入后续
短切片，不与这项现场阻断修正混做。

| 阶段 | 当前状态 | 进入下一阶段前必须观察到的结果 |
|---|---|---|
| 0. 原则和架构 | 完成 | 当前架构和 ADR 对关键边界给出一致答案 |
| 1. 后端重整 | 完成 | Configuration、Credentials、App、Session、Supervisor 和 Persistence 各自拥有明确职责 |
| 2. 可运行骨架 | 条件完成：现场待验 | Windows 上的 `sidravia status` 能冷启动或连接 daemon，并通过 WebSocket 返回状态 |
| 3. 最小 Session 应用边界 | 代码和自动验证完成，已进入生产装配 | daemon app 和 typed IPC handler 能一次性启动、停止和查询 Session，且不泄漏秘密 |
| 4. D520 协议 Run | 代码和自动验证完成，校园现场待验 | Factory/Run 能用真实 D520 线级协议执行登录、保活、取消和尽力 Logout |
| 5. 持久输入和真实环境 | 部分完成：Detector、Profile 加载和生产装配完成，持久 Configuration/Credential IPC 入口未开始 | Configuration、Credentials 和 Environment 能生成与一次性启动相同的运行定义 |
| 6. Windows 产品纵向链路 | 只读 Review 为 NO-GO：虚拟接口自动选择待修正 | CLI、IPC、daemon、真实环境和 D520 组成可运行的一次性认证产品链路，并且自动模式不会选择 Windows 软件/虚拟接口 |
| 7. 校园网络验证 | 未开始 | 产品在真实校园网络完成认证，并保存可复查的证据 |

阶段 3 的第一个切片已经完成：typed 一次性输入可以在 daemon app 中解析为现有
`RuntimeDefinition`，并通过 Supervisor 启动、读取和停止 Session；该路径不创建
Configuration 或 Credential 所有权，公开 Snapshot 不包含凭据。

第二个切片也已完成：严格 typed 的一次性 start、stop、get IPC payload 和 app
handler 已通过 Review；完整公开 Snapshot 被映射为稳定 DTO，秘密和底层诊断不会
进入 Response。

第三个切片也已完成：Supervisor 保存最新 typed 网络快照，按 revision 向现有和新建
Session 分发，并保证新 Session 返回初始 Snapshot 前已得到最新网络状态。daemon
`Application` 提供窄委托，`app.IPCHandler` 在内存中组合 status 与 Session 方法。
真实 Windows Detector 随后也已实现：Windows host information 使用真实系统事实，
网络 Observer 立即发布首个快照并每两秒轮询，只在归一化事实变化时递增 revision；
非 Windows 明确返回 Unsupported。其生命周期测试、完整 Go verifier 和 Windows
交叉编译通过；Detector 已接入生产 daemon，但尚未在 Windows 原生环境验证真实网卡。
本地 Profile 加载器随后也已完成：默认读取用户配置目录下的
`Sidravia/institution-profiles`，每个规范短 ID 对应一个严格版本化 JSON 文件；缺目录
表示空 Catalog，任一坏文件使整次启动加载失败，协议配置由 Registry 中对应 Factory
验证。

生产组合随后完成：`cmd/sidraviad` 注册唯一 D520 Factory，加载 Profile，打开
Configuration/Credential 存储，构造 Supervisor、Resolver、Application 和 typed IPC，
并统一拥有 host、Environment Observer、Snapshot 转交和最终 Supervisor 清理。并发
Review 修正了外部取消、Observer 提前返回、timer goroutine、活动等待和并发故障保留；
当前代码和自动验证完成。首套 CLI authentication 命令随后也已完成：用户可以用
一次性 Profile ID、用户名和安全密码输入启动 Session，并查询或停止它。真实
`jlu.json` 与 Windows/校园现场仍未完成。

人类已选择直接实现真实 Go Dr.COM 5.2.0(D)，不在产品中加入假协议。参考收敛已经完成：
线级规范、来源冲突和虚构确定性向量已经进入
仓库，旧 Drcom-Core verifier 已删除，本地源码快照已移到仓库外归档。私有 Go 报文
与密码学 codec 已按 ASCII fixture 写成并通过聚焦测试；非 ASCII 编码因 GBK/UTF-8
来源冲突保留为 `Unresolved`。真实 D520 Factory/Run、严格 Profile、阻塞登录与保活、
取消唤醒、尽力 Logout 和结构化失败已经实现并通过独立 Review。该结果证明代码行为，
不冒充真实校园服务器兼容性。

现有 Python mock 来源于同一批历史资料，不作为下一切片完成门槛。Factory/Run 的自动
测试可以使用包内最小 UDP test peer 证明确定性流程、取消和错误分类，但不得把该 peer
或 Python mock 的通过结果描述成真实协议正确或校园现场通过。

## 阶段 1：后端重整

这一阶段已经完成。当前代码满足以下条件：

- Configuration Catalog 保存非秘密配置。
- Credentials Store 单独保存秘密。
- daemon app 解析配置、凭据、Profile、协议和环境，然后组装运行定义。
- Supervisor 管理 Session 集合，并且首版只允许一个活动 Session。
- Session 管理自己的认证运行和 Snapshot。
- jsonfile 提供严格解码、原子替换和 Windows ACL。
- 旧 Session Repository、持久 Session 恢复和复杂 watcher 调度已经删除。

Windows ACL 的现场证据位于
[2026-07-23-windows-jsonfile-acl.md](evidence/2026-07-23-windows-jsonfile-acl.md)；
其他行为由当前相邻测试和完整仓库验证保护。

## 阶段 2：可运行骨架

这一阶段只证明 CLI 和 daemon 能作为两个真实 Windows 进程通信：

```text
sidravia status
  -> CLI 读取或等待运行信息
  -> CLI 必要时启动同目录 sidraviad
  -> client 使用 token 和 BuildID 建立回环 WebSocket
  -> server 调用 daemon.status
  -> CLI 显示 daemon 返回的版本、BuildID、PID 和状态
```

本阶段代码和自动验证的完成条件：

1. contract 测试证明 decoder 拒绝未知字段、缺失字段和尾随数据。
2. host 测试证明 daemon 只删除属于自己的运行信息。
3. CLI 测试证明热连接、冷启动、失效信息恢复和五秒超时。
4. 验收脚本的静态 Review 和测试证明它只终止自己启动的进程，也不会删除正在运行的
   其他 daemon 的运行信息。
5. 完整 Go 测试、vet、格式检查和两个 Windows amd64 交叉编译通过。

仍待人类现场执行：Windows 原生冷启动、热连接、单实例和参数拒绝。只有实际执行后
才能写入原生 Go 版本、源码提交、完整命令和结果；这项缺口不冒充通过，也不阻塞
Stage 3 的纯代码切片。

## 后续阶段

阶段 3 先实现最小 Session 运行链和协议契约。CLI 通过 IPC 提交 typed 一次性启动请求；daemon app 把请求转换为 `RunDefinition`，再交给 Supervisor 和 Session。首个切片只要求 start、stop、get 和可复查 Snapshot，不提前完成配置管理。

阶段 4 把 Go D520 Factory/Run 接入相同协议契约，先证明登录、保活、取消、错误分类和
尽力 Logout 的代码行为。一次性连接是永久产品能力，不是之后删除的临时接口。秘密不得
出现在命令行参数、日志、Snapshot 或 Response。现有 Python mock 不作为完成门槛。

阶段 5 再补齐 Configuration CRUD、Credential 写入/替换/删除和真实 Windows Environment Detector。按 `ConfigurationID` 启动与一次性启动必须生成同一种 `RunDefinition`；IPC server 只调用 daemon app，不直接操作这些模块。daemon 仍不提供读取凭据明文的操作。

阶段 6 已将 typed Session handler、D520 Factory/Profile 和真实 Environment Detector
接入生产 daemon，并补齐统一取消、等待和正常退出。CLI 安全密码输入及
authentication 命令也已完成；下一步是在 Windows 原生环境运行一次性认证纵向链路。
登录后自启动可以随后加入；Service、管理员权限和登录前认证仍然可以推迟。
机构 Profile 从本地可编辑文件加载，而不是作为机构专用常量编译进程序；普通认证 IPC
仍只引用 Profile ID。首版每个 `<InstitutionProfileID>.json` 对应一个 Profile，使用
`jlu` 这类简短 ID；修改后重启 daemon 生效，不做热重载。

阶段 7 会在真实校园网络运行产品。现场结果必须与 mock 结果分开记录。

## 当前不做

当前阶段不实现 GUI、多会话优先级、抢占、远程 IPC、事件历史、Windows Service、自动更新或没有现实故障证据的并发排列。
