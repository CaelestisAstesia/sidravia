# Sidravia

**网络接入认证客户端**

Sidravia 旨在提供一个开源的第三方网络认证客户端，追求比官方认证器更稳定、现代、高效的使用体验。项目包含图形界面与命令行工具，支持开机自启、自动登录、连接保持与断线恢复，以及安静的后台运行，并为开发者和高级用户提供诊断与日志工具。

目前支持 **吉林大学 Dr.Com 5.2.0 D 协议认证**，欢迎更多开发者贡献其他学校和认证协议的适配。

Sidravia 的核心采用跨平台设计，注重运行效率；图形界面以 Windows 为主要平台，其他平台的可用组件见发行说明。

[下载](https://github.com/CaelestisAstesia/sidravia/releases) · [快速开始](docs/quick-start.md) · [使用指南](docs/guide.md)

## 使用

从 [Releases](https://github.com/CaelestisAstesia/sidravia/releases) 下载 Windows x64 安装包或图形界面便携包。安装或完整解压后，打开 Sidravia，在「设置 → 连接配置」选择机构、填写账号和密码，然后保存并连接。

默认关闭窗口会隐藏到托盘，连接继续保持；需要退出时，在托盘菜单选择「退出 Sidravia」。如需启动后自动认证，可在设置中开启自动登录；如需断线后自动恢复，可开启自动重连。开机自启可在「启动与窗口行为」中设置。

当前预发布版本为 [`0.3.0-alpha.1`](https://github.com/CaelestisAstesia/sidravia/releases/tag/v0.3.0-alpha.1)。Windows x64 提供安装程序、图形界面便携包和核心便携包；Linux x64 及 Windows/Linux ARM64 提供核心便携包。ARM64 包为实验性构建，尚未经实机测试。下载前请查看发行说明中的平台限制和升级指南。

**预发布（Pre-release）版本之间可能引入不兼容的改动，配置格式、命令参数或使用流程可能发生变化。升级前务必阅读目标版本的发行说明并备份配置，不要假定旧版配置可直接沿用，或新版保存的数据可直接用于回退。**

## 文档

- [快速开始](docs/quick-start.md)：安装与首次连接。
- [使用指南](docs/guide.md)：连接设置、托盘、自启和公告。
- [常见问题](docs/troubleshooting.md)：连不上、断线与问题反馈。
- [数据与隐私](docs/privacy.md)：密码保存位置、日志和卸载数据。
- [命令行高级使用](docs/advance.md)：配置管理、认证操作、诊断与脚本。
- [Windows 维护工具](docs/manager.md)：用户 PATH、开机自启、卸载准备与数据清理。
- [协议上报覆写](docs/override.md)：字段格式、保存和清除覆写。

开发相关：[构建与测试](docs/development.md) · [架构](docs/architecture.md) · [协议适配](docs/protocols.md)

## 反馈与贡献

欢迎在 [Issues](https://github.com/CaelestisAstesia/sidravia/issues) 反馈问题或提出建议。报告故障时，请附上版本、操作系统、使用方式、复现步骤和错误提示；诊断信息的获取方法见[反馈说明](docs/troubleshooting.md#反馈问题)。分享前请遮住账号，不要上传密码、账号配置文件或原始认证日志。

也欢迎通过 Pull Request 贡献代码、文档和协议适配。参与开发前，可先阅读[构建与测试](docs/development.md)和[架构说明](docs/architecture.md)。

项目采用 [GNU AGPL v3](LICENSE) 开源许可证。

感谢 [drcom-generic](https://github.com/drcoms/drcom-generic) 项目及其贡献者公开的 Dr.Com 协议研究与适配资料。字体及第三方依赖的许可信息见[许可证与致谢](docs/acknowledgements.md)。
