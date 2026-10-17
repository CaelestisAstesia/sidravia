# 命令行高级使用

`sidraviactl`（下文简称 **CTL**）提供一次性认证、保存配置、多配置管理、独立后台和网络诊断等功能。日常使用 GUI 连接网络，可直接阅读[快速开始](quick-start.md)和[使用指南](guide.md)。

本页示例基于 `v0.3.0-alpha.1`。预发布版本可能调整命令和参数，使用其他版本时，请同时核对对应发行说明及命令的 `--help`。

Windows 示例使用 PowerShell，假定当前目录为完整解压或安装后的程序目录，以 `.\sidraviactl.exe` 运行。已加入用户 PATH 时，可直接输入 `sidraviactl`。Linux 核心包使用 `./sidraviactl`，不支持 Windows 专用的自启和 PATH 集成功能。

`<configuration-id>`、`<session-id>` 和 `<账号>` 等表示需要替换的内容，实际输入时不保留尖括号。交互式输入密码时不会回显；不要把密码直接写进命令行参数。

[后台与命令副作用](#后台与命令副作用) · [认证参数](#认证启动参数) · [配置参数](#更新密码与删除参数) · [MGR 完整参考](manager.md) · [协议覆写](override.md)

## 与 GUI 配合使用

Sidravia 有两种互斥的认证后台模式：

| 模式 | 使用方 | 生命周期 |
| --- | --- | --- |
| `desktop` | GUI | 由 GUI 启动和管理；隐藏到托盘后继续运行，真正退出 GUI 后结束 |
| `headless` | CTL | 独立运行；单条 CTL 命令执行结束后，后台及认证可以继续运行 |

**GUI 和 CTL 不能同时控制同一认证后台。**

- **GUI → CTL**：从托盘菜单选择「退出 Sidravia」，再执行 `.\sidraviactl.exe daemon start`。
- **CTL → GUI**：执行 `.\sidraviactl.exe daemon stop`，再打开 GUI。

切换会断开当前连接，原 Session ID 也会失效。GUI 运行期间，CTL 仅允许通过 `daemon status` 跨模式查询后台状态，其他认证和配置操作应先切换模式。

GUI 目前只管理一份保存的连接配置。使用 CTL 保存多份配置后，如需返回 GUI，请先备份并确认要保留哪份，再决定是否删除其他配置；删除配置会同时删除其中保存的账号与密码。

CTL 保存的协议上报覆写也会影响 GUI 后续使用该配置建立的连接，但 GUI 不提供覆写编辑入口。修改或清除方式见[协议上报覆写](override.md)。

## 独立后台

启动并查询后台：

```powershell
.\sidraviactl.exe daemon start
.\sidraviactl.exe daemon status
```

`daemon status` 用于确认后台状态；命令执行成功不代表已经认证成功。已有保存配置开启自动登录时，启动后台可能自动发起认证。手动连接前，先查看当前会话：

```powershell
.\sidraviactl.exe auth list
```

停止后台会结束相关认证会话：

```powershell
.\sidraviactl.exe daemon stop
```

需要重启时执行：

```powershell
.\sidraviactl.exe daemon restart
```

重启会中断当前连接，使旧 Session ID 失效；开启自动登录的配置可能随后重新发起认证。单条 CTL 命令执行结束或关闭终端，不等于停止独立后台。

## 后台与命令副作用

| 命令 | 后台不存在时 | 主要行为与影响 |
| --- | --- | --- |
| `daemon status` | 查询，不启动 | 可跨模式查询；执行成功不表示后台在线或认证成功 |
| `daemon start [--log-level LEVEL]` | 启动独立后台 | 复用已有兼容后台；不会热更新其日志等级；可能触发配置的自动登录 |
| `daemon stop` | 已停止也可成功 | 停止独立后台及其认证会话 |
| `daemon restart [--log-level LEVEL]` | 启动独立后台 | 中断已有连接，旧 Session ID 失效；可能重新触发自动登录 |
| `profile list`、`auth list/status/watch`、`config list/show`、`network interfaces` | 报告无法连接，不启动 | 查询已有独立后台 |
| `network diagnose` | 报告无法连接，不启动 | 获取诊断结果；使用 `--probe` 时额外发送探测请求 |
| `diagnostics export` | 报告无法连接，不启动 | 导出诊断到新文件，不覆盖已有文件；不支持 `--probe` |
| `auth start/stop/restart/remove`、`config create/update/set-password/remove` | 可按需启动独立后台 | 改变会话或配置；操作失败时，后台启动或自动登录仍可能已经发生 |
| `integration ...` | 不依赖认证后台 | 调用同目录 MGR，读取或修改 Windows 当前用户系统集成 |
| `help`、公开命令的 `--help` / `-h` | 不启动 | 显示帮助 |

除 `daemon status` 外，表中需要连接认证后台的命令均要求 `headless` 模式，不会接管 GUI 使用的 `desktop` 后台。CTL 与后台必须来自同一构建的配套程序。

命令失败不一定意味着没有发生任何操作。遇到启动或执行结果未确认的提示时，请先查询后台、会话或配置状态，再决定是否重试。

## 一次性认证

确认独立后台已启动，并通过 `auth list` 检查是否已有自动登录或其他认证会话，再列出机构配置并发起认证：

```powershell
.\sidraviactl.exe profile list
.\sidraviactl.exe auth start --profile jlu --username '<账号>'
```

将 `<账号>` 替换为实际账号，不保留尖括号。程序随后交互式读取密码，输入不会回显。`jlu` 是吉林大学的机构 Profile ID，其他机构应使用 `profile list` 中实际提供的 ID。

本次认证不会创建持久保存的账号配置，但会在当前后台保留会话记录。示例未指定网卡与 IPv4，采用自动选择，通常建议保留此方式。

`auth start` 返回不代表认证已成功。使用返回的 Session ID 查询状态，或持续观察：

```powershell
.\sidraviactl.exe auth list
.\sidraviactl.exe auth status <session-id>
.\sidraviactl.exe auth watch
```

`auth watch` 显示当前状态并持续观察更新；按 Ctrl+C 退出观察不会断开认证。它不提供完整的历史事件记录，通信断开后需要重新运行。

按需管理已有会话：

```powershell
.\sidraviactl.exe auth stop <session-id>
.\sidraviactl.exe auth restart <session-id>
.\sidraviactl.exe auth remove <session-id>
```

- `stop`：停止认证，保留会话记录。
- `restart`：重新发起认证，可能中断当前连接。
- `remove`：停止认证并移除会话记录。

上述操作不会删除持久保存的账号配置；后台结束后，一次性会话的 Session ID 不再有效。

## 认证启动参数

`auth start` 支持三种互斥形式，每次只能选择一种：

```text
sidraviactl auth start --profile PROFILE --username USER [--password-stdin] [--interface-id ID --local-ipv4 IPv4] [--protocol-override-file PATH]
sidraviactl auth start --config CONFIGURATION_ID
sidraviactl auth start --session SESSION_ID
```

| 参数 | 作用与限制 |
| --- | --- |
| `--profile PROFILE`、`--username USER` | 使用指定机构和账号发起一次性认证，不创建保存配置 |
| `--password-stdin` | 从标准输入读取一行密码；仅用于一次性认证，省略时交互式读取 |
| `--interface-id ID`、`--local-ipv4 IPv4` | 一次性认证的实际网卡绑定，必须成对提供；省略时自动选择 |
| `--protocol-override-file PATH` | 为一次性会话提供协议上报覆写，不保存为配置；格式见[覆写参考](override.md) |
| `--config CONFIGURATION_ID` | 使用保存的账号、绑定和覆写，启动或确保对应会话运行 |
| `--session SESSION_ID` | 确保当前后台保留的会话运行；不会强制重启正在认证、已连接、等待或错误状态的会话 |

`--config` 与 `--session` 不能混用，也不能附加一次性认证的账号、密码、网卡或覆写参数。需要修改保存配置时，应使用 `config` 命令。

一次性认证不提供 `--auto-login`、`--auto-reconnect` 或 `--automatic-binding` 参数。通常省略网卡绑定参数，使用自动选择；手动绑定的限制见[指定网卡](#指定网卡)。

参数名后的 `PROFILE`、`USER`、`CONFIGURATION_ID`、`SESSION_ID` 等都是待替换值，使用空格与参数名分隔。

`auth start --session ID` 可恢复已停止且仍被后台保留的会话；需要明确重新尝试失败或等待中的认证时，使用 `auth restart ID`。命令返回成功不等于认证完成，仍需通过 `auth status ID` 或 `auth watch` 确认结果。

## 保存配置

交互式创建配置，按提示填写机构、账号、密码和连接偏好：

```powershell
.\sidraviactl.exe config create
```

查看已保存配置：

```powershell
.\sidraviactl.exe config list
.\sidraviactl.exe config show <configuration-id>
```

`config list` 和 `config show` 显示公开配置信息，不回显密码。需要认证时，使用配置 ID 启动：

```powershell
.\sidraviactl.exe auth start --config <configuration-id>
```

创建配置本身不会立即发起该配置的认证。不过，配置命令可能按需启动后台，已有配置的自动登录仍可能发生，详见[后台与命令副作用](#后台与命令副作用)。

- **Configuration ID**：持久保存的配置标识，程序重启后仍存在，不能通过 `config update` 修改。
- **Session ID**：当前后台中的认证会话标识，后台结束后失效，不能替代 Configuration ID。
- CTL 可保存多份配置，但同一后台同时最多只有一个活动认证会话，且最多只有一份配置开启自动登录。

按需修改自动连接偏好或密码：

```powershell
.\sidraviactl.exe config update <configuration-id> --auto-login true
.\sidraviactl.exe config update <configuration-id> --auto-reconnect true
.\sidraviactl.exe config set-password <configuration-id>
```

`config update` 的 `--auto-login` 和 `--auto-reconnect` 必须明确提供 `true` 或 `false`。开启自动登录不会立即认证；自动重连修改在下次连接时生效。

**修改认证输入可能结束旧会话。** 账号、机构、网卡绑定或协议覆写实际变化后，旧会话会退役，需要重新连接；调用 `set-password` 即使输入相同密码，也会清理旧会话。仅修改显示名称或自动连接偏好，不会立即替换当前会话。

配置含明文密码，保存与备份前请了解[数据与隐私](privacy.md#账号与密码)。

## 创建参数、脚本与多配置

以下示例创建 ID 为 `daily` 的配置，指定机构、账号、显示名称和自动连接偏好；密码仍通过隐藏提示输入。`daily` 是示例配置 ID，后续命令应替换为实际 ID。

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
| `--auto-login[=true\|false]` | 非交互创建省略时为 `false`；交互创建未指定时会询问 |
| `--auto-reconnect[=true\|false]` | 非交互创建省略时为 `true`；交互创建未指定时会询问 |
| `--interface-id ID` 与 `--local-ipv4 IPv4` | 必须成对使用，指定实际网络绑定；未提供时可自动选择 |
| `--protocol-override-file PATH` | 保存[协议上报覆写](override.md) |
| `--allow-insecure-storage` | 明确允许在存储保护不足时继续；**不会加密或修复文件权限** |

**交互创建**：未提供的配置 ID、机构、账号和自动连接偏好会通过提示补充；未提供绑定参数时，会询问网卡选择，通常建议选择自动绑定。创建命令不提供 `--automatic-binding` 或 `--clear-protocol-override`。

**脚本创建**：完全非交互调用必须提供 `--id`、`--profile`、`--username` 和 `--password-stdin`。省略绑定参数时自动选择网卡；建议显式指定自动登录和自动重连偏好，让脚本行为清楚可见。

自动化程序应通过标准输入传入一行 UTF-8 密码，不要将密码直接写入命令行参数、终端历史或公开脚本。Windows PowerShell 5.1 的默认管道编码可能改变非 ASCII 字符，使用时应确认传入编码。

**输入限制**：标准输入密码上限为 4096 字节，末尾的 LF 或 CRLF 会被移除。这是输入通道限制，不代表协议支持任意同等长度的密码。D520 账号按严格 GBK 编码后最多 36 字节；账号、密码及主机文本不接受 NUL 或 GBK 无法表示的字符。字节数不等于字符数。

## 更新密码与删除参数

常用更新示例：

```powershell
.\sidraviactl.exe config update daily --name '宿舍连接' --username '<新账号>'
.\sidraviactl.exe config update daily --auto-login true --auto-reconnect false
.\sidraviactl.exe config set-password daily
```

完整语法：

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

**省略字段**：未指定的字段保持原值，密码通过 `set-password` 单独修改，配置 ID 不可更改。单独运行 `config update ID` 只会进入显示名称修改，不是所有字段的逐项向导；空输入表示保留，`-` 表示清除，没有实际更新项时会报错。脚本更新必须明确提供至少一个更新项。

**修改密码**：`config set-password ID` 交互式读取新密码；非交互调用必须使用 `--password-stdin`。改密码会清理关联旧会话，即使输入与原来相同的密码，也需要按需重新连接。

**删除配置**：`config remove ID` 会要求确认，删除持久配置及其中的账号密码，并结束关联会话。脚本使用 `--yes` 明确确认删除；它与只移除运行会话的 `auth remove ID` 不同。

```powershell
.\sidraviactl.exe config remove daily
```

**存储保护与操作结果**：创建、更新、改密码和删除均支持 `--allow-insecure-storage`，该选项不会加密密码或修复权限，删除确认的 `--yes` 也不能替代存储保护同意。不要遇到错误就自动追加这些选项。

输入校验失败不会替换配置，但保存后的会话清理失败、通信中断或输出失败，可能发生在修改已生效之后。遇到结果未确认时，先用 `config show ID` 和 `auth list` 核对，再决定是否重试；不要仅凭非零退出码判断「未保存」。

## 指定网卡

**通常建议保留自动选择网卡。** 网络自持机制依赖自动选择来处理接口变化；仅在自动选择未选中认证网络，或明确需要固定接口时，使用手动绑定。

在已有独立后台中，查询当前观察到的网卡与 IPv4：

```powershell
.\sidraviactl.exe network interfaces
```

从输出中选择同一接口下当前可绑定的接口 ID 和 IPv4；也可以参考输出提供的 PowerShell 绑定参数。列表是已观察到的状态，实际使用时仍会重新检查绑定是否可用。

```powershell
.\sidraviactl.exe config update <configuration-id> --interface-id '<接口ID>' --local-ipv4 '<IPv4>'
```

绑定实际发生变化后，关联旧会话会退役，需要按需重新连接。恢复自动选择：

```powershell
.\sidraviactl.exe config update <configuration-id> --automatic-binding
```

一次性认证也支持成对提供 `--interface-id` 与 `--local-ipv4`；省略时自动选择。

**手动绑定不可用时，程序会等待其恢复，不会自动切换到其他网卡或地址。** 更换网络或 IPv4 后，请更新绑定或恢复自动选择。

协议上报 IPv4 覆写只改变认证报文中的字段，不改变实际网卡或 socket 源地址。

## 诊断与日志

以下查询和导出要求已有独立后台，不会自动启动后台。

**网络诊断**：选择一份保存配置或当前后台保留的会话，检查认证目标及本机路由：

```powershell
.\sidraviactl.exe network diagnose --config <configuration-id>
.\sidraviactl.exe network diagnose --session <session-id>
```

`--config` 与 `--session` 必须且只能提供一个。需要额外发送有限 IP 探测请求时，加上 `--probe`：

```powershell
.\sidraviactl.exe network diagnose --config <configuration-id> --probe
```

诊断命令成功或探测收到回复，都不代表认证及互联网访问已经成功，仍需结合会话状态和实际网页访问判断。

**诊断导出**：将诊断摘要写入新文件：

```powershell
.\sidraviactl.exe diagnostics export --output .\sidravia-diagnostics.json
```

`--output PATH` 必填，父目录必须已存在，目标文件必须尚不存在。导出不覆盖已有文件，也不支持 `--probe`。

导出摘要不含密码或原始认证报文，分享前仍应检查网络和环境信息。写入失败可能留下不完整文件；完成提示输出失败时，文件也可能已经完整写入。请先检查实际文件，重新导出时使用尚不存在的路径。

**日志等级**：独立后台支持 `info`、`debug`、`trace`，默认使用 `debug`。启动时可指定：

```powershell
.\sidraviactl.exe daemon start --log-level debug
```

已有兼容后台时，`daemon start` 会复用它，不会热更新日志等级。需要更改运行中后台的等级时，显式重启：

```powershell
.\sidraviactl.exe daemon restart --log-level info
```

重启会中断当前连接，使旧 Session ID 失效。`trace` 可能记录完整认证报文，仅在排障需要时开启；结束后恢复原日志等级并重启后台。降低等级不会删除已有 TRACE 日志，分享前请阅读[数据与隐私](privacy.md#日志与诊断)。

## Windows 自启与 PATH

Windows 包通过同目录的 `sidraviamgr.exe`（MGR）维护当前 Windows 用户的系统集成。CTL 的 `integration` 命令转交 MGR 执行，不要求认证后台已启动。请使用完整程序包中的配套工具。

**查询状态：**

```powershell
.\sidraviactl.exe integration status
```

**用户 PATH**：按需选择启用或撤销，不必依次执行：

```powershell
.\sidraviactl.exe integration path enable
.\sidraviactl.exe integration path disable
```

启用后，新打开的终端可以直接使用命令；已有终端不会自动更新环境，请重新打开。

**开机自启**：按需选择一种模式，或关闭自启：

```powershell
.\sidraviactl.exe integration autostart set gui
.\sidraviactl.exe integration autostart set daemon
.\sidraviactl.exe integration autostart disable
```

- `set gui`：下次登录 Windows 时启动到托盘，需要同目录包含 GUI 组件。
- `set daemon`：下次登录 Windows 时仅启动独立后台。
- `disable`：关闭自启，不代表立即停止当前程序。

两种自启模式互斥，设置不会立即启动程序；需要自动认证时，还应在保存配置中开启自动登录。

系统集成操作返回 JSON，失败时退出码非零。发生超时或结果未确认时，先重新查询状态。遇到自启任务冲突，请核实任务归属，不要覆盖或删除未知任务。

Linux 不支持这些 Windows 专用命令。完整参数、返回字段、错误码及 MGR 独有的卸载和数据清理命令，见 [Windows 维护工具](manager.md)。

## 协议上报覆写

D520 支持通过 `--protocol-override-file PATH` 调整实现明确允许的认证报文上报字段及兼容参数。通常连接网络无需修改这些设置；GUI 目前没有覆写编辑入口。

- **一次性认证**：在 `auth start --profile ... --username ...` 中传入覆写文件，仅用于该次会话，不保存为配置。
- **保存配置**：在 `config create` 或 `config update` 中传入覆写文件，后续使用该配置认证时继续生效，包括通过 GUI 建立的连接。
- **修改与清除**：`config update <configuration-id> --protocol-override-file PATH` 整体替换原覆写；清除时使用 `config update <configuration-id> --clear-protocol-override`，不会与文件内容逐字段合并。

保存配置中的覆写实际变化后，关联旧会话会退役，需要按需重新连接。

覆写不改变真实网卡、IP、MAC、DNS、路由、socket 源地址、认证服务器或认证策略，也不意味着已支持其他学校或协议变体。文件格式、字段限制和完整操作示例见[协议上报覆写](override.md)。

## 命令速查

下表按命令组列出常用入口。`ID`、`PROFILE`、`USER`、`PATH` 均需替换为实际值；同一行用 `/` 分隔的项目表示不同命令，不应原样输入。

| 命令 | 用途 |
| --- | --- |
| `daemon status` / `start` / `stop` / `restart` | 后台状态及生命周期 |
| `profile list` | 查看内置机构 Profile |
| `config list` / `show ID` | 查询已保存配置的公开字段 |
| `config create` / `update ID` / `set-password ID` / `remove ID` | 配置和凭据管理 |
| `auth list` / `status ID` / `watch` | 查询或持续观察认证会话 |
| `auth start --profile PROFILE --username USER` | 交互式读取密码，发起不保存账号配置的一次性认证 |
| `auth start --config ID` / `--session ID` | 从保存配置或已有会话启动认证 |
| `auth stop ID` / `restart ID` / `remove ID` | 控制、清理会话 |
| `network interfaces` / `diagnose ...` | 网络观察及诊断 |
| `diagnostics export --output PATH` | 将诊断摘要写入新文件 |
| `integration ...` | Windows 当前用户自启、PATH 管理 |
| `help [命令路径]` | 查看帮助，不启动后台；也支持公开命令的 `--help`、`-h` |

## 内部桥接与兼容入口

以下入口用于同包 GUI 与核心之间的内部协作，供开发和排障参考，不作为通用脚本接口。日常认证和自动化请使用前文公开命令；预发布版本可能调整内部契约。

| 内部命令 | 参数与职责 |
| --- | --- |
| `gui bootstrap --owner-pid PID [--log-level LEVEL]` | 正整数 PID 必填，绑定 GUI 进程身份，启动或确认 desktop 后台；日志等级省略则采用启动默认行为，显式值仅允许 `info`、`debug`、`trace` |
| `gui restart --owner-pid PID [--log-level LEVEL]` | 同一 GUI 所有者才能重启 desktop 后台；日志等级默认 `debug`，允许值同上 |
| `gui open-log-directory` | 无业务参数；Windows 打开当前实例已存在的日志目录，不创建目录、不启动后台 |
| 旧顶层 `status` | 已退役，仅提示改用 `daemon status` 并返回失败；旧脚本应更新命令 |

`gui bootstrap` 和 `gui restart` 成功时向 stdout 输出 `schemaVersion: 1` 的 JSON 连接文档，包含连接端点、运行 token、产品版本、构建标识、后台 PID、模式和日志等级。**运行 token 属于敏感信息，不要公开输出文档或将其附在问题反馈中。**

进入内部错误契约的失败会向 stderr 输出 JSON，包含 `schemaVersion` 和 `error.code/message`。对应退出码为：`2` 参数无效、`10` 客户端身份无效、`11` 平台不支持、`12` 模式冲突、`13` 构建不兼容、`14` 启动未确认、`15` 启动失败、`16` 输出失败。

并非所有失败都使用该 JSON 契约。命令行解析错误、打开日志目录失败等情况仍可能走普通 CTL 错误路径，不能仅凭调用了内部命令就假定 stderr 一定是 JSON。这些命令在帮助中隐藏，不应依赖帮助枚举发现它们。

## 帮助与退出结果

具体参数可以查询本机版本：

```powershell
.\sidraviactl.exe help
.\sidraviactl.exe help config create
.\sidraviactl.exe help config update
.\sidraviactl.exe help auth start
.\sidraviactl.exe help network diagnose
.\sidraviactl.exe help integration
```

**退出码**：公开命令成功返回 `0`，失败（包括用法错误）返回 `1`。成功仅表示该命令完成，不保证认证成功、网络可用或后台处于某个特定状态；应结合返回内容判断。内部桥接命令的特殊退出码见上一节。

**结果未知**：通信超时或中断不代表后台未执行操作。修改配置、控制会话或调整系统集成后遇到结果未确认，应先查询实际状态，再决定是否重试。

**脚本读取**：普通查询输出用于终端阅读，不保证固定排版。仅对明确声明为 JSON 的结果按结构读取：系统集成 JSON 输出到 stdout，失败时还可能向 stderr 输出错误文本；诊断导出 JSON 写入目标文件，终端只显示完成提示。

**参数边界**：当前版本没有通用 `--json`、`--password`、`--version` 或 shell completion 命令。密码通过隐藏提示或受支持的 `--password-stdin` 传入，具体参数以本机版本帮助为准。

返回：[项目首页](../README.md) · [使用指南](guide.md) · [协议上报覆写](override.md) · [数据与隐私](privacy.md)
