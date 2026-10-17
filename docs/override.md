# 协议上报覆写（Override）

Sidravia 的 D520 协议实现允许通过 `sidraviactl` 指定部分**认证报文上报字段**与兼容参数。这是面向已知网络环境的高级功能，通常连接网络无需调整这些设置。**GUI 目前没有覆写编辑入口**；如需使用 ctl，请先阅读[命令行与高级使用](advance.md#与-gui-配合使用)。

## 适用范围与边界

覆写文件中的设置仅影响协议允许的输入，例如向服务器报告的主机名、部分 IP/MAC 字段或 D520 兼容常量。它**不会修改** Windows/Linux 的真实网卡配置、IP、MAC、DNS、路由，也不会改变 UDP socket 的实际源地址。

以下内容**不能**通过 Override 改变：账号密码、受信认证服务器及端口、固定本地端口策略、协议算法、超时、心跳与重试策略，以及执行期间生成的认证材料、盐值或保活尾部。`reportedIPv4` 等字段不是 `--interface-id` / `--local-ipv4` 的替代品。

覆写只适用于实现明确支持的协议字段；**不能据此宣称 Sidravia 已兼容其他学校或不同的 Dr.Com 变体**。

## 文件格式

新建一个 UTF-8 **无 BOM** 的 JSON 文件，例如 `d520-override.json`：

```json
{
  "schemaVersion": 1,
  "hostName": "Sidravia-PC"
}
```

未填写的字段继续采用机构 Profile 或实际网络、主机信息。文件约束：

- 最多 **16,384 字节**；必须是一个完整 JSON 对象，不允许注释、重复键、未知字段或尾随文本。
- **非空对象**需要整数 `"schemaVersion": 1`；空对象 `{}` 合法，表示没有覆写字段。
- 不接受 `null` 作为字段值。要清除已保存的覆写，请使用专门的命令选项。
- 字段名区分大小写；十六进制字段只能使用连续、**小写**的十六进制字符，不带 `0x`、空格或分隔符。

### 网络与主机上报字段

| JSON 字段 | 类型 | 约束 |
| --- | --- | --- |
| `reportedIPv4` | 字符串 | 规范 IPv4；不得为 `0.0.0.0`、组播或 `255.255.255.255` |
| `reportedMAC` | 字符串 | 六字节、小写冒号分隔，如 `02:00:00:00:00:01`；不得全为零 |
| `reportedDNSIPv4` | 字符串数组 | 0–2 个规范 IPv4；不足两项的槽位补零 |
| `reportedDHCPIPv4` | 字符串 | 规范 IPv4；允许 `0.0.0.0` |
| `hostName` | 字符串 | 严格 GBK 编码后不超过 32 字节，无 NUL |
| `osFamily` | 字符串 | 严格 GBK 编码后不超过 32 字节；无 NUL、首尾空白 |
| `osRelease` | 字符串 | 与 `osFamily` 同样的约束 |

DNS 和 DHCP 字段还拒绝组播及 `255.255.255.255`，但允许 `0.0.0.0`。`reportedDNSIPv4: []` 将两个上报 DNS 槽位都清零。

`osFamily` 与 `osRelease` 的非空部分以一个空格拼接，上报的组合文本同样不得超过 **32 GBK 字节**。仅覆写其中之一时，另一部分仍来自实际主机信息，因此创建认证时还会校验组合结果。**JSON 文件本身是 UTF-8；GBK 限制针对报文编码，而不是文件编码。**

### 协议兼容字段

这些字段按固定字节数替换 Profile 提供的对应兼容值：

| 字段 | 字节数 | 小写十六进制字符数 |
| --- | ---: | ---: |
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

请勿凭猜测修改兼容常量。它们不会改变固定握手布局或协议算法，错误设置可能导致认证失败。

## 一次性使用覆写

在 headless 后台中发起一次认证，并读取指定文件：

```powershell
.\sidraviactl.exe auth start --profile jlu --username '<账号>' --protocol-override-file .\d520-override.json
```

密码仍经交互式隐藏输入，也可使用 `--password-stdin`。一次性认证的覆写只供创建的 Session 使用，**不保存为持久配置**。该选项不可与 `auth start --config` 或 `auth start --session` 同时使用。

## 保存、替换与清除覆写

以下用 `daily` 代表一份已存在的 Configuration ID；请替换为自己的 ID。

保存到已有配置：

```powershell
.\sidraviactl.exe config update daily --protocol-override-file .\d520-override.json
.\sidraviactl.exe config show daily
.\sidraviactl.exe auth start --config daily
```

`config create` 也支持 `--protocol-override-file`。程序保存的是**提交时的文件内容**，不是文件路径的动态引用；之后修改本地文件不会自动更新配置。再次提交会**整体替换**旧覆写，需要保留的字段必须全部包含在新文件里。

清除该配置上的持久覆写，使未覆盖的字段重新采用默认 Profile 或实际主机信息：

```powershell
.\sidraviactl.exe config update daily --clear-protocol-override
.\sidraviactl.exe auth start --config daily
```

`--clear-protocol-override` 和 `--protocol-override-file` 互斥；不能使用空路径、重复覆写选项或 `--clear-protocol-override=false`。

**更改已保存的覆写会影响运行中的会话。** 覆写内容实际发生变化后，关联的旧 Session 会被结束，程序不会因此自动重新认证。逐字节相同的 JSON 再次提交或清除原本不存在的覆写，通常不会触发这种结束；仅改变字段顺序、空白或排版，也可能被视作内容变更。先确认结果，再决定是否执行 `auth start --config`。

`config show` 不会回显原始覆写内容；请自行保管所用文件。覆写文件可能含有主机名、MAC 和 IP 等可识别信息，分享前应检查。

## 如果更新报错或结果未知

先检查 JSON 编码、字段拼写、长度与大小写。ctl 会检查输入文件格式，后台会校验具体协议字段；**输入校验失败不会替换原配置**。

对其他失败，要区分以下情况：

1. **确认未保存：** 修改输入或文件权限后重试。
2. **配置已提交，旧会话清理失败：** 新配置已经保存；先处理旧会话，再决定是否连接，**不要当成保存失败连续提交**。
3. **通信中断、结果未知：** 先恢复通信，核对配置和会话。`config show` 不展示原始覆写，因此不能仅凭该命令判断特定 JSON 是否已被保存。只有确认目标配置、接受可能中断当前连接后，才考虑重新提交明确的文件或清除操作。

实现与校验规则可参阅 [D520 override 源码](../internal/daemon/authentication/protocol/drcom/d520/override.go)。

返回：[命令行与高级使用](advance.md) · [协议支持与适配](protocols.md) · [数据与隐私](privacy.md)
