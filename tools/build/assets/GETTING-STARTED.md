# Sidravia 便携版入门

这是 Sidravia 的当前 Windows amd64 便携版（portable）。完整解压后即可直接使用，
不需要安装、管理员权限或 PowerShell 7；随包可选集成命令使用系统自带的
Windows PowerShell 5.1。候选包可能未签名；请从可信来源下载并核对
`SHA256SUMS.txt` 中的外部 SHA-256。

## 解压与运行

1. 把 zip 内全部文件完整解压到固定目录，例如 `%LOCALAPPDATA%\Programs\Sidravia`。
   该目录必须对当前普通用户可写：程序运行后会在同一目录生成 `config\`、`runtime\`
   与 `logs\` 数据目录。
2. 先确认 daemon 与 Profile：

   ```powershell
   .\sidravia.exe --help
   .\sidravia.exe daemon start
   .\sidravia.exe daemon status
   .\sidravia.exe profile list
   ```

3. 认证：运行 `.\sidravia.exe auth start --profile jlu --username <账号>`。命令会返回初始
   Snapshot；认证可能仍在 daemon 中继续。复制实际返回的 SessionID 后运行
   `.\sidravia.exe auth status <session-id>` 查看进度。

4. 不再需要认证时，可选运行 `.\sidravia.exe daemon stop`。

本 zip 包含两个程序 `sidravia.exe` / `sidraviad.exe`、便携标记
`sidravia.portable`、官方 Profile `institution-profiles\jlu.json`、可选集成脚本
`scripts\install.ps1` / `scripts\uninstall.ps1`、本入门说明、`LICENSE`、
`BUILD-INFO.txt` 与内部校验 `SHA256SUMS`。

## 可选用户态集成

`scripts\install.ps1` / `scripts\uninstall.ps1` 是可选的用户态集成脚本，不是
Installer，也不要求 PowerShell 7：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\uninstall.ps1
```

install 只把解压目录加入当前用户 PATH，并注册当前用户的 `SidraviaDaemon` 登录任务；
uninstall 只撤销这两项，绝不删除你的 Configuration、凭据、Profile 或日志。

- 移动或重命名解压目录前，先运行 uninstall 撤销旧路径，移动后再对新路径重跑
  install。
- 彻底移除：先 `.\sidravia.exe daemon stop`，再运行 uninstall，最后删除整个解压目录
  （其中包括运行后生成的 `config\`、`runtime\`、`logs\`）。

## 日志

后台日志位于解压目录 `logs\sidraviad.log`。`SIDRAVIA_LOG_LEVEL` 接受 info、debug、
trace；trace 会记录完整网络报文并可能包含敏感认证材料，分享前请按秘密处理。
