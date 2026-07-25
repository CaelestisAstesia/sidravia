# Dr.COM transcript v1

`transcript_version: 1` 是 `sidravia` Go 客户端和本地 Python mock server
共享的线级记录格式。它用于离线复现和对齐；记录本身不会发起网络请求。

## Packet record

每行是一个 UTF-8 JSON object。packet record 的字段如下：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `transcript_version` | integer | 固定为 `1`。 |
| `record_type` | string | packet record 固定为 `"packet"`。 |
| `side` | string | 产生记录的一侧：`"client"` 或 `"server"`。 |
| `direction` | string | 相对该侧的 `"send"` 或 `"receive"`。 |
| `operation` | string/null | `challenge`、`login`、`ka1`、`ka2`、`logout`，无法分类时为 `null`。 |
| `payload_hex` | string | UDP payload 的小写十六进制全文。 |
| `payload_sha256` | string | 原始 payload 的 SHA-256 小写十六进制摘要。 |
| `payload_length` | integer | 原始 payload 字节数。 |
| `local_endpoint` | endpoint/null | 记录侧本地的地址和端口。接受 Go object 或 Python 二元素 array。 |
| `remote_endpoint` | endpoint/null | 对端地址和端口。接受 Go object 或 Python 二元素 array。 |

endpoint 的两种等价 JSON 输入为：

```json
{"host":"127.0.0.1","port":61440}
["127.0.0.1",61440]
```

## Decision record

服务端完整 trace 和 Go client transcript 都可以包含判断记录：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `transcript_version` | integer | 固定为 `1`。 |
| `record_type` | string | decision record 固定为 `"decision"`。 |
| `side` | string | 观察判断的一侧：`"client"` 或 `"server"`。 |
| `operation` | string | 判断所属协议操作。 |
| `outcome` | string | 例如 `success`、`failure`、`drop` 或 `timeout`。 |
| `wire_error_code` | integer/null | 协议线上错误码；无线码时为 `null`。 |
| `drop_reason` | string/null | 丢弃/超时原因；不适用时为 `null`。 |

`extract_transcript_records` 从完整 server trace 每行的 `details` 同时提取 packet
与 decision record，忽略普通诊断事件。

## 格式与自完整性

上表列出的字段全部必填。允许 `null` 的字段也必须显式存在，不能用省略字段表示
`null`。额外的 timestamp、monotonic time、PID、sequence 等运行元数据可以存在，但
不参与对齐。

每条 record 在按类型分流和语义比较前必须通过 schema 验证：

- `transcript_version` 必须是整数 `1`，`record_type` 必须是 `packet` 或
  `decision`，`side` 必须与输入侧一致；
- packet 的 `direction`、`operation`、三项 payload 字段和两个 endpoint 都必须存在；
- `payload_hex` 必须是无空格的小写规范十六进制；`payload_length` 必须等于解码后的
  字节数；`payload_sha256` 必须等于这些字节的 SHA-256；
- endpoint 只能是 `null`、恰含 `host`/`port` 的 Go object，或恰含两项的 Python
  array；host 是非空字符串，port 是 `0..65535` 的整数；
- decision 的 `operation`、`outcome` 是非空字符串，`wire_error_code` 是整数或
  `null`，`drop_reason` 是字符串或 `null`，四个字段都不能缺少。

因此，即使 client/server 两侧包含完全相同的 typo 或相同的伪造 payload 元数据，也
不会被过滤或误报成功。公开 Python API 抛出 `TranscriptFormatError`。server trace
`details` 没有 transcript 标识时仍视为普通诊断事件并忽略；只要带有
`transcript_version` 或 `record_type` 标识，非法 version/type/schema 就是格式错误。

Go 端可使用等价的 JSON 结构（endpoint 的具体 Go 类型可按已有网络层调整）：

