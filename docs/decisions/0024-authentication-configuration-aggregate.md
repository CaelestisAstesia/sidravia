# ADR 0024：持久认证配置是包含唯一私有密码的聚合

**状态：** Implemented

## 背景

早期 ADR 0002 和 ADR 0006 把非秘密 Configuration 与独立 Credentials Store 分开：
Configuration 通过 CredentialID 引用另一份 JSON 中的用户名和密码。这个边界为可复用
凭据或立即替换系统秘密后端留下空间，但当前产品已经确认：

- 一份 Authentication Configuration 恰好拥有一个用户名和一份密码；
- 密码不在多份 Configuration 间共享；
- Credential 没有独立的用户身份、名称或生命周期；
- 用户只管理 Configuration，不读取 CredentialID；
- 删除 Configuration 必须删除其密码；
- 当前单用户威胁模型允许明文落盘，系统秘密后端和 WSS 延后。

因此双 Store 让一次创建或删除跨越两个文件，却没有当前产品收益。两个独立原子替换也
不能组成一个文件系统原子事务，容易产生悬空 Credential 或缺失密码的 Configuration。

显式便携模式还可能位于不支持 Windows ACL 的文件系统。完全静默降级不可接受，但只因
权限模型不可用而永久禁止绿色版保存配置，也不符合已经确认的产品方向。

## 决定

### 一个聚合和一个权威文件

`AuthenticationConfiguration` 是唯一持久聚合根。它拥有：

```text
ConfigurationID
DisplayName
InstitutionProfileID
Username
Password（私有）
NetworkBindingPolicy
ProtocolContextOverride
```

ConfigurationID 长度为 1–64，只接受小写 ASCII 字母、数字和 `-`，首尾必须是字母或
数字。ID 由用户提供、持久稳定且首版不可重命名。DisplayName 可以为空；为空时 CLI
使用 ConfigurationID 呈现。Username 必须非空，Password 允许为空，与一次性认证一致。

整个聚合保存在一个 `configurations.json` 中。新的严格文档使用 schema version 2，
每个 record 同时包含 username 和 password，不再包含 CredentialID。文件按
ConfigurationID 稳定排序、严格拒绝未知字段、重复 ID、缺失字段、尾随数据和不受支持
的 schema。当前双文件 schema 从未形成公开管理入口，本切片不自动迁移、合并或删除
手工创建的旧文件；新 Store 对旧 Configuration schema 返回明确的不支持错误。

聚合 Store 是这份可变状态的唯一所有者。它先构造和验证完整候选文档，再原子替换
`configurations.json`，最后提交内存状态。创建、修改普通字段、替换密码和删除因此各自
只跨一个权威文件。

独立 Credentials Store、CredentialID 和运行目录中的 `credentials.json` 路径被删除。
认证运行仍可使用一个只表示 username/password 值的私有
`AuthenticationCredential`；删除独立持久生命周期不意味着把密码散入 IPC、Session
Snapshot 或日志。

### 公开视图与秘密边界

Configuration 的公开 DTO 只包含：

```text
ConfigurationID
DisplayName
InstitutionProfileID
InstitutionDisplayName
AuthenticationProtocolID
Username
CredentialStored
StorageProtection
```

`CredentialStored` 只说明聚合具有密码字段；`StorageProtection` 只接受稳定值
`protected` 或 `unprotected`。任何 list/get/create/update/set-password/remove/start
响应都不包含 Password。产品不提供读取、显示、复制、导出或回传密码明文的 IPC/CLI。

密码只允许进入 create 和 set-password 的 typed 请求，以及 daemon 内部解析出的
`RunDefinition`。密码不得进入 argv、普通日志、错误、Snapshot、请求诊断、响应或测试
失败文本。Username 不是秘密，沿用当前完整显示和安全日志规则。

以后 DPAPI、Credential Manager、Keyring 或其他后端可以改变聚合 Store 内部的秘密字段
表示，或在新 schema 中保存受保护 blob/reference；它们不得改变顶层 CLI、公开 DTO、
Application 用例或 Session 契约。

### 安装版和便携版的保护策略

安装版继续严格要求当前用户保护。目录、既有目标或原子替换临时文件无法建立所需权限
时，读取和写入失败，不创建或放宽明文目标。

