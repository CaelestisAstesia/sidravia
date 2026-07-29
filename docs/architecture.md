# Sidravia 当前架构

本文说明 Sidravia 当前采用的系统设计。已经接受且需要长期解释的取舍记录在
`docs/decisions/`。

## 产品和进程

Sidravia 首先服务 Windows 用户。产品包含两个进程：

- `sidravia` 是短生命周期 CLI。它接收用户命令，通过 IPC 请求后端，然后显示结果。
- `sidraviad` 是长期运行的 daemon。它独占配置、凭据、环境检测、Session 和认证协议。

CLI 和 daemon 通过本机回环 WebSocket 通信。未来 GUI 应复用同一 IPC，不应直接导入 daemon 的内部包。

## 目录和依赖

```text
cmd/
  sidravia/
  sidraviad/

internal/
  cli/
  ipc/
    contract/
    client/
    server/
  daemon/
    app/
    authentication/
      protocol/
        drcom/
      session/
      supervisor/
    configuration/
    credentials/
    environment/
    persistence/
      jsonfile/
    host/
```

各层按以下方向依赖：

1. `cmd/sidravia` 只负责组合和启动 `internal/cli`。
2. `internal/cli` 只调用 IPC contract 和 client。CLI 不读取 daemon 的配置文件或内部状态。
3. `cmd/sidraviad` 组合 IPC server、daemon app 和系统 host。
4. IPC server 通过一个窄 Handler 调用 daemon app。IPC server 不依赖具体 Application 类型。
5. daemon app 依次调用 Configuration、Credentials、Environment、Supervisor 和协议注册表，以完成跨模块用例。
6. Supervisor 创建和管理 Session。Session 不读取 Supervisor 的状态。
7. Session 只依赖协议契约和环境事实。Session 不依赖 JSON、IPC、CLI 或具体持久化实现。

Windows 专用代码应使用 Go 构建约束，并留在它所实现的能力附近。平台无关核心不应为 Windows 单独复制一份。

“配置 IPC”“凭据 IPC”或“Session IPC”只表示通过 IPC 暴露相应的应用用例，不表示 IPC server 直接操作这些模块。调用方向始终是：

```text
typed IPC request -> IPC Handler -> daemon app -> domain module
```

IPC 不是 Configuration、Credentials、Environment 或 Session 的共同控制器。

## 运行目录模式

产品接受两种显式运行目录模式。默认的安装版模式把配置、机构 Profile 与凭据放在
`os.UserConfigDir()/Sidravia`，把运行信息和后台日志放在
`os.UserCacheDir()/Sidravia`。便携版模式由可执行文件同目录的普通标记文件
`sidravia.portable` 启用，并使用以下布局：

```text
<exe-dir>/
  sidravia
  sidraviad
  sidravia.portable
  config/
    configurations.json
    credentials.json
    institution-profiles/
  runtime/
    runtime.json
  logs/
    sidraviad.log
    sidraviad.log.1
```

CLI 与 daemon 经同一个平台边界 `internal/productlayout` 独立解析出相同的绝对路径，
它是唯一的运行目录 resolver。解析不使用当前工作目录，不根据可写性猜测，不扫描另一种
模式，也不自动迁移或回退。`daemon status` 保持严格只读：它可以检查标记和读取运行
信息，但不能因为路径解析创建目录或文件。具体决策见 ADR 0020。

## 构建和分发边界

产品构建只有一个权威入口：计划中的 `tools/build` 标准库 Go 工具。它负责固定 Go
版本、target、编译参数、ProductVersion/BuildID 注入、zip manifest、时间戳、权限和
SHA-256；本地开发者与 GitHub Actions 都调用这个入口，不分别维护 shell、PowerShell
或 CI 专用打包实现。

Windows amd64 一次构建同一对二进制，再生成默认用户目录包和带
`sidravia.portable` 空 marker 的便携包。两个包不得通过不同编译产生。输出目录原先必须
不存在，只有完整成功后才发布，失败不覆盖旧产物。构建工具不读取 Git 猜版本，也不拥有
代码签名、tag、GitHub Release、安装器或上传权限。

push/pull request 工作流只运行公共 verifier；手动打包工作流以显式版本和当前 commit
作为输入，上传构建工具已经生成的文件作为临时 workflow artifact。Windows amd64 是
当前唯一产品包；Linux/WSL 尚未实现 daemon host 和现场验证，因此本阶段不发布 Linux
包。具体决策见 ADR 0023。

