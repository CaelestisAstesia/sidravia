# Dr.COM 5.2.0(D) 协议规范

> 本文件是 Sidravia 下一个 Go D520 客户端的实现权威。它描述可观察的协议行为，
> 不保留任何 Python/Rust 项目的模块结构或偶然控制流。Go 实现应基于本规范从零编写，
> 不得复制 Drcom-Core、Drcom-CLI 或 ProjectLAINS 的源码结构。

## 1. 范围、稳定 ID、来源与干净重实现

- **稳定协议 ID**：`drcom-5.2.0-d`。Sidravia 内部以此 ID 注册协议、绑定到 Profile 与
  institution 配置，并以此 ID 命名本文件。
- **传输**：单一 UDP socket，服务端默认端口 `61440`。客户端向服务端发送请求，服务端
  原路返回响应；无连接握手，无重连会话恢复。
- **生命周期**：`Challenge -> Login -> (KA1, KA2)* -> Logout`。Login 成功后进入保活
  循环；Logout 结束会话。
- **来源标记**：下文每条非自明事实后标注来源——`[DC]` = Drcom-Core、
  `[DCli]` = Drcom-CLI、`[PL]` = ProjectLAINS、`[Mock]` = 本仓库 mock server。
  多源一致时标注组合；仅 mock 特有的行为标注 `[Mock]` 并说明它不声称复现真实校园
  服务器。

### 来源与许可

三套参考项目与 Sidravia 均为 AGPL-3.0-only，许可兼容。完成本规范和确定性向量的
独立 Review 后，来源快照已从工作仓库移至本机归档
`/home/astesia/code/sidravia-reference-archive/2026-07-22-snapshot/`。该路径只记录
本次提取的来源，不是构建、测试或后续实现的输入：

| 项目 | 快照 HEAD | 用到的文件 | 角色 |
| --- | --- | --- | --- |
| Drcom-Core | `bcc636f` | `config.py`, `core.py`, `d_series/{constants,packets,strategy}.py` | Python 参考实现 |
| Drcom-CLI | `1c63497` | `common/{consts,crypto,encoding,protocol}.py`, 3 个测试 | 退出码/心跳间隔旁证 |
| ProjectLAINS | `d5419ee` | `auth/{context,netrunner}.rs`, `auth/wire/{constant,crypto,response,v520d}.rs` | Rust 参考实现 |
| mock server | 本仓库 | `codec.py`, `packet_factory.py`, `test_codec.py`, `test_server_core.py` | 行为权威与向量来源 |

> 归档中的 `Drcom-Core/src/drcom_core/utils.py` 未纳入本次提取范围；其中 `md5_bytes`
> 为标准 MD5，`checksum_d_series` 的算法由 `[PL]` 与 `[Mock]` 一致确认（见 §4）。

### 干净重实现规则

- 本规范只定义线级行为：opcode、偏移、编码、字节序、密码学输入输出、序列与错误分类。
- Go 客户端不得导入或链接任何参考项目，也不得复制其类、模块或函数划分。
- 仅 `Mock` 特有的派生机制（Auth Info / KA2 Tail 的 HMAC、计费字段单位）用于使本地
  测试确定可追踪；真实校园服务器的对应算法标注为 `Unresolved`，Go 客户端只读取这些
  字段而不生成它们。
- 任何无法从上述来源解析的值标注 `Unresolved` 并说明何种 mock 或校园观察可决定它，
> 不得编造默认值。

## 2. 输入映射

协议报文由五层输入拼装。Go 实现应把这些输入收敛到一个不可变 `RunDefinition`，再由
协议 Run 读取。

