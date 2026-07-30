# ADR 0026：Windows 安全配置目录的可继承 DACL

**状态：** Accepted

## 背景

2026-07-30 的 Windows 便携版现场运行暴露了一个独立的重启回归：daemon 在便携模式下
打开 Authentication Configuration catalog 后正常停止，随后的启动在组合阶段失败。根因
是安全 Windows catalog 根目录持有一个受保护（protected）的 owner/SYSTEM DACL，但其中
两条 ACE 都不带对象/容器继承标志。这个目录因此不把 owner/SYSTEM 权限传播给已经存在的
`institution-profiles` 子树，使 Profile 加载阶段无法访问既有 Profile 文件。CLI 最终只
报出一个通用的就绪超时，掩盖了真实的组合失败。

旧实现用一个既不继承也不可继承的安全描述符同时保护目录和文件。对单个秘密文件而言这是
正确的边界；但配置根是一个容器，它的子目录（`institution-profiles/`）和孙文件
（`institution-profiles/<id>.json`）必须从父目录继承 owner/LocalSystem 访问，否则一次
安全加固就会把既有子树关在门外。

## 决定

### 目录与文件使用不同的安全描述符

安全 Windows 存储不再用同一个不可继承描述符同时保护容器和文件。

- 受保护目录持有一个 protected DACL 与恰好两条显式 allow ACE：意图 owner 与
  LocalSystem，均为 Full Control。两条目录 ACE 都带 object-inherit 与
  container-inherit 标志，并请求 Windows 自动继承，使 `SetNamedSecurityInfoW` 把这些
  ACE 传播到既有子对象以及未来创建的子对象。
- 受保护文件仍持有 protected DACL 与恰好意图 owner 和 LocalSystem 的 Full Control
  ACE，但不带任何对象/容器继承标志。文件不得把 ACE 传播给子对象。
- 目录与文件的 owner、primary group 都仍是意图 owner。
- 安全临时文件在写入任何 payload 字节之前就获得最终的非继承文件描述符。
- 普通访问拒绝、无效参数/名称/路径与未知 Win32 错误仍是普通失败。既有的 Unsupported
  分类集合不扩大。
- 不使用 `TreeSetNamedSecurityInfo`、`icacls`、递归 Go 文件系统遍历、宽松临时 ACL、
  Everyone/Users/Administrators ACE，也不在真实权限失败后回退。

私有描述符与检查辅助按对象类别拆分。生产接口不为测试单独加宽。

### 安全 catalog 准备先于 Profile 加载

协议注册表构造完成之后，生产组合根先打开 Authentication Configuration catalog，再加载
机构 Profile。`OpenCatalog` 执行安全存储 `Read`，它通过 `prepareDirectory` 准备并修复
共享配置根目录。只有该步骤成功之后，Profile 加载器才遍历
`<config-root>/institution-profiles`。

- catalog 错误在 Profile 加载之前停止组合。
- Profile 错误仍在 Supervisor/runtime 活动开始之前停止。
- 两种失败期间都不启动 host、observer、IPC 或 Session 活动。
- 重新排序这两个独立构造步骤不改变它们的公开模型、持久化格式、路径所有权或运行期生命周期。

因此一次 daemon 重启会修复旧版本留下的“子目录空继承 DACL”状态：catalog 打开时先把配置
根加固成可继承模型，传播到既有 `institution-profiles` 子树，Profile 加载器随后才能读它。

### 安装版与便携版使用同一份权限契约

安装版与便携版都先严格保护配置根与秘密文件。两种模式使用同一个目录/文件描述符契约，
区别只在于便携版在平台明确报告不支持所需权限模型时才进入可观察的 `unprotected`，与
ADR 0024 的降级语义一致。本切片不改变该降级条件，也不扩大 Unsupported 集合。

## 结果

- 受保护目录的 owner/LocalSystem ACE 现在可继承，既有和未来子对象都能获得访问；受保护
  文件的 ACE 保持不可继承，秘密文件边界不变。
- 生产组合顺序改为“打开 catalog → 加载 Profile”，使重启修复既有 Profile 子树的访问。
- 本切片完成代码与自动验证（聚焦测试、race、Windows amd64 测试编译与完整公开 verifier）。
- Windows 原生与校园现场复核仍是独立证据，本决定不声称 Windows 原生成功。
- 本切片不修改 D520、launcher、IPC、Session、Supervisor、依赖或 Linux 行为。
