# Sidravia 产品路线图

本文记录长期阶段和当前进度。我们分别报告“代码已经写完”“自动测试已经通过”和“Windows 现场已经验证”，不得把它们合并成一个“完成”。

## 当前结论

后端模块已经完成职责重整，并通过独立 Review。

早期顶层 `sidravia status`、host 运行信息清理和验收脚本安全清理已经通过独立 Review；
当前命令树已迁移到规范入口 `sidravia daemon status`，旧入口只返回迁移提示。
完整 Go 测试、vet、格式检查和两个 Windows amd64 交叉编译均已通过。Windows 原生
双进程与真实校园 D520 已在提交 `508197d` 上完成首次现场验证：CLI/daemon 选择物理
以太网，完成登录、持续心跳与主动 Logout。阶段 2、阶段 4 和阶段 6 的首个 Windows/JLU
纵向验收因此通过；它仍是单台机器、单个校园环境的 Alpha 证据。typed 系统网络快照已经贯通 daemon
app、Supervisor 和新旧 Session；应用层名称已在生产装配前收敛为 `Application` 和
`AuthenticationResolver`。真实 Windows Environment Detector 已实现 host facts 和
两秒轮询的网络快照，代码与自动验证完成；它现已接入生产 daemon，Windows 原生网卡
事实已在上述现场中验证，Clash TUN 在场但未被自动选中。
本地机构 Profile 加载器也已完成：它从用户配置目录严格、全有或全无地加载版本化
`<InstitutionProfileID>.json`，并在进入 Catalog 前交给已注册协议 Factory 验证。
生产 `sidraviad` 现已注册 D520、加载 Profile、打开 Configuration/Credential 存储、
接入真实 Environment Observer、typed IPC 与统一生命周期；代码、race 检查和完整 Go
verifier 已通过。首套 `auth start/status/stop` CLI、Windows 隐藏密码输入、
`--password-stdin` 和共享 Session Snapshot 输出也已完成并通过聚焦测试、race 与
Windows amd64 交叉编译。

资源命令树的下一切片补齐 `sidravia auth list` 与 `sidravia profile list`。daemon
应用层、typed IPC 和 CLI 分别列出当前进程保留的 Session Snapshot 与安全 Profile
摘要；空列表保持非 null 数组，CLI 在完整验证响应后一次写出。该切片完成代码和自动
验证后仍未进行 Windows 原生复核，不改变 `508197d` 的校园现场证据范围。

CLI 顶层分发随后迁移到 Cobra 资源命令树。daemon 查询只由
`sidravia daemon status` 执行，并完全复用既有热连接、单次冷启动、失效信息恢复和
五秒等待链；当前没有独立 `daemon start` 或缺少 typed IPC 所需的 `daemon stop`。
这项迁移的代码和自动验证完成，但 Windows 原生尚未重新验证。提交 `508197d` 的首轮
现场证据仍来自当时有效的顶层 `sidravia status`，不得倒推成新命令已经现场通过。

Session 主动停止语义随后收敛为两阶段：Stop 立即返回公开 `stopping`，当前协议 Run
完成有界清理并退出后才发布 `suspended`，Supervisor 到此时才释放单活动准入。重复
Stop 幂等，清理失败只保留为 Session 私有诊断，不把本地 Session 卡在过渡态。该切片
只改变生命周期代码、相邻契约测试和人类文档；Windows 原生与校园现场状态尚未重新
验证。

安全结构化 daemon 日志随后完成：`cmd/sidraviad` 使用标准库 `log/slog` 在 stderr
输出 TextHandler/Info 日志，带稳定 `event` 码、固定简体中文 `msg` 和安全属性
白名单；进程边界 fatal reporting 与 `os.Exit` 分离，构造和运行失败各只记一条事件
且不含原始 error，原始 error 仍由错误传播保留；IPC server 删除 package-global
`log.Printf`，按归一化事件记录连接、请求和响应失败，peer 提供的 method/error
归一化为契约白名单值。该切片完成代码和自动验证后仍未进行 Windows 原生复核，不改变
`508197d` 的校园现场证据范围。

