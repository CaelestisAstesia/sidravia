# ADR 0022：持久凭据先于 IPC 传输强化

**状态：** Accepted

## 背景

当前 IPC 是本机回环 `ws://`，连接前以 daemon 生成的随机 token 和精确 BuildID 鉴权。
它不加密 WebSocket 帧。持久凭据管理会让 CLI 向 daemon 提交密码，因此可以选择先升级
为 pinned、daemon-ephemeral `wss://`，也可以先完成产品功能，再在 GUI 需要更强 IPC
能力时一并升级。

产品当前声明为单操作系统用户使用，威胁边界不包含已经以同一用户权限运行的恶意代码；
这类代码本就能读取用户文件或观察进程。当前真实风险与开发目标更需要稳定的配置、凭据
和跨平台边界。

## 决定

持久 Configuration/Credentials 在当前大版本先于 WSS 实施。Windows 首版继续遵循
ADR 0006：凭据单独明文保存，并用当前用户与 SYSTEM 的文件 ACL 保护；daemon 不提供
读取凭据明文的 IPC。CLI 可以创建、替换、删除凭据并查询安全元数据，正常认证通过
`ConfigurationID` 启动。

本机回环 `ws://`、随机 token 和精确 BuildID 暂时保留。不增加自制的应用层加密，也不
把 token 当作传输加密。文档必须明确这一局限。

WSS、IPC 并发请求、事件重连/重同步和 GUI 所需的长期连接能力放入下一个大版本的同一
IPC/GUI 演进阶段；届时 CLI、daemon 和 GUI 共享产品版本与 BuildID，不建立独立兼容
矩阵。

## 结果

- 当前实现顺序是运行目录契约、持久 Configuration/Credentials、Linux/WSL 基线、
  网络诊断与选择修正，而不是先做 WSS。
- 同用户恶意代码不属于凭据存储能够解决的威胁；这不意味着密码可以进入日志、命令行、
  Snapshot、查询响应或错误文本。
- Credential Manager、DPAPI 和其他后端仍可作为以后替换或增强 Credentials Store 的
  平台实现，但不改变 Configuration、daemon app、Session 或 CLI 用例。
- 下一大版本强化 IPC 时保持 typed contract 和 daemon app 边界，不让 GUI 直接读取
  存储或导入 daemon 内部包。