截至提交 `7d1421a`，上述构建工具与 GitHub Actions 尚未实现；当前仍只有 README 的
手工开发构建命令和公共 verifier。

## 状态由谁负责

每项可变状态只能有一个权威所有者。

| 状态 | 权威所有者 | 其他模块如何使用 |
|---|---|---|
| 持久连接配置 | Configuration Catalog | 通过 ID 读取不可变快照 |
| 认证秘密 | Credentials Store | daemon app 按 CredentialID 读取 |
| 自动连接设置 | daemon app | 保存 ConfigurationID |
| 当前主机和网络事实 | Environment Detector | daemon app 转交 typed 快照 |
| 已接受的最新网络快照和 Session 分发顺序 | Supervisor | 新旧 Session 接收同一 revision |
| 单次认证的运行状态 | Session | 对外发布 Snapshot |
| Session 集合和单活动准入 | Supervisor | 按 SessionID 操作 |
| 跨模块用例的执行顺序 | daemon app | 调用各模块的公开边界 |
| 单个 IPC 连接的发送队列 | 该 IPC 连接 | Application 不等待慢客户端 |

模块之间发送意图并读取快照。两个模块不得同时修改同一份状态。

## 三种标识

- `ConfigurationID` 持久化，它标识一份连接配置。
- `CredentialID` 持久化，它标识一份秘密记录。
- `SessionID` 只在当前 daemon 进程中有效，它标识一次正在运行或已经停止的认证。

自动连接设置保存 `ConfigurationID`。daemon 重启后会重新读取配置，并创建新的 `SessionID`。

## daemon app 如何启动认证

daemon app 接受两种长期存在的启动来源：

1. typed 一次性启动请求；
2. 持久化 `ConfigurationID`。

一次性启动时，daemon app 验证 typed 参数和秘密，不把秘密持久化、记录到日志或放入公开结果。

按 `ConfigurationID` 启动时，daemon app 让 Configuration Catalog 读取并验证配置，再根据其中的 `CredentialID` 读取凭据。

两种来源随后汇入相同流程：

1. daemon app 读取机构 Profile 和协议 Factory；
2. daemon app 组装不依赖 JSON 的 `RunDefinition`；
3. daemon app 把 `RunDefinition` 交给 Supervisor；
4. Supervisor 检查是否已有活动 Session，然后分配 `SessionID` 并创建 Session；
5. Supervisor 先把已保存的最新环境快照交给 Session；
6. Session 选择网络绑定并运行协议；
7. daemon app 返回初始 Snapshot，之后 IPC 发送带 revision 的最新完整 Snapshot。

聚焦测试可以由测试装配提供环境事实，但普通 IPC 请求不接受任意环境参数袋。真实
Windows 认证仍必须使用 Environment Detector。

如果任一步失败，daemon app 应保留底层原因并增加业务语义。失败不得留下半创建的 Session 或半写入的配置。

## 生产 daemon 装配和生命周期

`cmd/sidraviad` 是生产对象图和进程生命周期的组合根。首版启动时依次：

1. 解析当前用户的 Sidravia 配置目录和运行信息路径；
2. 创建当前用户的 SecureStore；
3. 注册唯一生产协议 D520；
4. 一次性加载机构 Profile；
5. 打开 Configuration Catalog 和 Credentials Store；
6. 读取真实 Windows host information；
7. 创建带生产重试策略的 Supervisor、AuthenticationResolver 和 Application；
8. 用 `app.IPCHandler` 组合 IPC server；
9. 创建真实 Windows Environment Observer；
10. 最后进入 host、Observer 和网络快照转交的共同运行期。

组合根通过统一运行目录解析边界 `internal/productlayout` 获得 Profile、
Configuration、Credential 和运行信息的绝对路径，再把路径交给各模块。模块不自行
判断安装版或便携版。Settings Store 和自动连接尚未接入这条一次性认证链路。

运行期由组合根统一拥有三个并发活动：

- Windows host/HTTP server；
- 阻塞式 Environment Observer；
- 把 Observer Snapshot 交给 Application 的转交循环。

