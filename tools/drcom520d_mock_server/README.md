# Dr.COM 5.2.0(D) 本地模拟认证服务器

这是一个只依赖 Python 标准库的、带状态的 Dr.COM 5.2.0(D) D 系列 UDP 模拟服务器，
用于在本机复现 Challenge、Login、KA1、KA2 和 Logout。默认只监听
`127.0.0.1:61440`，不会连接、代理或转发到真实校园认证服务器。

普通使用者不需要手工构造二进制报文。通常只需启动 `--example`，再让待测客户端连接
ready 文件公布的回环地址；文末的 payload/PCAP 工具仅用于协议开发和离线排障。

> **隔离边界：** 示例凭据全部虚构。不要把真实账号、密码或抓包放进本工具；不要把
> 测试客户端指向校园服务器。`--listen-host` 可以显式绑定非回环地址，但只应在完全隔离
> 的测试网络中使用，服务器也会输出中文警告。

## 一分钟快速启动

在仓库根目录打开 PowerShell：

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"
python -m drcom520d_mock_server --example
```

服务器会立即在终端实时显示中文完整追踪，并把相同事件完整写入自动创建的 UTF-8
JSONL 文件：`tools/drcom520d_mock_server/traces/时间戳.jsonl`。

## 客户端向量 fixture（仅虚构凭据）

`tools/drcom520d_mock_server/tests/fixtures/d520_client_vectors_v1.json` 是一份版本化的
D520 客户端向量，覆盖 Challenge、Login（成功与拒绝）、KA1、KA2 Type 1/3 与 Logout 的
完整 lowercase hex 报文，以及命名的 crypto 中间值（MD5-A/B/C、MAC XOR、CRC-1968、Auth
Info、KA2 Tail）。它只使用固定虚构凭据（`student-test` / `local-test-password`），
不含真实账号、密码、MAC、服务器地址、Auth Info、Tail 或抓包。

fixture 是数据，不是可执行源码，也不依赖 `参考/` 快照。配套的
`tools/drcom520d_mock_server/tests/test_client_vectors.py` 严格校验 fixture schema
（拒绝未知/缺失字段）、lowercase 规范 hex 与声明长度的自洽，并把每个客户端请求喂给
mock `ServerCore`、把每个 crypto 中间值喂给 mock `codec`，证明 fixture 与当前 mock
行为一致。该测试纯内存运行，不启动 UDP 监听，也不导入 `参考/`。

真实的 Go D520 客户端对 mock 的活线验证（loopback UDP 六阶段）待 Go D520 任务提供；
fixture 测试只保证静态报文与 mock 行为一致，不等同于活线客户端验收。

## 固定示例账户与客户端设置

唯一的示例账户配置源是
`tools/drcom520d_mock_server/examples/accounts.json`；README 中的值只是便于核对：

| 客户端字段 | 值 |
| --- | --- |
| 用户名 | `student-test` |
| 密码 | `local-test-password` |
| 服务端 | `127.0.0.1` |
| UDP 端口 | `61440`，或 ready 文件中的实际 `port` |
| 客户端上报 IPv4 | `10.0.0.2` |
| MAC | `02:00:00:00:00:01` |
| 协议 | Dr.COM `5.2.0(D)` D 系列 |

该文件明确设置 `credentials_are_fictional: true`。CLI 没有 `--password` 参数；自定义
虚构凭据也必须写入单独的 `--accounts` JSON，并在绑定端口前通过严格校验。

## 启动方式

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"

# 固定示例配置
python -B -m drcom520d_mock_server --example

# 自定义虚构账户、端口和完整追踪文件
python -B -m drcom520d_mock_server `
  --accounts .\my-fictional-accounts.json `
  --listen-host 127.0.0.1 --port 61441 `
  --trace-file .\.tmp\server-trace.jsonl

