# ADR 0002：配置与凭据分开保存

状态：Superseded by ADR 0024

## 决定

Configuration 只保存非秘密连接信息。它通过 `CredentialID` 引用 Credentials Store 中的秘密。

## 原因

CLI 可以安全显示 Configuration，但不能读取密码。独立 Credentials Store 也允许未来使用 DPAPI 或 Credential Manager，而不改变上层模型。

## 结果

Configuration 不得包含用户名或密码。IPC 可以写入、替换和删除凭据，但不能读取凭据明文。

daemon app 编排配置和凭据的删除或替换。Supervisor 和 Session 不知道秘密如何持久化。
