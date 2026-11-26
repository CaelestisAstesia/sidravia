# Windows 维护工具（MGR）

`sidraviamgr.exe`（下文简称 **MGR**）负责 Windows 当前用户范围的 PATH 和登录自启。它每次执行一项操作后退出，不管理认证会话。

日常连接与配置使用 GUI 或 [CTL](advance.md)；普通安装版卸载从 Windows 应用列表进行。仅在需要命令行维护、排查系统集成时，使用本页命令。

本文描述 `0.5.0-alpha.2` 源码的六项 MGR 操作。旧版遗留的卸载/清理命令已退役，相关职责由 Inno 安装器承担。预发布版本可能调整命令及返回结构，请结合所用版本核对。Linux 不提供 MGR；其 GUI 使用独立的平台集成实现，见 [Linux 便携版](linux-portable.md)。非 Windows 的 MGR 构建执行维护操作时返回 `unsupported`。

## 调用范围与参数规则

在目标安装或便携目录打开 PowerShell，使用配套的 MGR：

```powershell
.\sidraviamgr.exe status
```

**维护目标由 MGR 自身所在目录确定，不由终端当前目录决定。** 不要将 MGR 单独复制到其他目录执行维护。

PATH 与自启任务作用于运行命令的当前 Windows 用户。正常维护无需提升权限，不支持 `--all-users`、`--force` 或指定其他账号。安装器可用 `status --directory <绝对目录>` 只读查询目标目录的集成状态，输出仍是同一 JSON 契约；写入操作始终只作用于 MGR 自身目录。

MGR 使用当前用户的串行锁协调维护。无法取得锁、确认相关进程身份或核实系统状态时，会返回失败；它不会强制终止或接管认证后台。

需要程序启动后自动认证时，在保存配置中开启自动登录。

## 完整命令表

下表列出完整命令路径，除上述只读查询目录外，各操作不接受额外选项或位置参数。查看帮助可单独运行：

```powershell
.\sidraviamgr.exe --help
```

无参数、`help` 和 `-h` 也可显示帮助。**MGR 不支持在具体操作后追加 `--help`**，例如 `path enable --help` 会被视为参数无效。

| 命令 | 作用 | 写入或限制 |
| --- | --- | --- |
| `status` | 查询本目录的用户 PATH、自启状态和 GUI 文件可用性 | 不修改 PATH、计划任务、认证配置或会话 |
| `path enable` | 将 MGR 所在目录加入当前用户 PATH | 已存在时不重复添加，写后重新核对 |
| `path disable` | 从当前用户 PATH 移除匹配本目录的条目 | 保留其他目录，不卸载程序 |
| `autostart set gui` | 下次登录 Windows 时启动本目录 GUI 到托盘 | 需要同目录 `Sidravia.exe`；不立即启动 GUI |
| `autostart set daemon` | 下次登录 Windows 时启动本目录独立后台 | 需要同目录 `sidraviactl.exe`；设置时不立即启动，后台启动后是否认证取决于自动登录配置 |
| `autostart disable` | 删除已确认归属于本目录和当前用户的自启任务 | 不停止当前程序；不存在任务也可成功 |


MGR 不提供安装、升级、修复或删除程序文件的命令，这些工作由安装器承担。CTL 的 `integration` 仅转发状态查询、PATH 和自启六项操作；MGR 不再提供卸载检查、卸载准备或数据删除命令。

## 查询、PATH 与自启

按需要分别执行，不要把启用和禁用示例当作一段批处理：

```powershell
.\sidraviamgr.exe status
.\sidraviamgr.exe path enable
.\sidraviamgr.exe path disable
```

PATH 写入保留其他条目和原有字符串类型，目录匹配忽略大小写和末尾路径分隔符；维护时请使用完整路径。写后会校验并广播环境变更，现有终端不会自动刷新其环境，请打开新终端。

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

MGR 管理计划任务根目录中的 `SidraviaDaemon`，按当前用户登录触发、使用普通用户权限。GUI 模式执行 `Sidravia.exe`，窗口可见性按 GUI 的启动偏好决定。旧的托盘启动任务仍可识别，重新选择 GUI 自启会按当前方式登记。后台模式执行 `sidraviactl.exe daemon start --log-level debug`。只有保存配置启用了自动登录，后台启动后才会按该配置自动认证。