# 内置或自定义故障场景；--scenario-file 覆盖 --scenario
python -B -m drcom520d_mock_server --example --scenario busy-then-success
python -B -m drcom520d_mock_server --example `
  --scenario-file tools/drcom520d_mock_server/examples/busy_then_success.json
```

### 动态端口、ready 文件与 Logout 后退出

以下组合适合 CI、Go 子进程测试和其他无人值守 E2E。`--port 0` 让操作系统选择空闲
端口；绑定成功后，`--ready-file` 原子发布**实际端口**；成功发送 Logout ACK 后，
`--exit-after-logout` 才会让进程退出。

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"
python -B -m drcom520d_mock_server --example --port 0 `
  --ready-file .\.tmp\drcom-ready.json --exit-after-logout
```

另一个终端读取 ready 文件，把 `host`/`port` 注入待测客户端而不是写死 `61440`
（Go D520 任务提供活线客户端）：

```powershell
$ready = Get-Content -Raw -Encoding utf8 .\.tmp\drcom-ready.json |
  ConvertFrom-Json
if ($ready.status -ne "ready" -or $ready.port -eq 0) { throw "server not ready" }
# 待测客户端使用 $ready.host 与 $ready.port
```

ready JSON 包含 `schema_version`、`transcript_version`、`status`、`host`、实际
`port`、`pid`、绝对 `trace_file` 和 `scenario`。停止后同一路径被原子改写为
`status: "stopped"`，并增加 `stop_reason`；正常 Logout 为 `logout_complete`。
`server_ready` trace 的端口应与 ready 文件一致且非零。

Go 子进程测试应按以下顺序执行：启动上述 Python 命令；轮询到完整的 `ready` JSON；
把 `host`/`port` 注入 Go 客户端而不是写死 `61440`；完成六阶段并发送 Logout；等待
Python 子进程退出；最后断言 ready 变为 `stopped`、`stop_reason=logout_complete`，再读取
trace/transcript。仅发出伪造 Logout、未得到 ACK 或发送 ACK 失败都不会触发退出。

## 完整追踪、终端显示与秘密边界

完整追踪同时写往两个目标：终端以“序号 + 中文事件名 + operation + endpoint + 缩进
details”实时显示；JSONL 每个事件一行并立即 flush。每行顶层字段为：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `schema_version` | integer | trace schema，当前为 `1` |
| `transcript_version` | integer | 可提取 transcript 的版本，当前为 `1` |
| `sequence` | integer | 本进程内递增事件序号 |
| `timestamp` | string | 带时区的 ISO 8601 墙钟时间 |
| `monotonic_ns` | integer | 单调时钟纳秒值 |
| `event` | string | 如 `datagram_received`、`crypto_check`、`state_changed` |
| `operation` | string/null | `challenge/login/ka1/ka2/logout` 或 `null` |
| `endpoint` | object/null | 客户端 `ip` 与 `port` |
| `details` | object | 该事件的完整结构化细节 |

典型终端片段如下；实际 `details` 会展开全部对象：

```text
[000005] 收到 UDP 报文 operation=challenge endpoint=('127.0.0.1', 50000)
{
  "direction": "receive",
  "payload_hex": "0102...",
  "payload_length": 20,
  "transcript_version": 1
}
[000006] 报文操作已分类 operation=challenge endpoint=('127.0.0.1', 50000)
```

省略 `--trace-file` 时自动写到 `tools/drcom520d_mock_server/traces/时间戳.jsonl`；显式
指定路径时父目录会自动创建，但文件必须是新文件，防止覆盖旧证据。trace 创建失败发生
在 UDP 绑定之前；运行中写入失败是致命错误，服务器停止并以退出码 2 报告。

> **完整秘密警告：** 这不是脱敏日志。完整追踪有意包含明文密码、完整账户配置、有效
> `server_secret`、收发 raw packet/`payload_hex`、salt、MD5 输入/收到值/期望值、
> CRC、Auth Info、Tail、HMAC 输入输出以及完整状态快照。只允许虚构凭据；配置必须设置
> `credentials_are_fictional=true`，否则服务器会在创建 trace 和绑定端口之前拒绝启动。
> 不要上传、提交或长期共享 trace。

