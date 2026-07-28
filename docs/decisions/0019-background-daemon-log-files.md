# ADR 0019：Windows 后台 daemon 日志文件

**状态：** Accepted

## 背景

ADR 0013 决定生产 logger 使用 stderr，并在当时明确 CLI 不拥有日志文件。ADR 0016
随后加入 Windows 后台 child，但它继承前端 stdout/stderr，导致 PowerShell 持续显示
daemon 日志，也没有有界的无人值守诊断历史。

## 决定

本决定替代 ADR 0013 中“后台 CLI 启动不拥有日志文件”的结论，并补充 ADR 0016 的
Windows child output ownership。直接运行 `sidraviad.exe` 时 logger 仍使用 stderr；
Windows CLI 真正创建后台 child 时，将 stdout/stderr 同时指向
`%LOCALAPPDATA%\Sidravia\logs\sidraviad.log`，不继承 stdin，并使用无新控制台窗口的
创建标志。

launcher 请求 `0700` 日志目录和 `0600` 文件权限，以 append 打开当前文件。启动前文件
达到 10 MiB 时，删除旧 `sidraviad.log.1`，把当前文件改名为唯一 `.1` 备份，再创建新
当前文件。任一文件操作或 child start 失败都返回安全操作语义并保留 cause；父进程在
Start 后关闭自己的句柄，child 保留继承句柄。

## 结果

后台 CLI 安静返回，并保留当前日志与一个有界备份。删除旧备份是单备份产品契约的一部分；
未达阈值的当前日志继续 append。ADR 0013/0015 的事件、等级、stderr logger 和隐私规则
保持不变，Trace 报文字节可能持久化，因此仍是显式敏感 opt-in。不增加日志 IPC、tail、
异步队列、压缩、上传或非 Windows daemon control。
