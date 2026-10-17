# Sidravia GUI

Sidravia 的 Flutter 桌面界面，当前开发版本为 `0.3.0-alpha.1`，Windows 是主要交付平台。日常操作见[快速开始](../docs/quick-start.md)和[使用指南](../docs/guide.md)。本文只介绍 GUI 开发；统一验证与构建入口见[开发指南](../docs/development.md)。

GUI 面向单份保存配置的日常连接。协议覆写、一次性认证、多配置和脚本操作保留在 [ctl 高级使用](../docs/advance.md)，本目录不为这些能力提供界面入口。

GUI 通过同包的 `sidraviactl.exe` 启动 desktop 后台，并使用本机 typed WebSocket IPC。账号、凭据、配置和认证状态由后台拥有；窗口、托盘、外观和公告呈现由前端负责。desktop 与独立 headless 模式互斥。

## 原生开发

使用仓库固定的 Flutter 3.47.0，在 Windows 的本目录运行：

```powershell
flutter pub get --enforce-lockfile
flutter run -d windows
```

生产 GUI 会启动后台并使用实际数据。Windows 编译、验证和发行请使用仓库统一入口，见 [developer 工具说明](../tools/developer/README.md)。

## 隔离视觉预览

只调整布局时，可运行使用虚构内存快照的预览入口：

```powershell
flutter run -d windows -t lib/dev/preview_main.dart
```

该入口不启动 CTL、连接 IPC 或更改认证配置。预览与热重载有助于调整布局，不能代替发行包的原生交互验收。可配合 Flutter Inspector 查看组件约束与间距。

## 桌面行为与公告

默认关闭窗口隐藏到托盘，可以在设置中改为退出；显式退出会结束桌面后台和连接。通知、启动隐藏和公告行为可配置，具体使用语义见[使用指南](../docs/guide.md)。

当前学校公告由 Profile 提供网页地址，在独立窗口中加载学校原网页。旧版静态 JSON feed 与 `SIDRAVIA_ANNOUNCEMENT_FEED_URL` 不是当前公告实现。

GUI 设置与认证配置分开保存，见[数据与隐私](../docs/privacy.md)。界面使用 HarmonyOS Sans，许可证位于 [assets/fonts/LICENSE.txt](assets/fonts/LICENSE.txt)。仓库中的其他平台 runner 不构成相应平台的发行支持声明。