普通 `--log-level`/`--json-events` 是另一条“安全事件日志”通道，只记录启动、地址、
场景、账户名和汇总指标，不包含密码、报文、digest、Auth Info、Tail 或 server secret；
它不能替代完整追踪。

主要事件链为 `datagram_received` → `operation_classified` →
`scenario_action_selected` → `packet_parsed`/`packet_parse_failed` → `crypto_check` →
`validation_decision` → `state_changed` → `datagram_sent`；静默失败还会出现
`datagram_dropped`。Challenge/Login/KA1/KA2/KA2/Logout 的真实回环测试会检查这条链。

## accounts JSON schema

根对象必须**恰好**包含 `server` 和非空 `accounts` 数组；未知字段、缺字段、重复用户名、
错误类型和 JSON 语法错误都在绑定端口前拒绝。

### `server` 字段

| 字段 | 类型/范围 | 必填与省略规则 |
| --- | --- | --- |
| `challenge_ttl_seconds` | 有限正数 | 必填；Challenge 有效秒数 |
| `session_ttl_seconds` | 有限正数 | 必填；最后活动后的会话有效秒数 |
| `max_sessions_per_account` | integer `1..255` | 必填；账户未覆盖时的上限 |
| `auth_version_hex` | 恰好 2 byte 的 hex string | 必填；示例 `2c00` |
| `keep_alive_version_hex` | 恰好 2 byte 的 hex string | 必填；示例 `dc02` |
| `control_check_status_hex` | 恰好 1 byte 的 hex string | 必填；示例 `20` |
| `ipdog_hex` | 恰好 1 byte 的 hex string | 必填；示例 `01` |
| `initial_month_traffic_kib` | 非负 integer | 必填；Login/KA2 计费初值 |
| `initial_balance_cents` | 非负 integer | 必填；服务器计费状态初值 |
| `server_secret_hex` | `null` 或恰好 32 byte 的 hex string | 可省略/为 `null`；每次启动随机生成 32 byte；指定时可复现实验 |
| `credentials_are_fictional` | boolean | 可省略但默认为 `false`；完整追踪启动必须为 `true` |

### `accounts[]` 字段

| 字段 | 类型/范围 | 必填与省略规则 |
| --- | --- | --- |
| `username` | 非空 string | 必填；用户名必须唯一，线上按严格 GBK 解析 |
| `password` | 非空 string | 必填；MD5 计算时严格编码为 GBK |
| `enabled` | boolean | 必填；`false` 按错误密码类拒绝 |
| `frozen` | boolean | 必填 |
| `require_dhcp` | boolean | 必填；为真时上报 DHCP 不得为 `0.0.0.0` |
| `expected_ipv4` | IPv4 string 或 `null` | 可省略/为 `null`，表示不限制账户 IPv4 |
| `expected_mac` | 6-byte MAC string 或 `null` | 可省略/为 `null`；接受 `:` 或 `-` 分隔 |
| `bind_ipv4_and_mac` | boolean | 必填；为真时 IP 与 MAC 必须共同匹配 |
| `max_sessions` | integer `1..255` 或 `null` | 可省略/为 `null`；回退到服务器上限 |
| `balance_cents` | 非负 integer | 必填；`0` 触发余额不足 |

## 故障场景 schema

`--scenario-file` 的根对象只允许：必填 object `operations` 与可选 boolean
`repeat_last`（默认 `false`）。`operations` 的键只能是 `challenge`、`login`、`ka1`、
`ka2`、`logout`，值必须是非空 action 数组。每种操作独立计数；动作耗尽后默认回到
`normal`，`repeat_last=true` 则持续重复最后一项。未知字段一律拒绝。

八种 action：

