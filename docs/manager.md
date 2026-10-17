# Windows 维护工具（MGR）

`sidraviamgr.exe`（下文简称 MGR）负责 Windows 当前用户的 PATH、登录自启及安装卸载维护。它每次执行一项操作后退出，不拥有认证会话。日常认证使用 [CTL](advance.md)；普通卸载通过 Windows 应用列表启动安装器的卸载程序。

本文对应 `v0.3.0-alpha.1` 的全部九项 MGR 操作。MGR 仅实现 Windows 能力，Linux Core 包不提供它；在非 Windows 编译运行时，实际操作返回 `unsupported`。帮助查看不依赖 Windows 系统集成。

## 调用范围与参数规则

在完整程序目录打开 PowerShell，运行 `./` 的 Windows 写法 `.\sidraviamgr.exe`。操作目标由 **MGR 自身所在目录**确定，不由终端当前目录决定；没有 `--directory`、`--all-users`、`--force` 或自定义账号参数。应使用目标安装或便携目录中的配套 MGR，不要把它单独复制到其他目录执行维护。

PATH 与计划任务作用于当前 Windows 用户，不需要为正常操作提升权限。MGR 使用当前用户的串行锁协调维护；无法取得锁、无法确认运行进程或系统状态时会返回失败。它不负责启动、强制终止或接管认证后台。系统集成也不等于保存配置中的自动登录开关。

## 完整命令表

表内操作均不接受额外选项或位置参数。没有参数、`help`、`-h`、`--help` 显示纯文本帮助并成功退出；不支持通用的 `命令 --help` 形式。

| 命令 | 作用 | 写入或限制 |
| --- | --- | --- |
| `status` | 查询本目录的用户 PATH、自启状态和 GUI 文件可用性 | 不修改 PATH、计划任务、认证配置或会话 |
| `path enable` | 将 MGR 所在目录加入当前用户 PATH | 已存在时不重复添加，写后重新核对 |
| `path disable` | 从当前用户 PATH 移除匹配本目录的条目 | 保留其他目录，不卸载程序 |
| `autostart set gui` | 下次登录 Windows 时启动本目录 GUI 到托盘 | 需要同目录 `Sidravia.exe`；不立即启动 GUI |
| `autostart set daemon` | 下次登录时启动本目录 headless 后台 | 需要同目录 `sidraviactl.exe`；不立即认证 |
| `autostart disable` | 删除已确认归属于本目录和当前用户的自启任务 | 不停止当前程序；不存在任务也可成功 |
| `uninstall-check` | 检查本目录产品进程及自启任务冲突 | 不移除系统集成或数据；当前帮助文本漏列此已实现命令 |
| `uninstall-prepare` | 为卸载移除自启，再移除本目录用户 PATH | 要求本目录产品进程退出；不删除程序或账号数据 |
| `data delete` | 删除当前用户正式布局中的指定产品数据 | **无二次确认，不可自动恢复**；仅支持安装布局，详见下文 |

MGR 没有安装、升级、修复或删除程序二进制的命令，这些工作由安装器承担。CTL 的 `integration` 仅转发上表前六项系统集成操作，不提供后三项卸载与数据操作。

## 查询、PATH 与自启

按需要分别执行，不要把启用和禁用示例当作一段批处理：

```powershell
.\sidraviamgr.exe status
.\sidraviamgr.exe path enable
.\sidraviamgr.exe path disable
```

PATH 写入保留其他条目和原有字符串类型，目录匹配忽略大小写和末尾路径分隔符；不承诺识别环境变量展开、符号链接或短路径等所有等价拼法。写后会校验并广播环境变更，现有终端不会自动刷新其环境，请打开新终端。

自启模式二选一：

```powershell
.\sidraviamgr.exe autostart set gui
```

或：

```powershell
.\sidraviamgr.exe autostart set daemon
```

关闭自启：

```powershell
.\sidraviamgr.exe autostart disable
```

MGR 管理计划任务根目录中的 `SidraviaDaemon`，按当前用户登录触发、使用普通用户权限。GUI 模式执行 `Sidravia.exe --start-hidden`；后台模式执行 `sidraviactl.exe daemon start --log-level debug`，没有自定义自启日志等级参数。只有保存配置启用了自动登录，后台启动后才会按该配置自动认证。

