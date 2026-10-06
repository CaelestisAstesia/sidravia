# 设置页与前端偏好交付（2026-10-06）

仓库 `CaelestisAstesia/sidravia`；origin `git@github.com:CaelestisAstesia/sidravia.git`；唯一分支 `codex/gui-platform-boundary`。起始干净 HEAD `5e9b82e134f8b04276dc263d222a00c9bbb54c4b`；追溯基线 `fe3da26` 未回退。实时远端指定分支仍为 fe3da26。本轮不推送、不合并，不批准或覆盖 golden。

## 三批成果与提交

| 批次 | 提交 | 结果 |
|---|---|---|
| A | a48a20a | 设置选择器、文案、诊断用户名称 |
| B | 1db857e | 独立外观持久化、平台路径、内存/隔离 Demo |
| C | 包含本报告的最终提交，SHA见交付BUILD-INFO.json | 自动重连最小接线、最终回归与证据；另修正路径根目录的分隔符拼接 |

A：`features/settings/settings_page.dart` 明确文字显示区，右侧箭头独立24宽；宽度根据最长标签与文字缩放固定，三种模式尺寸相同。局部标准 Dropdown 保留选中、Tab/Enter、方向键行为，桌面最低30、移动最低48高，不改全局密度或文字大小。外观左侧无描述、不留空行；自动登录与重连描述使用用户指定文字。`app/app_destination.dart`、`features/advanced/advanced_page.dart` 及相关导航/视觉测试统一“诊断”；类名、协议和原始字段不改。禁用原因与说明分开：未配置、忙碌、通信未就绪、服务未运行、平台不支持和关系不明确各有准确提示。

B：`shared/theme/{appearance,gui_settings}.dart` 保留唯一 Appearance 所有者，通过小型存储接口保存严格 schema1。模型无Windows路径或dart:io；仅两项schemaVersion、appearanceMode（system/light/dark）。系统模式保存system，不保存实际亮度。`platform/gui_settings_storage{,_io,_stub}.dart` 拥有文件与路径适配。`main.dart` 在runApp和IPCbootstrap前加载，`app/sidravia_app.dart` 注入已恢复同一所有者；没有第二套主题状态。`dev/interactive_demo_main.dart` 默认内存，只有显式SIDRAVIA_DEMO_SETTINGS_DIR才启用隔离文件。`pubspec.yaml/lock` 固定新增官方path_provider2.1.6，仅用于Android/iOS应用私有目录；Windows明确使用APPDATA，未采用插件品牌目录。生成的Windows插件CMake随依赖更新。`gui/.gitignore` 精确忽略gui-settings.json与它的临时文件。

C：`ipc/{sidravia_ipc_client,web_socket_ipc_client}.dart` 新增configurationSetAutoReconnect；实际仍使用已有configuration.update，payload只含configurationId与autoReconnect，false不省略。`application/gui_controller.dart` 仅新增14行设置方法，复用_mutate的请求串行、忙碌、刷新、错误及目标检查。`gui_capabilities.dart` 复用canManage。正式开关读取ConfigurationSummary.autoReconnect；`dev/offline_demo_client.dart` 模拟保存并标记，不产生后台认证。其他配置字段和手动断开动作保持原样。

新增/调整测试：`settings_preferences_test.dart`、`gui_settings_storage_test.dart`、独立进程`settings_persistence_process.dart`，客户端/Controller假夹具及原导航/窗口/通用测试；R51短窗滚动测试把已移除的旧说明替换为仍真实存在的底部“关于 Sidravia”目标，滚动与边界断言保留。完整修改清单见提交diff，候选和测量见settings-evidence。

## 路径与磁盘行为

Windows执行程序位置使用Platform.resolvedExecutable的父目录，严格检查该目录的sidravia.portable，不使用CWD、仓库或启动脚本目录。因Dart stat/type可能吞并部分OS失败为notFound，标志检查采用不跟随链接的目录枚举，再校验普通文件mode；非法目录、链接、特殊文件及枚举失败都报解析失败。namespace复制现有严格语法，并以Go namespace_test.go的同组用例验证，未放宽。

