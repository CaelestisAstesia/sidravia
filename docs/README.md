# Sidravia 技术文档

本目录保存公开且长期有效的产品技术资料。

- [architecture.md](architecture.md)：当前系统结构、模块职责和数据流。
- [engineering.md](engineering.md)：代码设计、错误、并发、持久化和测试实践。
- [roadmap.md](roadmap.md)：产品阶段、完成条件和当前进度。
- [decisions/](decisions/)：已经接受的架构决策记录。
- [protocols/](protocols/)：协议实现规范和已知未决事实。
- [evidence/](evidence/)：不能只由源码或交叉编译证明的验收事实。

实现变化如果改变稳定架构边界，应同步更新架构文档或新增替代 ADR。自动测试、Windows
原生验证和校园网络验证必须分别陈述，不能合并成笼统的“已经完成”。
