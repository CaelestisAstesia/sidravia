# ADR 0007：采用两个系统边界依赖

状态：Accepted

## 决定

Sidravia 直接依赖：

- `github.com/coder/websocket v1.8.15`
- `golang.org/x/sys v0.47.0`

## 原因

Go 标准库不提供 WebSocket client/server。`coder/websocket` 提供小型且支持 context 的 API，因此项目不需要自行实现 RFC 6455。

Windows host 需要命名 mutex、访问 token 和读取 SID。`x/sys/windows` 提供这些低级 Windows API。

## 结果

只有 IPC client/server 可以导入 `coder/websocket`。只有带 Windows 构建约束的平台文件可以导入 `x/sys/windows`。

项目不为这两个依赖建立通用抽象层。升级版本前，执行工具必须重新运行相关测试、Windows 交叉编译和 Windows 原生验收。