便携版同样先尝试严格保护。只有平台明确报告“当前文件系统不支持所需权限模型”时，
存储边界才可进入 `unprotected`；普通拒绝访问、无效路径、IO、损坏、竞争或未知错误
不得降级。便携 daemon 可以在 unprotected 状态启动、读取既有文件并写运行信息，但
必须发出固定 Warn，不记录路径、用户名、密码、token 或错误文本。

在 unprotected 状态：

- create 第一份或新增一份含密码的 Configuration；
- set-password 替换密码；

必须获得本次操作的显式授权。交互 CLI 显示风险并使用默认否的确认；非交互调用必须
提供 `--allow-insecure-storage`。该授权是 typed request 的布尔意图，不是密码的一部分，
不写入配置，不允许安装版绕过权限错误。

读取、list/show、删除以及只修改非秘密字段不要求重复授权，但每次 CLI 结果仍显示
`StorageProtection=unprotected`。警告必须说明同目录访问者可能读取或修改
Configuration/Password、daemon runtime token，以及持久化的敏感 Trace 日志。便携模式
本身不等于用户已经授权写入新密码。

### Session 关联和操作语义

每个 Configuration 在一个 daemon 进程中最多关联一个 retained Session。

`auth start --config <id>` 是按 Configuration 表达的 ensure-running：

- 无关联 Session 时，从聚合当前值解析并创建新 Session；
- 关联 Session 已活动时返回同一权威 Snapshot，不增加 revision 或创建 Run；
- 关联 Session 为 suspended 时重新激活同一 Session；
- 关联 Session 正在 stopping 时等待现有清理，再按已有 ensure 语义恢复；
- Configuration 已更新而旧 Session 仍存在时，旧 Session 继续使用创建时的不可变
  RuntimeDefinition；命令返回同一 Session，不暗中替换账号、密码、Profile 或网络行为。

create/update/set-password 不自动停止或重启 Session。CLI 必须说明新值只会在删除旧
Session 并从 Configuration 再次启动后生效。

`config remove <id>` 由 daemon app 在同一 Configuration 操作边界内：

1. 预留该 Configuration；
2. 对关联 Session 执行 Supervisor remove；在线时先停止并等待协议、actor、cleanup 和
   revision forwarder 全部退出；
3. 从 Session 集合和 Configuration 关联中删除它；
4. 原子删除包含密码的聚合 record。

如果 Session 清理失败，持久 record 保持不变。Session 已成功删除但文件替换失败时，
完整 Configuration 和密码仍存在，重试 remove 可以继续；不得留下半个持久聚合。
一次性 Session 不属于任何 Configuration，不受 config remove 影响。

### CLI 与 Settings 边界

持久配置使用顶层资源：

```text
sidravia config list
sidravia config show <configuration-id>
sidravia config create
sidravia config update <configuration-id>
sidravia config set-password <configuration-id>
sidravia config remove <configuration-id>
sidravia auth start --config <configuration-id>
```

`auth` 继续只管理当前认证和 Session，`profile` 继续只读机构模板。未来全局产品设置使用
顶层 `settings`；本决定不接入已有的 AutoConnect SettingsStore，也不实现自动连接。

CLI 使用 ADR 0021 的 Cobra + termenv 逐行交互。裸 create/update 在真实终端可以提示
缺失字段、选择 Profile、隐藏输入密码并确认破坏性或不安全操作；重定向时不得隐式进入
向导。所有能力同时有确定的非交互 flag 形式。remove 的非交互形式要求 `--yes`。

## 结果

- 本决定替代 ADR 0002 和 ADR 0006；ADR 0003 中“CredentialID 持久化”和 ADR 0008
  中“双 Store 解析”的细节也由本决定修正，SessionID 运行期边界和永久一次性启动不变。
- ADR 0022 的实施顺序、当前单用户威胁模型和 WSS 延后决定继续有效；其中“独立
  Credentials Store”的具体存储方式由本决定替代。
- 一个聚合只有一个持久所有者，消除跨文件正常操作事务和孤立 Credential 生命周期。
- Configuration 文件现在整体属于秘密文件；CLI 可显示的是显式公开投影，不是磁盘
  record。
- Portable unprotected 是明确、可观察且逐次授权新增秘密的降级，不是任意权限错误的
  静默 fallback。
- Settings、自动连接、配置导入/导出/克隆/重命名、DPAPI/Keyring、WSS、GUI、
  Linux/WSL host 和 route-aware 网络选择仍属于后续切片。
