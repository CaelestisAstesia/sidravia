# Sidravia Windows Dr.COM 验收基础设施（第一阶段）

本目录提供离线三方证据比对、中文环境预检、Wireshark Lua 加载验证，以及只允许
`dumpcap.exe` 继承提升令牌的抓包 broker。它不实现 Dr.COM 认证协议，也不修改
Sidravia 的认证核心、Session 或 Supervisor。

第一阶段完成表示“离线验收基础设施可测试”。它不表示吉林大学校园网认证成功，
也不表示真实抓包、Session 生命周期或清理流程已经在校园网环境通过。

## 当前可运行命令

从仓库根目录运行：

```powershell
$env:PYTHONPATH = "$PWD\tools\windows_drcom_acceptance\src"
python -m sidravia_drcom_acceptance --preflight
python -m unittest discover -s tools\windows_drcom_acceptance\tests -v
```

离线比对已有的四份敏感证据：

```powershell
python -m sidravia_drcom_acceptance --validate `
  --transcript <application-transcript.jsonl> `
  --transport <transport-observation.json> `
  --snapshot <session-snapshot.json> `
  --tshark-json <tshark.json> `
  --require-keepalive
```

如果验收窗口不要求 KA1/KA2，省略 `--require-keepalive`。比对通过只证明提供的
三方证据内部一致，不证明证据来自实网。

## 中文 preflight

`--preflight` 是只读操作，不调用 capture broker、不提示凭据、不启动认证。退出码为：

- `0`：当前只读检查通过；不代表存在可运行的真实网络入口；
- `2`：工具、Lua、网卡映射、权限或 Sidravia 集成契约不完整；
- `3`：验收器自身发生已清洗的内部错误。

截至 2026-07-22 的本机复测发现，机器上已经存在 Wireshark/tshark/dumpcap 4.6.4
和运行中的 Npcap；本任务没有下载或安装它们。Lua schema v1 已由真实 tshark 的
离线 fixture 加载通过；额外的合成 PCAP runtime 测试已覆盖 13 个 JLU/legacy 分支
以及正常、unknown、malformed、truncated 四类状态。预检仍以 `2` 退出，因为普通用户读取 `Get-NetAdapter`
被拒绝，且未找到 `sidravia.exe`、`sidraviad.exe`，所以 acceptance contract 被
跳过。该结果没有启动抓包或认证。

单元测试另覆盖完全缺少 Wireshark/Npcap 的环境：它会给出中文安装操作，并把 Lua
加载、抓包枚举、网卡映射和权限项标为“跳过：前置工具缺失”，不会误报 Lua 损坏。

## 凭据与敏感制品

CLI 没有 `--username` 或 `--password`。当前公开入口只有 `--preflight` 和
`--validate`；前者只读检查，后者只比对用户已提供的离线证据。保留的 broker、
process helper、fixture 和 evidence 模块是下一大版本协议研究的基础设施，不构成
可运行的真实网络验收合同，也没有公开的真实编排入口。

任何未来的生产合同都必须先定义凭据输入、敏感制品处理、清理责任和现场授权边界；
在此之前，不应以本工具执行抓包、认证或真实网络操作。

## 安装工具后才能执行的验证

若目标机尚未安装 Wireshark/Npcap，必须由用户自行批准并完成官方安装；本工具不会
下载、打开或静默运行安装器。安装后先执行：

```powershell
dumpcap.exe -D
tshark.exe --version
python -m sidravia_drcom_acceptance --preflight
```

对不含真实凭据的 fixture PCAP 验证稳定字段：

```powershell
tshark.exe -n -r <fixture.pcap> `
  -X lua_script:"$PWD\tools\wireshark\drcom.lua" `
  -T fields -E header=y -E separator=tab `
  -e frame.number -e ip.src -e udp.srcport -e ip.dst -e udp.dstport `
  -e drcom.schema_version -e drcom.opcode -e drcom.packet_kind `
  -e drcom.direction -e drcom.profile -e drcom.captured_length `
  -e drcom.reported_length -e drcom.valid -e drcom.truncated `
  -e drcom.malformed -e drcom.malformed_reason -e drcom.unknown

tshark.exe -n -r <fixture.pcap> `
  -X lua_script:"$PWD\tools\wireshark\drcom.lua" `
  -T json -J "frame ip udp drcom" > <sensitive-tshark.json>
```

需分别提供正常、未知 opcode、固定字节畸形和 snaplen 截断 fixture。生成的 JSON 与
PCAP 仍是敏感临时制品，验证后必须删除。

真实编排暂时没有公开入口。下一大版本需先由协议研究定义生产合同，再单独决定任何
现场验证的授权和操作边界；本 README 不把保留研究模块描述为可运行的真实网络路径。

## 历史 Lua 参考边界

用户提供的 `drcom_2011.lua` 是 GBK 编码的只读历史参考，来源和许可尚未确认。
本仓库不分发、不复制其源码；现代 dissector 只保留经设计规格列出的历史操作码和
子型分类，并使用独立字段、显式长度保护和测试。