任一活动意外失败都会取消共同 context；组合根等待三个活动全部退出，再关闭并等待
Supervisor。正常系统退出由 host 返回成功触发相同清理顺序。Observer 和转交循环不得
在各自内部留下无主 goroutine。启动构造失败发生在运行信息发布前，因此客户端不会看到
一个尚未完成装配的 daemon。

首版 Session 恢复策略是产品级固定策略：普通瞬时网络失败等待 5 秒，协议报告持续 busy
等待 30 秒，阻塞分类不创建 timer。它不做指数退避。D520 自己的 exchange timeout、
heartbeat 和单个 Run 内 busy 重试仍来自机构 Profile；如果现场证据证明 Session 恢复
延迟也因机构不同，再把该策略迁入 Profile，而不是让协议直接控制 Session timer。

详细生命周期所有权见
`docs/decisions/0011-daemon-composition-owns-runtime-lifecycle.md`。

## 运行日志边界

`cmd/sidraviad` 构造唯一生产 `*slog.Logger`，使用 `TextHandler`、stderr，默认
Info 级别。`SIDRAVIA_LOG_LEVEL` 精确接受小写 `info`、`debug` 和 `trace`；未设置
表示 `info`；`trace` 是低于 `debug` 的自定义级别，启用完整 D520 数据报日志；非法
值导致构造失败且不回显该值。logger 显式传入生产装配、`composedRuntime` 和 IPC
Server，不修改 package-global default logger。核心 Session、Supervisor、D520、
持久化和配置包不导入 `log/slog`。

每条运行日志都带稳定 `event` 码、固定简体中文 `msg` 和该事件允许的安全属性。
事件、等级与安全属性以 ADR 0013 与 ADR 0015 为准。`method` 只接受当前 IPC 契约
方法，其他值归一化为 `unknown`；`error_code` 只接受当前契约错误码，其他值归一化
为 `internal_error`；`stage` 只接受 `encode` 和 `write`。固定消息绝不由 error、
请求或响应构造。成功 IPC 请求完成与连接建立在 Debug 记录；拒绝与响应失败保留
Warn。

Info/Debug 记录 operator-significant 的公开标识：完整账号名、机构显示名、友好
接口名与所选 IPv4。日志永不包含原始 error 或包装的诊断原因、请求/响应字节、
request ID、token、endpoint、密码、凭据 ID、Profile JSON、协议上下文或网卡
ID/MAC/网关/DNS/DHCP/主机名。Trace 数据报记录是唯一含完整报文字节的位置，且
仅在显式启用 Trace 时出现；启用时先发一条 Warn `trace_logging_sensitive`。进程
边界把 fatal reporting 与 `os.Exit` 分离：构造失败只发一条 `daemon_start_failed`，
运行失败只发一条 `daemon_runtime_failed`，两者都不含返回的 error，但原始 error
仍由错误传播保留。日志是观察层，不改变 IPC 响应、错误码、生命周期取消、
goroutine 所有权或退出结果。

## 分层诊断边界

Session 与协议诊断由窄的、传输中立的接口承载：`session.Diagnostics` 观察
Session 生命周期（每次已提交 revision、命令、协议运行代际、重试调度），
`protocol.AuthenticationProtocolDiagnostics` 观察协议阶段边界与 UDP 数据报。
组合根注入生产 adapter；Nil/禁用诊断使用显式 no-op 实现。Session 的
`SessionSnapshot` 在每次已提交 revision 时直接调用，不经过 coalescing 的
`RevisionEvents` 流。D520 的 UDP exchange 接收稳定 phase 与诊断 sink，不从报文
内容派生字段；`ProtocolDiagnosticsFactory` 把每个协议 sink 绑定到 Session，使
Trace 数据报记录含 `session_id` 而协议运行本身不知道它。诊断 sink 无返回值，
永不改变协议、重试、Session 状态、IPC 响应、关闭顺序或返回值。完整等级与隐私
契约见 ADR 0015。

logger 的 sink 仍是 stderr TextHandler。用户直接运行 `sidraviad.exe` 时 stderr
保持前台可见；Windows CLI launcher 真正创建后台子进程时拥有输出文件句柄，把 child
stdout/stderr 都重定向到运行目录解析结果中的 `logs/sidraviad.log`，并在启动前按
10 MiB 阈值轮转为唯一 `.1` 备份。安装版解析为
`<UserCacheDir>/Sidravia/logs/sidraviad.log`，便携版解析为
`<exe-dir>/logs/sidraviad.log`。launcher 的文件所有权不改变 logger、事件、等级或
隐私契约，也不增加日志 IPC/CLI 命令。日志行为见 ADR 0019，目录选择由 ADR 0020
补充。

