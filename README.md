# Sidravia

Sidravia 是一个以 Windows 为首要现场支持平台的 Dr.COM 网络认证客户端。产品由短生命周期
CLI `sidravia` 和长期运行的本地 daemon `sidraviad` 组成：

```text
sidravia CLI -> loopback WebSocket IPC -> sidraviad -> Dr.COM -> network
```

Linux/WSL 已具备共享核心的原生运行时基线，但不是首版产品包；macOS 当前明确返回
Unsupported。

## 发布状态

已发布的 `v0.1.0-alpha.1` 是历史 Alpha 基线：它曾在一台 Windows 11 机器和吉林大学
校园网完成认证、持续心跳与主动 Logout。当前 `main` 是下一未发布源码，包含 Alpha 之后
的配置自动化、retained Session 生命周期、官方 Profile、运行目录、打包和安装集成等改动。
不要把下文的当前源码能力倒推为已发布 Alpha 的能力，也不要据此猜测下一个版本号、签名或
Release 日期。

当前源码的首版代码面已经完成，剩余发布门是：

1. 使用当前 HEAD 的包完成 Windows 原生回归；
2. 复核真实校园登录、心跳、热点恢复和 Logout；
3. 决定版本与签名，运行托管打包并审核产物；
4. 只有获得明确人类授权后才发布。

详细状态见[产品路线图](docs/roadmap.md)。

## 当前源码能力

- `daemon status/start/stop/restart`，其中 status 严格只读；
- `config list/show/create/update/set-password/remove`；
- `auth list/start/status/stop/restart/remove` 和 retained Session ensure-running；
- `profile list` 安全摘要；
- Configuration schema 3、`AutoLogin` 和 `AutoReconnect`；
- 单活动 Session、两阶段 `stopping -> suspended`、可取消可等待的运行生命周期；
- Dr.COM 5.2.0(D) Challenge、Login、保活和尽力 Logout；
- Windows 与 Linux/WSL 的共享 domain core、IPC 和运行目录契约；
- 安全结构化日志、稳定机器码和简体中文 CLI 呈现；
- 可复现的 portable Release 与 field-validation Windows amd64 zip；
- 随包官方 `jlu` Profile；
- Windows 用户 PATH 与登录任务集成脚本。

CLI 不读取 daemon 配置或内部状态。IPC server 只经窄 Handler 调用 daemon app；daemon
app 负责跨模块用例顺序，Supervisor 拥有 Session 集合与准入，Session 拥有单次认证意图、
重试和协议 Run。详见[当前架构](docs/architecture.md)。

## 运行目录

`internal/productlayout` 是 CLI 与 daemon 唯一的运行目录 resolver。两种模式都从
`<exe-dir>/institution-profiles/` 直接读取随版本发布的官方 Profile。

安装版：

- `configurations.json`：操作系统用户配置目录下的 `Sidravia`；
- `runtime.json` 与后台日志：操作系统用户缓存目录下的 `Sidravia`；
- 官方 Profile：`<exe-dir>/institution-profiles/`。

便携版由可执行文件同目录的普通 `sidravia.portable` 标记启用：

```text
<exe-dir>/
  sidravia
  sidraviad
  sidravia.portable
  institution-profiles/
    jlu.json
  config/
    configurations.json
  runtime/
    runtime.json
  logs/
    sidraviad.log
    sidraviad.log.1
```

resolver 不读取当前工作目录，不猜模式、不扫描另一模式，也不自动复制或迁移文件。增删
marker 前必须先停止 daemon。

## Configuration 自动化

每份持久 Authentication Configuration 拥有自己的 `AutoLogin` 与 `AutoReconnect`：

- 最多一份 Configuration 可以启用 `AutoLogin`；
- daemon 每个 generation 在首个已接受 Environment Snapshot 后只评估一次自动登录；
- `AutoReconnect` 冻结进 retained Session 的不可变运行定义；
- 一次性认证始终保持自动重连；
- 显式 stop/remove/restart 始终优先；
- 不存在全局 Settings 所有者或持续 desired-state controller。

schema 3 是唯一写出格式。严格 schema 2 文档仍可读取，默认
`AutoLogin=false`、`AutoReconnect=true`；仅打开旧文档不会重写它。

## Windows 包和用户态集成

当前 `tools/build` 一次编译同一对 Windows amd64 二进制，生成一个 portable Release zip、
一个 field-validation zip 和外部 `SHA256SUMS.txt`。正式 Release 精确包含 10 个
产品/首次使用文件：

- `sidravia.exe` 与 `sidraviad.exe`；
- `sidravia.portable` 便携标记；
- `institution-profiles/jlu.json`；
- `scripts/install.ps1` 与 `scripts/uninstall.ps1`；
- `GETTING-STARTED.md`、`BUILD-INFO.txt`、`LICENSE` 和内部 `SHA256SUMS`。