| 输入层 | 字段 | 报文落点 | 来源 |
| --- | --- | --- | --- |
| 凭据 | `username`, `password` | Login/Logout username 与所有 MD5 | `[DC] [PL]` |
| 选定绑定 | `mac` (6B), `client_ipv4` (=host_ip/bind_ip), 前两个 IPv4 DNS, DHCP server IPv4 | MAC XOR、Login IP/DNS/DHCP section、KA2 Type3 IP | `[DC] [PL]` |
| 主机事实 | `host_name`, OS family, OS release | Login host_name/host_os 区 | `[DC] [PL]` |
| 机构配置 | server address/port、`os_info` (20B)、认证与保活版本、固定字段、Challenge padding、各阶段 timeout、heartbeat interval、busy retry policy | Challenge/Login/KA/Logout 固定区和 Run 调度 | `[DC] [PL]` |
| D520 Run 私有状态 | `salt` (4B), `auth_info` (16B), `keep_alive_tail` (4B), `keep_alive_serial_num` (u8) | 只能由 Challenge/Login/KA2 响应回填并由当前 Run 持有；不得由 Profile、IPC 或 context override 注入 | `[DC] [PL]` |

说明与冲突：

- `client_ipv4` 同时用于 MD5-C 输入与 KA2 Type3 上报。`[DC]` 称 `host_ip_bytes`，
  `[PL]` 称 `bind_ip`；语义相同 `[DC] [PL]`。
- Factory 从选定绑定读取 client IPv4 和 MAC；client IPv4 为 unspecified/`0.0.0.0`、
  MAC 长度不是 6B 或 MAC 全零时拒绝创建 Run。DNS 取选定网卡中前两个 IPv4 地址，
  缺失位置填 `0.0.0.0`；DHCP server 缺失时填 `0.0.0.0`。
- `host_name` 直接取 `SystemHostInformation.HostName`。`host_os` 由去除首尾空白后的
  OS family 与 OS release 以单个空格连接；任一为空时只使用另一项，两项均空时发送空
  字段。Machine architecture 首版不进入 D520 报文。最终 wire 字节仍必须满足 §5 编码
  和长度限制。
- `adapter_num`：`[DC]` 可配置（默认 `01`）；`[PL]` 硬编码 `01`；`[Mock]` 校验其与
  服务器配置一致。默认值三源一致，本规范取 `01`。
- `os_info`：`[DC]` 默认 `940000000600000000000000280a000002000000` (20B)；`[Mock]`
  fixture 采用同一默认值；`[PL]` 来自配置。三源默认一致。
- `control_check_status`/`ipdog`/`auth_version`/`keep_alive_version`：`[Mock]` 把它们
  作为服务器期望值校验；`[DC] [PL]` 作为客户端注入值。本规范以 mock 期望值为兼容基准
  （`20`/`01`/`2c00`/`dc02`）。
- 每个机构 Profile 必须明确提供 `serverAddress`、`serverPort`、`authVersionHex`、
  `keepAliveVersionHex`、`controlCheckStatusHex`、`ipdogHex`、`adapterNumberHex`、
  `osInfoHex`、`challengePaddingHex`、Challenge/Login/Keepalive/Logout timeout、
  heartbeat interval，以及 busy 最大尝试次数和退避上下限。首版不在 Run 内为机构差异
  隐藏默认值；wire grammar 的 opcode、长度、偏移和 checksum 常量仍是代码常量。
- **Challenge padding 来源冲突**（不影响兼容，见 §3、§10）：`[DC]` 填 15B 零；
  `[PL]` 填 `protocol_version` 字符串再补零；`[Mock]` 不校验该字段。Sidravia 从
  Profile 读取精确 15B；首个 Profile 采用 `[DC]` 的全零值，后续由真实抓包校准。
- `ProtocolContextOverride` 首版不提供 D520 覆盖字段。Factory 接受未提供的 override
  和严格空对象 `{}`，拒绝 `null`、含任意字段的对象、其他 JSON 类型、重复或尾随值。
  override 不能注入网络事实、Profile 字段或 Run 私有状态。

## 3. 报文布局

所有偏移为从 0 开始的半开区间 `[start,end)`。字节序逐字段注明：`LE` = little-endian，
`BE` = big-endian。长度均为字节。

### 3.1 Challenge

请求（≥20B，本规范固定 20B）`[DC] [PL] [Mock]`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `[0,2)` | `01 02` | 固定 |
| `[2,4)` | seed | `LE u16` = `(timestamp_seconds + random) % 0xFFFF` |
| `4` | `09` | 固定 magic |
| `[5,20)` | padding (15B) | Profile 提供精确值；首个 Profile 采用 `[DC]` 全零；`[PL]` 携带 `protocol_version`+零；`[Mock]` 不校验 |

