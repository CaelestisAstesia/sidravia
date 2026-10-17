# 协议上报覆写（Override）

本文面向已确认需要修改协议上报字段的高级用户。普通 GUI 连接不需要设置 override，GUI 也不提供编辑入口。执行命令前先阅读[高级使用](advance.md#与-gui-配合使用)，正常退出 GUI 并切换到独立后台。

当前支持下列 D520 字段；这是具体协议的校验规则，不能直接套用到其他协议。示例使用已存在的配置 `daily`，替换为自己的配置 ID；一次性认证示例不要求该配置存在。

## 改变什么

D520 的协议上下文覆写只改变**认证报文中上报的字段及允许的兼容参数**。它不会修改 Windows 网卡、系统 IP、MAC、DNS、路由，也不改变 socket 实际绑定的源 IPv4。

真实绑定仍由 `--interface-id` 与 `--local-ipv4` 或自动选择决定。比如 `reportedIPv4` 是报文中的上报地址，不是为系统分配地址；`reportedDNSIPv4` 也不是设置系统 DNS。

不能通过覆写改变账号密码、服务器地址与端口、本地端口策略、超时、保活间隔、重试策略或运行期的 salt、认证信息及保活尾部。未知字段会被拒绝。覆写不承诺让未适配的网络环境变得兼容。

## JSON 格式

创建一个 UTF-8、无 BOM 的 JSON 文件，例如 `d520-override.json`：

```json
{
  "schemaVersion": 1,
  "hostName": "Sidravia-PC"
}
```

这里的主机名只是格式示例。按目标环境已确认的要求选择字段，省略的字段继续取 Profile 或实际网络与主机信息；不需要为了使用覆写而填满整张字段表。

规则如下：

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

DNS 与 DHCP 地址也拒绝组播、`255.255.255.255`，允许 `0.0.0.0`。字段名区分大小写。MAC 示例仅展示语法，不是建议的认证值。

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

这些字段覆写 Profile 中对应的兼容输入。固定握手常量、长度、算法和其他报文布局仍由协议实现决定，不能借此切换到另一种协议。

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

也可以在 `config create` 时加 `--protocol-override-file`。读取的是执行命令时的文件内容，不是长期监听文件路径；之后修改文件不会自动更新配置或已创建的会话。

再次传文件会**整体替换覆写对象，不逐字段合并**。要保留原有某个覆写字段，需在新文件中继续提供。不传该选项的更新则保留原覆写。

清除所有持久覆写，恢复 Profile 和实际主机信息作为相应输入：

```powershell
.\sidraviactl.exe config update daily --clear-protocol-override
.\sidraviactl.exe auth start --config daily
```

保存的覆写内容实际改变后，关联旧会话会结束，不会自动重新连接；逐字节相同的覆写重复提交，或清除本就不存在的覆写，不会因此结束会话。比较包含 JSON 原始表示，重新排版或调整字段顺序也可能被视为变更。应先检查命令结果，再按需连接。

`--clear-protocol-override` 与 `--protocol-override-file` 互斥。不支持用空路径、重复传入文件选项或 `--clear-protocol-override=false` 表达保留。当前 `config show` 的人类可读输出不展示覆写内容，不能据此判断是否存在覆写；请保留自己使用的覆写文件与命令记录。覆写中可能包含主机、MAC、IP 等信息，分享前也应检查。

## 失败时如何处理

文件无法读取、不是严格 JSON 对象或超过大小上限时，ctl 会拒绝读取；协议不接受的字段、类型或内容由后台校验。错误信息不会回显完整覆写对象。先检查文件编码、字段拼写、版本和字段表中的长度要求。

校验阶段被拒绝的输入不会替换原配置。命令返回失败时，需要区分以下情况：

- **明确未保存**：按错误提示修正输入或存储问题，再重试。
- **配置已提交、旧会话清理失败**：新设置已经保存。先查询并处理旧会话，再显式连接，不要当作保存失败而反复提交。
- **通信中断、结果未知**：先恢复通信并核对目标配置与会话。`config show` 和公开查询不返回原始覆写，无法仅凭查询确认覆写是否已保存；本地文件和操作记录也不能证明后台已接收。

结果未知时，如需确保使用某份明确的覆写，可以在确认目标配置、接受关联连接可能中断后，重新提交同一文件；希望恢复默认则重新提交清除选项。等待命令明确成功，再按需连接。文件内容须与希望采用的设置一致，不能只凭文件名判断。

清除覆写只恢复相应字段的默认来源，不会迁移配置，也不改写系统 IP、MAC 或 DNS。

继续阅读[高级使用](advance.md)、[协议支持与适配](protocols.md)或[实际校验实现](../internal/daemon/authentication/protocol/drcom/d520/override.go)。
