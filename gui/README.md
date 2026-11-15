# Sidravia GUI

Sidravia 的 Flutter 桌面界面，当前开发版本为 `0.4.0-alpha.1`，已提供 Windows 与 Linux x64 GUI 资产及原生开发链路。日常操作见[快速开始](../docs/quick-start.md)和[使用指南](../docs/guide.md)。本文只介绍 GUI 开发；统一验证与构建入口见[开发指南](../docs/development.md)。

GUI 面向单份保存配置的日常连接。协议覆写、一次性认证、多配置和脚本操作保留在 [ctl 高级使用](../docs/advance.md)，本目录不为这些能力提供界面入口。

GUI 通过同包的 `sidraviactl`（Windows 为 `sidraviactl.exe`） 启动 desktop 后台，并使用本机 typed WebSocket IPC。账号、凭据、配置和认证状态由后台拥有；窗口、托盘、外观和公告呈现由前端负责。desktop 与独立 headless 模式互斥。

## 原生开发

使用仓库固定的 Flutter 3.47.0，在 Windows 的本目录运行：

```powershell
flutter pub get --enforce-lockfile
flutter run -d windows
```

生产 GUI 会启动后台并使用实际数据。Windows 编译、验证和发行请使用仓库统一入口，见 [developer 工具说明](../tools/developer/README.md)。

Linux 从仓库根使用固定SDK与统一入口：

```bash
.local/bin/dev linux --check-gui
.local/bin/dev linux --integration
```

前者构建含GUI、CTL、daemon及机构资源的开发bundle；后者在项目内隔离XDG根/namespace中用虚构凭据验证真实链路，不访问生产配置或校园端点。普通GUI入口会使用对应运行目录的数据；不要以开发实例接管正在使用的headless实例。详见[developer工具说明](../tools/developer/README.md#native-linux-desktop-development)。Linux 已提供 GUI 便携包和 Fedora RPM，依赖与验收范围见[发行说明](../CHANGELOG.md)及[RPM说明](../docs/linux-rpm.md)。

## 隔离视觉预览

从仓库根启动浏览器交互预览（固定 Flutter 3.47.0）：

```bash
.local/bin/dev preview
```

打开 <http://127.0.0.1:7357>。可用 `--port 7358` 改端口；验证入口为
`.local/bin/dev preview --check`（分析、离线演示/布局测试与 Web 编译，不启动服务）。
其他环境可运行 `python3 tools/developer/dev.py preview --flutter /path/to/flutter`。
该命令复用 `lib/dev/interactive_demo_main.dart`、生产 shell、页面与主题，
工具栏可切换连接状态、账户业务快照和深浅外观，也可预览公告占位及托盘内容。
Web 预览默认在居中的 400 × 690 逻辑尺寸窗口内展示，可通过“预览窗口尺寸”切换
720 × 690 宽窗口。原生桌面最小窗口为400 × 690；首页连接信息不做内部滚动，超长内容自然展开。浏览器可用空间不足时窗口会缩到可用范围，内容仍可滚动；
预览背景和外框只用于开发，不改变原生程序窗口大小。
浏览器预览中的配置、设置、自启/PATH 都只在内存模拟，刷新后重置；不要输入真实账号密码。
认证后台、生产配置、学校网页及系统登记均不参与，渲染资源从本机服务加载。

终端中按 `r` 热重载、`R` 热重启，浏览器未更新时刷新页面；按 `q` 或 Ctrl+C 停止服务。
本次验收中 `R` 曾出现 `disposed EngineFlutterView` 报错，原因未定位；刷新后恢复，
普通 `r` 热重载与后续交互未出现新增错误。日常修改优先使用 `r`，热重启后刷新浏览器。
预览服务只监听 `127.0.0.1`。Web 入口仅用于开发，不构成 Web 产品支持；
原生窗口、托盘服务、通知、自启及 WebKit/WebView2 公告仍需桌面验收。

只调整布局时，可运行使用虚构内存快照的预览入口：

```powershell
flutter run -d windows -t lib/dev/preview_main.dart
```

该入口不启动 CTL、连接 IPC 或更改认证配置。预览与热重载有助于调整布局，不能代替发行包的原生交互验收。可配合 Flutter Inspector 查看组件约束与间距。

## 桌面行为与公告

默认关闭窗口隐藏到托盘，可以在设置中改为退出；显式退出会结束桌面后台和连接。通知、启动隐藏和公告行为可配置，具体使用语义见[使用指南](../docs/guide.md)。

当前学校公告由 Profile 提供网页地址，在独立窗口中加载学校原网页。旧版静态 JSON feed 与 `SIDRAVIA_ANNOUNCEMENT_FEED_URL` 不是当前公告实现。

运行状态快照使用随包 Source Code Pro 等宽字体，中文回退 HarmonyOS Sans；字体许可及来源见[许可证与致谢](../docs/acknowledgements.md)。

GUI 设置与认证配置分开保存，见[数据与隐私](../docs/privacy.md)。界面使用 HarmonyOS Sans，许可证位于 [assets/fonts/LICENSE.txt](assets/fonts/LICENSE.txt)。仓库中的其他平台 runner 不构成相应平台的发行支持声明。

Linux窗口复用共享Flutter自绘标题栏，GTK承载窗口并处理真实指针事件下的合成器拖动/边缘缩放，Ayatana提供系统托盘菜单，WebKitGTK提供应用内公告。Linux PNG图标直接复用已接受Windows ICO中的256px PNG内容。没有托盘服务时保留主窗口；启动隐藏只作用于本次启动，之后修改偏好不改变当前窗口可见性。Wayland的坐标、焦点和最小化归合成器管理；未激活的已映射窗口无法可靠判断是否最小化，返回未知观察，通知策略不会猜测可见性。公告复用共享导航决策：加载完成后的用户HTTP(S)外链交浏览器，同文档锚点、脚本导航与重定向仍在窗口内；非用户弹窗、非HTTP(S)、权限请求与下载被拒绝。加载提示、30秒有界失败、Web进程终止与关闭重开按原生生命周期管理。

Linux系统集成支持登录桌面会话后的GUI或仅后台自启，两者互斥登记且不接管当前运行模式；拒绝覆盖其他安装的自启文件。PATH维护在HOME/.local/bin发布本安装命令链接，不改shell配置，目录不在当前PATH或有前序同名命令时明确提示；关闭只移除本安装链接。移动portable前应关闭自启和PATH，移动后重新启用。两端已移除独立主动网络诊断，认证所需网卡观察、绑定、恢复和日志/诊断导出继续保留。GUI偏好默认位于`$XDG_CONFIG_HOME/Sidravia/gui-settings.json`（空/相对值回`$HOME/.config`），namespace位于`Sidravia/namespaces/<name>/gui-settings.json`。程序旁的普通文件`sidravia.portable`启用portable：分别为`config/gui-settings.json`和`namespaces/<name>/gui-settings.json`。没有隐式配置迁移或发行/系统注册行为。

GTK3/Wayland还需显式向合成器声明客户端装饰，不能仅检查GTK decorated属性。窗口图标优先使用可选`xdg_toplevel_icon_v1`协议传递原图像素；协议不可用时诊断明确为unsupported，本次不隐式注册用户桌面项。托盘图标有独立的StatusNotifier路径。
