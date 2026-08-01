# ADR 0033：Windows 用户态集成脚本

**状态：** Accepted；Supersedes ADR 0031

## 决定

Windows 用户态集成由随包 `scripts/install.ps1` 与 `scripts/uninstall.ps1`
拥有，不再使用 Go CLI install/uninstall 叶子。

- 两个 zip 都携带脚本；
- 脚本注册自身所在目录，安装/便携模式均可用；
- install 管理用户 PATH 与登录任务 `SidraviaDaemon`；
- 登录任务运行 `sidravia daemon start --log-level &lt;level&gt;`；
- 脚本幂等，并在执行前后验证 PATH 和任务动作；
- uninstall 绝不删除 Configuration、凭据、Profile 或日志。

脚本是 Windows 的每平台部署单元。未来 Windows Service 或 Linux systemd 属于
同类独立单元；统一安装器只负责编排，不必是 Go 实现。
