# Sidravia GUI

这里是 Windows 与 Linux x64 共用的 Flutter 桌面界面。安装和连接操作见[快速开始](../docs/quick-start.md)及[使用指南](../docs/guide.md)。

## 开发与验证

使用仓库固定的 Flutter 3.47.0。从仓库根运行分析与测试：

```sh
python3 tools/developer/dev.py check --scope gui --flutter /absolute/path/to/flutter
```

在配置好 Windows 工具链后，按[开发指南](../docs/development.md#windows-gui)构建 GUI。从 Windows 的 `gui` 目录使用 `flutter run -d windows` 时，应用会使用当前用户的实际后台与数据。

Linux 开发包构建与隔离集成测试从仓库根运行：

```sh
python3 tools/developer/dev.py linux --check-gui --flutter /absolute/path/to/flutter
python3 tools/developer/dev.py linux --integration --flutter /absolute/path/to/flutter
```

集成测试使用虚构凭据、独立 XDG 目录和本机环回网络。原生依赖与其他验收入口见[开发工具说明](../tools/developer/README.md#native-linux-desktop-development)。

## 浏览器预览

从仓库根启动离线交互预览：

```sh
python3 tools/developer/dev.py preview --flutter /absolute/path/to/flutter
```

打开 <http://127.0.0.1:7357>。工具栏可切换界面尺寸、外观与连接状态。配置和设置只保存在本次预览内存中，刷新即清空，请使用虚构账号。按 `r` 热重载，按 `q` 或 Ctrl+C 结束。加 `--check` 可运行预览测试并生成 Web 构建。窗口、托盘、系统通知和学校公告仍需原生验收。

## 源码入口

- [应用入口](lib/main.dart)和[页面装配](lib/features/shell/sidravia_shell.dart)
- [前端状态](lib/application)和[本机 IPC](lib/ipc)
- [平台能力](lib/platform)及 [Linux](linux/runner)、[Windows](windows/runner) 原生窗口

后台拥有账号、配置和认证状态。窗口、托盘及外观由 GUI 管理，运行模式见[架构概览](../docs/architecture.md#运行模式与生命周期)。
