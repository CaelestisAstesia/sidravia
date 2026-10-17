# Sidravia

**网络接入认证客户端**

Sidravia 提供图形界面和命令行工具，支持保存账号、自动登录、断线重连和托盘运行。

目前支持 **吉林大学 Dr.Com 5.2.0 D 协议认证**，以 Windows 为主要平台。其他网络环境和认证协议需要单独适配。

[下载](https://github.com/CaelestisAstesia/sidravia/releases) · [快速开始](docs/quick-start.md) · [使用指南](docs/guide.md)

## 使用

下载包含图形界面的 Windows x64 安装包或便携包。打开 Sidravia，在「设置 → 连接配置」填写账号和密码，保存并连接。

默认关闭窗口会隐藏到托盘，连接继续保持。需要自动连接时，在设置里开启自动登录和自动重连。

当前开发版本为 `0.3.0-alpha.1`。下载前请查看对应版本的发行说明。Linux amd64 可构建命令行核心包，暂不提供图形界面。

## 文档

- [快速开始](docs/quick-start.md)：安装与首次连接。
- [使用指南](docs/guide.md)：连接设置、托盘、自启和公告。
- [常见问题](docs/troubleshooting.md)：连不上、断线与问题反馈。
- [数据与隐私](docs/privacy.md)：密码保存位置、日志和卸载数据。
- [高级使用](docs/advance.md)：ctl 命令、配置管理、诊断与脚本。
- [协议上报覆写](docs/override.md)：字段格式、保存和清除覆写。

开发相关：[构建与测试](docs/development.md) · [架构](docs/architecture.md) · [协议适配](docs/protocols.md)

## 反馈与贡献

欢迎在 [Issues](https://github.com/CaelestisAstesia/sidravia/issues) 反馈问题，请附上版本、复现步骤和错误提示。诊断信息的获取方法见[反馈说明](docs/troubleshooting.md#反馈问题)。分享前请遮住账号，不要上传密码、账号配置文件或原始认证日志。

项目采用 [GNU AGPL v3](LICENSE)。感谢 [drcom-generic](https://github.com/drcoms/drcom-generic) 的协议研究与公开资料，其他资源见[致谢](docs/acknowledgements.md)。