| 模式 | namespace | GUI 文件 |
|---|---|---|
| installed | 正式空串 | APPDATA/Sidravia/gui-settings.json |
| installed | 非正式 | APPDATA/Sidravia/namespaces/<id>/gui-settings.json |
| portable | 正式空串 | exe-dir/config/gui-settings.json |
| portable | 非正式 | exe-dir/namespaces/<id>/gui-settings.json |

与Go对应configurations.json同目录，GUI不打开它或凭据文件。Windows磁盘probe使用D:/Downloads/Sidravia-Settings-Demo/evidence/windows-layout-final/program作为注入exe目录、user-config作为隔离用户根；四组实际Windows路径见settings-evidence/persistence.json。这是隔离路径验证，没有读取正式APPDATA设置。

Android/iOS组装用官方getApplicationSupportDirectory提供应用私有目录，再放gui-settings.json（非正式namespace另隔离）。不检查exe旁标志。平台实际目录插件、iOS工程/设备与移动端磁盘持久化未实测；不能宣称全平台持久化通过。Linux/macOS正式存储暂不支持，安全反馈；Linux测试可注入明确文件位置。

缺失文件默认system且不报告故障。损坏、错误类型、额外不允许字段、未知版本、读取失败保留原字节并禁写，不自动用默认值覆盖；选择仍能在当前界面生效并说明不保存。保存采用同目录随机排他临时文件、flush、File.rename替换，不先删除旧文件，不回退installed。Dart3.13 Windows实现使用MoveFileExW REPLACE_EXISTING | WRITE_THROUGH；仅保证该文件系统/OS调用的替换语义，不声称断电实验完成。快速切换串行保存，最终选择胜出；较晚启动读取不覆盖用户新选择。较旧保存失败不会覆盖较新成功反馈。失败显示“本次外观已生效，但未能保存”；不阻断认证或退出。不新增存储迁移、文件轮询或数据库。

## 自动重连保存与实际生效链路

只读核对Go源码：

1. `internal/ipc/contract/methods.go` 已有bool指针autoReconnect，false有效；`internal/daemon/app/configuration_handler.go:71` 传递到Catalog Update；`configuration/catalog.go:211` 更新并经原持久化边界保存。
2. `app/authentication_resolver.go:123` 把值放入RuntimeDefinition；`authentication/session/session.go:131` 克隆冻结运行定义，516/559处用于网络中断/失败后的自动恢复判断，核心确实消费它。
3. `app/application.go:74` 启动配置时优先查已有Session，86处EnsureRunning，不重解配置；Session.handleRestart（445处）也只更新原会话意图/重试状态，不替换定义。
4. 因此修改仅供未来**新建Session**采用。已有Session、暂停后恢复、普通重新连接或原会话重试不保证采用新值。UI如实提示；本轮不擅自删除、重建Session或重启daemon使其生效。
5. 手动断开走SuspendAuthentication，先完成停止，不被解释为网络中断后应自动恢复。本轮没有改这条链路。

自动登录仅修改文案。既有后台在每个daemon generation首次接受环境Snapshot时评估一次（cmd/sidraviad/runtime.go:567；app/application.go:149）；GUI启动/恢复窗口/切开关未新增认证调用。文案“启动Sidravia后自动开始认证”与“每次打开已运行GUI都认证”存在语义差异，后者本轮未实现。

## 命令与验证层次

实际执行：

```sh
python3 .project/tooling/agent-workflow/boundary_probe.py --only coreutils python windows-interop
python3 .project/tooling/agent-workflow/agent_check.py
# gui/，Flutter3.47.0
flutter pub get
dart format <本轮修改的Dart文件>
flutter analyze
flutter test test/settings_preferences_test.dart test/web_socket_ipc_client_test.dart test/gui_controller_test.dart
flutter test test/gui_settings_storage_test.dart test/settings_preferences_test.dart test/r51_adaptation_test.dart
flutter test
flutter test --dart-define=GEN_FRONTEND_OUTPUT=D:/Downloads/Sidravia-Settings-Demo/screenshots test/general_visual_test.dart --plain-name 'candidate settings'
dart run test/settings_persistence_process.dart <隔离文件> dark
dart run test/settings_persistence_process.dart <同一文件>
```

