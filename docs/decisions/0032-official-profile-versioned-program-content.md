# ADR 0032：官方 Profile 作为随版本程序内容

**状态：** Accepted

## 决定

官方机构 Profile 是项目随版本发布的程序内容，不是用户维护的秘密或数据。
安装版与便携版统一位于 `&lt;exe-dir&gt;/institution-profiles/`，随包文件即 daemon
读取位置，无复制、seed 或迁移步骤。

安装版的秘密 Configuration 仍位于 OS 用户配置目录，runtime/日志位于
OS 用户缓存目录；便携版将秘密配置、runtime 与日志集中到程序目录。

`institution-profiles/` 也是临时调试入口，但本地文件不保证跨版本保留；正式
新增机构走上游提交与独立现场验证。

本 ADR 只取代 ADR 0020 的 Profile 位置、ADR 0026 的配置根 Profile 子树修复、
以及 ADR 0030 的安装版复制/seed 陈述；其他历史边界保留。