| action | 附加字段与范围 | 行为/限制 |
| --- | --- | --- |
| `normal` | 无 | 正常处理 |
| `reject` | `code`: integer `0..255` | 仅 Login；直接构造线上拒绝响应 |
| `drop` | 无 | 静默丢弃，不发响应 |
| `delay` | `milliseconds`: 非负 integer | 处理前确定性延迟 |
| `truncate` | `length`: integer `0..64` | 截短正常响应 |
| `wrong_opcode` | `opcode`: integer `0..255` | 替换正常响应首字节 |
| `expire_session` | 无 | 处理前移除当前 endpoint 会话 |
| `wrong_tail` | 无 | 仅 KA2；翻转响应 Tail 的最后一字节 |

内置场景：

| 名称 | 确定性行为 |
| --- | --- |
| `normal` | 全部正常 |
| `busy-then-success` | Login 前两次返回 `0x02`，第三次正常；KA1 第一次正常、之后 drop；重复最后动作 |
| `flaky-login` | 第一次 Login drop，第二次恢复正常 |
| `keepalive-timeout` | KA1 始终 drop |
| `session-expired` | 每次 KA1 前使会话过期，因此静默失败 |
| `malformed-response` | 每次 KA2 返回错误 Tail |

## 协议流程与关键偏移

所有表使用从 0 开始的半开区间 `[start,end)`。

1. **Challenge**：请求以 `01 02` 开始且至少 20 byte；服务端创建 4-byte salt，返回
   16 byte `0x02` 响应，salt 位于 `[4,8)`，客户端源 IPv4 位于 `[8,12)`。
2. **Login**：必须使用同一 UDP endpoint 的有效 Challenge，发送严格 330-byte Login；
   服务器依既定优先级验证并返回 64-byte `0x04` 成功包，Auth Info 在 `[23,39)`；明确
   账户错误返回 32-byte `0x05` 包，错误码在 byte `4`。
3. **KA1**：发送 38 或 42 byte 保活，校验 MD5-A 与 Auth Info，成功返回 20 byte
   `0x07`。
4. **KA2**：首包为 serial 0、Type 1、version `0f27`、零 Tail；随后使用返回 Tail 和
   `dc02` 版本。允许启动序列 `1-3` 或 `1-1-3`，之后 Type 1/3 交替且 serial 递增；
   完全相同的重传返回缓存响应且不重复推进状态。成功响应 60 byte，Tail `[16,20)`，
   计费字段 `[44,60)` 是四个 little-endian uint32。
5. **Logout**：同 endpoint 发送严格 80-byte 登出包；MD5、MAC 与 Auth Info 全部通过后
   返回 `04 00 00 00`，移除会话并保留 2 秒 ACK tombstone 处理重传。

### Login 请求：330 byte

| 偏移 | 字段 |
| --- | --- |
| `[0,3)` | 固定 `03 01 00`；byte 3 为 `20 + GBK 用户名长度` |
| `[4,20)` | MD5-A |
| `[20,56)` | NUL 填充的严格 GBK 用户名 |
| `56`, `57` | control status、adapter number |
| `[58,64)` | MAC XOR |
| `[64,80)` | MD5-B |
| `[80,97)` | 17-byte IP section；reported IPv4 为 `[81,85)` |
| `[97,105)` | MD5-C 前 8 byte |
| `105` | IPDog |
| `[146,150)` | DHCP IPv4 |
| `[310,312)` | auth version |
| `[312,314)` | 固定 auth extension marker `02 0c` |
| `[314,318)` | CRC-1968 |
| `[318,320)`、`[326,328)` | 必须为零的保留位 |
| `[320,326)` | 再次出现的原始 MAC |
| `[328,330)` | auth extension tail |

### KA1 请求：38/42 byte