## CLI 呈现边界

`internal/cli/presentation.go` 是唯一的 CLI 呈现边界，也是唯一可以导入
`github.com/muesli/termenv`（固定 `v0.16.0`）的生产文件。它拥有终端能力选择、Windows
虚拟终端启用/恢复、可信静态标签与映射状态文本的样式、动态值控制字符清理、稳定机器
码的简体中文映射，以及在写入前完成整块渲染。业务操作仍只返回错误和 DTO；呈现边界不
拥有 daemon 发现、IPC 调用、密码内容、Session 状态、重试决策、持久化或协议行为，且
任何 termenv 类型都不跨出 `internal/cli`。

终端能力以 termenv 对 writer 的实际 `ColorProfile()` 作为第一道门：当它是 `Ascii`
（重定向或非交互 writer）时，无论 `CLICOLOR_FORCE` 如何都强制纯文本；只有彩色真实
终端才由 `EnvColorProfile()` 精炼，此时 `NO_COLOR` 禁用 ANSI。因此重定向或管道输出
始终纯文本，生产代码绝不强制 ANSI。呈现边界不查询前景/背景色、终端宽度、光标位置或
明暗主题，也不修改 termenv 的 package-global default output。Windows 虚拟终端启用是
可选能力：失败时继续以纯文本输出，恢复函数在命令输出后执行，且任何 VT 错误都不替代
业务错误。

每个动态字符串在写入前都把 C0/C1 控制字符和 DEL 替换为替换符 `�`，保留普通
Unicode、空格、标点、ID、时间戳和中文。动态值绝不进入颜色解析器，CLI 也永不打印
daemon `Error.Message`、Session `Description`、失败 `Description`、包装原因、请求
payload、凭据或原始终端环境值。命令令牌和 flag 不变；顶层、分组和叶子命令使用确定性
中文 help 渲染器；bare root/group、`help <path>`、`-h` 和 `--help` 解析到同一
canonical help 规格，按固定顺序含描述、`用法`、`可用命令`、`参数`、`选项`、`示例`
六段（适用时），每条用法独占缩进行且段间留一空行。`cmd/sidravia/main.go` 通过呈现
边界打印静态中文错误前缀，不打印底层原因。

持久 Configuration/Credentials 的引导交互仍留在这个呈现边界中，但采用短生命周期、
逐行的私有构件，而不是全屏 TUI。当前大版本继续使用 Cobra 与 termenv；普通文本输入、
枚举选择和确认必须把结果交给与非交互命令相同的 typed 用例，重定向时不得隐式进入
向导。具体决策见 ADR 0021。

## Session 和 Supervisor

每个 Session 从创建开始就拥有稳定的 `SessionID`。Session 管理自己的协议运行、取消、重试、状态和 Snapshot。Session 不知道其他 Session，也不读写配置或凭据。

Session 可以在内部使用私有 epoch 来拒绝过期异步结果。这个值不会持久化，也不会通过 IPC 暴露。Supervisor 不使用它进行调度。

Supervisor 管理 Session 集合。首版 Supervisor 最多允许一个活动 Session。它可以启动、停止、重启、读取、列出和遗忘已经停止的 Session。

停止采用两阶段状态。Session 接受 Stop 后立即发布 `stopping` 并请求活动协议 Run
执行有界的尽力 Logout；只有 Run 已退出时才发布 `suspended`。没有活动 Run 时也通过
私有队列事件依次发布这两个 revision。`stopping` 期间重复 Stop 幂等，其他输入和异步
回调不能恢复认证或启动新 Run。Supervisor 在观察到权威 `suspended` Snapshot 前持续
保留单活动准入槽，因此旧 Run 清理与新 Run 不会因过早释放准入而重叠。详细理由见
`docs/decisions/0012-two-phase-session-stop.md`。

Supervisor 还保存 Environment Detector 已经产生、daemon app 已经接受的最新 typed
网络快照。新 revision 会分发给现有 Session；新 Session 在返回初始 Snapshot 前获得
最近快照；旧 revision 被忽略；相同 revision 可以重放已保存的权威内容，以恢复部分
分发失败。Supervisor 不解释网卡事实，也不替 Session 选择绑定。