响应（16B）`[Mock]`；`[DC] [PL]` 只读 `[4,8)`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `02` | 固定 |
| `[1,4)` | 零 | 保留 |
| `[4,8)` | salt (4B) | 客户端后续所有 MD5 输入 |
| `[8,12)` | source IPv4 | `[Mock]` 填客户端源 IP；`[DC] [PL]` 不读；真实用途 `Unresolved` |
| `[12,16)` | 零 | 保留 |

### 3.2 Login

请求（固定 330B）`[DC] [PL] [Mock]`，byte 3 = `20 + len(encoded_username)`：

| 偏移 | 字段 | 编码/说明 |
| --- | --- | --- |
| `[0,3)` | `03 01 00` | 固定 |
| `3` | 长度字节 | `20 + len(encoded_username)` |
| `[4,20)` | MD5-A (16B) | §4 |
| `[20,56)` | username (36B) | 协议文本字节，NUL 填充；非 ASCII 编码仍为 `Unresolved`（§5） |
| `56` | control_check_status | `[Mock]` 校验 |
| `57` | adapter_num | `[Mock]` 校验 |
| `[58,64)` | MAC XOR (6B) | §4 |
| `[64,80)` | MD5-B (16B) | §4 |
| `[80,97)` | IP section (17B) | `01 + client_ipv4 + 00*12`；reported IPv4 在 `[81,85)` |
| `[97,105)` | MD5-C 前 8B | §4 |
| `105` | IPDog | `[Mock]` 校验 |
| `[106,110)` | padding_after_ipdog (4B) | 零 |
| `[110,142)` | host_name (32B) | 协议文本字节，NUL 填充；非 ASCII 编码仍为 `Unresolved`（§5） |
| `[142,146)` | primary_dns (4B) | 网络序 IPv4 |
| `[146,150)` | dhcp_ipv4 (4B) | 网络序 IPv4；`require_dhcp` 时不得为 `0.0.0.0` |
| `[150,154)` | secondary_dns (4B) | 网络序 IPv4 |
| `[154,162)` | padding_after_dhcp (8B) | 零 |
| `[162,182)` | os_info (20B) | 指纹 |
| `[182,214)` | host_os (32B) | 协议文本字节，NUL 填充；非 ASCII 编码仍为 `Unresolved`（§5） |
| `[214,310)` | 零 (96B) | HOST_OS_SUFFIX |
| `[310,312)` | auth_version (2B) | `[Mock]` 校验 |
| `[312,314)` | `02 0c` | auth ext marker，`[Mock]` 校验 |
| `[314,318)` | CRC-1968 (4B) | §4 |
| `[318,320)` | 零 | 保留，`[Mock]` 校验为零 |
| `[320,326)` | 原始 MAC (6B) | 必须与 MAC XOR 恢复值一致，`[Mock]` 校验 |
| `[326,328)` | 零 | 保留，`[Mock]` 校验为零 |
| `[328,330)` | auth ext tail (2B) | `[DC]` 随机；`[Mock]` 不校验；fixture 固定 `12 34` |

成功响应（64B）`[Mock]`；`[PL]` 解析计费，`[DC]` 只取 auth_info：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `04` | 固定 |
| `[1,9)` | 零 | 保留 |
| `[9,13)` | traffic | `LE u32`；`[Mock]` 单位 KiB，`[PL]` 解析 `/1024`→MB |
| `[13,17)` | balance | `LE u32`；`[Mock]` 单位 cents，`[PL]` 解析 `/100`→元 |
| `[17,23)` | 零 | 保留 |
| `[23,39)` | Auth Info (16B) | 客户端回填到上下文；`[Mock]` HMAC 派生（§4） |
| `[39,64)` | 零 | 保留 |

失败响应（32B）`[DC] [PL] [Mock]`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `05` | 固定 |
| `[1,4)` | 零 | 保留 |
| `4` | 错误码 | `u8`，见 §9 |
| `[5,32)` | 零 | 保留 |

### 3.3 KA1