已有同名任务若无法确认属于当前用户、本目录及预期动作，会显示 `conflict`。查询 `status` 可以成功返回这一冲突状态；设置、禁用和卸载检查会拒绝冲突。不要靠覆盖或删除未知任务来消除提示。移动便携目录前，应从旧目录正常撤销自启和 PATH，移动后再从新目录设置。

## 卸载检查与准备

先正常退出本目录的 GUI、daemon 及正在运行的 CTL，再按需调用：

```powershell
.\sidraviamgr.exe uninstall-check
```

检查成功只说明此刻未检测到阻止条件，不预留卸载资格，也不能证明完整安装卸载已经验收。准备卸载时：

```powershell
.\sidraviamgr.exe uninstall-prepare
```

准备步骤依次移除自启和本目录用户 PATH；之后仍需运行安装器的卸载程序删除程序文件。准备失败可能已移除其中一项，并非事务回滚；先用 `status` 核对实际状态。本目录的 `Sidravia.exe`、`sidraviad.exe` 或 `sidraviactl.exe` 仍在运行时，检查和准备会拒绝操作；进程身份无法查询时也会失败，而不是猜测安全。

## 显式删除正式数据

**`data delete` 会直接删除文件，没有交互确认，也不接受 `--yes`。** 仅在确实要删除本机已保存账号与设置时使用；先备份需要保留的数据，再正常退出当前用户的全部 Sidravia GUI、daemon 和 CTL 实例，包括其他安装目录和测试实例。

以下命令是单独的破坏性维护操作，不是日常卸载的必做步骤：

```powershell
.\sidraviamgr.exe data delete
```

它只支持安装布局，删除当前用户下列正式数据：

