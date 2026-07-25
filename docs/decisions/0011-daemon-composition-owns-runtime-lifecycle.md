# ADR 0011：daemon 组合根统一拥有生产运行期

状态：Accepted

## 决定

`cmd/sidraviad` 是生产依赖和进程生命周期的唯一组合根。领域包继续提供构造器和阻塞式
能力，但不自行发现其他模块或启动整个产品。

生产运行期包含三个并发活动：

1. Windows host 和 HTTP/WebSocket server；
2. 阻塞式 Environment Observer；
3. 从 Observer channel 读取完整 Snapshot 并调用
   `Application.ApplySystemNetworkSnapshot` 的转交循环。

组合根为三者创建一个共同可取消 context。任一活动在共同 context 仍有效时返回错误，
组合根保留该错误、取消其他活动并等待全部退出。host 因正常退出信号返回成功时也执行
相同的取消和等待。所有活动退出后，组合根关闭 Supervisor 并等待其内部 Session 和
revision 转发 goroutine。

启动依赖、Profile 或 host facts 构造失败时，不启动上述运行期，也不发布 runtime info。
运行期开始后，Environment Observer 或 Snapshot 转交失败属于 daemon 级故障，不允许
daemon 悄悄继续运行并保留失效的环境事实。

## 原因

Environment Observer 的契约故意是阻塞式的，而 host 也需要阻塞到进程退出；两者必须由
更高层协调。让 Observer、Application 或 Supervisor 各自启动对方会产生多套取消源和
无人等待的 goroutine，也会让错误只能被静默吞掉。

`cmd/sidraviad` 已经是架构规定的组合位置。用一个小型私有运行期协调器即可形成真实纵向
链路，不需要新增通用 daemon framework、第三方 errgroup、全局 service locator 或把
Windows host 细节移入领域核心。

## 结果

- host 继续独占 mutex、listener、runtime info 和 HTTP server 的清理。
- Environment Observer 继续独占采集和 revision 生成。
- Supervisor 继续独占最新已接受网络快照和 Session 集合。
- Application 只转交 typed Snapshot，不创建第二份环境缓存。
- 组合根只拥有启动顺序、共同取消、等待和最终错误；它不解释网络事实或认证状态。
- 后续结构化日志可以在这一进程边界记录一次最终故障，不要求当前切片先建立日志系统。
- CLI 认证命令和真实 Windows/校园验收仍是后续独立边界。
