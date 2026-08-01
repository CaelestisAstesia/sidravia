# ADR 0030：官方非秘密机构 Profile 打包

**状态：** Accepted；Profile 位置细节后由 ADR 0032 收窄

## 决定

只有经过独立现场验证且不含账号、密码等秘密的机构 Profile 才随包发布。
首个官方 Profile 是 `jlu`；未验证的机构不以模板或占位形式打包。

规范源文件是 `internal/daemon/configuration/profiles/jlu.json`，`tools/build` 将其
逐字节嵌入普通与便携 zip。daemon 只读 Profile，不写入或覆盖。

ADR 0032 后续取代了本 ADR 的安装版复制/seed 和用户配置目录位置：
当前两种包都从程序根 `institution-profiles/` 原地读取随包 Profile。