| 位置 | 删除内容 |
| --- | --- |
| `%APPDATA%\Sidravia\` | `configurations.json`、`gui-settings.json` |
| `%LOCALAPPDATA%\Sidravia\` | `runtime.json` |
| `%LOCALAPPDATA%\Sidravia\logs\` | `sidraviad.log`、`sidraviad.log.1`、符合 `sidraviad-YYYY-MM-DD-HHMMSS.log` 命名规则的文件 |

这会移除保存的配置、账号密码和 GUI 偏好。它不递归删除目录，不清理命名空间数据、无关文件、公告 WebView2 缓存、程序文件或用户另存的覆写/诊断文件；也不负责撤销 PATH 和自启。文件不存在时可跳过。

便携目录包含 `sidravia.portable` 时拒绝此操作，返回 `portable_data_removal_unsupported`，不会借此清理便携配置。路径或文件保护检查失败时也会拒绝；**不要通过删除便携标记绕过限制**，那会改为定位正式用户数据。

删除中途发生错误可能已有部分文件被删除，不保证回滚。退出码非零时，应保留现场并核对文件，不能断言数据完整保留。数据路径的详细含义见[数据与隐私](privacy.md)。

## JSON 输出与退出码

除帮助外，每次操作向 stdout 输出一个 UTF-8 JSON 对象，当前版本为 `1`。不输出原始内部错误原因。退出码：`0` 操作成功、`1` 操作或输出失败、`2` 参数无效。没有 JSON 或 JSON 不完整时应视为结果无法确认，不把它当作成功。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `version` | 整数 | 结果结构版本，当前 `1` |
| `ok` | 布尔值 | 本次操作是否确认成功 |
| `code` | 字符串 | 稳定结果码，成功通常为 `ok` |
| `capabilities` | 字符串数组 | 当前平台公布的能力，如 `user_path`、`user_autostart`、`production_data_removal` |
| `directory` | 字符串，可省略 | 本次维护目录 |
| `pathEnabled` | 布尔值 | 本次 PATH 查询或操作观察的结果 |
| `changed` | 布尔值 | 成功时本次操作是否发生修改；失败时不能据此判定未修改 |
| `warning` | 字符串，可省略 | 已成功操作的附加提醒，如环境广播失败 |
| `autostart` | 字符串，可省略 | `disabled`、`gui`、`daemon`、`conflict` |
| `guiAvailable` | 布尔值 | 同目录存在常规 GUI 文件；不证明 GUI 可启动或运行验收通过 |

每个结果只反映该操作实际检查的内容，并非完整状态快照。例如自启操作中的 `pathEnabled: false` 不代表 PATH 已禁用。需要综合状态时另调用 `status`。`capabilities` 也不表示每项操作在当前布局和进程状态下都可成功。

PowerShell 脚本应先保存退出码，再解析 JSON：

```powershell
$managerOutput = & .\sidraviamgr.exe status
$managerExit = $LASTEXITCODE
$managerResult = $managerOutput | ConvertFrom-Json
if ($managerExit -ne 0 -or $managerResult.version -ne 1 -or -not $managerResult.ok) {
    throw '无法确认 Sidravia 系统集成状态'
}
# 另行检查 autostart 是否为 conflict；ok 本身不排除任务冲突。
$managerResult
```

CTL `integration ...` 也输出相同的公开结果字段，但其失败退出码统一为 `1`，包括 CTL 参数错误；不要套用 MGR 的参数错误退出码 `2`。

## 结果码与处理

| 结果码 | 含义和处理 |
| --- | --- |
| `ok` | 本次操作已确认成功；仍需检查 `warning` 和具体状态 |
| `invalid_arguments` | 命令、参数数量或参数组合无效；MGR 没有通用附加选项 |
| `executable_path_failed` | 无法确定 MGR 自身的绝对路径 |
| `unsupported` | 当前平台未实现系统集成 |
| `integration_process_boundary_failed` | 无法建立维护进程及其子进程的生命周期边界 |
| `integration_lock_failed` | 无法取得当前用户的维护锁，或等待超时 |
| `invalid_directory` | 目录无法安全表示为 PATH 条目 |
| `path_read_failed` | 无法读取用户 PATH |
| `path_write_failed` | PATH 写入失败，可能已部分改变；重新查询 |
| `path_verify_failed` | 写后校验失败或遇到并发变化；核对现值，不覆盖其他程序的后续修改 |
| `scheduled_task_query_failed` | 无法查询自启计划任务 |
| `scheduled_task_conflict` | 同名任务归属、动作或用户等不匹配；保留未知任务并核实 |
| `scheduled_task_verify_failed` | 自启操作后校验失败；查询实际状态 |
| `scheduled_task_operation_failed` | 计划任务操作或受控 PowerShell 调用失败 |
| `scheduled_task_response_invalid` | 平台任务返回值无效，不能确认成功 |
| `unsupported_release_script_host` | 任务管理要求系统 Windows PowerShell 5.1 Desktop；安装 PowerShell 7 不替代此依赖 |
| `autostart_target_missing` | 同目录缺少选定自启目标程序 |
| `process_query_failed` | 无法确认相关产品进程；暂停清理 |
| `application_running` | 相关产品进程仍运行；正常退出后再处理 |
| `data_location_failed` | 无法确认数据布局或位置 |
| `portable_data_removal_unsupported` | 当前为便携布局，不支持通过此命令删除数据 |
| `data_deletion_failed` | 数据文件检查或删除失败；可能已部分删除 |
| `integration_failed` | 其他维护失败的兜底分类，不泄露原始内部原因 |

`warning: environment_notification_failed` 表示 PATH 写入及校验已成功，但环境变更广播失败；先打开新终端核对，不必把它当成写入失败重复修改。超时、输出中断或其他失败均不保证没有副作用，先读取实际状态再决定是否重试。

## 实现与验收边界

入口见 [MGR 命令解析](../cmd/sidraviamgr/main.go)、[结果结构与 PATH](../internal/installationmanager/path.go)、[Windows 操作分派](../internal/installationmanager/platform_windows.go)、[计划任务规则](../internal/installationmanager/task.ps1)、[数据清理清单](../internal/installationmanager/cleanup.go)。本文说明代码契约，不代表已在当前设备执行自启、卸载或数据删除验收。

返回：[CTL 高级使用](advance.md) · [使用指南](guide.md) · [数据与隐私](privacy.md)