| 偏移 | 字段 |
| --- | --- |
| `0` | 固定 `ff` |
| `[1,17)` | MD5-A |
| `[17,20)` | 必须为零 |
| `[20,36)` | 16-byte Auth Info |
| `[36,38)` | big-endian uint16 timestamp |
| `[38,42)` | 可选；若存在必须全零 |

### KA2 请求：40 byte

| 偏移 | 字段 |
| --- | --- |
| `0`, `1` | `07`、serial |
| `[2,5)`、`5` | 固定 `28 00 0b`、Type `1` 或 `3` |
| `[6,8)` | version：首个 Type 1 为 `0f27`，之后为 `dc02` |
| `[8,10)`、`[10,16)` | 固定 `2f12`、6 byte 零 |
| `[16,20)` | 上一个服务端 Tail |
| `[20,24)` | 4 byte 零 |
| `[24,40)` | Type 1 全零；Type 3 为 4 byte 零 + IPv4 `[28,32)` + 8 byte 零 |

### Logout 请求：80 byte

| 偏移 | 字段 |
| --- | --- |
| `[0,3)` | 固定 `06 01 00`；byte 3 为 `20 + GBK 用户名长度` |
| `[4,20)` | MD5-A |
| `[20,56)` | NUL 填充的严格 GBK 用户名 |
| `56`, `57` | control status、adapter number |
| `[58,64)` | MAC XOR |
| `[64,80)` | 16-byte Auth Info |

## 密码学与校验公式

记 `P = password.encode("gbk", "strict")`，`||` 为拼接，整数注明网络序或小端序：

- **MD5-A** = `MD5(03 01 || salt || P)`，Login、KA1、Logout 都验证完整 16 byte。
- **MD5-B** = `MD5(01 || P || salt || 00 00 00 00)`，验证完整 16 byte。
- **MD5-C** = `MD5(login[80:97] || 14 00 07 0b)[:8]`。
- **MAC XOR** = `MAC XOR MD5-A[:6]`；服务端异或恢复 MAC，并要求 Login `[320,326)`
  再次出现同一 MAC。
- **CRC-1968** 输入为 `login[0:312] || 01 26 07 11 00 00 || login[320:326]`。
  从整数 `1234` 开始，将输入按 4 byte 分组（末组补零）解释为 little-endian uint32 并
  逐组 XOR，结果乘 `1968` 后截断为 uint32，最后输出 4-byte little-endian。
- **Auth Info HMAC** =
  `HMAC-SHA256(server_secret, "auth-info\0" || endpoint_ip_ascii || 00 ||`
  `endpoint_port_uint16_be || username_gbk || 00 || salt || session_number_uint64_be)[:16]`。
- **KA2 Tail HMAC** =
  `HMAC-SHA256(server_secret, "ka2-tail\0" || auth_info || old_tail ||`
  `byte(serial) || byte(type))[:4]`。

所有 digest、MAC、Auth Info 和 Tail 等值比较使用恒定时间比较。`server_secret` 只属于
本地模拟器，用于使 Auth Info/Tail 确定且可追踪，并不声称复现真实校园服务器秘密。

## Login 错误码

安全地完成报文分类与相关校验后，Login 失败响应 opcode 为 `0x05`：

| 错误码 | 含义 |
| --- | --- |
| `0x01` | 单会话账户已在另一 endpoint 使用 |
| `0x02` | 服务器忙；内置场景使用 |
| `0x03` | 账户不存在、禁用或密码错误 |
| `0x04` | 余额不足 |
| `0x05` | 账户冻结 |
| `0x07` | IPv4 或 MD5-C 错误 |
| `0x0B` | MAC 或重复 MAC 错误 |
| `0x14` | 超过多会话上限 |
| `0x15` | control status、adapter number、IPDog 或 auth version 不兼容 |
| `0x16` | 要求联合绑定时 IPv4/MAC 不匹配 |
| `0x17` | 账户要求 DHCP，但客户端上报 `0.0.0.0` |

## 静默丢弃

