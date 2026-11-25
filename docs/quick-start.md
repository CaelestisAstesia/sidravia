# 快速开始

本文介绍 Windows 与 Linux 图形界面的安装与首次连接，适用于吉林大学 Dr.Com 5.2.0 D 认证环境。核心包没有图形界面，使用方法见[命令行高级使用](advance.md)。

本页适用于 `0.5.0-alpha.2`。从旧版升级时，请先阅读[发行说明](https://github.com/CaelestisAstesia/sidravia/releases/tag/v0.5.0-alpha.2)。

## 下载与安装

先在[项目首页的下载表](../README.md#选择下载包)确认系统、架构和 GUI／核心包型，再到 [Releases](https://github.com/CaelestisAstesia/sidravia/releases) 下载对应文件。`amd64` 表示 Intel／AMD 的 x86-64；ARM64 当前仅提供核心便携包，尚未经实机验证。

### Windows x64

Windows GUI 需要 [Microsoft Visual C++ x64 运行库](https://aka.ms/vc14/vc_redist.x64.exe)。学校公告还需要系统的 [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)。

- **安装版**（`…-windows-amd64-setup.exe`）：运行安装器，保留「图形界面」组件，按向导完成安装。登录 Windows 后需要自动启动时，勾选「开机自启」并选择「启动图形界面和后台」。需要在任意终端使用命令行工具时勾选「加入用户 PATH」。桌面快捷方式按需创建。取消 GUI 组件即为仅核心安装。
- **GUI 便携版**（`…-windows-amd64-gui-portable.zip`）：完整解压到固定、可写的目录，运行 `Sidravia.exe`。保留 `sidravia.portable` 及其他随包文件，不要在 ZIP 内运行或只复制 EXE。

安装版将账号配置保存在 `%APPDATA%\Sidravia`，便携版保存在程序目录下的 `config` 文件夹，两者不会自动迁移数据。不要把安装版覆盖到便携目录。完整数据位置见[数据与隐私](privacy.md#数据位置)；从旧版本升级时，请先阅读对应版本的发行说明并备份配置。

### Linux x64

- **GUI 便携版**（`…-linux-amd64-gui-portable.tar.gz`）：先安装[运行依赖](linux-portable.md#运行环境)，完整解压到可写目录，在包根目录运行 `./sidravia_gui`。保留 `gui-runtime/` 和其他随包文件。
- **Fedora RPM**：按 [0.5.0-alpha.2 的 RPM 说明](https://github.com/CaelestisAstesia/sidravia/blob/v0.5.0-alpha.2/docs/linux-rpm.md)使用 `dnf` 安装 GUI 包，然后从应用菜单打开 Sidravia。RPM 的配置位于当前用户的 XDG 目录，不会自动导入便携版数据。

Linux 的自启在登录桌面后生效，自动认证需另行开启。便携版保留程序目录内的数据，具体位置见[数据与隐私](privacy.md#linux)。

### 校验下载

同时下载该版本的 `SHA256SUMS.txt`，与程序包放在同一目录。Windows 在该目录打开 PowerShell，文件名替换为实际下载的名称：

```powershell
Get-FileHash .\sidravia-v0.5.0-alpha.2-windows-amd64-gui-portable.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
```

Linux 可用 `sha256sum <下载文件名>` 计算哈希，与清单中对应项比较；只下载部分资产时无需检查其他未下载项。两者应完全一致，十六进制字母大小写不影响比较。不一致时暂勿运行。

## 第一次连接

1. 接入需要认证的校园网络，退出官方客户端及其他认证客户端。
2. 打开 Sidravia，进入「设置 → 连接配置」。
3. 选择「吉林大学」，填写校园网账号和密码。网卡通常保留「自动选择可用网卡」；电脑有多个网络接口时，可按需指定校园网络对应的网卡与 IPv4 地址。
4. 点击「保存修改」，在确认窗口选择「保存并连接」。
5. 等待界面显示认证成功，再打开网页确认网络可用。若认证失败，或认证成功后仍无法访问网页，请参阅[常见问题](troubleshooting.md)。

**密码会以明文保存在本地，未加密，依靠文件权限保护。** 配置文件及其备份请妥善保管，不要公开上传。出现存储保护提示时，请先阅读[密码保存说明](privacy.md#账号与密码)，了解后再决定是否继续保存。

## 以后怎么用

首次手动连接成功后，可按需在设置中开启：

- **启动时自动登录**：后台启动并取得网络状态后，使用保存的配置自动认证；开启开关不会立即发起连接。
- **自动重连**：断线或发生可恢复错误后自动重试。修改在下次连接时生效；需要立即应用时，断开后重新连接。
- **自动启动**：在「启动与窗口行为」中选择「图形界面和后台」或「仅后台」。图形界面启动时是否隐藏，由「图形界面启动时隐藏到托盘」决定。如需后台自动认证，还应开启「启动时自动登录」，并关闭其他认证客户端的自启。

默认点击关闭按钮会隐藏到托盘，连接继续保持。可通过托盘菜单重新打开窗口；需要断开并退出时，选择「退出 Sidravia」。

更多设置见[使用指南](guide.md)，连接或网页访问异常时见[常见问题](troubleshooting.md)。