请求（38B 或 42B）`[DC] [PL] [Mock]`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `ff` | 固定 |
| `[1,17)` | MD5-A (16B) | 与 Login 同公式 |
| `[17,20)` | 零 | `[Mock]` 校验为零 |
| `[20,36)` | Auth Info (16B) | Login 成功回填值 |
| `[36,38)` | timestamp | `BE u16` = `epoch_seconds % 0xFFFF` |
| `[38,42)` | 零（可选） | 存在则必须全零；`[DC]` 可选，`[PL]` 总含，`[Mock]` 接受两者 |

响应（20B）`[Mock]`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `07` | 固定；`[DC] [PL]` 仅校验首字节为 `07` |
| `[1,20)` | 零 | 保留 |

### 3.4 KA2

请求（固定 40B）`[DC] [PL] [Mock]`：

| 偏移 | 字段 | 编码/说明 |
| --- | --- | --- |
| `0` | `07` | 固定 |
| `1` | serial | `u8`，递增、模 256 回绕 |
| `[2,5)` | `28 00 0b` | 固定 |
| `5` | type | `1` 或 `3`；`[Mock]` 拒绝其他 |
| `[6,8)` | version | 首个 Type1 用 `0f 27`；其余用 keep_alive_version (`dc02`) |
| `[8,10)` | `2f 12` | 固定 |
| `[10,16)` | 零 (6B) | `[Mock]` 校验为零 |
| `[16,20)` | tail (4B) | 上一次 KA2 响应回填的 Tail；首包为零 |
| `[20,24)` | 零 (4B) | `[Mock]` 校验为零 |
| `[24,40)` | type 区 (16B) | Type1：全零；Type3：`00*4 + client_ipv4[28,32) + 00*8` |

响应（60B）`[Mock]`；`[PL]` 解析计费，`[DC]` 只取 Tail：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `0` | `07` | 固定 |
| `1` | serial | 回显 |
| `[2,5)` | `28 00 0b` | 固定 |
| `5` | type | 回显 |
| `[6,16)` | 零 (10B) | 保留 |
| `[16,20)` | Tail (4B) | 客户端回填；`[Mock]` HMAC 派生（§4） |
| `[20,24)` | 零 (4B) | 保留 |
| `[24,44)` | 零 (20B) | 保留 |
| `[44,48)` | month_time | `LE u32`；`[Mock]` 分钟，`[PL]` 原值 |
| `[48,52)` | traffic | `LE u32`；`[Mock]` KiB，`[PL]` `/1024`→MB |
| `[52,56)` | balance | `LE u32`；`[Mock]` 万分之一元，`[PL]` `/10000`→元 |
| `[56,60)` | remaining_time | `LE u32`；`[Mock]` 分钟，`[PL]` `/60`→分钟 |

### 3.5 Logout

请求（固定 80B）`[DC] [PL] [Mock]`，byte 3 = `20 + len(encoded_username)`：

| 偏移 | 字段 | 编码 |
| --- | --- | --- |
| `[0,3)` | `06 01 00` | 固定 |
| `3` | 长度字节 | `20 + len(encoded_username)` |
| `[4,20)` | MD5-A (16B) | §4；Sidravia 优先在 Logout 前重新 Challenge 并使用 fresh salt；fresh Challenge 失败时用保存的 login salt 尽力发送一次（这是实现清理策略，不冒充所有服务器的通用事实） |
| `[20,56)` | username (36B) | 协议文本字节，NUL 填充；非 ASCII 编码仍为 `Unresolved`（§5） |
| `56` | control_check_status | |
| `57` | adapter_num | |
| `[58,64)` | MAC XOR (6B) | §4 |
| `[64,80)` | Auth Info (16B) | Login 成功回填值 |

响应（4B）`[Mock]`：`04 00 00 00`。

## 4. 密码学与校验

记 `E(text)` 为 §5 所述的协议文本编码，`P = E(password)`，`||` 为拼接。当前确定性
向量只使用 ASCII，因此 GBK 与 UTF-8 对这些向量生成相同字节。

- **MD5-A** = `MD5(03 01 || salt || P)`，16B。Login、KA1、Logout 均验证完整 16B。
  `[DC] [PL] [Mock]` 一致。
- **MD5-B** = `MD5(01 || P || salt || 00 00 00 00)`，16B。仅 Login。`[DC] [PL] [Mock]`
  一致。