简体中文 CLI 呈现边界随后完成：`internal/cli/presentation.go` 作为唯一导入
`termenv v0.16.0` 的生产文件，统一拥有帮助、状态、Session 详情/列表、Profile 列表、
密码提示和错误消息的简体中文呈现、终端能力选择、Windows 虚拟终端启用/恢复、动态值
控制字符清理和稳定机器码映射。着色只在真实交互终端出现，重定向/管道和 `NO_COLOR`
始终纯文本，`CLICOLOR_FORCE` 无法在重定向时重新启用颜色，动态值无法注入 ANSI 序列或
新输出行。该切片完成代码和自动验证后仍未进行 Windows 原生复核，不改变 `508197d` 的
校园现场证据范围；持久认证配置、引导式设置和交互式表单库仍在本 Campaign 之外。

Operator Control 与完整诊断 Campaign 随后启动，首个工作包完成 CLI 信息契约：帮助规范扩展为固定的描述/用法/可用命令/参数/选项/示例六段（适用时段间空一行）；普通 Session 与 Profile 输出把内置 JLU 渲染为 `吉林大学（JLU）`；普通 Session 详情/列表不再显示 InterfaceID，网络行只显示 `<友好名称> — <IPv4>` 或仅 `<IPv4>`；公开 Snapshot 到 IPC 到 CLI 的 `AccountLabel`/`accountLabel` 全链路替换为 `AccountName`/`accountName`，`RuntimeDefinition.AccountName()` 返回完整用户名，不再脱敏。该切片完成代码和自动验证后仍未进行 Windows 原生复核，不改变 `508197d` 的校园现场证据范围。

Work Package 3 完成 daemon 只读 status、Windows start、已提交响应驱动的 graceful stop
和精确 generation restart，并提供确定的子进程日志环境。代码与自动验证和 Windows
原生验证是分离状态；本切片尚未新增 Windows 原生或校园证据。Work Package 4 已实现
retained Session ensure/restart/remove；自动验证与 Windows 原生验证分别报告。

2026-07-28 的首次 Windows operator 检查确认进程 start/shutdown 与其余检查正常，同时
发现生产 `daemon status` 仍误接冷启动链、根 help 拥挤且缺少普通 `help <path>`、CLI
后台 daemon 日志仍附着前端。当前 correction 已把 status 接到严格只读 probe，统一
bare/help/`-h`/`--help` 的分层中文规格，并让 Windows 后台日志写入 LocalAppData、按
10 MiB 保留唯一 `.1` 备份。修正代码与自动验证完成后仍需最小 Windows-native
correction verification；这不扩大提交 `508197d` 的校园认证证据。

ADR 0023 的可复现构建与打包切片随后完成：`tools/build` 标准库 Go 工具一次编译同一对
Windows amd64 二进制，生成默认安装版 zip、便携版 zip 和外部 `SHA256SUMS.txt`，相同输入
逐字节复现；`Verify` 与 `Package` 两个 GitHub Actions 工作流已提交。代码与本地自动验证
完成；GitHub-hosted workflow 实跑、Windows-native 包复核、签名与 Release 仍为 pending，
不改变提交 `508197d` 的校园认证证据范围。下一功能切片为持久 Configuration/Credentials。

## 已确认的后续顺序

提交 `81dfbdf` 之后，当前大版本按以下顺序推进，每一步保持独立计划、独立提交和分开的
验证声明：

1. **运行目录与构建工作流。** ADR 0020 的显式安装版/便携版解析已经实现并通过代码与
   自动验证：CLI 与 daemon 经共享 `internal/productlayout` 在同一模式下得到相同的
   Profile、Configuration、Credential、运行信息和日志路径；便携模式不依赖 AppData，
   安装模式继续使用操作系统用户目录。Windows-native 运行目录复核仍为 pending。
   ADR 0023 的可复现 Windows 构建与普通/便携包工具（`tools/build`）也已实现并完成
   本地自动验证：聚焦测试、race、Windows amd64 工具编译、两次真实构建逐字节复现和
   完整公开 verifier 均通过。`Verify`（push/PR）与 `Package`（手动 `workflow_dispatch`）
   两个 GitHub Actions 工作流已提交但尚未在 GitHub 上实跑；Windows-native 包复核、
   代码签名与 Release 仍为 pending。Linux 当前只保留可编译的平台边界，不发布不可运行
   的产品包。
2. **持久 Configuration/Credentials 与 CLI 体验。** 完成配置的列出、创建、查看、
   修改、删除以及按 ConfigurationID 启动；凭据每份只保存一个密码，允许创建、替换和
   删除，但不提供明文读取。Windows 当前版本接受 ACL 保护的独立明文 JSON。交互使用
   Cobra + termenv 上的轻量逐行向导，同时保留完整非交互参数，不引入全屏 TUI。
