# ADR 0014：CLI 呈现边界

**状态：** Accepted

## 背景

`sidravia` CLI 首版的帮助、状态、Session 详情/列表、Profile 列表、密码提示和错误
消息混合了英文机器码和零散中文，且没有统一的终端能力判定：任何着色都可能在重定向
或管道中泄漏 ANSI 序列，动态值也没有控制字符清理。随着命令树、列表、两阶段停止和
daemon 日志切片相继完成，CLI 需要一个独立、安全且可维护的呈现边界，使人类输出与
机器契约、daemon 结构化日志分离。

## 决定

在 `internal/cli` 建立唯一的 CLI 呈现边界 `internal/cli/presentation.go`，负责终端
能力选择、Windows 虚拟终端启用/恢复、可信静态标签与映射状态文本的样式、动态值控制
字符清理、稳定机器码的简体中文映射，以及在写入前完成整块渲染。业务操作仍然只返回
错误和 DTO；呈现边界不拥有 daemon 发现、IPC 调用、密码内容、Session 状态、重试决策、
持久化或协议行为。

样式与能力依赖只使用 `github.com/muesli/termenv v0.16.0`，且只有
`internal/cli/presentation.go` 这一个生产文件可以导入它。任何 termenv 类型都不跨出
`internal/cli`。不引入 Huh、Bubble Tea、Lip Gloss、表格渲染库或本地化框架。

终端能力以 termenv 对 writer 的实际 `ColorProfile()` 作为第一道门：当它是 `Ascii`
（重定向或非交互 writer）时，无论 `CLICOLOR_FORCE` 如何设置都强制纯文本。只有彩色
真实终端才由 `EnvColorProfile()` 精炼，此时 `NO_COLOR` 使输出为纯文本。因此重定向或
管道输出始终是纯文本，`NO_COLOR` 始终禁用 ANSI，而生产代码绝不强制 ANSI。测试可以
通过私有构造函数强制 profile，但生产代码不能。呈现边界不查询前景/背景色、终端宽度、
光标位置或明暗主题，也不修改 termenv 的 package-global default output。

Windows 虚拟终端启用是可选能力：启用失败时强制纯文本并安装安全的空操作恢复函数，因
此该路径不输出 ANSI；恢复函数在命令输出后执行，恢复错误通过呈现完成路径与写入错误
一同可观察，但任何 VT 错误或终端查询都不替代已有业务操作错误。

在写入任何动态字符串前，把每个 Unicode 控制字符（包括 CR、LF、TAB、ESC、C0/C1 控制
和 DEL）替换为替换符 `�`，保留普通 Unicode、空格、标点、ID、时间戳和中文。所有来自
固定 CLI 字面量之外的值都经过清理，包括版本、BuildID、状态码、SessionID、状态和原因
码、Profile ID/名称、协议 ID、账号标签、接口 ID/名称和 IPv4 文本、时间戳、失败/建议
码以及任何 daemon 错误码。动态值绝不进入 termenv 颜色解析器。CLI 永不打印 daemon
`Error.Message`、Session `Description`、失败 `Description`、包装原因、请求 payload、
凭据或原始终端环境值。

命令令牌和 flag 保持不变。顶层、分组和叶子命令安装确定性的中文 help 渲染器，只使用
`用法` 和 `可用命令` 标题，只列出非隐藏的规范命令，显示每个叶子的精确语法，且不含
Cobra 默认英文标题。`--help` 永不派发操作。未知参数仍只返回静态中文用法行，且不回显
用户提供的值。已迁移的顶层 `status` 仍返回固定迁移提示。

所有当前 IPC 错误码映射为固定中文指引，未知或缺失码使用安全兜底消息。命令对相同的
参数、输入、发现、IPC、解码、操作和写入失败仍返回非零退出码；Session 状态仍不决定
进程退出码。`cmd/sidravia/main.go` 通过 CLI 呈现边界打印静态中文错误前缀，且不打印
底层原因。

## 结果

- CLI 人类输出与机器契约、daemon 结构化日志明确分离：呈现边界只消费 DTO 和错误码，
  不改变 IPC、Session、D520、持久化或路由选择。
- 着色只在真实交互终端出现：重定向/管道和 `NO_COLOR` 始终纯文本，`CLICOLOR_FORCE`
  无法在重定向时重新启用颜色，生产代码永不强制 ANSI。
- 动态值经过控制字符清理，无法注入 ANSI 序列或新输出行；可信标签和映射状态文本才
  被样式化，`suspended` 和未知状态保持纯文本。
- termenv v0.16.0 是唯一的样式/能力依赖，且被限制在 `internal/cli` 内部。
- 在持久认证配置和网络选择契约稳定之前，不引入交互式表单库、引导式设置或全屏 TUI。
- 本切片完成代码和自动验证，但 Windows 原生与校园现场尚未重新验证，不改变提交
  `508197d` 的现场证据范围。
