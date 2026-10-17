# 协议上报覆写（Override）

需要调整 D520 上报字段时，可以通过 ctl 传入覆写文件。GUI 没有编辑入口；使用前先[退出 GUI 并切换到 ctl](advance.md#与-gui-配合使用)。

下面的 `daily` 表示已有配置 ID，请替换成自己的配置。

## 改变什么

覆写修改认证报文中的上报字段和兼容参数，系统网卡、IP、MAC、DNS、路由及 socket 的实际源地址保持不变。

实际绑定仍由自动选择或 `--interface-id`、`--local-ipv4` 决定。`reportedIPv4`、`reportedDNSIPv4` 只影响报文上报。

不能通过覆写改变账号密码、服务器地址与端口、本地端口策略、超时、保活间隔、重试策略或运行期的 salt、认证信息及保活尾部。未知字段会被拒绝。覆写不承诺让未适配的网络环境变得兼容。

## JSON 格式

创建一个 UTF-8、无 BOM 的 JSON 文件，例如 `d520-override.json`：

```json
{
  "schemaVersion": 1,
  "hostName": "Sidravia-PC"
}
```

省略的字段继续取 Profile 或实际网络、主机信息，只填写需要调整的项。

- 文件最多 **16384 字节**，只允许一个 JSON 对象；不支持注释、重复键、额外尾随内容或未知字段。
- 非空对象必须有整数 `"schemaVersion": 1`。空对象 `{}` 合法，表示不覆写任何字段。
- 所有已提供字段均不能为 `null`。清除持久覆写使用命令选项，不能把文件写成 `null`。
- 十六进制字段使用连续小写字符，无 `0x`、空格或分隔符，长度必须精确匹配。

### 上报的网络与主机字段

| 字段 | JSON 类型 | 约束 |
| --- | --- | --- |
| `reportedIPv4` | 字符串 | 规范 IPv4；不接受 `0.0.0.0`、组播及 `255.255.255.255` |
| `reportedMAC` | 字符串 | 六字节、小写冒号分隔，如 `02:00:00:00:00:01`；不能全零 |
| `reportedDNSIPv4` | 字符串数组 | 0–2 个规范 IPv4；未填满的槽位为零，空数组使两项都为零 |
| `reportedDHCPIPv4` | 字符串 | 规范 IPv4；允许 `0.0.0.0` |
| `hostName` | 字符串 | 严格 GBK 编码后不超过 32 字节，不含 NUL |
| `osFamily` | 字符串 | 严格 GBK 编码后不超过 32 字节，不含 NUL 和首尾空白 |
| `osRelease` | 字符串 | 同上；会与系统族拼接后用于上报 |

DNS、DHCP 地址也拒绝组播与 `255.255.255.255`，允许 `0.0.0.0`。字段名区分大小写；表中 MAC 仅为格式示例。

`osFamily` 与 `osRelease` 的非空部分以一个空格连接，最终 GBK 字节数也不能超过 32。只覆写其中一项时，另一项仍取实际主机信息，因此还需在创建连接时校验组合结果。JSON 文件本身仍是 UTF-8，GBK 只用于协议报文编码。

### 协议兼容字段

| 字段 | 字节数 | 十六进制字符数 |
| --- | --- | --- |
| `authVersionHex` | 2 | 4 |
| `keepAliveVersionHex` | 2 | 4 |
| `controlCheckStatusHex` | 1 | 2 |
| `ipdogHex` | 1 | 2 |
| `adapterNumberHex` | 1 | 2 |
| `osInfoHex` | 20 | 40 |
| `challengePaddingHex` | 15 | 30 |
| `loginIPDogPaddingHex` | 4 | 8 |
| `loginDHCPPaddingHex` | 8 | 16 |
| `loginAuthExtensionPaddingHex` | 2 | 4 |

这些字段替换 Profile 中对应的值。固定握手常量、报文布局和算法仍由 D520 实现决定。

## 一次性使用覆写

```powershell
.\sidraviactl.exe auth start --profile jlu --username '<账号>' --protocol-override-file .\d520-override.json
```

密码仍通过隐藏提示输入，也可使用 `--password-stdin`。本次文件内容随创建的会话使用，不写入持久配置；不能与 `auth start --config` 或 `auth start --session` 同时传入。

## 保存、替换与清除覆写

为已有配置保存覆写：

```powershell
.\sidraviactl.exe config update daily --protocol-override-file .\d520-override.json
.\sidraviactl.exe config show daily
.\sidraviactl.exe auth start --config daily
```

创建配置时也可以加 `--protocol-override-file`。程序保存当时的文件内容，之后修改文件需要重新提交。

再次提交会**整体替换覆写**。需要保留的字段也要写入新文件；更新其他配置时不传此选项，会保留原覆写。

清除所有持久覆写，恢复 Profile 和实际主机信息作为相应输入：

```powershell
.\sidraviactl.exe config update daily --clear-protocol-override
.\sidraviactl.exe auth start --config daily
```

保存的覆写内容实际改变后，关联旧会话会结束，不会自动重新连接；逐字节相同的覆写重复提交，或清除本就不存在的覆写，不会因此结束会话。比较包含 JSON 原始表示，重新排版或调整字段顺序也可能被视为变更。应先检查命令结果，再按需连接。

`--clear-protocol-override` 与 `--protocol-override-file` 互斥。不接受空路径、重复文件选项或 `--clear-protocol-override=false`。`config show` 不显示覆写内容，请自行保留使用的文件。分享时检查其中的主机名、MAC 和 IP。

## 失败时如何处理

文件报错时，检查编码、JSON 语法、字段拼写和长度。ctl 检查文件格式，后台检查具体协议字段；错误信息不回显完整内容。

校验阶段被拒绝的输入不会替换原配置。命令返回失败时，需要区分以下情况：

- **明确未保存**：按错误提示修正输入或存储问题，再重试。
- **配置已提交、旧会话清理失败**：新设置已经保存。先查询并处理旧会话，再显式连接，不要当作保存失败而反复提交。
- **通信中断、结果未知**：先恢复通信并核对目标配置与会话。`config show` 和公开查询不返回原始覆写，无法仅凭查询确认覆写是否已保存；本地文件和操作记录也不能证明后台已接收。

结果未知时，如需确保使用某份明确的覆写，可以在确认目标配置、接受关联连接可能中断后，重新提交同一文件；希望恢复默认则重新提交清除选项。等待命令明确成功，再按需连接。文件内容须与希望采用的设置一致，不能只凭文件名判断。

校验规则的源码见 [override.go](../internal/daemon/authentication/protocol/drcom/d520/override.go)。
