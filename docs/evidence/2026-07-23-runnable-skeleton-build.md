# Runnable Skeleton 构建证据

**日期**: 2026-07-23
**分支**: codex/runnable-skeleton

## 源码基线

| Slice | 提交 | 说明 |
|---|---|---|
| Slice 1 | 6e8cf7c | feat(ipc): add authenticated daemon status transport |
| Slice 2 | 73da4bc | feat(daemon): add Windows status host |
| Slice 3 | (待提交) | feat(cli): complete Windows daemon status slice |

## WSL 交叉编译验证



产物：
- sidravia.exe: PE32+ executable for MS Windows, x86-64
- sidraviad.exe: PE32+ executable for MS Windows, x86-64

## WSL 测试验证



全部通过。

## Windows 原生验证

Windows 原生验收需使用以下脚本：



前置条件：
1. 在 Windows 原生 Go 环境构建两个 exe
2. 将 sidravia.exe 和 sidraviad.exe 放在同一目录
3. 在该目录运行上述 PowerShell 脚本

验收脚本覆盖：
- Test 1: 冷启动（daemon 未运行）
- Test 2: 热连接（daemon 已运行）
- Test 3: 第二 daemon 被 mutex 拒绝
- Test 4: 无效 CLI 参数

## 结论

交叉编译成功，WSL 测试全部通过。Windows 原生双进程验收待执行。
