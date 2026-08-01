# ADR 0031：Windows install/PATH/登录集成

**状态：** Superseded by ADR 0033

## 历史决定

本 ADR 曾把 Windows 用户 PATH 与登录任务集成实现为
`sidravia install` / `sidravia uninstall` Go CLI 叶子，并规定：

- 注册当前可执行文件目录，不复制二进制；
- 管理当前用户 PATH 与登录任务 `SidraviaDaemon`；
- uninstall 只撤销集成，不删除 Configuration、凭据、Profile 或日志；
- Windows-native 验证与交叉编译分开报告。

ADR 0033 保留了用户 PATH、登录任务、幂等和不删除数据的行为，但将
实现替换为随包 PowerShell 部署单元。当前产品不存在这两个 Go CLI 命令。
