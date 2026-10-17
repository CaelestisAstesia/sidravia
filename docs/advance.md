# 命令行与高级使用

Sidravia 除图形界面外，还提供 `sidraviactl` 命令行工具（下文简称 **ctl**），适合一次性认证、多配置、脚本、独立后台及网络诊断。**如果只想通过图形界面连接网络，不需要使用本页命令**；请先阅读[快速开始](quick-start.md)。

本页示例基于 `v0.3.0-alpha.1`。Windows 示例假定已在程序目录打开 PowerShell，并使用 `./` 的 Windows 写法 `.\sidraviactl.exe`；如果已加入用户 PATH，可以直接输入 `sidraviactl`。Linux 核心包使用 `./sidraviactl`，不支持 Windows 自启和 PATH 集成命令。

命令中的 `<configuration-id>`、`<session-id>` 和 `<账号>` 均为占位符，**实际使用时替换内容，不保留尖括号**。交互式输入密码时终端不会回显；不要把密码直接放进命令行参数。

[后台与命令副作用](#后台与命令副作用) · [认证参数](#认证启动参数) · [配置参数](#更新密码与删除参数) · [MGR 完整参考](manager.md) · [协议覆写](override.md)

## 与 GUI 配合使用

Sidravia 有两种互斥的认证后台模式：

| 模式 | 谁使用 | 运行方式 |
| --- | --- | --- |
| **desktop** | Windows GUI | 由图形界面启动、管理；隐藏到托盘时继续运行，真正退出后结束 |
| **headless** | ctl | 独立运行；一条 ctl 命令结束，并不代表后台及认证立即停止 |

**不能让 GUI 和 ctl 同时控制同一认证后台。** 从 GUI 改用 ctl 时，先在托盘菜单选择「退出 Sidravia」；从 ctl 改回 GUI 时先执行 `sidraviactl daemon stop`。切换会断开当前连接，先前的 Session ID 也会失效。GUI 正在运行时，ctl 仅允许以 `daemon status` 跨模式检查后台状态，不应尝试跨模式修改认证配置。

GUI 目前只能管理**一份**保存的连接配置。使用 ctl 保存多份配置后，如要返回 GUI，应先确认需要保留哪份；**删除配置同时会删除其中保存的账号和密码**。ctl 保存的协议覆写也会影响以后通过 GUI 使用该配置的连接，但 GUI 不提供覆写编辑界面。

## 独立后台

```powershell
.\sidraviactl.exe daemon start
.\sidraviactl.exe daemon status
```

停止后台（会结束相关认证会话）：

```powershell
.\sidraviactl.exe daemon stop
```

已有保存配置启用自动登录时，启动后台后可能自动发起认证。建议先执行 `auth list`，确认当前会话，再决定是否手动连接。需要重启后台可以使用 `daemon restart`，但它会中断原连接。

## 后台与命令副作用

| 命令 | 后台不存在时 | 对当前认证的影响 |
| --- | --- | --- |
| `daemon status` | 查询，不启动 | 只读；可跨 desktop/headless 模式查询 |
| `daemon start [--log-level LEVEL]` | 启动 headless 后台 | 已有兼容 headless 后台时复用，不强制重启；指定等级不等于热更新已有后台 |
| `daemon stop` | 已停止也可成功 | 停止 headless 后台及其所有会话 |
| `daemon restart [--log-level LEVEL]` | 启动 headless 后台 | 已运行时先停止再启动，旧会话失效；配置自动登录可能重新发起认证 |
| `profile list`、`auth list/status/watch`、`config list/show`、`network interfaces/diagnose`、`diagnostics export` | 报告无法连接，不启动 | 连接已有 headless 后台；导出另会创建本地文件，`--probe` 会主动探测 |
| `auth start/stop/restart/remove`、`config create/update/set-password/remove` | 可按需启动 headless 后台 | 按具体操作改变会话或配置；即使操作失败，已启动后台及自动登录也可能已生效 |
| `integration ...` | 不依赖认证后台 | 调用同目录 MGR；读取或修改 Windows 当前用户系统集成 |
| `help`、公开命令的 `--help` / `-h` | 不启动 | 仅显示帮助 |

以上操作不会接管 desktop 后台。只读命令也要求相同构建的配套程序。`daemon status` 的退出成功仅表示状态查询完成，不能用退出码 `0` 推断后台一定在线或认证成功。

## 一次性认证

在已经启动的 headless 后台中，先列出机构 Profile，然后发起一次认证：

```powershell
.\sidraviactl.exe profile list
.\sidraviactl.exe auth start --profile jlu --username '<账号>'
```

程序会交互式读取密码。本次会话的账号设置**不会成为一份持久保存的配置**。`auth start` 返回不表示认证已经成功；它可能仍在进行中。使用命令返回的 Session ID 查看结果：

```powershell
.\sidraviactl.exe auth list
.\sidraviactl.exe auth status <session-id>
.\sidraviactl.exe auth watch
```

`auth watch` 持续显示最新状态；按 **Ctrl+C** 退出观察，**不会因为退出观察而断开连接**。它也不是完整、持久的事件历史。

对已有 Session，可以按需使用：

```powershell
.\sidraviactl.exe auth stop <session-id>
.\sidraviactl.exe auth restart <session-id>
.\sidraviactl.exe auth remove <session-id>
```

`stop` 结束认证但保留会话记录；`remove` 停止并移除该运行记录。两者都**不会删除保存的 Configuration**。

## 认证启动参数

`auth start` 有三种互斥形式：

```text
sidraviactl auth start --profile PROFILE --username USER [--password-stdin] [--interface-id ID --local-ipv4 IPv4] [--protocol-override-file PATH]
sidraviactl auth start --config CONFIGURATION_ID
sidraviactl auth start --session SESSION_ID
```

| 参数 | 作用与限制 |
| --- | --- |
| `--profile PROFILE`、`--username USER` | 一次性认证必填；密码由隐藏提示或 stdin 读取 |
| `--password-stdin` | 从标准输入读取一行密码；仅用于一次性认证 |
| `--interface-id ID`、`--local-ipv4 IPv4` | 必须成对使用；省略则自动绑定；仅用于一次性认证 |
| `--protocol-override-file PATH` | 为一次性会话提供覆写；不保存配置，格式见[覆写参考](override.md) |
| `--config CONFIGURATION_ID` | 使用已保存配置，启动或确保其会话运行 |
| `--session SESSION_ID` | 确保当前后台保留的指定会话运行；已处于认证、连接、等待或错误状态时不强制重启 |

`--config` 与 `--session` 不能混用，也不能与一次性认证的账号、密码、网卡或覆写参数混用。一次性认证没有单独的 `--auto-login`、`--auto-reconnect` 或 `--automatic-binding` 参数。`--profile`、`--username`、`--config`、`--session` 使用空格分隔值；不要套用创建配置的布尔开关写法。

`auth list` 列出当前后台保留的全部会话；`auth status ID` 查询一条公开快照；`auth watch` 显示初始状态并持续观察会话和网络，通信断开后需重新运行。它可能合并更新，不保证逐条事件历史。

`auth stop ID` 停止并保留记录；`auth start --session ID` 可恢复已停止会话；`auth restart ID` 则明确发起一次重启，包括对失败或等待中的会话重新尝试。`auth remove ID` 停止并删除运行记录。这些操作不删除持久配置；它们成功返回也不代表认证已经完成。

## 保存配置

交互式创建配置：

```powershell
.\sidraviactl.exe config create
.\sidraviactl.exe config list
.\sidraviactl.exe config show <configuration-id>
.\sidraviactl.exe auth start --config <configuration-id>
```

创建配置本身不会立即认证；`auth start --config` 才会启动或确保该配置对应的会话运行。

- **Configuration ID** 是持久配置标识，程序重启后仍存在，不能通过 `config update` 修改。
- **Session ID** 是当前后台中的运行标识，后台结束后不再有效。
- ctl 可以保存多份配置，但**同一后台同时最多有一个活动认证会话**；只有一份配置可开启自动登录。

修改现有配置：

```powershell
.\sidraviactl.exe config update <configuration-id> --auto-login true
.\sidraviactl.exe config update <configuration-id> --auto-reconnect true
.\sidraviactl.exe config set-password <configuration-id>
```

注意 `config update` 的 `--auto-login` 和 `--auto-reconnect` 要明确给出 `true` 或 `false`；自动登录不会因为刚打开开关就立即认证。更改自动重连设置要到下一次连接才生效。

**变更认证输入可能终止旧会话。** 账号、机构、网卡绑定或协议覆写实际变化后，旧的认证 Session 会退役，需按需重新连接；即使输入相同的新密码，调用 `set-password` 也会清理旧会话。单独调整显示名称或自动连接偏好，不会因此立即替换当前认证会话。

## 创建参数、脚本与多配置

可以指定 ID、机构、账号和显示名称，仍以交互方式输入密码：

```powershell
.\sidraviactl.exe config create --id daily --profile jlu --username '<账号>' --name '日常连接' --auto-login=false --auto-reconnect=true
```

| 创建参数 | 说明 |
| --- | --- |
| `--id ID` | 自定义配置 ID；非交互创建必填 |
| `--profile ID` | 内置机构 Profile ID；非交互创建必填，吉林大学为 `jlu` |
| `--username USER` | 认证账号；非交互创建必填 |
| `--name NAME` | 可选显示名称 |
| `--password-stdin` | 从标准输入读取密码，供非交互调用 |
| `--auto-login[=true\|false]` | 创建时默认 `false` |
| `--auto-reconnect[=true\|false]` | 创建时默认 `true` |
| `--interface-id ID` 与 `--local-ipv4 IPv4` | 必须成对使用，指定实际网络绑定；未提供时可自动选择 |
| `--protocol-override-file PATH` | 保存[协议上报覆写](override.md) |
| `--allow-insecure-storage` | 明确允许在存储保护不足时继续；**不会加密或修复文件权限** |

交互创建时，未显式提供的自动登录、自动重连偏好会询问用户；未提供绑定参数时会进入网卡选择，允许自动绑定或选择已观察的接口与 IPv4。非交互创建省略绑定参数则自动选择。创建没有 `--automatic-binding` 或 `--clear-protocol-override` 参数。

完全非交互式创建必须显式提供 `--id`、`--profile`、`--username` 和 `--password-stdin`。自动化程序应直接启动 ctl，并向其标准输入传入**一行 UTF-8 密码**；不要使用会把密码记录在终端历史中的命令，也不要把凭据提交到仓库。尤其要注意 Windows PowerShell 5.1 的默认管道编码可能改写非 ASCII 输入。

从 stdin 读取的密码上限是 **4096 字节**，结尾 LF 或 CRLF 会被移除。D520 账号严格按 GBK 编码后最多 **36 字节**；账号、密码及主机文本不接受 NUL 或 GBK 无法表示的字符。字节上限不等于字符数上限。

更新公开字段示例：

```powershell
.\sidraviactl.exe config update daily --name '宿舍连接' --username '<新账号>'
.\sidraviactl.exe config update daily --auto-login true --auto-reconnect false
.\sidraviactl.exe config set-password daily
```

删除配置时会要求确认，且会结束关联的会话：

```powershell
.\sidraviactl.exe config remove daily
```

在明确知道目标及后果的脚本中可以加 `--yes` 跳过确认。**`config remove` 删除持久配置及凭据，`auth remove` 只移除运行会话**，两者不可混淆。

## 更新密码与删除参数

```text
sidraviactl config update ID [--name NAME] [--profile PROFILE] [--username USER] [--auto-login true|false] [--auto-reconnect true|false] [--interface-id ID --local-ipv4 IPv4 | --automatic-binding] [--protocol-override-file PATH | --clear-protocol-override] [--allow-insecure-storage]
sidraviactl config set-password ID [--password-stdin] [--allow-insecure-storage]
sidraviactl config remove ID [--yes] [--allow-insecure-storage]
```

| 更新参数 | 作用与限制 |
| --- | --- |
| `--name NAME` | 修改显示名称；`--name=` 清除名称，避免旧版 PowerShell 丢弃空字符串参数 |
| `--profile PROFILE` | 修改机构 Profile；当前内置 `jlu`，可先用 `profile list` 查询 |
| `--username USER` | 修改认证账号；密码通过 `set-password` 单独修改 |
| `--auto-login true\|false` | 必须明确提供值；开启不会立即连接，且只能有一份配置开启自动登录 |
| `--auto-reconnect true\|false` | 必须明确提供值；下次连接生效 |
| `--interface-id ID`、`--local-ipv4 IPv4` | 成对设置真实绑定，与 `--automatic-binding` 互斥 |
| `--automatic-binding` | 恢复自动选择；创建配置和一次性认证通过省略绑定参数使用自动选择 |
| `--protocol-override-file PATH` | 整体替换覆写，与 `--clear-protocol-override` 互斥 |
| `--clear-protocol-override` | 清除覆写，不接受 `=false` |
| `--allow-insecure-storage` | 在存储保护不足时明确同意操作，不修复权限、不启用加密 |

不提供待更新字段的 `config update ID` 会进入交互式**显示名称**修改：空输入表示保留，`-` 表示清除。它不是所有字段的逐项向导；没有实际更新项会报错。省略的字段和密码保持原值，配置 ID 不可更改。

`config set-password ID --password-stdin` 从 stdin 读取新密码；非交互调用必需。`config remove ID --yes` 跳过删除确认；非交互调用需显式确认。两者也支持 `--allow-insecure-storage`，`--yes` 不替代存储保护同意。创建、更新、改密码和删除均可使用存储保护选项；不要因为遇到错误就自动加上它。

`config list` / `show ID` 返回公开配置信息，不回显密码或原始覆写。输入校验失败不会替换配置；但保存后旧会话清理失败、通信中断、输出失败可能发生在操作已生效之后。先核对配置和会话，不能把所有非零退出都当作“未保存”。

## 指定网卡

先获取当前后台观察到的网卡与 IPv4：

```powershell
.\sidraviactl.exe network interfaces
```

明确选择同一接口上的接口 ID 和 IPv4：

```powershell
.\sidraviactl.exe config update <configuration-id> --interface-id '<接口ID>' --local-ipv4 '<IPv4>'
```

恢复自动选择：

```powershell
.\sidraviactl.exe config update <configuration-id> --automatic-binding
```

一次性认证也支持同时提供 `--interface-id` 与 `--local-ipv4`。**手动绑定不可用时程序会等待该绑定，不会悄悄回退到其他网卡。** 请勿把协议上报的 IPv4 覆写误认为真实 socket 绑定。

## 诊断与日志

```powershell
.\sidraviactl.exe network diagnose --config <configuration-id>
.\sidraviactl.exe network diagnose --config <configuration-id> --probe
.\sidraviactl.exe diagnostics export --output .\sidravia-diagnostics.json
```

`network diagnose` 也支持 `--session <session-id>`，但 `--config` 与 `--session` 必须二选一。`--probe` 发起有限的 IP 探测；即使返回成功，也**不代表认证或互联网访问已成功**。

`diagnostics export` 的 `--output PATH` 必填，父目录需要已存在，目标文件必须**尚不存在**，不会覆盖现有文件。它生成的是有意限制字段的诊断摘要，不包含密码与原始认证报文；分享前仍应检查其中的网络及环境信息。写入失败可能留下不完整文件，提示输出失败也可能发生在文件已完整写入之后；先检查文件再决定重试，并为新导出选用尚不存在的路径。此类只读检查要求已有 headless 后台，不会代你启动新的后台。

调整独立后台日志等级：

```powershell
.\sidraviactl.exe daemon start --log-level debug
.\sidraviactl.exe daemon restart --log-level info
```

可用等级为 `info`、`debug`、`trace`，默认是 `debug`。重启会中断现有会话；`trace` 可能记录完整认证报文，只应短时间用于受控排障，**不要直接公开日志**。详见[数据与隐私](privacy.md)。

## Windows 自启与 PATH

现代 Windows 包通过配套的 `sidraviamgr.exe` 维护**当前 Windows 用户**范围的系统集成。用 ctl 查询、配置：

```powershell
.\sidraviactl.exe integration status
.\sidraviactl.exe integration path enable
.\sidraviactl.exe integration path disable
.\sidraviactl.exe integration autostart set gui
.\sidraviactl.exe integration autostart set daemon
.\sidraviactl.exe integration autostart disable
```

`set gui` 对应「启动到托盘」，`set daemon` 对应「仅后台」，二者只能选一个；自启在**下一次登录 Windows**时生效。PATH 变更后需要重新打开终端。只有具备图形界面组件时才能使用 GUI 自启模式。

系统集成命令以 JSON 返回结果，错误时退出码非零。遇到自启任务冲突，不应覆盖或删除未知的系统任务；先确认原因。Linux 不支持这些 Windows 命令。`integration --help` 在该版本可能不列出所有子命令，以上为实际命令路径。完整的参数、JSON 字段、错误码以及仅 MGR 提供的卸载与数据清理命令，见 [Windows 维护工具（MGR）](manager.md)。

## 协议上报覆写

对于明确支持的 D520 上报字段，可以通过 `--protocol-override-file PATH` 传入严格的 JSON 文件。此功能可用于 `auth start` 的一次性认证，或 `config create` / `config update` 的持久配置。

它**不会更改真实网卡、socket 源地址、认证服务器或认证策略**。完整字段、边界和清除操作请阅读[协议上报覆写](override.md)。

## 命令速查

| 命令 | 用途 |
| --- | --- |
| `daemon status` / `start` / `stop` / `restart` | 后台状态及生命周期 |
| `profile list` | 查看内置机构 Profile |
| `config list` / `show ID` | 查询已保存配置的公开字段 |
| `config create` / `update ID` / `set-password ID` / `remove ID` | 配置和凭据管理 |
| `auth list` / `status ID` / `watch` | 观察认证 Session |
| `auth start --config ID` / `--session ID` | 从保存配置或已有会话启动认证 |
| `auth stop ID` / `restart ID` / `remove ID` | 控制、清理会话 |
| `network interfaces` / `diagnose ...` | 网络观察及诊断 |
| `diagnostics export --output PATH` | 导出有限的诊断摘要 |
| `integration ...` | Windows 当前用户自启、PATH 管理 |
| `help [命令路径]` | 查看帮助，不启动后台；也支持公开命令的 `--help`、`-h` |

## 内部桥接与兼容入口

下列命令存在于 CTL 中，但由同包 GUI 调用，不是通用脚本接口。这里列出边界，避免把它们误认成另一套公开认证命令：

| 内部命令 | 参数与职责 |
| --- | --- |
| `gui bootstrap --owner-pid PID [--log-level LEVEL]` | 正整数 PID 必填，绑定 GUI 进程身份，启动或确认 desktop 后台；日志等级省略则采用启动默认行为，显式值仅允许 `info`、`debug`、`trace` |
| `gui restart --owner-pid PID [--log-level LEVEL]` | 同一 GUI 所有者才能重启 desktop 后台；日志等级默认 `debug`，允许值同上 |
| `gui open-log-directory` | 无业务参数；Windows 打开当前实例已存在的日志目录，不创建目录、不启动后台 |
| 旧顶层 `status` | 已退役，只提示改用 `daemon status`，返回失败；不作为兼容查询使用 |

bootstrap/restart 成功向 stdout 输出带 `schemaVersion: 1` 的连接文档，包括 endpoint、**运行 token**、构建、PID、模式和日志等级；这些内容不得公开。bootstrap 系列错误通过 stderr 输出带 `schemaVersion` 与 `error.code/message` 的 JSON，退出码为：`2` 参数无效、`10` 客户端身份无效、`11` 平台不支持、`12` 模式冲突、`13` 构建不兼容、`14` 启动未确认、`15` 启动失败、`16` 输出失败。普通参数解析或打开日志目录失败仍可能走普通 CTL 错误路径。不要依赖隐藏命令帮助发现这些能力。

## 帮助与退出结果

具体参数可以查询本机版本：

```powershell
.\sidraviactl.exe help
.\sidraviactl.exe help config create
.\sidraviactl.exe help config update
.\sidraviactl.exe help auth start
.\sidraviactl.exe help network diagnose
```

公开命令成功退出码为 `0`，失败（包括用法错误）为 `1`。**通信超时或中断可能导致“结果未知”，不代表后台一定未执行操作。** 先查询配置和会话状态，再决定是否重试。脚本不应依赖普通查询的固定排版；只有明确声明为 JSON 的结果才适合按其结构消费。系统集成的 JSON 在 stdout，失败还可能在 stderr 给出安全错误文本；诊断导出的 JSON 在目标文件中，终端输出是完成提示。当前没有通用 `--json`、`--password`、`--version` 或 shell completion 命令。

返回：[项目首页](../README.md) · [使用指南](guide.md) · [协议上报覆写](override.md) · [数据与隐私](privacy.md)
