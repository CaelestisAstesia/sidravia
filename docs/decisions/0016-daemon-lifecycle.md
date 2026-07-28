# ADR 0016：daemon 生命周期与精确 generation 控制

**状态：** Accepted

## 决定

`daemon status` 是严格只读探测：缺少运行信息表示 stopped；格式错误或合法但不可达
返回固定安全错误，不删除、改写或替换运行信息。普通网络呈现使用
`<友好名称> — <IPv4>`（U+2014 EM DASH），没有友好名称时只显示 IPv4。

stop/restart 把首次成功探测得到的 endpoint、token、BuildID 和 PID 视为一个不可变
daemon generation。stop 向该 generation 发送 `daemon.stop`，并等待同一 generation
不可达；探测后连接消失视为该已联系 generation 已停止。restart 直接把首次探测结果
交给停止过程，不重新读取运行信息；malformed/unreachable 时不删除文件，而由正常
Windows start 与 host 的原子发布规则决定能否替换。

IPC server 的 response-committed callback 只在 `NewServer` 构造时注入，之后不可变。
它只在成功响应编码并写入后触发；拒绝、编码失败或写入失败均不触发。组合根用容量为一
的 channel 与 `sync.Once` 保存首个已提交 stop，即使 runtime 尚未进入 select 也不会
丢失，重复提交不会阻塞或排入额外 shutdown。组合根继续拥有取消、等待、Supervisor
关闭与最终日志顺序。

daemon 子进程启动仅支持 Windows。CLI 的平台无关 helper 从显式 parent environment
移除所有大小写不同的 `SIDRAVIA_LOG_LEVEL`，再追加唯一确定值；空选项解析为 `info`，
且不修改 parent slice 或进程环境。Linux/macOS 明确返回 Unsupported，domain 与 CLI
契约不因此分叉。

## 结果

生命周期代码和自动测试、race 测试及 Windows 交叉构建可以在非 Windows 环境完成；
Windows 原生验证与校园验证必须单独报告，本决定不声称它们已经完成。Work Package 4
的 Session ensure/restart/remove、WSS、持久 Configuration/Credentials 和 Linux
daemon 控制仍未实现。