3. **Linux/WSL 平台基线。** 在不分叉 domain core 的前提下实现 Linux 的运行目录、
   daemon host/控制、终端密码输入、文件权限和基本 Environment Observer；WSL 作为
   Linux 构建、IPC、配置、日志和 mock 运行的首个验证环境。WSL 中真实校园 D520 是否
   可用由后续路由证据决定，不能由“能够启动”倒推。
4. **网络诊断与选择修正。** 增加面向认证服务器目标地址的 route/source-IP 事实和
   可解释诊断，再调查 Windows 热点/ICS、多 IPv4、WSL 与物理网卡的相互影响。不得仅凭
   网卡显示名称黑名单解决选择问题。
5. **文本与文档收敛。** 功能边界稳定后进行一次不改变功能代码的全局注释、用户文本、
   README、快速开始和架构文档复核。

WSS、IPC 并发请求与事件重同步、GUI、更多协议包放在下一大版本统一演进。持久凭据明确
先于 WSS；当前回环 `ws://` 继续由随机 token 与精确 BuildID 鉴权，不增加自制应用层
加密。GUI 与 CLI/daemon 共享产品版本，不建立独立升级节奏。

“显式安装版/便携版运行目录”以及 ADR 0023 的可复现构建与打包工具都已经实现并完成代码
与本地自动验证。构建工具只生成 Windows amd64 普通包、便携包与 SHA-256 产物，不自动
发布 Release。下一功能切片是实现持久 Configuration/Credentials 与良好的 CLI 管理；
Linux/WSL 基线、网络诊断与选择修正、WSS 和 GUI 均未开始。

Campaign 第二个工作包完成分层 daemon 诊断：`SIDRAVIA_LOG_LEVEL` 精确接受 `info`/`debug`/`trace`，`trace` 为低于 `debug` 的自定义级别；Info 记录每次已提交 Session revision、所选机构与完整账号名、友好接口名与所选 IPv4、认证状态与重试；Debug 增加 Session 命令、协议运行代际、D520 阶段边界、重试调度与 IPC 连接/完成；Trace 记录每个 D520 UDP 数据报的完整小写 hex，启用前先发 `trace_logging_sensitive` Warn。窄诊断接口（`session.Diagnostics`、`protocol.AuthenticationProtocolDiagnostics`）与显式 no-op 实现保持领域核心不导入 `log/slog`；诊断 sink 无返回值，永不改变行为。该切片完成代码和自动验证后仍未进行 Windows 原生复核，不改变 `508197d` 的校园现场证据范围。

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
| 2. 可运行骨架 | 完成：Windows 首轮现场通过；operator correction 待原生复核 | Windows 上 CLI 能显式启动 daemon，严格只读 status 能报告 stopped/running |
| 3. 最小 Session 应用边界 | 代码和自动验证完成，已进入生产装配 | daemon app 和 typed IPC handler 能一次性启动、停止、查询和列出 Session，且不泄漏秘密 |
| 4. D520 协议 Run | 完成：JLU 首轮现场通过 | Factory/Run 能用真实 D520 线级协议执行登录、保活、取消和尽力 Logout |
| 5. 持久输入和真实环境 | 部分完成：Detector、Profile 加载和生产装配完成；运行目录模式已实现，持久 Configuration/Credential IPC 入口未实现 | 两种运行目录解析一致，Configuration、Credentials 和 Environment 能生成与一次性启动相同的运行定义 |
| 6. Windows 产品纵向链路 | 完成：Windows/JLU 首轮现场通过 | CLI、IPC、daemon、真实环境和 D520 组成可运行的一次性认证产品链路，并且自动模式不会选择 Windows 软件/虚拟接口 |
| 7. 校园网络验证 | 首轮完成：扩大环境覆盖待进行 | 产品在真实校园网络完成认证，并保存可复查的证据 |

阶段 3 的第一个切片已经完成：typed 一次性输入可以在 daemon app 中解析为现有
`RuntimeDefinition`，并通过 Supervisor 启动、读取和停止 Session；该路径不创建
Configuration 或 Credential 所有权，公开 Snapshot 不包含凭据。

第二个切片也已完成：严格 typed 的一次性 start、stop、get IPC payload 和 app
handler 已通过 Review；完整公开 Snapshot 被映射为稳定 DTO，秘密和底层诊断不会
进入 Response。

