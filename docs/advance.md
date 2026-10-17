# 高级使用

命令行工具为 `sidraviactl`，下文简称 ctl。示例在 Windows PowerShell 的程序目录运行；已加入 PATH 时可省略 `.\` 和 `.exe`。Linux 使用 `./sidraviactl`。

`<configuration-id>`、`<session-id>` 等需替换为实际值，不保留尖括号。密码通过隐藏提示或 `--password-stdin` 输入。

[配置与脚本](#创建参数脚本与多配置) · [命令速查](#命令速查) · [网卡](#指定网卡) · [诊断](#诊断与日志) · [自启与 PATH](#windows-自启与-path) · [协议覆写](override.md)

## 与 GUI 配合使用

GUI 与 ctl 使用相同数据目录中的配置，但不能同时运行认证后台：

- GUI 使用 desktop 后台，随 GUI 退出。隐藏到托盘时继续运行。
- ctl 使用独立的 headless 后台，命令结束后仍可保持连接。

从 GUI 切到 ctl，先在托盘选择「退出 Sidravia」；切回 GUI 前执行 `sidraviactl daemon stop`。切换会断开连接，原 Session ID 随后台结束失效。`daemon status` 可以跨模式查询，其他认证和配置命令不能控制已打开的 GUI。

GUI 目前只管理一份配置。用 ctl 创建多份后，需先核对、保留所需的一份，再回到 GUI；删除配置也会删除其中的账号与密码。

ctl 保存的覆写也会用于后续 GUI 连接。GUI 没有覆写编辑入口，清除方法见[协议上报覆写](override.md)。安装版与便携版的数据目录不同，不自动共享配置。

## 独立后台

启动并查看后台：

```powershell
.\sidraviactl.exe daemon start
.\sidraviactl.exe daemon status
```

结束使用时运行 `sidraviactl daemon stop`。停止后台会结束会话；只想断开认证时使用下文的 `auth stop`。

如果已有配置开启自动登录，后台启动后可能已发起连接。先用 `auth list` 确认当前会话，再决定是否发起其他认证。

## 一次性认证

直接创建会话，不保存账号配置。希望下次还能使用这些设置，见[保存配置](#保存配置)。

```powershell
.\sidraviactl.exe daemon start
.\sidraviactl.exe profile list
.\sidraviactl.exe auth start --profile jlu --username '<账号>'
.\sidraviactl.exe auth list
```

`auth start` 返回后认证可能还在进行。用返回的 Session ID 查询，或持续观察：

```powershell
.\sidraviactl.exe auth status <session-id>
.\sidraviactl.exe auth watch
```

`auth watch` 显示最新状态，按 Ctrl+C 停止观察，连接仍然保留。它不记录完整状态历史。

## 保存配置

交互式创建配置，并用实际配置 ID 连接：

```powershell
.\sidraviactl.exe config create
.\sidraviactl.exe config list
.\sidraviactl.exe config show <configuration-id>
.\sidraviactl.exe auth start --config <configuration-id>
```

创建配置后需另行连接。自动登录和重连的生效时机见[使用指南](guide.md#自动连接与开机自启)。

```powershell
.\sidraviactl.exe config update <configuration-id> --auto-login true
.\sidraviactl.exe config update <configuration-id> --auto-reconnect true
.\sidraviactl.exe config set-password <configuration-id>
```

`Configuration ID` 标识保存的配置，重启后保留；`Session ID` 标识当前后台的运行记录，重启后失效。

## 创建参数、脚本与多配置

配置 ID 不可修改，显示名称可以修改。可以保存多份配置，但同时最多一个活动会话，且只有一份配置可开启自动登录。

带部分参数交互创建，密码仍由隐藏提示读取，未指定的网卡由交互流程选择：

```powershell
.\sidraviactl.exe config create --id daily --profile jlu --username '<账号>' --name '日常连接' --auto-login=false --auto-reconnect=true
```

| 创建选项 | 作用与默认值 |
| --- | --- |
| `--id ID` | 指定配置 ID；非交互创建必填 |
| `--profile ID` | 指定机构 Profile；非交互创建必填 |
| `--username USER` | 指定账号；非交互创建必填 |
| `--name NAME` | 可选显示名称，可以省略或留空 |
| `--password-stdin` | 从标准输入读取密码；非交互创建必填 |
| `--auto-login[=true\|false]` | 默认 false；交互创建未指定时会询问 |
| `--auto-reconnect[=true\|false]` | 默认 true；交互创建未指定时会询问 |
| `--interface-id ID` 与 `--local-ipv4 IPv4` | 必须成对使用；非交互创建未指定时自动选择 |
| `--protocol-override-file PATH` | 保存协议覆写对象，格式见[专项说明](override.md) |
| `--allow-insecure-storage` | 明确允许在未受保护的存储环境中保存敏感配置 |

完全非交互创建时，必须显式提供 `--id`、`--profile`、`--username` 和 `--password-stdin`。自动化调用方应直接启动 ctl，并向进程的标准输入写入一行 UTF-8 密码。若经由 shell 管道提供输入，还需确认管道编码不会改写非 ASCII 字符，尤其不要依赖 Windows PowerShell 5.1 的默认输出编码。

密码读取上限为 4096 字节，结尾的 LF 或 CRLF 会被移除，不读取后续行。D520 文本还需满足下面的编码限制。不要通过 `echo 密码`、命令行字面量或提交到仓库的密码文件提供凭据。

D520 账号按 GBK 编码后最多 36 字节；账号、密码和主机文本不接受 NUL 或 GBK 无法表示的字符。字节数不等于字符数，不能把输入读取上限当作协议字段长度。

更新公开字段：

```powershell
.\sidraviactl.exe config update daily --name '宿舍连接' --username '<新账号>'
.\sidraviactl.exe config update daily --auto-login true --auto-reconnect false
.\sidraviactl.exe config set-password daily
```

注意：`config update` 的两个自动连接选项要求显式传入 `true` 或 `false`；创建命令则使用布尔开关语法。`update` 还支持 `--profile`、网卡绑定、覆写替换和清除；不支持改配置 ID。密码通过 `set-password` 单独更新，也可加 `--password-stdin`。

账号、Profile、绑定或覆写等认证输入实际改变后，旧会话会被清理，需重新连接；更新密码也会清理旧会话，即使提交的是相同密码。单独修改显示名称或自动连接偏好，不会因此立即替换当前会话。自动重连偏好下次连接生效。

删除配置需要确认。脚本中可使用 `config remove ID --yes` 跳过询问；该操作会停止关联会话并删除保存的账号与密码。`auth remove ID` 只移除会话，不删除持久配置。

`--allow-insecure-storage` 允许在存储保护缺失时创建、更新、改密码或删除配置，不会修复权限。使用前请阅读[数据与隐私](privacy.md)。

## 命令速查

| 命令 | 作用 |
| --- | --- |
| `daemon status` | 只读查看后台状态 |
| `daemon start` / `stop` / `restart` | 启动、停止或重启独立后台 |
| `profile list` | 查看已加载的机构配置 |
| `config list` / `show ID` | 列出或查看保存的配置公开信息 |
| `config create` / `update ID` / `set-password ID` | 创建配置、更新设置或密码 |
| `config remove ID` | 停止相关会话并删除配置，要求确认 |
| `auth list` / `status ID` / `watch` | 查看当前后台保留的会话及状态 |
| `auth start --config ID` | 从配置开始连接，或确保相关会话运行 |
| `auth start --session ID` | 确保已有会话运行 |
| `auth stop ID` / `restart ID` | 停止或重新启动会话 |
| `auth remove ID` | 停止并移除会话记录，不删除保存的配置 |
| `network interfaces` | 查看后台已观察到的网卡与 IPv4 |
| `network diagnose --config ID` | 诊断配置对应的受信认证目标 |
| `network diagnose --session ID` | 诊断会话对应的受信认证目标 |
| `diagnostics export --output PATH` | 将诊断摘要写入一个尚不存在的文件 |

只读查询和诊断需要已有 headless 后台；认证和配置变更命令可按需启动它。

## 指定网卡

先查询当前网卡：

```powershell
.\sidraviactl.exe network interfaces
```

使用实际接口 ID 与该接口的 IPv4，同时指定两个参数：

```powershell
.\sidraviactl.exe config update <configuration-id> --interface-id '<接口ID>' --local-ipv4 '<IPv4>'
```

恢复自动选择：

```powershell
.\sidraviactl.exe config update <configuration-id> --automatic-binding
```

一次性 `auth start --profile ... --username ...` 也支持同时指定 `--interface-id` 与 `--local-ipv4`。显式绑定不可用时等待该绑定，不回退到其他网卡。

## 诊断与日志

```powershell
.\sidraviactl.exe network diagnose --config <configuration-id>
.\sidraviactl.exe network diagnose --config <configuration-id> --probe
.\sidraviactl.exe diagnostics export --output .\sidravia-diagnostics.json
```

`--probe` 额外发起有限 IP 探测，用于检查网络路径。认证和网页访问结果需另行确认。

后台默认日志级别为 `debug`，可在启动或重启时指定：

```powershell
.\sidraviactl.exe daemon restart --log-level info
```

支持 `info`、`debug`、`trace`；重启会结束原会话。TRACE 含原始认证数据报，分享限制见[数据与隐私](privacy.md)。

## Windows 自启与 PATH

现代 Windows 包通过同目录的 `sidraviamgr.exe` 执行维护操作，不依赖旧安装脚本：

```powershell
.\sidraviactl.exe integration status
.\sidraviactl.exe integration path enable
.\sidraviactl.exe integration path disable
.\sidraviactl.exe integration autostart set gui
.\sidraviactl.exe integration autostart set daemon
.\sidraviactl.exe integration autostart disable
```

按需执行，`set gui` 与 `set daemon` 二选一，下次登录时生效。修改 PATH 后打开新终端。Linux 不支持本节命令。结果以 JSON 返回，失败时退出码非零。

`integration --help` 暂不列出子命令，具体用法见上面的示例。

## 协议上报覆写

`auth start`、`config create` 和 `config update` 支持 `--protocol-override-file PATH`。字段和清除方法见[协议上报覆写](override.md)。

## 帮助与退出结果

```powershell
.\sidraviactl.exe help
.\sidraviactl.exe help config create
.\sidraviactl.exe help config update
.\sidraviactl.exe help auth start
.\sidraviactl.exe help network diagnose
```

普通命令成功返回 0，失败返回非零；帮助命令不发起认证。遇到超时或通信中断时，先查询配置和会话确认结果，不要假定所有失败都表示操作未生效。

诊断导出和系统集成结果为 JSON；普通查询的文本格式可能变化，不适合脚本解析。
