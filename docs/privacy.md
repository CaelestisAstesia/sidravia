# 数据与隐私

## 账号与密码

账号、密码和连接设置保存在本地 `configurations.json`。**密码以明文保存，没有加密。**

Windows 通过文件权限限制到当前用户与 LocalSystem，Linux 使用用户私有权限。能以当前用户或更高权限运行的程序仍可能读取密码。保护无法建立时，Sidravia 会要求确认后再保存。

备份、复制或同步配置时，请按密码文件保管。ctl 的交互密码不回显，脚本用 `--password-stdin` 输入，不要把密码写进命令行或公开脚本。

## 数据位置

常规 Windows 数据目录如下；开发和测试命名空间另行隔离。

| 数据 | 安装版 | 便携版 |
| --- | --- | --- |
| 账号配置 | `%APPDATA%\Sidravia\configurations.json` | `<程序目录>\config\configurations.json` |
| GUI 设置 | `%APPDATA%\Sidravia\gui-settings.json` | `<程序目录>\config\gui-settings.json` |
| 后台运行信息 | `%LOCALAPPDATA%\Sidravia\runtime.json` | `<程序目录>\runtime\runtime.json` |
| 后台日志 | `%LOCALAPPDATA%\Sidravia\logs\` | `<程序目录>\logs\` |

`institution-profiles` 是随版本提供的机构参数，不含账号密码。GUI 设置也不保存密码。

程序目录中的 `sidravia.portable` 文件决定是否采用便携布局。添加或删除它只会改变数据位置，不会迁移数据。Linux 便携包也把配置、运行信息和日志放在程序目录。

学校公告的 WebView2 缓存例外：两种 Windows 版本都保存在 `%LOCALAPPDATA%\Sidravia\announcement-webview\production\`，卸载或删除便携目录时不会清理它。

## 网络通信

D520 向认证服务器发送协议要求的账号、认证、主机和网络信息，不能视为加密传输。GUI 与后台通过本机回环 WebSocket 通信。

公告窗口加载学校原网页，学校网站会收到普通网页请求。当前吉林大学公告地址使用 HTTP。

## 日志与诊断

TRACE 会记录完整协议报文，可能包含账号和认证信息。排障结束后恢复原日志等级，重启核心使其生效。

GUI 复制的诊断摘要和 ctl 的 `diagnostics export` 不含密码或原始报文。分享前仍需检查截图、摘要和附加材料，不要直接上传整个数据目录。覆写文件中的主机名、MAC 和 IP 也可能暴露个人信息。

## 备份与卸载

备份前退出程序。需要保留账号时备份 `configurations.json`，无需备份临时运行信息。

安装版卸载默认保留数据，也可以选择删除账号配置、GUI 设置和日志。删除便携目录会连同其中的数据一起删除。移动或移除前先撤销自启与 PATH，具体步骤见[使用指南](guide.md#更新移动与卸载)。