第三个切片也已完成：Supervisor 保存最新 typed 网络快照，按 revision 向现有和新建
Session 分发，并保证新 Session 返回初始 Snapshot 前已得到最新网络状态。daemon
`Application` 提供窄委托，`app.IPCHandler` 在内存中组合 status 与 Session 方法。
Session 停止边界也已明确为 `stopping -> suspended` 两个 revision；Supervisor 在
第二个 revision 前不释放单活动准入。
真实 Windows Detector 随后也已实现：Windows host information 使用真实系统事实，
网络 Observer 立即发布首个快照并每两秒轮询，只在归一化事实变化时递增 revision；
非 Windows 明确返回 Unsupported。其生命周期测试、完整 Go verifier 和 Windows
交叉编译通过；Detector 已接入生产 daemon，并已在 Windows 现场正确选择物理以太网。
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
`jlu` Profile 已作为本地文件在 Windows/JLU 现场使用；个人 Profile 不进入仓库。

人类已选择直接实现真实 Go Dr.COM 5.2.0(D)，不在产品中加入假协议。参考收敛已经完成：
线级规范、来源冲突和虚构确定性向量已经进入
仓库，旧 Drcom-Core verifier 已删除，本地源码快照已移到仓库外归档。私有 Go 报文
与密码学 codec 已按 ASCII fixture 写成并通过聚焦测试；非 ASCII 编码因 GBK/UTF-8
来源冲突保留为 `Unresolved`。真实 D520 Factory/Run、严格 Profile、阻塞登录与保活、
取消唤醒、尽力 Logout 和结构化失败已经实现并通过独立 Review。该结果证明代码行为，
随后提交 `508197d` 的现场证据证明了当前 JLU 环境中的真实服务器兼容性，但不外推到
其他学校或尚未观察到的协议变体。

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
sidravia daemon start
  -> CLI 启动同目录 sidraviad
sidravia daemon status
  -> CLI 严格只读探测运行信息描述的 generation
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

Windows 原生冷启动、热连接、认证状态查询、主动停止和运行信息清理已在提交
`508197d` 的现场验收中通过。更广泛的 Windows 版本、单实例冲突和故障注入仍属于后续
兼容性覆盖，不改变首轮 Alpha 纵向链路已经通过的结论。

## 后续阶段

阶段 3 先实现最小 Session 运行链和协议契约。CLI 通过 IPC 提交 typed 一次性启动请求；daemon app 把请求转换为 `RunDefinition`，再交给 Supervisor 和 Session。首个切片只要求 start、stop、get 和可复查 Snapshot，不提前完成配置管理。

阶段 4 把 Go D520 Factory/Run 接入相同协议契约，先证明登录、保活、取消、错误分类和
尽力 Logout 的代码行为。一次性连接是永久产品能力，不是之后删除的临时接口。秘密不得
出现在命令行参数、日志、Snapshot 或 Response。现有 Python mock 不作为完成门槛。

阶段 5 已统一安装版/便携版运行目录，下一步补齐 Configuration CRUD、Credential
写入/替换/删除。真实 Windows Environment Detector 已实现，但 destination-aware
route/source-IP 事实仍待网络修正切片。按 `ConfigurationID` 启动与一次性启动必须生成
同一种 `RunDefinition`；IPC server 只调用 daemon app，不直接操作这些模块。daemon
仍不提供读取凭据明文的操作。

阶段 6 已将 typed Session handler、D520 Factory/Profile 和真实 Environment Detector
接入生产 daemon，并补齐统一取消、等待和正常退出。CLI 安全密码输入及
authentication 命令也已完成；Windows 原生一次性认证纵向链路已在 JLU 现场通过。
登录后自启动可以随后加入；Service、管理员权限和登录前认证仍然可以推迟。
机构 Profile 从本地可编辑文件加载，而不是作为机构专用常量编译进程序；普通认证 IPC
仍只引用 Profile ID。首版每个 `<InstitutionProfileID>.json` 对应一个 Profile，使用
`jlu` 这类简短 ID；修改后重启 daemon 生效，不做热重载。

阶段 7 的首次真实校园运行已经完成并与 mock 结果分开记录。后续继续扩大 Windows、
网卡和校园协议变体覆盖。

## 当前不做

当前阶段不实现全屏 TUI、WSS、GUI、多会话优先级、抢占、远程 IPC、事件历史、
Windows Service、自动更新、更多协议包或没有现实故障证据的并发排列。
