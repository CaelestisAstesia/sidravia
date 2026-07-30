# ADR 0027：D520 本地端点与线级 Profile 对齐

**状态：** Accepted

## 背景

2026-07-30 的 Windows 便携版现场运行在启用热点/ICS 时观察到 D520 Challenge 持续超时：
Sidravia 仍显示预期的物理以太网与 IPv4，但自动重试、显式 Session 重启与全新 Session 在
热点开启期间全部失败，关闭热点后认证立即恢复。这是失败证据，不是验收证据；它不能证明本地
端口绑定是根因，但促使重新核对 D520 本地端点与线级输入。

历史 Drcom-CLI 协议设计由人类持有并确认以下事实：

- D520 为 JLU 同时绑定本地与远端 UDP 端口 `61440`。
- 本地端口与远端端口是两个独立的协议配置值。
- D520 必须支持显式 fixed 与 operating-system-assigned 两种本地端口模式；JLU 使用 fixed
  `61440`。
- 所选接口 IPv4 仍是本地绑定地址，且同一个所选绑定拥有 MAC/IP/DNS/DHCP 线级事实。
- 连接式 UDP 的远端校验仍然必需。
- Challenge 种子的随机偏移应遵循 Drcom-CLI。
- 历史固定宽度填充输入应作为严格校验的 Profile 字段回归。

在此之前的实现把本地端口硬编码为 OS 分配（`Port: 0`），没有 fixed 模式，也没有把三段
固定宽度 Login 填充作为显式 Profile 字段。

## 决定

### `localPort` 与 `serverPort` 是独立的端点维度

机构 Profile 新增必填字段 `localPort`，独立于 `serverPort`。`serverPort` 是远端 Profile
端点端口；`localPort` 是单个连接式 UDP socket 绑定的源端口。二者是严格 JSON tagged 对象，
恰为下列两种形式之一：

```json
{"mode":"fixed","value":61440}
```

```json
{"mode":"system_assigned"}
```

- `mode` 必填，只接受 `fixed` 或 `system_assigned`。
- `fixed` 需要整数 `value` 在 `1..65535`。
- `system_assigned` 禁止 `value`。
- 缺失、null、非对象、畸形、未知嵌套字段、缺 mode、未知 mode、非法 value 与尾随嵌套 JSON
  一律拒绝。错误可以命名字段与违反规则，但不得复现原始 JSON。
- 不存在隐藏默认或兼容回退。

解码后的选择表示为一个私有不可变值，不进入 `ProtocolContextOverride` 或任何公开协议接口。

### 连接式 UDP 本地端点归属

D520 Run 拥有的唯一 socket 仍是 IPv4 连接式 UDP socket。

- 本地地址是已选 `SelectedSystemNetworkBinding` 的 IPv4。
- 本地端口是解码后的 fixed 值，或仅 `system_assigned` 时为 `0`。
- 远端端点仍是 Profile `serverAddress:serverPort`。
- fixed 绑定冲突或权限失败经既有网络失败路径返回，绝不回退到端口 `0` 重试。
- 连接式 UDP 仍是把数据报过滤到预期远端地址与端口的机制。
- socket 取消、deadline、响应大小上限、诊断、归属与清理不变。

不新增 `SO_REUSEADDR`、`SO_REUSEPORT`、全局 socket 状态或第二个 socket。不在 D520 内重新
选择接口。

### JLU 选择 fixed 61440

真实 JLU Profile 使用 fixed 本地端口 `61440` 与 server 端口 `61440`。fixed 绑定失败是一次
真实 Run 失败，绝不回退到系统分配端口。已经校园接受的 330 字节 Login、D520 Type 6 引导
行为、响应过滤、重试策略、Run 状态与注销语义不变。

包内不证明 fixed 行为的测试显式使用 `{"mode":"system_assigned"}` 以保持无冲突；JLU 人类
Profile 与组合 fixture 使用 `{"mode":"fixed","value":61440}`。

### 所选绑定拥有本地线级事实

Run 继续从同一个已选绑定推导 MAC、上报 IPv4、DNS 与 DHCP。本切片只修正 UDP 本地端点，不做
新的网络选择决策。route-aware 选择与全局接口集合变化时重建 Session 仍是后续工作。

### Challenge 种子偏移范围

Challenge 字节 `[2,4)` 仍是 little-endian：

```text
(timestamp_seconds + random_offset) % 0xFFFF
```

`random_offset` 从闭区间 `0x0f..0xff` 采样，匹配经审计的 Drcom-CLI 行为。使用标准库随机源
与一个私有纯投影 helper，使测试能在 `0x0f` 与 `0xff` 两个闭端点、时间戳回绕与 little-endian
编码上确定性证明，而不依赖概率断言或生产测试缝。熵回退若保留，仍使用该区间内的偏移。
Challenge padding 仍是既有的独立 15 字节 Profile 字段。

### 三段固定宽度 Login 填充

新增三个必填 Profile 字段，使用确切的 lower-camel JSON 名与字节宽度：

- `loginIPDogPaddingHex`：4 字节，写入 `[106,110)`；
- `loginDHCPPaddingHex`：8 字节，写入 `[154,162)`；
- `loginAuthExtensionPaddingHex`：2 字节，写入 `[326,328)`。

它们是严格十六进制字符串，输入大小写不敏感，并使用与既有固定宽度字段相同的校验/错误纪律。
缺失、宽度错误或非十六进制值使配置校验失败。它们按值穿过私有不可变 config 与 `loginInput`，
只写入其文档区间。它们不得改变：

- Login 长度 `330`；
- header、username、hash、MAC XOR/raw MAC、DNS/DHCP/IP/host/OS 字段；
- CRC-1968 输入或输出（JLU 全零填充时 CRC 不变；非零填充时 CRC 按新输入正确重算）；
- 随机 auth-extension tail `[328,330)`；
- Logout 或 keepalive 报文。

文档化 JLU Profile 使用全零值，因此既有 byte-exact 330 字节接受 Login fixture 不变。

## 结果

- `localPort` 与 `serverPort` 现为独立端点维度；fixed/system-assigned 语义明确，fixed 绑定
  失败不回退。
- JLU D520 选择 fixed `61440` 两端；所选绑定仍拥有本地 IPv4/MAC/DNS/DHCP 事实。
- 三段 Login 填充与 Challenge 偏移区间成为显式 Profile 字段与经审计范围。
- 热点证据促成本次变更，但在 Windows 现场 rerun 之前不证明因果性。
- 本切片完成代码与自动验证（聚焦测试、race、Windows/Darwin 测试编译与完整公开 verifier）。
- Windows 原生与校园现场复核仍是独立证据，本决定不声称 Windows 原生或校园成功，不修改已接受
  的 330 字节报文声明。
- route-aware 选择与全局接口变化反应仍为后续工作；Environment 选择与 launcher 行为不变。