截图的实际WSL输出参数是`/mnt/d/Downloads/Sidravia-Settings-Demo/screenshots`。原生Windows Dart3.13通过cmd.exe在隔离副本执行相同进程脚本（--layout、写dark、另一进程读），最新Windows PID14092→59312、初始null→dark；LinuxPID5403→5442，同样恢复dark。正式SidraviaApp用文件存储真正重新创建后恢复主题，恢复system后系统亮度变更仍生效。

最终analyze无问题；**全套259项通过**，包括R5窗口生命周期及R51滚动/连续布局/弹层回归。新增检查包含三选项对齐和尺寸、Tab/键盘、窄场景/放大文字、路径/CWD/namespace、非法标志与注入权限错误、读取竞态、损坏/未来版本、真实Linux不可写目录、替换前失败旧字节不变、正式绑定/忙碌/目标保护/失败清除/字段不变。Loopback WebSocket测试通过真实Dart序列化通道对假server验证true/false精确payload，**没有连接真实daemon IPC**。Windows正常磁盘替换成功；Windows真实ACL拒绝及断电中断未实测。早期失败属于已授权机械修正：括号lint、路径规范化、widget真实IO/FakeAsync夹具、fixture默认重连false的点击预期；没有降低断言或跳过测试。

Windows临时副本命令：

```bat
flutter.bat build windows --release -t lib/main.dart
flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart
flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart --dart-define=SIDRAVIA_DEMO_SETTINGS_DIR=D:\Downloads\Sidravia-Settings-Demo\isolated-preferences
```

正式入口与两种Demo Release构建均成功；只构建正式入口、未启动它。最终源码对构建副本哈希一致。Windows原生UI工具再次初始化失败：sandboxCwd is not a local file URI: file:///home/astesia/code/sidravia。**Windows GUI交互未验收**，不以构建、widget或Windows文件进程代替。没有真实校园认证、断线重连、用户配置操作。R6真实IPC/校园联调待单独授权。

## 图片、Demo与人工检查

settings-evidence/settings-light.png与settings-dark.png已亲自查看。Flutter widget Linux软件渲染、Windows平台配置、398×642业务区、DPR1/文字1、固定虚构数据，真正加载HarmonyOS Sans字重与MaterialIcons；字体与HTML参考的既有差异未宣称消除，系统字体未进入交付包。新图仅候选、不批准golden；本轮没有再设计首页/窗框。

交付：D:/Downloads/Sidravia-Settings-Demo.zip；桌面`Sidravia 设置与偏好 Demo`。

- `打开离线 Demo.cmd`：内存Demo，后台与外观均与真实设置隔离；每次启动外观默认system。
- `打开持久化验收.cmd`：独立子目录可执行文件；后台仍完全内存模拟，只有外观存到固定隔离目录`D:/Downloads/Sidravia-Settings-Demo/isolated-preferences/gui-settings.json`。mode为isolated-demo、无正式namespace读写，初次缺失默认system。保存的文件不要提交；此目录未预置用户文件。
- 验收步骤：进入设置选深色，等待磁盘文件出现，关闭Demo后用同一持久化入口重新打开，检查深色恢复；选system，重开并改变Windows系统亮度检查跟随；自动重连开关仅展示假后台保存反馈，不能称真实断线重连成功。若文件加载/保存失败，查看设置页明确提示。
- 包内review含报告、源码清单哈希、候选、磁盘测量和实际日志；没有真实凭据、token、系统字体或后端二进制。

## 受保护检查与停止

相对5e9b82e：Go全部、IPC线协议、认证引擎、权限、凭据、daemon/bootstrap、托盘、Windows runner、窗口Dart封装均无差异。Controller只有本轮授权的新设置方法，通用生命周期/并发未变。主仓库main的3份Go未提交修改未碰；工作发生在既有专用worktree。新增依赖是官方平台私有目录适配所需，其余产品范围不扩展。源码检查、mock/平台模拟、Windows磁盘、Windows构建、原生GUI、真实IPC和校园层次分别报告。完成A/B/C后停止；不推送、不合并、不继续R6。

参考：官方[path_provider2.1.6](https://pub.dev/packages/path_provider)、[Dart3.13 Windows File.rename源码](https://github.com/dart-lang/sdk/blob/3.13.0/runtime/bin/file_win.cc#L726)、[MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)。
