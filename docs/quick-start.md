# 快速开始

以下步骤适用于吉林大学 Dr.Com 5.2.0 D 认证环境。只用命令行的话，直接看[高级使用](advance.md)。

## 下载与安装

到 [Releases](https://github.com/CaelestisAstesia/sidravia/releases) 下载包含图形界面的 Windows x64 包。

- **安装版**（`…-setup.exe`）：运行安装器，选择图形界面组件。自启、PATH 和桌面快捷方式按需勾选。
- **便携版**（`…-portable.zip`）：完整解压到一个固定、可写的目录，运行 `Sidravia.exe`。保留 `sidravia.portable` 文件，不要在 ZIP 内运行或只复制 EXE。

两种版本的数据位置不同，不会自动迁移。不要把安装版覆盖到便携目录。保存位置见[数据与隐私](privacy.md#数据位置)。

需要校验下载文件时，在 PowerShell 中计算哈希，与同次发行的 `SHA256SUMS.txt` 对照。文件名替换为实际下载的名称：

```powershell
Get-FileHash .\sidravia-v0.3.0-alpha.1-windows-amd64-portable.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
```

## 第一次连接

1. 退出其他认证客户端，接入需要认证的网络。
2. 打开 Sidravia，进入「设置 → 连接配置」。
3. 选择吉林大学，填写账号和密码。网卡先用自动选择；选错时再指定网卡与 IPv4。
4. 点击「保存修改」，选择「保存并连接」。
5. 等待认证成功，然后打开网页试试。

密码会以明文保存在本地，依靠文件权限保护。出现存储保护提示时，请先看[密码保存说明](privacy.md#账号与密码)。

## 以后怎么用

手动连接成功后，可以在设置中开启「启动时自动登录」和「自动重连」。想登录 Windows 后自动启动，再把「开机自启」设为「启动到托盘」，同时关闭其他认证客户端的自启。

默认点关闭按钮会隐藏到托盘，连接不会断开。要退出并断开连接，在托盘菜单选择「退出 Sidravia」。

更多设置见[使用指南](guide.md)，连接失败见[常见问题](troubleshooting.md)。