```go
type EndpointV1 struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

type TranscriptPacketV1 struct {
    TranscriptVersion int       `json:"transcript_version"`
    RecordType        string    `json:"record_type"`
    Side              string    `json:"side"`
    Direction         string    `json:"direction"`
    Operation         *string   `json:"operation"`
    PayloadHex        string    `json:"payload_hex"`
    PayloadSHA256     string    `json:"payload_sha256"`
    PayloadLength     int       `json:"payload_length"`
    LocalEndpoint     *EndpointV1 `json:"local_endpoint"`
    RemoteEndpoint    *EndpointV1 `json:"remote_endpoint"`
}

type TranscriptDecisionV1 struct {
    TranscriptVersion int     `json:"transcript_version"`
    RecordType        string  `json:"record_type"`
    Side              string  `json:"side"`
    Operation         string  `json:"operation"`
    Outcome           string  `json:"outcome"`
    WireErrorCode     *int    `json:"wire_error_code"`
    DropReason        *string `json:"drop_reason"`
}
```

## 对齐规则

记录先按 `record_type` 分成 packet 与 decision 两个流，每个流保持原出现顺序。
工具先比较 packet 流，再比较 decision 流，绝不把两种 record 按原始行号混配。

packet 流规则：

- `client/send` 对应 `server/receive`；
- `client/receive` 对应 `server/send`；
- `operation`、`payload_hex`、`payload_sha256`、`payload_length` 必须相等；
- client `remote_endpoint` 对应 server `local_endpoint`，作为 server endpoint，
  host 和 port 都必须相等；
- client `local_endpoint` 对应 server `remote_endpoint`，作为 client endpoint，
  host 必须相等，只有 client 临时 port 可以不同。

decision 流通常严格比较 `outcome`、`wire_error_code` 和 `drop_reason`。唯一的视角
映射是：server `outcome=drop`、`wire_error_code=null`、非空协议 `drop_reason`
对应 client `outcome=timeout`、`wire_error_code=null`、`drop_reason=timeout`。该映射
不比较两侧 reason 文本；任何其他组合都按三个字段严格比较。

任一侧 version、record type、side 或 packet direction 非法都会定位到该类型流索引；
长度不同时，差异定位到该类型流第一条缺失记录。首个差异含 `record_type`、类型流内
`index`、`field_path`、`reason` 和 client/server 两侧完整 record。

只有 timestamp、monotonic time、PID、sequence 和 client 临时 port 不参与相等
判断。server IP/port、client IP 和三项 payload 身份字段都不能忽略。

## 离线命令

在仓库根目录设置 Python source path 后，可运行：

```powershell
$env:PYTHONPATH = "tools/drcom520d_mock_server/src"
python -B -m drcom520d_mock_server.offline payload 0102000000000000000000000000000000000000
python -B -m drcom520d_mock_server.offline pcap .\capture.pcap --server-port 61440
python -B -m drcom520d_mock_server.offline compare .\client.jsonl .\server-trace.jsonl
```

`payload` 和 `pcap` 输出 UTF-8 JSONL。`compare` 成功返回 0；只有通过完整 schema 和
payload 自完整性验证后的稳定语义差异返回 1；文件、JSON、hex、PCAP 或 malformed
transcript 返回 2，并输出中文格式错误。语义差异输出一行 UTF-8 JSON，包含中文
摘要、六项定位信息和两侧完整 record。

PCAP 支持 classic PCAP 的大端/小端、微秒/纳秒时间戳，linktype 必须为 Ethernet
(`1`)，且只解析 IPv4 UDP。按 `server_port` 过滤后，工具以 server 为记录侧：目标
端口是 server port 时为 `server/receive`，源端口是 server port 时为
`server/send`，endpoint 也以 server 为 local。UDP length 必须精确等于 IPv4
payload 长度；源端口和目标端口同时等于 server port 时因方向歧义而拒绝。

CI 和单元测试完全不依赖 Npcap、Scapy 或任何实时抓包能力。classic PCAP 解析仅供
离线诊断，所有测试 fixture 都由 Python 标准库在内存中构造。