Environment Snapshot 保留操作系统报告的 hardware、connector、filter 和 endpoint
接口分类。首版 `automatically_select_latest_available` 只把已启动、有 IPv4、由真实
硬件支持、存在物理适配器并且不是 filter 或 endpoint 的接口作为候选；Windows 虚拟
Ethernet、Hyper-V/WSL/Docker 内部接口和软件 VPN 不得仅因被报告为 Ethernet 或较晚
出现而取代校园物理网卡。该规则使用操作系统提供的接口事实，不依赖显示名称黑名单。
未来若加入显式手动绑定，可以单独决定是否允许选择虚拟接口，不改变自动模式的安全
默认值。

当前 Windows Environment Observer 已提供主机信息、接口名称与稳定 ID、Up 状态、
wired/wireless、hardware/connector/filter/endpoint 分类、MAC、IPv4/前缀、网关、
DNS、DHCP 以及单调 snapshot revision。这些事实足以保护当前物理接口自动候选规则，
但还不是完整的跨平台和路由模型：它不包含面向认证服务器目标地址的路由、接口 metric、
操作系统最终选择的源 IPv4，也没有 Linux/WSL observer 与 host 实现。Linux/WSL 基线
应先复用平台无关模型并明确 Unsupported/可观察范围；热点、ICS、多 IPv4 和目标路由
修正随后以新证据扩展 typed facts，不用网卡显示名称黑名单掩盖系统差异。

Supervisor 不读取 JSON、配置、凭据、文件路径或 ACL。未来如果产品需要多个并发 Session，应只扩展 Supervisor 的调度策略，不应重写 Session 模型。

retained Session 的 ensure-running、restart 和 remove 也由 Supervisor 原子拥有。
同一 ID 的 Stop/ensure/restart/remove/ForgetStopped 串行；`stopping` 期间的继续运行
意图预留单活动准入并等待 `suspended`。remove 只在 actor、协议清理和 revision
forwarder 全部退出且 ID 从集合删除后成功。

## 配置、凭据和持久化

Configuration 文件只保存非秘密配置。Configuration 通过 `CredentialID` 引用 Credentials Store 中的记录。

机构 Profile 从本地可编辑文件加载，并在 daemon 启动时进入 `ProfileCatalog`。D520
endpoint、超时、重试边界、固定协议字段和其他机构差异必须保留在 Profile 中，不得编译
成协议包里的机构专用常量。普通一次性认证请求仍只提交 `InstitutionProfileID`，不通过
IPC 携带任意 Profile JSON 或覆盖系统网络事实。

首版使用 `institution-profiles/` 目录，每个 Profile 对应一个
`<InstitutionProfileID>.json` 文件。ID 使用简短稳定标识，例如吉林大学使用 `jlu` 和
`jlu.json`，不追加 `campus` 等冗余后缀。daemon 只在启动时加载目录；本地编辑后重启
daemon 生效，首版不监听目录也不热替换运行中的 Profile。

Windows 首版允许 Credentials Store 在独立 JSON 文件中保存明文密码。SecureStore
必须把文件权限限制为当前用户和 SYSTEM。如果 SecureStore 无法建立所需 ACL，它不得
留下新的明文目标文件。当前产品威胁边界不包含已获得同一操作系统用户权限的恶意代码；
这不放宽密码不得进入命令行、日志、Snapshot、查询响应或错误文本的规则。

持久化模块必须先构造完整候选内容，然后原子替换目标文件。如果磁盘写入失败，内存中的权威状态不得提前改变。

密码、随机 token 和内部错误链不得出现在 Configuration、Session Snapshot、IPC 查询结果或普通日志中。

CLI 必须能够用人类可读的形式显示非秘密结构化配置。CLI 不提供读取凭据明文的命令。

## IPC

IPC 使用本机回环 WebSocket。连接建立前，server 必须验证随机 token 和精确 BuildID。

contract 只定义 Request、Response 和 Event：

- 每个 Request 都有非空 ID、明确 Method 和明确 Payload 类型。
- server 接受一个 Request 后，必须为同一 ID 返回一个 Response。
- 每个 Event 都包含资源 ID、单调 revision 和最新完整公开 Snapshot。

