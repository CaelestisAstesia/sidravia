# Sidravia Dr.COM Wireshark Lua dissector

`drcom.lua` 是 Sidravia Windows Dr.COM 验收工具使用的现代 Wireshark Lua
dissector。它注册 UDP 61440，也可通过 Wireshark 的 **Decode As** 手动选择。

## 稳定机器接口

`drcom.schema_version=1` 下，`drcom.packet_kind`、`drcom.direction`、
`drcom.profile`、长度字段以及 `valid/truncated/malformed/unknown` 状态字段是
供 `tshark -T json` 和 `-T fields` 消费的接口。状态字段应整体判断，不能只看
Wireshark 协议列。`drcom.payload` 可能包含认证报文原始字节，应按敏感数据处理。

吉林大学 5.2.0(D) 布局按方向、长度和单包可证明的固定字节严格验证。历史
Dr.COM 2011 类型只保留经过长度保护的操作码/子型分类；未知子型不会被猜成某种
业务包。历史分类参考了用户提供的只读 GBK 脚本，但该脚本来源和许可未确认，
本仓库没有复制或分发其源码、说明文字或推测性字段。

## 加载与验证

仓库中的 Python 静态契约测试不等于 Wireshark Lua 运行时验证。安装
Wireshark/Npcap 后，应从仓库根目录运行：

```powershell
tshark.exe -n -r <fixture.pcap> `
  -X lua_script:tools\wireshark\drcom.lua `
  -T fields `
  -e drcom.schema_version `
  -e drcom.packet_kind `
  -e drcom.direction `
  -e drcom.valid `
  -e drcom.truncated `
  -e drcom.malformed `
  -e drcom.malformed_reason `
  -e drcom.unknown
```

再用 `-T json` 验证字段形状，并以正常、未知、畸形和 snaplen 截断 PCAP 覆盖
四类状态。不要把 PCAP 或完整 JSON 上传到 issue、聊天或普通日志。

每次运行时都会动态发现 `tshark.exe`：可用时，`test_tshark_runtime.py` 执行真实 Lua
runtime fixture；不可用时，该测试作为 unittest 跳过。Windows 验收预检同样动态解析
Wireshark、tshark、dumpcap 并查询 Npcap，前置工具缺失或不可用会报告可操作的 FAIL，
其依赖的 Lua、设备、映射和权限检查会明确跳过。实际命令输出才是该次宿主机的事实；
静态 Python/Lua 契约检查、真实 tshark fixture 执行和真实网络抓包是不同证据，自动
检查不声称真实网络抓包或认证已经完成。
