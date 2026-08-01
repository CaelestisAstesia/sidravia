# Sidravia 产品路线图

本文是公开路线图投影。它区分产品代码、自动验证、Windows-native、WSL/Linux-native、
校园网络和 Release readiness；这些状态不能合并成笼统的“完成”。

## 发布基线与当前源码

`v0.1.0-alpha.1` 是已发布的历史 Windows Alpha。当前 `main` 是下一未发布源码，不承诺
下一个版本号、签名或发布日期。

当前源码的首版代码面已完成：

- Windows CLI/daemon 与 typed loopback IPC；
- 持久 Configuration、schema 3、AutoLogin/AutoReconnect；
- retained Session 生命周期与单活动准入；
- D520 登录、保活、重试与 Logout；
- 官方 JLU Profile、统一程序根 Profile 布局；
- 普通/便携 Windows 可复现包；
- Windows PATH 与登录任务 PowerShell 集成；
- Linux/WSL 共享核心运行时基线；
- 安全日志、CLI 呈现和完整 CLI smoke 脚本。

这些能力在自动验证、Windows 原生和校园网络上的证据范围并不相同。

## 首版剩余顺序

### 1. Windows regression

使用当前 HEAD 构建的包重新检查：

- 普通与便携包内容；
- daemon status/start/stop/restart 和早退提示；
- Profile discovery；
- 便携秘密配置保护；
- Configuration create/update/conflict、AutoLogin 与 AutoReconnect；
- retained Session start/status/restart/stop/remove；
- fixed `localPort=61440`；
- 热点恢复与 TUN-proxy 环境指导；
- `scripts/install.ps1` / `scripts/uninstall.ps1`；
- 真实校园登录、心跳和 Logout。

任何现场缺口都单独记录，不把它自动变成产品代码修复。

### 2. First Release readiness

Windows 回归证据闭合后：

1. 决定版本；
2. 决定签名或明确 unsigned；
3. 运行托管 `Package` workflow 或复核等价候选产物；
4. 审核两个 zip 的内容与校验和；
5. 只有获得明确人类授权后才 push/tag/Release。

文档、构建干跑或本地 zip 不等于 Release readiness。

## 当前证据边界

| 状态 | 当前结论 |
|---|---|
| 产品代码 | 首版代码面完成 |
| 自动验证 | 已有切片分别通过；每个新提交仍需完整 verifier |
| Windows-native | 多个历史/局部链路通过；当前 HEAD 整包回归待完成 |
| WSL/Linux-native | 共享核心 runtime baseline 已验证；不发布 Linux 包 |
| campus-network | JLU 链路与热点/TUN 根因有现场证据；当前 HEAD 全链回归待完成 |
| Release readiness | 未达到 |

Mihomo/Clash-family TUN 捕获私有校园目的地址的结论是环境指导：
`tun.route-exclude-address` 排除 RFC 1918 私网范围可以恢复正确路由；它不批准 Sidravia
修改系统路由，也不推广到所有代理。

## 下一大版本

首版稳定后再规划：

- WSS 与 GUI-oriented IPC hardening；
- 并发请求、事件重连和重同步；
- GUI；
- 更多协议包与协议变体；
- 更广泛的平台产品包和部署单元。

这些能力不得进入当前 Windows regression 或 First Release readiness 切片。
