# ADR 0003：SessionID 只在当前 daemon 中有效

状态：Accepted

## 决定

`ConfigurationID` 和 `CredentialID` 会持久化。`SessionID` 只标识当前 daemon 进程中的一次认证，不跨 daemon 重启。

## 原因

Session 表示一次具体运行，而不是账户或配置。持久化 Session 会迫使首版处理恢复、对账和删除屏障，但当前产品不需要这些复杂度。

## 结果

产品不存在特殊的 default Session。所有 Session 使用同一模型。

自动连接保存 `ConfigurationID`。daemon 重启后创建新的 Session，前端重新查询 Session 列表。