以下情况不发任何 UDP 响应，客户端表现为 timeout，以免用错误响应泄露账户或会话状态：

- 无法分类或无处理器：`unknown_operation`、`operation_not_handled`；场景主动
  `scenario_drop`；
- Challenge：长度/头错误 `challenge_parse_failed`，或 endpoint 不是有效 IPv4 的
  `challenge_source_ip_invalid`；
- Login：`login_parse_failed`、`login_challenge_missing`、
  `login_challenge_expired`、`login_crc_mismatch`；
- KA1：`ka1_parse_failed`、`ka1_session_missing`、`ka1_account_missing`、
  `ka1_authentication_failed`；
- KA2：`ka2_session_missing`、`ka2_parse_failed`、`ka2_serial_mismatch`、
  `ka2_tail_mismatch`、`ka2_type_or_version_invalid`、`ka2_reported_ip_mismatch`；
- Logout：`logout_parse_failed`、`logout_session_mismatch`、`logout_account_missing`、
  `logout_authentication_failed`。

每次 drop 同时写 `validation_decision(outcome="drop", wire_error_code=null,
drop_reason=...)` 与 `datagram_dropped`。Challenge/session 到期、故障场景主动过期和
Logout tombstone 到期还会留下对应的状态变化/过期事件。

## CLI 参数

| 参数 | 含义 |
| --- | --- |
| `-h`, `--help` | 显示中文主 CLI help |
| `--example` | 使用固定虚构 `examples/accounts.json`；与 `--accounts` 二选一且必须选一个 |
| `--accounts PATH` | 严格 JSON 账户配置 |
| `--listen-host HOST` | UDP 绑定地址，默认 `127.0.0.1`；非回环会警告 |
| `--port PORT` | `0..65535`，默认 `61440`；`--port 0` 自动选择空闲端口 |
| `--scenario NAME` | `normal`、`busy-then-success`、`flaky-login`、`keepalive-timeout`、`session-expired`、`malformed-response` |
| `--scenario-file PATH` | 严格 scenario JSON；若提供则覆盖 `--scenario` |
| `--trace-file PATH` | 完整追踪 JSONL；省略时自动使用 `traces/时间戳.jsonl` |
| `--ready-file PATH` | 绑定后原子写 ready JSON，停止时改写为 stopped |
| `--exit-after-logout` | 成功发送 Logout ACK 后停止 |
| `--log-level LEVEL` | 安全事件日志级别：`DEBUG/INFO/WARNING/ERROR`，默认 `INFO` |
| `--json-events` | 把安全事件日志改为每行 `{level,event}` JSON，而非完整 trace |

## transcript v1 与离线工具

完整规范见 `TRANSCRIPT_V1.md`。`transcript_version: 1` 包含两类严格记录：

- packet record 必须有 `record_type=packet`、`side`、`direction`、显式可空
  `operation`、完整 `payload_hex`、`payload_sha256`、`payload_length`、显式可空
  `local_endpoint`/`remote_endpoint`；hex 必须为无空格小写规范形式，hash/length 必须
  与 payload 自洽。
- decision record 必须有 `record_type=decision`、`side`、`operation`、`outcome`、显式
  可空 `wire_error_code` 与 `drop_reason`。允许为 `null` 的字段也不能省略。

endpoint 可用 Go object `{"host":"127.0.0.1","port":61440}` 或 Python 二元素
array `["127.0.0.1",61440]`。compare 先分别验证双方 schema，再把 packet 与 decision
拆成两个保持各自顺序的流：client send 对 server receive、client receive 对 server
send；payload 身份和 server endpoint 必须严格相同，只忽略时间、PID、sequence 和
client 临时端口。server drop 可映射为 client timeout；其他 decision 字段严格相等。

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"