客户端重连后应主动查询权威 Snapshot。系统不保存事件历史，也不实现事件确认或补发。

如果客户端持续跟不上事件，IPC server 应关闭该连接。慢客户端不得阻塞 daemon app。

当前代码已经实现 `daemon.status`、`session.list` 和 `profile.list` 的 typed
contract，以及 WebSocket client/server 和 Windows host 骨架。列表请求只接受严格
空对象 `{}`；响应始终返回非 null 数组，并且 Profile 摘要只包含 ID、显示名和协议
ID。列表切片完成代码与自动验证后仍需 Windows 原生复核。

首版 IPC 只提供以下产品操作：

- CLI 查询 daemon 状态和版本。
- CLI 查询当前环境快照。
- CLI 列出、读取、保存和删除连接配置。
- CLI 写入、替换和删除凭据。daemon 不提供读取凭据明文的操作。
- CLI 用 typed 一次性参数启动认证，或者按 `ConfigurationID` 启动认证，并获得 `SessionID`。
- CLI 按 `SessionID` 停止、重启、读取和列出 Session。
- CLI 订阅 Session Snapshot 事件。

首版不提供通用 RPC、批处理、远程 IPC 或历史事件重放。

上述配置、凭据、环境和 Session 方法都是 daemon app 应用用例的传输入口。IPC server 不直接依赖对应存储、Detector、Supervisor、Session 或协议包。

当前大版本允许持久 Credential create/replace/delete 继续使用回环 `ws://`、随机 token
和精确 BuildID，不增加自制应用层加密。WSS、并发请求、事件重连/重同步与 GUI 放在下一
大版本的同一 IPC 演进阶段；GUI 与 CLI/daemon 共享产品版本和 BuildID，并继续只调用
typed IPC。威胁边界和实施顺序见 ADR 0022。

## Windows host

Windows host 必须执行以下动作：

1. daemon 使用当前用户 SID 创建命名 mutex，以阻止同一用户启动第二个 daemon。
2. daemon 只监听 `127.0.0.1`，并让操作系统选择端口。
3. daemon 生成随机 token，然后用 SecureStore 写入 endpoint、PID、token、ProductVersion 和 BuildID。
4. daemon 收到退出信号后停止 HTTP server，等待连接退出，然后释放 mutex。
5. daemon 只有在运行信息仍包含自己的 PID 和 token 时，才删除该文件。

CLI 只启动与自身位于同一目录的 `sidraviad.exe`。如果现有运行信息无效或连接失败，CLI 应把它视为失效信息，并在五秒内尝试连接新 daemon。

Service、管理员权限、登录前认证、系统通知和自动更新不属于当前切片。

CLI 的 `daemon status` 只读取运行信息并探测其中描述的 generation，不启动、停止、
删除或改写任何状态。`daemon stop` 与 `daemon restart` 把首次成功探测得到的 endpoint、
token、BuildID 和 PID 作为不可变 generation，停止请求及后续不可达等待始终针对它。
IPC server 只在成功响应完成编码并写入后调用构造时注入的 commit callback；组合根用
容量为一的 channel 和 `sync.Once` 接收首个已提交的 `daemon.stop`，再拥有取消、等待
和 Supervisor 清理。Windows CLI 启动同目录、无新控制台窗口的子进程，为子进程设置
唯一确定的 `SIDRAVIA_LOG_LEVEL`，不继承 stdin，并把 stdout/stderr 交给上述后台日志
文件；平台无关 CLI/domain 边界不复制 Windows 生命周期实现。

## 第一条产品验收链路

产品先用一次性 typed 参数证明高风险核心链路：

1. CLI 通过 IPC 提交一次性启动请求；
2. daemon app 组装 `RunDefinition` 并交给 Supervisor；
3. Supervisor 创建 Session；
4. Session 运行 Go 实现的 Dr.COM；
5. D520 使用配置的 UDP endpoint 完成登录、保活和停止；
6. CLI 查询或收到带 revision 的完整 Snapshot。

随后补齐持久化输入链路：

1. CLI 保存 Configuration 和 Credential；
2. CLI 通过 IPC 请求 daemon app 启动指定 `ConfigurationID`；
3. daemon app 从 Configuration、Credentials 和 Environment 生成相同的 `RunDefinition`；
4. 后续 Supervisor、Session 和协议流程不变。

