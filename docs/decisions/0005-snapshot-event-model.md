# ADR 0005：事件发送最新完整 Snapshot

状态：Accepted

## 决定

每个 Session 状态事件包含资源 ID、单调 revision 和最新完整公开 Snapshot。

## 原因

前端不需要正确应用一系列增量补丁。IPC 可以把积压事件合并为最新状态，客户端重连后也只需查询一次权威 Snapshot。

## 结果

IPC 不实现事件确认、历史重放或补发。server 不能丢失请求对应的 Response。

如果客户端持续跟不上事件，server 应关闭连接。Snapshot 不得包含秘密或内部错误链。