Release 不携带开发者向根 `README.md`；包内 `GETTING-STARTED.md` 来自专用发布资产。
field-validation zip 在相同内容上增加 `scripts/field-test.ps1` 与
`scripts/cli-smoke.ps1`，用于 Windows 引导式实地验收。构建工具不运行 Git，不签名、
不上传、不创建 tag 或 Release。

Windows 用户可从解压目录运行：

```powershell
.\sidravia.exe --help
.\sidravia.exe daemon status
```

可选集成命令使用系统自带的 Windows PowerShell 5.1，不要求 PowerShell 7：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
```

install 把脚本所在目录加入当前用户 PATH，并创建当前用户登录任务 `SidraviaDaemon`，
执行 `sidravia daemon start --log-level <level>`。两个脚本只在 Windows PowerShell 5.1
Desktop 下运行，模式无关、幂等并进行前后自校验。卸载只撤销 PATH 条目和登录任务，绝不
删除 Configuration、凭据、Profile 或日志。产品中不存在 `sidravia install` /
`sidravia uninstall` 命令。

## Windows 引导式实地验收

field-validation zip 带有一份一次性实地验收入口 `scripts\field-test.ps1`，需要
PowerShell 7。它在当前用户临时目录创建带随机标记的便携沙箱，复用完整 CLI smoke，并可
在明确确认后测试校园认证、Session 生命周期、固定端口、心跳、Logout、自动登录/重连及
PATH/登录任务脚本。field-test 解析并预检系统 Windows PowerShell 5.1，用其执行正式
install/uninstall，不使用 pwsh 调用正式发行脚本：

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File .\scripts\field-test.ps1
```

真实账号和隐藏密码每次运行时输入；密码只经 `--password-stdin` 交给 CLI，不进入 argv、
环境变量或保留报告。完整自动登录检查会先征得同意，再把凭据临时写入受保护的测试沙箱；
拒绝或无法建立保护时该项跳过/阻塞，不会使用不安全存储。脚本最终只保留字段白名单报告，
并在 `finally` 中清理测试 Session、Configuration、daemon、PATH/任务和沙箱。

这不是法证级安全擦除承诺：PowerShell/.NET 可能产生瞬时内存副本，异常断电也可能留下带
明确标记的临时沙箱；下次运行会尝试清理，仅在无法清理时要求人工处理。物理网络/热点
切换、实际注销登录、Hosted Workflow、签名和发布仍是独立人工或外部证据。

## Profile 与 TUN 代理

机构 Profile 是随版本发布的非秘密程序内容。`jlu.json` 由 daemon 在程序根目录原地读取，
没有 AppData/config 复制或 seed 步骤。`institution-profiles/` 也可用于临时调试，但本地
文件不保证跨升级保留；正式新增机构应提交上游并经过独立现场验证。不得把账号或密码写进
Profile。

一条 Windows 现场证据确认：Mihomo/Clash-family TUN 捕获私有校园认证目的地址时，需要
停止该 TUN，或用 `tun.route-exclude-address` 排除 RFC 1918 私网范围。普通代理规则
`prepend-rules` 不能替代 Windows TUN 路由排除。这是特定现场链得出的环境指导，不是
Sidravia 修改系统路由，也不代表所有代理都存在同样问题。详见
[Windows 指南](docs/getting-started-windows.md)。

## 日志与秘密

直接运行 daemon 时日志写到 stderr；由 CLI 后台启动时写入运行目录的
`logs/sidraviad.log`，达到 10 MiB 后轮转为唯一 `.1` 备份。
`SIDRAVIA_LOG_LEVEL` 接受 `info`、`debug`、`trace`。Trace 包含完整 D520 数据报，
可能含认证材料，启用时先记录 `trace_logging_sensitive`。

密码不得进入 argv、Profile、公开 Snapshot、普通日志或错误文本。交互输入关闭回显；
自动化必须显式使用 `--password-stdin`。Info/Debug 不记录 token、密码、Profile JSON、
MAC、DNS/DHCP、网关、接口 ID、请求/响应字节或原始 error。

## 构建与验证

需要 Go 1.26.4：

```bash
python3 tools/developer/verify_repository.py --scope all --go /path/to/go1.26.4
```

规范可复现打包入口：

```bash
go run ./tools/build \
  --version <MAJOR.MINOR.PATCH[-prerelease]> \
  --build-id <stable-build-id> \
  --output <new-output-directory> \
  [--go <go-executable>]
```

自动验证、Windows 原生、WSL/Linux 原生、校园网络和 Release readiness 是不同证据状态，
不得合并成笼统的“完成”。

## 文档

- [文档入口](docs/README.md)
- [当前架构](docs/architecture.md)
- [工程实践](docs/engineering.md)
- [产品路线图](docs/roadmap.md)
- [架构决策](docs/decisions/README.md)
- [Dr.COM 5.2.0(D) 协议规范](docs/protocols/drcom-5.2.0-d.md)
- [验收证据](docs/evidence/README.md)
- [Windows 快速开始](docs/getting-started-windows.md)
- [Linux/WSL 快速开始](docs/getting-started-linux.md)

## License

Sidravia 使用 [GNU Affero General Public License v3.0](LICENSE)。