自动测试可以使用最小的本地 UDP test peer 证明 Run 的流程、取消和错误分类，但不建设
新的完整 mock 链，也不把本地 test peer 或现有 Python mock 当作真实协议正确性证据。
校园网络现场验证必须单独记录。

## Dr.COM 5.2.0(D) 实现位置

首个真实协议实现位于：

```text
internal/daemon/authentication/protocol/drcom/d520
```

运行时协议 ID 固定为 `drcom-5.2.0-d`。当前 `d520` 包含确定性 wire codec、严格机构
Profile 解码、`AuthenticationProtocolFactory` 实现和阻塞式
`AuthenticationProtocolRun` 实现。Session 只传入认证数据、网络绑定、主机事实和可取消
context，并接收认证建立或结构化失败。D520 内部的 Challenge、Login、Keepalive、报文和
UDP 状态全部私有，不为内部步骤创建没有真实替换点的接口。

CLI 的机构 Profile ID 和用户名可以使用命令行参数。密码不得进入 argv：交互终端使用
隐藏输入，自动化使用 `--password-stdin`。首版不提供 `--password`。

`auth start` 默认只在 stdin 是真实交互终端时读取密码。CLI 把 `密码： ` 提示写到
stderr，关闭输入回显，读取一行，并在成功、失败或取消路径恢复原控制台模式；它不要求
二次确认。stdin 不是终端而调用者又没有明确提供 `--password-stdin` 时，CLI 返回安全
用法错误，不静默读取管道。`--password-stdin` 读取一行，只移除该行结尾的 CR/LF；
额外输入不得被当作另一个参数或打印。密码允许为空，与 typed IPC 契约一致。CLI 不提供
`--password`，也不得把密码写入错误、普通输出或测试失败文本。

首版认证 CLI 使用一个稳定命令族：

```text
sidravia auth start --profile <profile-id> --username <username>
sidravia auth start --session <session-id>
sidravia auth restart <session-id>
sidravia auth remove <session-id>
sidravia auth list
sidravia auth status <session-id>
sidravia auth stop <session-id>
sidravia profile list
```

`auth start` 表示创建并由 daemon 持续维持一个认证 Session，`auth status` 查询该
Session 的公开 Snapshot，`auth stop` 停止 Session 并按协议要求执行尽力退出。顶层
`sidravia daemon status` 只表示 daemon 进程状态，不与认证状态复用。迁移期内旧的
`sidravia status` 不作为兼容别名执行，而是返回固定迁移提示。首版不增加
`login`/`logout` 兼容别名；面向普通用户的 GUI 可以使用“登录/退出”文案，而不改变
底层 CLI 和 Session 语义。

`auth list` 按 `SessionID` 稳定顺序列出当前 daemon 进程保留的全部 Session Snapshot；
daemon 重启后不会恢复旧列表。`profile list` 按 Profile Catalog 的稳定顺序列出安全
摘要，不读取或输出机构协议配置、用户名或密码。两个列表命令都复用相同的 daemon
发现链和呈现边界，验证完整响应后一次写出；重定向或管道输出始终为纯文本，且不使用宽度探测或对齐填充。

`auth start` 在 daemon 成功创建 Session 后立即打印返回的初始公开 Snapshot 和
SessionID，然后退出。它不轮询到认证成功，也不因 CLI 退出而停止 daemon 中继续保活或
重试的 Session。用户使用 `auth status <session-id>` 读取后续权威状态。首版不增加
`--wait`；以后需要持续观察时，应通过 Session Snapshot 事件提供独立的
`auth watch <session-id>`，而不是让 `auth start` 隐式变成长时间附着命令或跟随日志
文件。

认证 CLI 的退出码只表示请求和输出操作是否成功，不把 Session 状态当作命令执行失败。
成功创建 Session 的 `auth start`、成功读取任何公开状态的 `auth status` 和成功接受
停止请求的 `auth stop` 都返回零，包括 `waiting_for_network`、
`waiting_before_retry`、`blocked_by_error`、`stopping` 或 `suspended`。Stop 返回
`stopping` 只表示退出请求已被接受；需要确认资源已经退出的调用方继续查询，直到看到
`suspended`。参数、密码输入、daemon 启动/连接、IPC、解码或 Session 操作失败返回
非零。以后若脚本需要把“当前是否 authenticated”作为条件，应增加显式
`auth check`，不改变 `auth status` 的查询语义。