已有同名任务若无法确认属于当前用户、本目录及预期动作，会显示 `conflict`。查询 `status` 可以成功返回这一冲突状态；设置、禁用和安装器卸载检查会拒绝冲突。不要靠覆盖或删除未知任务来消除提示。移动便携目录前，应从旧目录正常撤销自启和 PATH，移动后再从新目录设置。

## 安装、恢复与卸载

这些流程由 Inno 安装器负责，详见[使用指南](guide.md#更新移动与卸载)。MGR 只按安装器要求逐项处理自启和 PATH，不能把两个独立操作当成事务；任何一步失败后，先查询实际状态。

安装后系统集成失败不会撤销已安装程序。错误页说明已完成及未完成项；重跑后请在安装选项页重新确认本次目标，默认值按当前实际状态显示，不自动重试旧选择，也不保存操作日志。静默运行必须通过 `/TASKS` 或含 Tasks 的 INF 明确自启和 PATH 目标。

完整清除需要正常退出当前用户全部产品实例。自启/PATH 撤销或额外清理失败会保留程序和卸载入口，部分数据可能已删除；请按提示重试。不得通过手工清空整个安装目录、AppData 父目录或删除未知计划任务来绕过失败。

## JSON 输出与退出码

除帮助外，每次操作向 stdout 输出一个 UTF-8 JSON 对象，当前版本为 `1`。不输出原始内部错误原因。退出码：`0` 操作成功、`1` 操作或输出失败、`2` 参数无效。没有 JSON 或 JSON 不完整时应视为结果无法确认，不把它当作成功。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `version` | 整数 | 结果结构版本，当前 `1` |
| `ok` | 布尔值 | 本次操作是否确认成功 |
| `code` | 字符串 | 稳定结果码，成功通常为 `ok` |
| `capabilities` | 字符串数组 | 当前平台公布的能力，如 `user_path`、`user_autostart` |
| `directory` | 字符串，可省略 | 本次维护目录 |
| `pathEnabled` | 布尔值 | 本次 PATH 查询或操作观察的结果 |
| `changed` | 布尔值 | 成功时本次操作是否发生修改；失败时不能据此判定未修改 |
| `warning` | 字符串，可省略 | 已成功操作的附加提醒，如环境广播失败 |
| `autostart` | 字符串，可省略 | `disabled`、`gui`、`daemon`、`conflict` |
| `guiAvailable` | 布尔值 | 同目录存在常规 GUI 文件；不证明 GUI 可启动或运行验收通过 |

每项操作只返回相关状态。需要同时查询 PATH、自启和 GUI 文件时，运行 `status`。`capabilities` 列出平台支持的能力，操作结果看 `ok` 和 `code`。

PowerShell 脚本应先保存退出码，再解析并核对 JSON；缺失、格式错误或不支持的结构版本均应报错：

```powershell
$managerOutput = & .\sidraviamgr.exe status
$managerExit = $LASTEXITCODE
try {
    $managerResult = $managerOutput | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw '维护工具未返回有效 JSON，操作结果无法确认'
}
if ($null -eq $managerResult -or $managerExit -ne 0 -or $managerResult.version -ne 1 -or -not $managerResult.ok) {
    throw '无法确认 Sidravia 系统集成状态'
}
if ($managerResult.autostart -eq 'conflict') {
    throw '存在自启任务冲突，请先核对任务归属'
}
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
| `integration_failed` | 其他维护失败的兜底分类，不泄露原始内部原因 |

`warning: environment_notification_failed` 表示 PATH 写入及校验已成功，但环境变更广播失败；先打开新终端核对，不必把它当成写入失败重复修改。超时、输出中断或其他失败均不保证没有副作用，先读取实际状态再决定是否重试。

## 实现与验收边界

入口见 [MGR 命令解析](../cmd/sidraviamgr/main.go)、[结果结构与 PATH](../internal/installation/shellintegration/path.go)、[Windows 操作分派](../internal/installation/shellintegration/platform_windows.go)、[计划任务规则](../internal/installation/shellintegration/task.ps1)、[Inno 安装与清理](../packaging/windows/sidravia.iss)。

返回：[CTL 高级使用](advance.md) · [使用指南](guide.md) · [数据与隐私](privacy.md)