- **MD5-C** = `MD5(ip_section || 14 00 07 0b)[:8]`，其中
  `ip_section = 01 || client_ipv4 || 00*12`（17B）。`[DC] [PL] [Mock]` 一致。
- **MAC XOR** = `mac[i] ^ MD5_A[i]` for `i in 0..6`，6B。服务端异或恢复 MAC，并要求
  Login `[320,326)` 再次出现同一 MAC。`[DC] [PL] [Mock]` 一致。
- **CRC-1968**（D-series checksum）`[DC] [PL] [Mock]` 一致：
  - 输入 = `login[0:312] || 01 26 07 11 00 00 || login[320:326]`。
  - 算法：初值 `ret = 1234`；输入按 4B 分组（末组右侧补零），每组按 `LE u32` 解释并
    `ret ^= group`；最后 `ret = (ret * 1968) mod 2^32`；输出 4B little-endian。
  - 向量校验：`checksum_d_series(010203040506) == 206d16d7` `[Mock test_codec]`。
- **Auth Info（mock 派生，非真实算法）** `[Mock]`：
  `HMAC-SHA256(server_secret, "auth-info\0" || endpoint_ip_ascii || 00 || endpoint_port_BE_u16 || username_gbk || 00 || salt || session_number_BE_u64)[:16]`。
  客户端不计算，只从 Login 成功响应 `[23,39)` 读取。真实校园服务器算法 `Unresolved`。
- **KA2 Tail（mock 派生，非真实算法）** `[Mock]`：
  `HMAC-SHA256(server_secret, "ka2-tail\0" || auth_info || old_tail || byte(serial) || byte(type))[:4]`。
  客户端不计算，只从 KA2 响应 `[16,20)` 读取。真实校园服务器算法 `Unresolved`。
- **HMAC 不存在于客户端**：`[DC]` 与 `[PL]` 的客户端均不生成 Auth Info 或 Tail，仅验证
  MD5-A/B/C、MAC XOR、CRC。Go 客户端同样只生成上述五项，不生成任何 HMAC。
- 所有 digest/MAC/字段比较在 `[Mock]` 中使用恒定时间比较（`hmac.compare_digest`）。

## 5. username/host 编码、填充、长度限制与拒绝

- **来源冲突**：username、password、host_name、host_os 在 `[DC] [Mock]` 中使用严格
  GBK（`encode("gbk", "strict")`）；`[PL]` 的 `as_bytes()` 使用 UTF-8。旧 Windows
  客户端也可能只是使用当时系统 ANSI code page，而非协议明确指定 GBK。现有 fixture
  全为 ASCII，无法裁决这个冲突。
- **当前实现边界**：ASCII 是两种候选编码的共同子集，当前 Go codec 只接受 ASCII，
  足以推进 mock 纵向链路。非 ASCII 字段暂时明确返回 unsupported，不引入 GBK 依赖，
  也不把 ASCII-only 描述成完整 GBK。
- **判定方式**：优先比较官方客户端发送含非 ASCII username 或 host_name 时的原始
  报文字节；其次在校园服务器上分别验证 GBK 与 UTF-8。得到证据后再固定编码，或将其
  作为机构 Profile 的明确配置。
- **填充**：username 填充到 36B，host_name/host_os 填充到 32B，均右侧 NUL 填充
  `[DC] [PL] [Mock]`。
- **长度限制**：Login/Logout byte 3 = `20 + len(encoded_username)`；`[Mock]` 按其
  GBK 规则校验该字节与 `[20,56)` 实际用户名长度一致，且总长分别恰为 330/80。
- **拒绝行为**：
  - 非 GBK 字符：`[DC]` 抛 `ConfigError`；`[Mock]` 以 `PacketFormatError` 静默丢弃。
  - `[Mock]` 对 username 字段含非填充尾部字节、总长不符、header 不符、保留位非零、
    auth ext marker 错误、CRC 不匹配均静默丢弃（`drop`，不回响应）。
  - 真实校园服务器对超长用户名的行为 `Unresolved`（mock 上限由 36B 固定字段隐含约束）。

## 6. serial、Tail 与 KA2 序列

