# ADR 0001：使用本机回环 WebSocket

状态：Accepted

## 决定

CLI、未来 GUI 和 daemon 通过本机回环 WebSocket 通信。消息使用小型自定义 JSON contract。

## 原因

WebSocket 可以同时服务 Go CLI 和常见 GUI 技术。它比 gRPC 或代码生成框架更小，也比 Windows named pipe 更容易复用于其他平台。

产品只需要本机通信，因此不需要远程 RPC 功能。

## 结果

IPC server 只监听回环地址。server 在升级连接前验证随机 token 和精确 BuildID。

IPC 不支持远程连接，也不提供通用 RPC。未来如果必须替换 transport，稳定 contract 和 daemon app 边界应保持不变。