三个认证命令使用同一个多行人类可读 Snapshot renderer，由 `internal/cli` 的呈现边界
拥有。它始终显示 SessionID、state、Profile、协议、完整账号名 和更新时间，并
只在存在时显示 state reason、所选网络绑定、认证建立时间、下次重试时间和最后一次
公开失败。输出把 IPC `SessionResult` 中的稳定状态/失败/建议码映射为简体中文，并在
括号内保留稳定机器码；它不显示描述、内部诊断或原始秘密，且每个动态值在写入前清理
控制字符。首版不把人类输出伪装成脚本格式；以后需要机器消费时增加显式 `--json`，不
要求脚本解析多行文本。`sidravia daemon status` 继续使用自己的单行 daemon 摘要。

`auth start`、`auth status`、`auth stop`、`auth list` 和 `profile list` 都复用 daemon status 已有的 daemon 发现语义：
先尝试运行信息中的现有 daemon，连接失败则至多一次启动与 CLI 同目录的
`sidraviad.exe`，并在同一个五秒总边界内轮询新的运行信息和连接。认证命令不要求用户
预先运行 `sidravia daemon status`。如果 `auth status/stop` 因冷启动进入了一个没有目标
Session 的新 daemon，它返回安全的 Session 操作失败，不猜测、缓存或复用旧进程的
SessionID。

## Dr.COM 5.2.0(D) Run 内部边界

Session 是产品层认证状态机。它拥有意图、公开状态、网络变化、取消、重连调度和
Snapshot。D520 的一次 Run 只拥有该次 UDP 执行的私有线级状态，不接收激活、暂停或
重启等 Session 命令。

Factory 验证并解码机构 Profile、凭据、选定网络绑定和主机事实，生成不可变的私有 Run
定义。每次 `Execute` 再创建私有 execution，独占：

- UDP socket；
- login salt 和 Auth Info；
- KA2 Tail、serial 和已发送 KA 交换的迟到响应识别状态；
- 本次执行使用的 deadline、timestamp 和随机值。

`Execute` 使用阻塞式确定性流程：

```text
Challenge
-> Login
-> KA1 + KA2 1-1-3
-> AuthenticationEstablished
-> 每个 heartbeat 周期执行 KA1 + KA2 1-3
-> best-effort Logout
```

Login 只对 server busy 在 Run 内做有界短重试。Run 结束后的重新认证、标准或延长延迟及
阻塞等待输入变化由 Session 根据结构化失败建议决定。

每个阻塞 request/response exchange 先设置自己的绝对 socket deadline，再注册一个与
本次 context 绑定的短生命周期取消回调。取消回调只把 socket deadline 推到当前时间以
唤醒 I/O，不关闭 socket。exchange 结束时停止该回调；如果回调已经开始，则等待它完成，
之后才能重设下一次 I/O deadline。best-effort Logout 使用独立 context 和有界 deadline。
实现不使用周期性 deadline 轮询，也不从已经取消的执行 context 派生 Logout timeout。
取消要求 Logout 而清理失败时，Run 返回稳定的内部清理 failure；Session 仍以取消为
权威并进入 `suspended`，且只把该 failure 保存在私有诊断中。它不进入当前 Snapshot、
IPC 或 CLI。

每次 request/response exchange 使用一个不会因忽略报文而延长的绝对 deadline。完整且
能证明属于已经发送过的旧 KA1/KA2 交换的响应可以忽略；当前阶段的合法响应才推动流程。
结构畸形、无法安全识别或不可能由旧交换产生的响应返回协议失败。

D520 wire codec 保留语义明确的请求 builder 和预期响应 parser。它不根据 opcode 对任意
收到的 UDP 包猜测流程阶段，因为 KA1 与 KA2 的 opcode 会重叠，KA2 还必须结合当前
serial 和 type 校验。固定 KA2 序列可以用私有步骤表复用，但不得演变为第二套通用状态机。

长 Login 报文按协议布局区域组织。固定长度、偏移和字段语义保持显式；文本编码、固定字段
验证和密码学使用小型私有 helper。不得使用反射、外部 schema 或 Go 内存布局自动序列化
线级报文。

详细理由和后续条件见
`docs/decisions/0009-d520-blocking-run-and-explicit-wire-codec.md`。