- **serial**：`u8`，每个 KA2 请求递增 1，到达 255 后回绕到 0 `[DC] [PL] [Mock]`。
  `[Mock]` 校验请求 serial 与会话期望值一致。
- **Tail**：4B，由 KA2 响应 `[16,20)` 回填，作为下一个 KA2 请求 `[16,20)`。首个 KA2
  请求 Tail 为 `00 00 00 00` `[DC] [PL] [Mock]`。
- **version**：首个 Type1 KA2 用 `0f 27`（init magic），其后所有 KA2 用
  keep_alive_version (`dc02`) `[DC] [PL] [Mock]`。
- **bootstrap 序列冲突（兼容）**：
  - `[DC]`：首次 `keep_alive()` 发 KA1 + KA2 序列 `Type1(first) -> Type1 -> Type3`
    （3 个 KA2）；之后每次发 KA1 + `Type1 -> Type3`（2 个 KA2）。
  - `[PL]`：每个心跳周期发 KA1 + 单个 KA2，Type 在 `1`/`3` 间交替；首周期 Type1
    (first)，次周期 Type3，依此交替。
  - `[Mock]`：同时接受 `[DC]` 的 `1-1-3` 与 `[PL]` 的 `1-3` 两种 bootstrap；之后要求
    Type 严格 `1,3,1,3,...` 交替且 serial 递增。完全相同的重传返回缓存响应且不推进
    状态。
  - **Sidravia 实现策略**：Go Run 采用 `[DC]` 的首次 `1-1-3`、后续每轮 `1-3`，
    因为 Drcom-Core 是既有校园行为的主基准且 mock 明确接受该序列。静态 fixture 仍采用
    最短的 `1-3`，它只证明两种 KA2 报文布局和 Tail 回填，不定义 Run 的调度节奏。
- **重传幂等**：`[Mock]` 对相同 KA2 请求返回缓存响应，不重复推进 serial/Tail/计费。

## 7. retry/timeout 边界（Run 内 vs Session）

- **协议 Run 内**（由协议模块拥有）：
  - Challenge：单次发送，等待响应超时则失败。`[DC]` `timeout_challenge` 默认 3.0s；
    `[PL]` 从机构配置读取。
  - Login：Sidravia 采用 `[DC]` 策略，只对 `0x02` (SERVER_BUSY) 在单个 Run 内最多
    重试 3 次并退避 1.0–2.0s；其余拒绝立即失败。`[PL]` 会对整个 Login 通用重试最多
    3 次（不区分 busy），该来源冲突保留在 §10。
  - KA1/KA2：单次发送，超时即心跳失败。
  - Heartbeat：interval 来自机构 Profile。Run 在建立认证后按该 interval 执行 KA1 +
    KA2 `1-3`，任何一轮失败即返回结构化失败。
  - Logout：best-effort。Run 先以 Profile 的 Logout timeout 重新 Challenge；成功则用
    fresh salt，失败则用保存的 login salt 尽力发送一次 Logout。发送或等待 ACK 失败
    不得覆盖原始失败，也不得阻塞停止。`[PL]` 直接使用 login salt，该来源差异保留在
    §10。
- **Session 层**（由 Session/Supervisor 拥有，不在协议 Run 内）：
  - Session 的 stop、restart、network change 或 shutdown 决定取消 Run，并通过 cancellation
    cause 指明是否需要 best-effort Logout。
  - Run 返回后是否重新认证、何时重新认证以及何时阻塞等待输入变化，由 Session 根据
    `AuthenticationProtocolRunFailure.HandlingRecommendation` 决定。
- **默认旁证**：历史实现的 heartbeat interval 为 20s `[DC] [DCli] [PL]`
  （`[DCli]` `DEFAULT_HEARTBEAT_INTERVAL=20`）；Sidravia 把该值放入每个 Profile，而非
  Session 全局常量。
- **冲突**：Login 重试语义、Logout 是否重新 Challenge 在两套客户端间不同（§10）。

## 8. 取消、Logout 尝试、UDP 归属与资源清理

- **UDP 归属**：单个 UDP socket 由一次协议 Run（`[PL]` `netrunner`，`[DC]` `NetworkClient`）
  独占拥有，Run 结束即关闭。多个 Run 不得共享 socket。
