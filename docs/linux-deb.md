# Debian 安装包

开发版目标环境为 Debian 13 的 amd64 与 arm64。使用与系统架构一致的 DEB，架构可用 `dpkg --print-architecture` 查看。需要桌面界面时选 `sidravia`，仅用命令行时选 `sidravia-core`。两个包各自包含核心，使用同一程序目录，安装时选择其一。

在下载目录执行 `sudo apt install ./实际文件名.deb`，APT 会解析该包声明的依赖。后续版本通过同一命令升级。打开应用菜单或运行 `sidraviactl update version` 核对版本；GUI 的更新检查提供当前安装对应的系统包，文件替换由包管理器执行。

程序及其私有库装在 `/usr/lib/sidravia`，命令入口在 `/usr/bin`。账号与设置使用当前用户的 XDG 目录。升级前保存未提交的 GUI 编辑，保持当前实例可正常退出；包维护期间暂缓再次启动。若 dpkg 报配置中断，可依其错误消息解决问题，然后运行 `sudo dpkg --configure -a` 重试。

用 `sudo apt remove sidravia` 或 `sudo apt remove sidravia-core` 移除已安装包。用 `sudo apt purge 包名` 清理该包的系统配置和维护状态。账号、个人日志、另一个便携安装和用户导出文件按各自归属处理；需要清除个人数据时参阅[数据位置](privacy.md#linux)。
