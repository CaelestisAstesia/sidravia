# Sidravia 工程实践

本文规定代码设计、错误处理、并发、持久化、测试和验证实践。系统结构由
[当前架构](architecture.md)负责。

## 领域和依赖

核心认证代码使用清楚的领域语言。外围 JSON、文件与系统调用保持直接，不机械套用
Aggregate、Repository、Factory 或“一类型一接口”。只有真实替换点、平台边界和跨模块
契约需要接口。领域核心不得导入 CLI、WebSocket、JSON、Windows API 或具体持久化。

少量局部重复优先于错误抽象。平台无关核心不得为 Windows/Linux 复制；平台差异留在能力
附近的构建约束实现。

## 错误与日志

底层保留原始原因，上层包装当前业务动作。IPC server 把内部错误转换为稳定机器码，CLI
根据机器码生成固定人类消息；内部错误链不得进入 IPC、普通日志或用户文本。预期失败返回
错误，不用 panic，也不返回空值伪装成功。

拥有完整流程的模块决定是否重试；底层文件、网络或协议实现不得无限重试。系统通常只在
进程边界记录一次失败。

生产日志由 `cmd/sidraviad` 的唯一 `log/slog` logger 拥有。Info/Debug 只使用稳定事件码、
固定消息和显式字段白名单；不得记录原始 error、token、密码、Profile JSON、MAC、网关、
DNS/DHCP、接口 ID 或报文字节。Trace 是唯一允许完整 D520 数据报的位置，启用前必须标记
敏感。Windows launcher 的子进程 Wait cause 保留在错误链中，但不进入用户消息。

未支持平台或功能必须返回明确 Unsupported/NotImplemented。

## 并发和生命周期

创建 goroutine 的模块必须拥有其取消与等待。模块关闭后不得留下后台 goroutine，也不得
把锁、channel 或可变指针暴露给其他模块共同管理。

retained Session 生命周期操作由 Supervisor 按 ID 串行。等待 actor revision 或协议清理
时不得持有 Supervisor map mutex；取消等待必须释放私有准入。daemon 生命周期测试分别证明：

- status 严格只读；
- stop 只在成功响应写入后提交；
- 首个 stop 信号不丢失，重复提交不阻塞；
- stop/restart 始终使用首次探测的精确 generation；
- 子进程环境去除大小写不同的重复变量且不修改调用者输入。

## 持久化与安全

持久化先构造并验证完整候选，再原子替换目标；写入失败时不得提前改变内存权威状态。

Configuration schema 3 是唯一编码格式。严格 schema 2 文档仍可读取，默认
`AutoLogin=false`、`AutoReconnect=true`，且仅打开不会重写。schema 3 要求两个布尔字段。
最多一份 Configuration 启用 AutoLogin。

安装版秘密配置必须受当前用户与 SYSTEM 保护，失败即失败。便携版只有文件系统明确不支持
权限模型时才能报告 `unprotected`；新增或替换秘密需要当次显式授权。官方 Profile 位于
程序根 `institution-profiles/`，不属于秘密配置树。

Windows 受保护目录的 owner/LocalSystem ACE 带对象/容器继承，秘密文件 ACE 保持不可继承；
安全临时文件在写入前获得最终描述符。不使用 `TreeSetNamedSecurityInfo`、`icacls`、递归
遍历或宽松临时 ACL。

JSON 只用于 IPC 与持久化边界；领域模型不承担 JSON 编解码。

## 测试

测试证明公开行为、领域规则、错误分类和真实风险，不为私有实现建立脆弱 mock，也不为了
测试扩大生产接口。

- Go 单元与包级契约测试放在相邻 `*_test.go`；
- 测试数据放 `testdata/`；
- 跨模块纵向测试和现场验收通过公开入口；
- 并发测试覆盖已有风险，不穷举无证据排列；
- CLI 呈现测试不修改 termenv package-global 状态；
- mock 与本地 test peer 不能替代 Windows 或校园证据。

删除生产代码、测试或文档前必须说明其保护的行为或知识，并指出替代保护。机械批量编辑应
使用格式无关命令和确定性范围检查。

## 二进制协议

线协议代码优先便于逐字节对照。不同语义的请求和响应保留命名 builder/parser，只共享真正
相同的长度、opcode、固定字段、编码和密码学逻辑。不得反射或序列化 Go struct 内存布局。

parser 由当前请求阶段选择，并严格验证长度、opcode、固定字段和回显值。随机数、时间、
序号与服务端回填值属于一次执行状态，不属于 packet builder。

固定宽度 Profile 字段是严格十六进制；`localPort` 是独立 tagged 字段，fixed 失败不回退，
system-assigned 才允许 OS 分配端口。

## 仓库验证

完整公开验证使用固定 Go 1.26.4：

```bash
python3 tools/developer/verify_repository.py --scope all --go /path/to/go1.26.4
```

验证状态必须分开报告：

1. 代码写入；
2. 自动验证；
3. Windows 原生；
4. WSL/Linux 原生；
5. 校园网络；
6. Release readiness。

交叉编译、mock 或 WSL 不能替代 Windows-native 或校园现场证据。