# 解析一个 UDP payload hex；输出 JSONL
python -B -m drcom520d_mock_server.offline payload `
  0102000000000000000000000000000000000000

# 从 classic PCAP 提取服务端视角 packet transcript
python -B -m drcom520d_mock_server.offline pcap .\capture.pcap --server-port 61440

# 对齐 Go client transcript 与完整 server trace
python -B -m drcom520d_mock_server.offline compare `
  .\client.jsonl .\server-trace.jsonl
```

`compare` 成功返回 0，已通过格式验证但存在语义差异返回 1，文件/JSON/hex/PCAP 或
malformed transcript 返回 2。classic PCAP 支持大/小端与微秒/纳秒时间戳，只接受
Ethernet linktype 1、IPv4、UDP，并严格校验长度和方向；不支持 pcapng。

实时抓包是**可选的外部诊断**：如果操作者自行安装 Npcap，可用其生成 classic PCAP，
再交给 `offline pcap`；本工具不会导入或调用 Npcap、Scapy。CI 和 Go/Python 自动化应以
应用层 transcript 为准，完全不依赖 Npcap、管理员权限或实时抓包。

## 排障

| 症状 | 检查与处理 |
| --- | --- |
| Windows UDP `61440` 被占用或 `WinError 10013` | 用 `--port 0 --ready-file ...` 自动选择，或同时把服务端和验证器改成同一空闲端口；检查防火墙/安全软件，不要提权规避隔离边界 |
| 客户端 timeout | 核对 `127.0.0.1`、ready 实际端口、虚构账户字段和 D 系列报文；查看 trace 中是否为静默丢弃或故障场景 drop |
| 配置启动失败 | JSON 必须为 UTF-8 严格 schema；检查未知/缺失字段、类型、hex 长度、重复用户名以及 `credentials_are_fictional=true` |
| 场景不符合预期 | `--scenario-file` 会覆盖 `--scenario`；检查每个 operation 的独立计数与 `repeat_last` |
| trace 创建/写入失败 | 路径父目录须可创建且目标文件不能已存在；检查磁盘、权限和编码。trace 是必需证据，失败会在绑定前拒绝或在运行中安全停止 |
| 客户端某阶段与 mock 不一致 | 确认服务端使用 `normal`、示例账户和相同 port；核对 `tests/fixtures/d520_client_vectors_v1.json` 的请求/响应与 crypto 中间值；从失败阶段前一条 `validation_decision`、`crypto_check` 与 `datagram_dropped` 定位 |
| ready 一直不是 ready | 检查进程退出码与 stderr；配置、scenario 或 trace 在绑定前失败时不会发布 ready |
| transcript compare 返回 1/2 | 1 看首个 `field_path` 语义差异；2 先修复 schema、自完整性、hex、endpoint 或输入文件格式 |

## 开发者验证

在仓库根目录运行。`PYTHONDONTWRITEBYTECODE` 和临时 pycache 避免把编译产物写入源码树：

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"
$env:PYTHONDONTWRITEBYTECODE = "1"
python -B -m unittest discover -s tools/drcom520d_mock_server/tests -v
python -B -m drcom520d_mock_server --help

$tempBytecode = Join-Path ([IO.Path]::GetTempPath()) `
  ("drcom-pyc-" + [guid]::NewGuid())
$env:PYTHONPYCACHEPREFIX = $tempBytecode
python -m compileall -q tools/drcom520d_mock_server/src `
  tools/drcom520d_mock_server/tests

# 仓库级 Go 回归（本模拟器本身不以 Go 为运行时依赖）
go test ./...

# 真实 loopback 使用前文 --port 0/ready-file/exit-after-logout 步骤
git diff --check
git diff --name-only -- 参考
```

Python 单元测试在内存中构造 classic PCAP fixture；自动化不要求 Npcap。真实 loopback
验收还应逐行解析 JSONL，确认六个 `datagram_received` operation 顺序为
`challenge, login, ka1, ka2, ka2, logout`，且包含完整秘密、实际 ready port 和
`server_stopped`。trace、ready 文件和任何真实抓包都不应暂存或提交。
