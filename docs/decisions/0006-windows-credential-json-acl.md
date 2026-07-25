# ADR 0006：Windows 首版用 ACL 保护凭据 JSON

状态：Accepted

## 决定

Windows 首版允许 Credentials Store 把明文凭据保存到独立 JSON 文件。SecureStore 必须把 DACL 限制为当前用户和 SYSTEM。

## 原因

这个方案用较少的系统耦合实现了真实可用的秘密边界。已有 Windows 验证证明 ACL 行为符合预期。

上层只依赖 Credentials Store，因此未来可以替换为 DPAPI 或 Credential Manager。

## 结果

如果 SecureStore 无法建立 ACL，它不得留下新的明文目标。替换文件时也不得放宽权限。

未来秘密存储适配器只能改变 Credentials Store 的内部实现，不能改变 daemon app 或 Session 模型。