- **取消**：`[PL]` 用 `CancellationToken`，在 Challenge/KA1/KA2 等待点响应取消并立即
  退出心跳循环；`[DC]` 用 `asyncio.Event` + 任务 `cancel`。Go 实现必须支持 Run 级取消
  且不遗留 goroutine。
- **Logout 尝试**：只有保存了 auth_info 才发送；否则直接本地结束。优先使用 fresh
  salt；fresh Challenge 失败时使用 login salt 尽力发送一次。`[Mock]` 会丢弃 fallback，
  但真实服务器可能接受；无论结果如何都继续关闭 socket（见 §3.5、§10）。
- **资源清理**：停止时关闭 socket、清除 salt/auth_info/tail/serial 等上下文
  `[DC] _reset_state`。`[Mock]` 在 Logout ACK 后保留 2s tombstone 以幂等处理重传，过期
  后移除会话与账户索引。
- **失控 goroutine 禁止**：心跳循环必须在取消或停止时确定退出；`[DC]` `_heartbeat_loop`
  与 `[PL]` `heartbeat_loop` 均保证这一点。

## 9. 服务器拒绝码与 D520 失败分类

Login 失败响应 opcode `0x05`，错误码在 byte 4 `[Mock]`（完整表）；`[DC]` 仅识别
`0x02` SERVER_BUSY，其余封装为 `AuthError`；`[PL]` 把 code 透传为 `AuthErrorCode`；
`[DCli]` 进程退出码 `EXIT_ERROR_AUTH=3`。

| 码 | mock 含义 | 建议稳定分类 | 可重试 |
| --- | --- | --- | --- |
| `0x01` | 单会话账户已在另一 endpoint 使用 | `session_in_use` | 否（需先 Logout） |
| `0x02` | 服务器忙 | `server_busy` | 是（Run 内退避重试） |
| `0x03` | 账户不存在/禁用/密码错误 | `credential_invalid` | 否 |
| `0x04` | 余额不足 | `insufficient_funds` | 否 |
| `0x05` | 账户冻结 | `account_frozen` | 否 |
| `0x07` | IPv4 或 MD5-C 错误 | `binding_ip_mismatch` | 否 |
| `0x0B` | MAC 或重复 MAC 错误 | `binding_mac_mismatch` | 否 |
| `0x14` | 超过多会话上限 | `too_many_sessions` | 否 |
| `0x15` | control/adapter/IPDog/auth_version 不兼容 | `incompatible_version` | 否 |
| `0x16` | 联合绑定时 IPv4/MAC 不匹配 | `binding_pair_mismatch` | 否 |
| `0x17` | 要求 DHCP 但上报 `0.0.0.0` | `dhcp_required` | 否 |

分类原则：

- **不泄露秘密**：`0x03` 在 `[Mock]` 中对 unknown/disabled/wrong-password 返回同一码，
  Go 客户端应保持这一不可区分性，不得向日志/UI 泄露账户是否存在。
- **可重试**：仅 `0x02` 属于 Run 内自动重试；其余属于不可重试的业务拒绝，向上层返回
  分类错误，由人决定后续动作。
- **静默丢弃**（无响应，客户端表现为超时）`[Mock]`：结构/CRC 错误、无有效 Challenge、
  Challenge 过期、KA 凭据不符、KA2 serial/tail/type/version/IP 不符、伪造 Logout 等。
  Go 客户端必须把这些超时归类为网络/会话层问题，不得猜测具体 drop 原因。
- **真实校园服务器错误码全集** `Unresolved`：上表来自 mock；真实服务器可能存在额外
  码或不同语义，需现场观察补全。

向 Session 返回的处理建议：

- Challenge/Login/KA/heartbeat 的 timeout 和瞬时 UDP I/O 失败使用
  `RetryAfterStandardDelay`。
- `server_busy` 在 Run 内耗尽 Profile 配置的有界尝试次数后使用
  `RetryAfterExtendedDelay`。
- 上表其他已知拒绝、未知拒绝码、结构畸形或 wire 不兼容使用
  `BlockUntilExplicitRestartOrRelevantInputChange`。
- context cancellation 不是协议失败；Run 完成 cancellation cause 要求的清理后返回，
  Session 以自身记录的 cancellation cause 决定下一动作。

