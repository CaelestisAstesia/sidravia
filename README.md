# Sidravia

Sidravia 是用于 Windows Dr.COM 网络认证的命令行客户端，由后台 daemon 保持认证状态。
当前 portable Alpha 自带 JLU Profile，解压后即可运行。

在包目录打开 PowerShell：

```powershell
.\sidraviactl.exe daemon start
.\sidraviactl.exe profile list
.\sidraviactl.exe auth start --profile jlu --username '<账号>'
.\sidraviactl.exe auth status <session-id>
```

密码由隐藏提示读取，请勿写入命令行。下载、安装和基本排障见[使用说明](docs/guide.md)。
