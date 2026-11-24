# Sidravia

**网络接入认证客户端**

Sidravia 是一个开源的网络认证客户端，提供图形界面和命令行工具，支持登录自启、自动认证、断线重连和日志诊断。

目前支持 **吉林大学 Dr.Com 5.2.0 D 协议认证**，欢迎更多开发者贡献其他学校和认证协议的适配。

Sidravia 使用共享的 Go 认证核心与 Flutter 桌面界面，面向 Windows 和 Linux。Linux GUI 当前适配 Fedora 45 x86_64、KDE Plasma / Wayland。

[下载](https://github.com/CaelestisAstesia/sidravia/releases) · [快速开始](docs/quick-start.md) · [使用指南](docs/guide.md) · [变更日志](CHANGELOG.md)

## 使用

当前公开预发布版为 **[0.5.0-alpha.2](https://github.com/CaelestisAstesia/sidravia/releases/tag/v0.5.0-alpha.2)**。下表对应此版本。下载后按[快速开始](docs/quick-start.md)安装。当前源码新增的功能及构建方式见[开发指南](docs/development.md)。

### 选择下载包

| 系统与架构 | 图形界面 | 仅核心（命令行） |
| --- | --- | --- |
| Windows x86-64 | 安装版、GUI 便携 ZIP | 安装版取消 GUI 组件，或核心便携 ZIP |
| Windows ARM64 | 暂不提供 | 核心便携 ZIP |
| Linux x86-64 | GUI 便携 tar.gz、Fedora RPM | 核心便携 tar.gz、Fedora 核心 RPM |
| Linux ARM64 | 暂不提供 | 核心便携 tar.gz |

`amd64`、`x86_64` 和 x64 都表示 x86-64，Intel 与 AMD 的 64 位电脑均适用；当前没有 32 位 x86 包。ARM64 核心包尚待实机验证。

**GUI 包包含界面和核心**，适合日常桌面使用。核心包通过 `sidraviactl` 使用，适合终端和脚本。安装版由安装器管理快捷方式、升级和卸载；便携版完整解压后运行；RPM 交给 Fedora 包管理器管理。

预发布版本可能调整配置格式和命令。升级前请阅读发行说明并备份配置。

## 文档

- [快速开始](docs/quick-start.md)：安装与首次连接。
- [Linux 便携版](docs/linux-portable.md) · [Fedora RPM](docs/linux-rpm.md)：运行依赖、系统集成和包管理。
- [使用指南](docs/guide.md)：连接设置、托盘、自启和公告。
- [常见问题](docs/troubleshooting.md)：连不上、断线与问题反馈。
- [数据与隐私](docs/privacy.md)：密码保存位置、日志和卸载数据。
- [命令行高级使用](docs/advance.md)：配置管理、认证操作、诊断与脚本。
- [Windows 维护工具](docs/manager.md)：用户 PATH、开机自启及安装器职责边界。
- [协议上报覆写](docs/override.md)：字段格式、保存和清除覆写。

开发相关：[构建与测试](docs/development.md) · [架构](docs/architecture.md) · [协议适配](docs/protocols.md)

## 反馈与贡献

欢迎在 [Issues](https://github.com/CaelestisAstesia/sidravia/issues) 反馈问题或提出建议。报告故障时，请附上版本、操作系统、使用方式、复现步骤和错误提示；诊断信息的获取方法见[反馈说明](docs/troubleshooting.md#反馈问题)。分享前请遮住账号，不要上传密码、账号配置文件或原始认证日志。

也欢迎通过 Pull Request 贡献代码、文档和协议适配。参与开发前，可先阅读[构建与测试](docs/development.md)和[架构说明](docs/architecture.md)。

项目采用 [GNU AGPL v3](LICENSE) 开源许可证。

感谢 [drcom-generic](https://github.com/drcoms/drcom-generic) 项目及其贡献者公开的 Dr.Com 协议研究与适配资料。字体及第三方依赖的许可信息见[许可证与致谢](docs/acknowledgements.md)。