## 10. 来源一致/冲突/未决表

| 议题 | Drcom-Core | Drcom-CLI | ProjectLAINS | Mock | 结论 |
| --- | --- | --- | --- | --- | --- |
| Challenge 请求头 `01 02`+seed LE+`09` | ✓ | — | ✓ | ✓ | Agreed |
| Challenge padding 15B 内容 | 全零 | — | 携带 protocol_version | 不校验 | **Conflict（不影响兼容）** |
| Challenge 响应 salt `[4,8)` | ✓ | — | ✓ | ✓ | Agreed |
| Challenge 响应 source IP `[8,12)` | 不读 | — | 不读 | 填源 IP | **Unresolved 真实用途** |
| 非 ASCII username/host 编码 | GBK | 诊断时 UTF-8 优先/GBK 兜底 | UTF-8 bytes | GBK | **Conflict：真实服务器待抓包；当前只支持 ASCII** |
| Login 330B 布局/MD5-A/B/C/MAC XOR/CRC | ✓ | — | ✓ | ✓ | Agreed |
| CRC-1968 算法（1234/LE XOR/×1968/LE） | ✓ | — | ✓ | ✓ | Agreed |
| KA1 38/42B、MD5-A、auth_info、BE timestamp | ✓ | — | ✓(42) | ✓ | Agreed |
| KA2 40B 布局、serial 回绕、Tail `[16,20)` | ✓ | — | ✓ | ✓ | Agreed |
| KA2 bootstrap 节奏 | 1-1-3 init + 每轮 1,3 | — | 每轮单 KA2 交替 | 接受两者 | **Conflict（mock 兼容）** |
| KA2 计费字段解析 | 不解析 | — | 解析(/1024,/10000,/60) | 存原始 | **Unresolved 真实单位** |
| Logout 80B 布局 | ✓ | — | ✓ | ✓ | Agreed |
| Logout salt 来源 | 重新 Challenge(fresh) | — | 用 login salt | 要求 fresh | **Conflict：PL 与 Mock 不兼容** |
| Login 重试语义 | busy 重试 3 次+退避 | — | 通用重试 3 次 | — | **Conflict** |
| 心跳间隔 20s | ✓ | ✓(20) | ✓ | — | Agreed |
| adapter_num | 可配置(默认 01) | — | 硬编码 01 | 校验匹配 | Agreed（默认 01） |
| Auth Info 生成 | 不生成(读取) | — | 不生成(读取) | HMAC 派生 | **Unresolved 真实算法** |
| KA2 Tail 生成 | 不生成(读取) | — | 不生成(读取) | HMAC 派生 | **Unresolved 真实算法** |
| Login 成功计费单位 | 不解析 | — | /1024,/100 | KiB,cents | **Unresolved 真实单位** |
| 错误码全集 | 仅 0x02 | 退出码 3 | 透传码 | 完整表 | mock 表为基准，真实 Unresolved |
| Logout 事件顺序 | stop 触发 logout | LOGOUT then STOP | — | — | Agreed `[DCli]` |
| UDP 归属/取消/清理 | NetworkClient+Event | — | netrunner+CancellationToken | — | Agreed |

### Unresolved 汇总与判定方式

1. **真实 Auth Info 生成算法**：客户端无需生成；判定需真实校园服务器 Login 成功响应
   与已知 salt/server secret 的对照（mock 用 HMAC 仅作确定性替身）。
2. **真实 KA2 Tail 生成算法**：同上，需真实 KA2 响应序列对照。
3. **计费字段真实单位/语义**：需真实服务器在已知流量/余额下的响应对照。
4. **Challenge 响应 `[8,12)` source IP 真实用途**：需真实服务器响应抓包确认。
5. **真实服务器错误码全集**：需现场触发各类拒绝并观察 byte 4。
6. **Challenge padding 是否被真实服务器校验**：需真实服务器对零填充与
   protocol_version 填充两种请求的响应对照。
7. **非 ASCII username/password/host 编码**：需官方客户端抓包或校园服务器分别接受
   GBK/UTF-8 的对照；在此之前 Go codec 只支持共同的 ASCII 子集。
