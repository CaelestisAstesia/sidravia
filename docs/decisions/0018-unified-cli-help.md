# ADR 0018：统一、分层且可导航的 CLI 帮助

**状态：** Accepted

## 背景

ADR 0014 建立了安全呈现边界，但根命令仍把所有叶命令拼成一条用法，bare root/group
返回错误，普通 `help <path>` 也不能导航。Windows operator 检查证明这使首版命令树
难以发现和阅读。

## 决定

bare root/group、`help <path>`、`-h` 和 `--help` 都解析到规范、非隐藏的 canonical
command path，并复用唯一 `helpSpecs` 节点、`renderHelp`/`renderHelpNode` 与 termenv
presentation completion 路径。不存在、隐藏或多余路径返回固定中文用法错误，不回显
用户 token，也不派发业务操作。

每个节点使用有序多行用法规格。帮助按描述、用法、可用命令、参数、选项、示例的固定
顺序显示适用段；标题自占一行，每条用法缩进两个 ASCII 空格，段间恰好一个空行。根与
资源组只显示 `<command>` 形状，叶子的完整语法由各自节点拥有。输出不探测宽度、不自动
换行，不显示 Cobra 英文标题或 completion 命令。

## 结果

同一节点经不同入口产生 byte-for-byte 相同输出；重定向、`NO_COLOR`、VT 恢复和写入
错误继续服从 ADR 0014。命令 token、私有业务参数 parser、退休顶层 `status` 的迁移
提示和业务操作语义不变。
