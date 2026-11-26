# 架构概览

Sidravia 将认证核心与用户界面分开。GUI 面向日常使用，CTL 提供高级控制；两者使用同一后台能力，不各自实现认证规则。当前协议实现为 Dr.Com 5.2.0 D，新增协议需要实现和验证对应代码。

## 程序组成

| 程序 | 职责 |
| --- | --- |
| `Sidravia.exe`（Windows）/ `sidravia_gui`（Linux） | Flutter 界面、窗口、托盘、通知和学校公告 |
| `sidraviactl` | 命令行意图收集、结果呈现，以及 GUI 启动所需的受限桥接 |
| `sidraviad` | 配置和凭据、网络观察、认证会话与协议执行 |
| `sidraviamgr` | Windows 当前用户 PATH 和登录自启；卸载与文件清理由安装器负责 |
| `sidraviaupdater` | 执行便携版和 Windows 安装版的更新下载、核验、应用和恢复 |

Windows 和 Linux 的 `sidraviactl update check` 在 CTL 进程内检查更新。应用更新需用户确认，并核验当前版本和目标版本的签名与文件清单。RPM 由系统包管理器维护。见[更新说明](advance.md#更新检查与下载)。

本文描述当前源码职责，各发行版的支持范围以对应说明为准。Linux 核心包不包含 MGR，Linux GUI 的系统集成见 [Linux 便携包说明](linux-portable.md)。

GUI 与 CTL 通过本机回环 WebSocket 调用后台。客户端不直接改写后台配置文件；维护工具不拥有认证会话。

## 对象与状态归属

| 对象或模块 | 拥有的事实 |
| --- | --- |
| Institution Profile | 随版本提供的非秘密机构参数、协议选择和可选公告入口 |
| Configuration Catalog | 保存的账号、密码、绑定、覆写与自动连接偏好 |
| Environment | 主机信息、网卡与网络观察 |
| Supervisor | 会话集合、单活动准入和网络快照分发 |
| Session | 连接意图、本次不可变运行定义、状态与重试 |
| 协议 Factory / Run | 协议输入校验和一次可取消的协议执行 |
| daemon app | 跨所有者用例的执行顺序 |
| GUI | 界面偏好、草稿、窗口、通知和公告呈现 |

配置 ID 随持久数据保留；Session ID 只属于当前后台，后台重启后失效。同一后台最多一个活动 Session，可保留多条运行记录。Profile 不是用户账号，也不是可执行插件。

## 运行模式与生命周期

**headless** 后台供 CTL 使用。命令结束不结束后台；后台继续维持连接，直到显式停止或自身退出。

**desktop** 后台绑定 GUI 的精确进程身份。GUI 退出或崩溃后，后台需要有界取消、等待与清理；窗口隐藏到托盘不是退出。

**systemd** 后台由 Linux 用户服务管理器托管。GUI 和 CTL 可连接同一后台，退出 GUI 后后台继续运行。主动停止服务或切换管理方式请用[用户服务命令](linux-portable.md#systemd-用户服务)。

同一用户的数据实例只能由一个后台管理。平台差异集中在主机、文件保护与系统集成等边界，认证核心保持共享。未支持的能力明确返回 Unsupported。

## 配置变更与状态读取

修改认证输入时，应用先校验候选值并提交持久配置，再退役旧的不可变 Session。保存本身不自动连接，GUI 可根据用户确认另外发起连接。保存失败与提交后的清理失败需要区别呈现。

客户端通过公开查询、快照和有界状态事件读取后台事实，不独立推导或拥有认证状态。事件只传递当前状态变化。

协议覆写由具体协议校验，只改变允许的有效输入，不改写实际网卡观察和 socket 绑定。错误保留稳定机器码、安全用户描述与内部原因各自的边界。

## 源码入口

1. [后台组合与生命周期](../cmd/sidraviad/runtime.go)。
2. [应用编排](../internal/daemon/app)和 [IPC 契约](../internal/ipc/contract)。
3. [认证会话](../internal/daemon/authentication/session)与[协议契约](../internal/daemon/authentication/protocol)。
4. [命令行](../internal/cli)与 [Flutter 应用层](../gui/lib/application)。
5. [Windows 维护工具](../internal/installation/inno)与[独立更新器](../internal/update)。

命名与领域术语见[命名与模块约定](code-conventions.md)。构建与验证见[开发指南](development.md)，协议扩展见[协议支持与适配](protocols.md)。
