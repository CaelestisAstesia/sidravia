# Sidravia 使用说明

Sidravia 以 Windows amd64 portable ZIP 提供。当前版本自带 JLU Profile。

## 下载和解压

同时下载 ZIP 和 `SHA256SUMS.txt`，在 PowerShell 中核对文件哈希：

```powershell
Get-FileHash .\sidravia-v0.1.0-alpha.2-windows-amd64-portable.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
```

确认两者一致后，将 ZIP 完整解压到固定、当前用户可写的目录。不要直接在 ZIP 内运行。

## 首次认证

先退出其他 Dr.COM 客户端，然后在解压目录打开 PowerShell：

```powershell
.\sidravia.exe daemon start
.\sidravia.exe daemon status
.\sidravia.exe profile list
.\sidravia.exe auth start --profile jlu --username '<账号>'
```

密码由隐藏提示读取，请勿写入命令行。命令返回 Session ID 后查询认证状态：

```powershell
.\sidravia.exe auth status <session-id>
```

不知道 Session ID 时，运行：

```powershell
.\sidravia.exe auth list
```

## 保存认证配置

交互式创建配置，然后使用配置发起认证：

```powershell
.\sidravia.exe config create
.\sidravia.exe auth start --config <configuration-id>
```

`AutoLogin` 控制 daemon 启动后的自动登录，`AutoReconnect` 控制断线后的自动重连。
可随时运行 `sidravia.exe help config` 查看相关命令。

## 可选的 Windows 集成

使用系统自带的 Windows PowerShell 5.1，将包目录加入当前用户 PATH，并创建登录启动任务：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

移动包目录或不再使用该集成前，先撤销：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
```

卸载脚本只撤销 PATH 和登录任务，不删除认证配置或日志。

## 基本排障

- 认证前退出其他 Dr.COM 客户端。
- 使用 Mihomo、Clash 等 TUN 时，停止 TUN，或让校园网私有地址绕过 TUN。
- 端口错误时检查 UDP `61440` 是否被其他程序占用。
- daemon 异常时先运行 `sidravia.exe daemon status`，再查看 `logs\sidraviad.log`。

`trace` 日志可能包含敏感认证信息，请勿公开分享。

不再需要后台认证时，可以正常停止 daemon：

```powershell
.\sidravia.exe daemon stop
```

## 当前版本

这是面向 Windows amd64、内置 JLU Profile 的未签名 portable Alpha。目前没有 GUI、
Installer 或自动更新。
