# Sidravia Windows 使用指南

## 先确认你使用的版本

已发布的 `v0.1.0-alpha.1` 是历史 Alpha，只应使用该 Release 随附的说明。本文其余部分
描述当前 `main` 的下一未发布源码和由 `tools/build` 生成的候选包；这些能力尚未成为新的
公开 Release，也不承诺候选版本号、签名或发布日期。

Alpha.1 曾在一台 Windows 11 机器和吉林大学校园网完成认证、保活与 Logout。当前 HEAD
仍需整包 Windows 回归。

## 当前候选包

`tools/build` 生成：

- `sidravia-v<version>-windows-amd64.zip`；
- `sidravia-v<version>-windows-amd64-portable.zip`；
- `SHA256SUMS.txt`。

两个 zip 复用同一对二进制并包含：

- `sidravia.exe`、`sidraviad.exe`；
- `institution-profiles\jlu.json`；
- `scripts\install.ps1`、`scripts\uninstall.ps1`；
- `BUILD-INFO.txt`、`GETTING-STARTED.md`、`README.md`、`LICENSE`；
- 内部 `SHA256SUMS`。

只有便携包包含 `sidravia.portable`。候选包可能未签名；必须从可信来源取得并核对外部
SHA-256。

## 运行目录

官方机构 Profile 是随版本发布的程序内容，两种模式都位于：

```text
<exe-dir>\institution-profiles\jlu.json
```

daemon 直接读取，不复制到 AppData 或 `config\`，也没有 seed/migration 步骤。

安装版：

- `configurations.json`：`%APPDATA%\Sidravia`；
- runtime 与日志：`%LOCALAPPDATA%\Sidravia`；
- 官方 Profile：程序根 `institution-profiles\`。

便携版：

```text
<exe-dir>\
  sidravia.exe
  sidraviad.exe
  sidravia.portable
  institution-profiles\
    jlu.json
  config\
    configurations.json
  runtime\
    runtime.json
  logs\
    sidraviad.log
    sidraviad.log.1
```

CLI 与 daemon 各自解析同一 marker。增删 marker 前必须先停止 daemon。

`institution-profiles\` 同时是调试入口，可以临时放入测试 Profile；本地文件不保证跨更新
或重新解压保留。正式增加机构应提交上游并经过独立现场验证。Profile 不得包含账号或密码。

## daemon 与帮助

在解压目录运行：

```powershell
.\sidravia.exe daemon status
.\sidravia.exe daemon start
.\sidravia.exe daemon restart --log-level debug
.\sidravia.exe daemon stop
```

`daemon status` 严格只读，不启动 daemon、不创建目录、不轮转日志。stop/restart 始终针对
命令首次探测的精确 daemon generation。

如果 Windows launcher 报告“sidraviad 在就绪前退出”，表示它观察到自己创建的精确子进程
已退出，并在最后一次 typed readiness 探测后仍未就绪。检查当前模式的
`logs\sidraviad.log`；CLI 不显示私有 Wait cause。

所有帮助入口共享同一份分层中文规格：

```powershell
.\sidravia.exe
.\sidravia.exe help daemon
.\sidravia.exe help auth start
.\sidravia.exe auth start --help
```

旧顶层 `status` 以及已删除的 `install` / `uninstall` 命令不是兼容别名。

## Configuration

当前源码提供：

```powershell
.\sidravia.exe config list
.\sidravia.exe config show <configuration-id>
.\sidravia.exe config create
.\sidravia.exe config update <configuration-id>
.\sidravia.exe config set-password <configuration-id>
.\sidravia.exe config remove <configuration-id>
```

非交互密码输入必须显式使用 `--password-stdin`；密码不得进入 argv、Profile、截图或
日志。安装版要求当前用户与 SYSTEM 保护；便携版只有文件系统明确不支持权限模型时才进入
`unprotected`，新增或替换秘密还需要当次明确授权。

Configuration schema 3 拥有两个自动化开关：

- `AutoLogin`：最多一份 Configuration 启用；每个 daemon generation 在首个网络 Snapshot
  后只评估一次；
- `AutoReconnect`：创建 retained Session 时冻结进不可变运行定义，决定失败或网络中断后
  是否自动重连。

一次性认证始终自动重连。显式 stop/remove/restart 始终优先。不存在全局 Settings
自动连接控制器。严格 schema 2 文件仍可读取，默认
`AutoLogin=false`、`AutoReconnect=true`，仅打开不会重写。

## Session 与认证

先退出其他 Dr.COM 客户端。确认官方 Profile：

```powershell
.\sidravia.exe profile list
```

一次性认证：

```powershell
.\sidravia.exe auth start --profile jlu --username '<账号>'
```

按持久 Configuration 启动：

```powershell
.\sidravia.exe auth start --config <configuration-id>
```

`auth start` 返回初始 Snapshot 后立即退出，不等待认证完成。记下 Session ID：

```powershell
.\sidravia.exe auth list
.\sidravia.exe auth status <session-id>
.\sidravia.exe auth start --session <session-id>
.\sidravia.exe auth restart <session-id>
.\sidravia.exe auth stop <session-id>
.\sidravia.exe auth remove <session-id>
```

stop 先发布 `stopping`，协议 Run 完成有界清理后才发布 `suspended`。Supervisor 在此之前
不释放单活动准入。remove 成功意味着 Session actor、协议、revision forwarder 和集合成员
均已退出。daemon 重启后不恢复旧 SessionID。

D520 JLU Profile 使用独立 fixed `localPort=61440`；绑定失败不回退到系统分配端口。
认证成功后应核对网络字段是实际校园物理接口和预期 IPv4。

## 引导式实地验收

解压当前候选包后，可以从包根运行随包脚本：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\field-test.ps1
```

