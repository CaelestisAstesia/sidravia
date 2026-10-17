# Sidravia

**网络接入认证客户端**

Sidravia 用于完成网络接入认证，并维持认证连接。通过图形界面保存账号、连接网络，按需开启自动登录和断线重连；也提供命令行工具，方便需要时进一步查看和控制连接。

[下载 Sidravia](https://github.com/CaelestisAstesia/sidravia/releases) · [快速开始](docs/quick-start.md) · [使用指南](docs/guide.md) · [高级使用](docs/advance.md)

当前支持 **吉林大学 Dr.Com 5.2.0 D 协议认证**，主要面向 Windows 用户。当前开发版本为 `0.3.0-alpha.1`，仍处于 Alpha 阶段；实际下载文件、已知问题与升级限制以所选版本的发行说明为准。

## 日常使用

- 保存账号与认证配置，手动连接或断开。
- 按需启用启动时自动登录和断线重连。
- 隐藏到托盘，在后台维持连接。
- 查看连接状态、选择网卡、检查网络诊断。
- 查看学校公告，调整通知与窗口行为。

首次使用，在 Releases 选择包含 GUI 的 Windows 安装包或便携包，打开 Sidravia，在「设置 → 连接配置」填写账号和密码，保存并连接。完整步骤见[快速开始](docs/quick-start.md)。

## 选择使用方式

**图形界面（GUI）面向日常使用。** 当前围绕一份保存的连接配置工作，常规连接不需要命令行。

**命令行（ctl）面向高级操作。** 提供多配置管理、一次性认证、按 ID 管理会话、协议上报覆写和脚本入口。两种运行模式互斥，切换前需要退出原模式；高级操作也可能影响 GUI 可管理的配置范围。详见[高级使用](docs/advance.md)。

## 支持范围

| 平台 | 当前范围 |
| --- | --- |
| Windows x64 | GUI、命令行、后台核心与安装管理，具备安装包和便携包构建能力 |
| Linux amd64 | 命令行与后台核心，具备便携归档构建能力；不含 GUI 或 Windows 集成 |
| 其他平台 | 尚未提供可用发行支持 |

当前随程序提供吉林大学 `jlu` 配置，协议 ID 为 `drcom-5.2.0-d`。其他网络即使也使用 Dr.Com，仍可能需要单独适配。产品架构不限定学校或协议，当前支持范围见[协议支持与适配](docs/protocols.md)。

## 文档

| 日常使用 | 内容 |
| --- | --- |
| [快速开始](docs/quick-start.md) | 选择下载文件、安装和首次连接 |
| [使用指南](docs/guide.md) | 连接配置、自动连接、托盘、公告与设置 |
| [常见问题](docs/troubleshooting.md) | 连接排障、模式冲突与问题反馈 |
| [数据与隐私](docs/privacy.md) | 密码存储、数据位置、日志与移除 |

| 高级使用与开发 | 内容 |
| --- | --- |
| [高级使用](docs/advance.md) | ctl 命令、配置与会话管理、诊断、脚本和系统集成 |
| [协议上报覆写](docs/override.md) | override 格式、支持字段、保存及恢复默认 |
| [协议支持与适配](docs/protocols.md) | 当前协议、机构配置与适配要求 |
| [架构概览](docs/architecture.md) | 程序组成、状态归属与生命周期 |
| [开发指南](docs/development.md) | 工具版本、验证、编译与打包 |
| [许可证与致谢](docs/acknowledgements.md) | 协议参考、字体和第三方依赖 |

## 反馈与参与

问题请提交到 [Issues](https://github.com/CaelestisAstesia/sidravia/issues)，附上版本、使用模式、复现步骤与实际提示。如何获取诊断信息见[反馈说明](docs/troubleshooting.md#反馈问题)。不要提交密码、账号配置文件或未经检查的原始日志。

欢迎贡献问题复现、文档修正和协议适配。开始前请阅读[开发指南](docs/development.md)。项目采用 [GNU AGPL v3](LICENSE)。
