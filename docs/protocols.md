# 协议支持与适配

## 当前支持

| 协议 ID | 内置 Profile | 当前范围 |
| --- | --- | --- |
| `drcom-5.2.0-d` | `jlu`（吉林大学） | 吉林大学 Dr.Com 5.2.0 D 协议认证 |

不同学校或部署即使使用相同的认证产品，也可能有不同协议、字段与服务器参数。当前吉林大学配置不能直接用于声明其他 Dr.Com 网络兼容。

Sidravia 的架构不限定于校园网或 Dr.Com，但目前没有自动识别任意协议或下载插件即可兼容的功能。实际发行支持和已知问题以对应版本说明为准。

## Profile、协议实现与 override

**机构 Profile** 提供协议 ID、服务器和环境参数，可选提供公告入口，不包含用户账号密码。当前随版本打包，没有面向普通用户的任意导入入口。相同协议的新环境，确认协议行为一致后可能只需要新 Profile。

**协议实现** 包含输入校验、报文编解码、认证、保活与退出。新协议或不兼容变体需要新的 Factory 与 Run，不能只修改名称或复制配置就宣称适配。

**Override** 通过 CTL 覆写已有协议允许的上报字段与兼容参数，不改变实际 socket 源地址、受信服务器或运行策略。它不能替代协议适配，完整格式见[协议上报覆写](override.md)。

## 开发入口

| 入口 | 内容 |
| --- | --- |
| [协议契约](../internal/daemon/authentication/protocol/protocol.go) | Factory、Run、输入与观察接口 |
| [协议注册](../internal/daemon/authentication/protocol/registry.go) | 按协议 ID 选择实现 |
| [失败契约](../internal/daemon/authentication/protocol/failure.go) | 错误码、描述与恢复建议 |
| [D520 实现](../internal/daemon/authentication/protocol/drcom/d520) | 当前编解码与执行流程 |
| [吉林大学 Profile](../internal/daemon/configuration/profiles/jlu.json) | 当前机构参数 |
| [后台组合](../cmd/sidraviad/main.go) | 注册并组合实际实现 |

现有契约基于当前真实需求。未来协议如确有不同输入或平台能力，需要相应调整契约，不能假定现有字段足以覆盖全部协议。

## 适配与验证

1. 确认版本、报文、编码、校验、错误含义、保活与退出的可靠来源，保留未知项。
2. 在持久化或运行前校验相应输入，确保协议执行可以取消、等待和清理，预期失败返回错误。
3. 用确定性向量和测试覆盖协议规则、异常响应、超时及取消，不把实现自身输出当作唯一正确依据。
4. 核对目标环境的服务器、端口与真实本机绑定，区分实际发送地址和报文上报地址。
5. 在获准环境验证认证、持续连接、恢复与退出，分别记录自动测试和现场结果。

本地模拟工具见 [D520 mock server](../tools/drcom520d_mock_server/README.md)。模拟通过与交叉编译不代表真实网络兼容。测试数据、日志和贡献材料不得包含密码、认证材料或未处理的敏感报文。

构建入口见[开发指南](development.md)，状态归属见[架构概览](architecture.md)。
