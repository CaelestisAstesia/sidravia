# 协议支持与适配

本文描述当前源码内置实现，实际发行范围以对应版本说明为准。

## 当前支持

| 协议 ID | 内置 Profile | 当前范围 |
| --- | --- | --- |
| `drcom-5.2.0-d` | `jlu`（吉林大学） | 原有 D520 行为 |
| `drcom-5.2.0-d` | `jlu-experimental`（吉林大学，实验性） | 显式选择的实验行为，真实校园兼容性与长期稳定性仍待验证 |

两个 Profile 共用一个协议 ID 和 Factory。`jlu-experimental` 通过机构参数 `behavior: "jlu-experimental"` 选择已实现的通信与恢复分支；`jlu` 保持原行为。个人 override 不能切换该参数。Profile 在后台启动时读取，修改源码或磁盘文件不会热切换正在运行的会话。

不同学校或部署即使使用相同的认证产品，也可能有不同协议、字段与服务器参数。当前吉林大学配置不能直接用于声明其他 Dr.Com 网络兼容。

Sidravia 的架构不限定于校园网或 Dr.Com，但目前没有自动识别任意协议或下载插件即可兼容的功能。实际发行支持和已知问题以对应版本说明为准。

## Profile、协议实现与 override

**机构 Profile** 提供协议 ID、服务器和环境参数，可选提供公告入口，不包含用户账号密码。当前随版本打包，没有面向普通用户的任意导入入口。相同协议的新环境，确认协议行为一致后可能只需要新 Profile。

**协议实现** 包含输入校验、报文编解码、认证、保活与退出。新协议或不兼容变体需要在 Factory / Run 中明确实现并验证差异，不能只修改名称或复制配置就宣称适配。

**Override** 保存在连接配置中，可用 GUI 或 CTL 编辑已有协议允许的上报字段与兼容参数。实际 socket 源地址、受信服务器和运行策略仍由各自模块决定。完整格式见[协议上报覆写](override.md)。

## 账户统计与服务器消息

当前实验性 Profile 提供可选的登录统计和服务器文字通知。历史计费流量与余额来自服务器登录快照，不是本机实时流量或实时余额；缺失值与零值区分。当前文字通知仅接收校验通过的 ASCII 纯文本子集；非 ASCII 编码和已识别的命令、弹窗类型会忽略，不保证展示任意中文服务器消息。服务器文字是数据，不作为命令执行。普通 Profile 不因共用协议 ID 就自动具备这些能力。

协议通过可选业务观察接口报告数据，Session 负责 Run 代次过滤与保留快照，再由 IPC 传给客户端；这条数据链路不接管认证意图、绑定或重连策略。

## 开发入口

| 入口 | 内容 |
| --- | --- |
| [协议契约](../internal/connection/auth/protocol/protocol.go) | Factory、Run、输入与观察接口 |
| [协议注册](../internal/connection/auth/protocol/registry.go) | 按协议 ID 选择实现 |
| [失败契约](../internal/connection/auth/protocol/failure.go) | 错误码、描述与恢复建议 |
| [D520 实现](../internal/connection/auth/d520/driver) | 当前编解码与执行流程 |
| [吉林大学 Profile](../internal/connection/profile/data/jlu.json) / [实验性 Profile](../internal/connection/profile/data/jlu-experimental.json) | 机构参数与显式行为选择 |
| [业务输出契约](../internal/connection/auth/protocol/business.go) / [会话业务快照](../internal/daemon/internal/session/business.go) | 登录统计和文字通知的可选链路 |
| [后台组合](../internal/daemon/runtime.go) | 注册并组合实际实现 |

现有契约基于当前真实需求。未来协议如确有不同输入或平台能力，需要相应调整契约，不能假定现有字段足以覆盖全部协议。

## 适配与验证

1. 确认版本、报文、编码、校验、错误含义、保活与退出的可靠来源，保留未知项。
2. 在持久化或运行前校验相应输入，确保协议执行可以取消、等待和清理，预期失败返回错误。
3. 用确定性向量和测试覆盖协议规则、异常响应、超时及取消，不把实现自身输出当作唯一正确依据。
4. 核对目标环境的服务器、端口与真实本机绑定，区分实际发送地址和报文上报地址。
5. 在获准环境验证认证、持续连接、恢复与退出，分别记录自动测试和现场结果。

本地模拟工具见 [D520 mock server](../tools/drcom520d_mock_server/README.md)。协议兼容性还需校园网络验证。测试数据、日志和贡献材料不得包含密码、认证材料或未处理的敏感报文。

构建入口见[开发指南](development.md)，状态归属见[架构概览](architecture.md)。