默认流程先校验包内二进制哈希，再在当前用户临时目录创建带随机标记的便携沙箱，运行完整
CLI smoke。随后按提示选择是否临时验证当前用户 PATH/`SidraviaDaemon` 任务、校园认证、
热点/网络转换，以及受保护的临时 Configuration 自动登录。可用参数：

```powershell
.\scripts\field-test.ps1 -SkipCampus
.\scripts\field-test.ps1 -SkipIntegration
.\scripts\field-test.ps1 -SkipNetworkTransition
.\scripts\field-test.ps1 -Profile jlu -ReportDirectory field-results
```

账号没有命令行参数；每次运行由 `Read-Host` 输入。密码用隐藏的 `SecureString` 提示，仅在
固定 stdin 调用边界转换并交给 `--password-stdin`，不进入子进程 argv、环境变量、保留
报告或原始证据。若同意测试 AutoLogin，账号和密码会暂时存在于带 ACL 保护的便携沙箱
Configuration 中；脚本绝不传 `--allow-insecure-storage`，无法建立保护时该阶段 BLOCKED。
拒绝临时存储则仍可运行一次性认证，该阶段记为 SKIPPED。

脚本使用一个外层 `finally` 依次删除测试 Session、Configuration，停止 daemon，撤销仅由
本次运行创建的 PATH/任务，并删除沙箱。保留的 JSON 报告只有包哈希、平台版本、检查 ID、
耗时和 `PASS`/`FAIL`/`BLOCKED`/`SKIPPED` 稳定结果，不含账号、密码、MAC、token、原始日志、
Profile JSON、抓包或路由表。退出码：`0` 为所有已请求阶段通过，`1` 为行为失败但清理
完成，`2` 为安全前提阻塞，`3` 为清理不完整。

PowerShell/.NET 不能保证每个瞬时托管内存副本都被法证级擦除；异常断电也可能留下带标记
的临时目录。下次运行会限定清理自身标记的旧沙箱，失败时以 `CLEANUP_REQUIRED` 要求人工
删除。脚本不会停止其他 Dr.COM 客户端、修改 VPN/TUN、路由、网卡或热点；这些物理/环境
动作只由测试者按提示完成。任务动作启动证明不等于真实注销/登录触发，发布、签名与 Hosted
Workflow 也不在该报告的证明范围。

## Windows 用户态集成

建议把安装版解压到 `%LOCALAPPDATA%\Programs\Sidravia`，然后从解压目录运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1 -LogLevel debug
```

脚本注册它所在的目录，因此安装版和便携版都可使用。它：

- 把目录加入用户 PATH；
- 创建登录计划任务 `SidraviaDaemon`；
- 任务执行 `sidravia daemon start --log-level <level>`；
- 执行前后验证 PATH 和任务动作；
- 重复执行安全。

撤销集成：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
```

uninstall 只移除精确 PATH 条目和登录任务，前后自校验；它绝不删除 Configuration、凭据、
官方/调试 Profile 或日志。当前没有 Windows Service。

## Mihomo/Clash-family TUN 排障

一条 Windows 现场链发现：Mihomo TUN 捕获私有校园认证目的地址时，系统 route 会先把目的
地址交给 TUN。此时在代理内部添加 `prepend-rules` 并不等于从 Windows TUN route 排除该
目的地址。

认证前应选择以下一种方式：

1. 停止该 TUN 代理；或
2. 在 Mihomo/Clash-family 配置中用 `tun.route-exclude-address` 排除 RFC 1918 范围：
   `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`。

正确排除后，现场 route 回到物理以太网，Challenge 与 Sidravia 认证恢复。不要把临时
`/32` 系统路由当作产品方案；Sidravia 不负责修改系统 route。这是一个机器和一条现场链的
环境指导，不证明所有代理都有相同行为，也不产生新的产品修复。

## 日志和证据

CLI 后台启动时，stdout/stderr 都写入当前模式的 `sidraviad.log`；达到 10 MiB 后轮转为
唯一 `.1`。`SIDRAVIA_LOG_LEVEL` 接受 `info`、`debug`、`trace`。Trace 含完整 D520
数据报和潜在认证材料，分享前必须按秘密处理。

Info/Debug 不包含密码、token、Profile JSON、MAC、DNS/DHCP、网关、接口 ID、请求/响应
字节或原始 error。遇到失败时保存非秘密 Snapshot、稳定错误码、包校验和与明确的平台/
网络条件，不公开原始抓包或认证派生字段。

## 当前限制

- 只计划发布 Windows amd64；
- 当前 HEAD 的完整 Windows/campus 回归尚未完成；
- 没有 GUI、Windows Service、自动更新或 WSS；
- Linux/WSL 只是共享核心运行时基线；
- 官方 Profile 当前只有经过现场验证的 `jlu`；
- 官方客户端其他 Login 变体仍不在本次发布范围。
