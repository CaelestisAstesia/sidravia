# 通用前端交付记录 · 2026-10-05

本轮代码与离线演示已完成；自动验证通过。视觉为待确认候选，Windows 原生交互未验证。没有推送、合并或批准 golden。

## 基线与提交

仓库 origin：git@github.com:CaelestisAstesia/sidravia.git；唯一开发分支 codex/gui-platform-boundary。
起始 HEAD：6adaf2d45d9c96c2df5af1b3864ae7975fec0e6f。
批次一：7b2b065469b2bc0182e743af03518f5b5e3c4304；批次二：2d990525bdceedc796c998182e1e2d96710d8af1。
批次三为包含本报告的提交，其准确 SHA 见交付包 BUILD-INFO.json 和 git log。
主工作区用户未提交 Go 修改未碰触；实现位于原有 sidravia-gui-platform worktree。

## 页面、状态与交互

|范围|结果|边界|
|---|---|---|
|首页|七态、浅深色、响应式、统一公告入口、真实错误反馈|沿用业务快照和能力判断|
|设置|配置导航、自动连接绑定、系统/浅/深主题、诊断、关于|主题只保存在本次运行内存中；自动重连没有业务能力时禁用|
|连接配置|外置标签、校验、保存/失败反馈、密码清除、确认移除/删除|不回显密码、不改存储或业务契约|
|连接详情|实际状态、IP、协议、时间等可得字段|没有数据则不编造|
|技术诊断|实际 daemon/session/error 数据、非秘密字段复制|日志目录缺平台能力，明确禁用|
|公告弹窗|原有数据/已读处理、关闭按钮、Escape、滚动与安全区域|不新增 WebView 或网页业务架构|
|离线 Demo|正式 Shell/HomePage/HomeView/页面和路由；内存注入|显著标记模拟；不启动 daemon、不访问账号/网络/凭据|

|真实状态|主动作|另一动作|
|---|---|---|
|已连接|断开连接|连接详情|
|未连接|开始连接|更改配置|
|正在认证|取消连接|连接详情|
|等待网络|取消等待|连接详情|
|等待重试|立即重试|停止重试|
|认证失败|重新连接|更改配置|
|尚未配置|添加配置|连接设置|

所有动作保持原有权限、忙碌、初始化、失联保护；无能力时禁用并解释。生产不显示虚构 30 秒倒计时。开发场景/公告/主题控制在正式页面外，固定截图入口保留。

## 修改文件及原因

- `lib/features/home/home_page.dart`：顶部完整点击区域、七态展示/动作、真实错误卡、局部主题与影子；保持默认首页像素。
- `lib/shared/widgets/design_widgets.dart`：页面约束/宽度上限/滚动、行、分组、表单、切换控件；局部尺寸，未全局修改密度。
- `lib/features/settings/settings_page.dart`、`configuration/configuration_page.dart`、`advanced/advanced_page.dart`、`announcements/announcement_widgets.dart`：移植 HTML 页面结构与实际绑定。
- `lib/app/sidravia_app.dart`、`shared/theme/{app_theme,appearance}.dart`、`features/shell/sidravia_shell.dart`：前端外观状态、既有导航与键盘返回。
- `lib/dev/{interactive_demo_main,offline_demo_client}.dart`：内存场景/公告/主题和明确模拟反馈。
- `windows/runner/{main,win32_window}.cpp`：仅初始/最小外框逻辑尺寸及原有 DPI 换算。
- `test/{general_frontend,general_visual,home_header,home_responsive,home_visual,offline_demo,windows_runner_contract}_test.dart`：正式绑定、动作、平台配置、几何、候选截图及旧窗口合同保护。
- `tool/{capture_general_frontend,record_general_demo}.cjs`、本报告、`tool/evidence/general-frontend-20261005/`：可复现参考测量/实际操作录屏和适量持久候选证据。

## 执行与验证

实际执行（工作目录 gui，工具脚本从仓库根运行）：

```sh
dart format <本轮修改的 Dart 文件>
flutter analyze
flutter test
flutter test --dart-define=GEN_FRONTEND_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R4/evidence test/general_visual_test.dart
flutter test --dart-define=HOME_RESPONSIVE_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R4/responsive test/home_responsive_test.dart
node gui/tool/capture_general_frontend.cjs <原始HTML> <evidence>
node gui/tool/capture_home_responsive.cjs <原始HTML> <responsive>
node gui/tool/record_general_demo.cjs <临时web构建目录> <recording-delivery>
```

实际 Node 环境采用隔离的 playwright1.63/pngjs7、Chromium153.0.8010.12；FONTCONFIG_FILE 指向 Windows 字体目录，动态库在临时工具目录。详细脚本参数/日志随附件提供。
格式化成功，analyze 无问题，全套 **213 项通过**。24 组七态/页面/弹窗浅深色候选；11 组响应式候选、69 个连续宽度，旧默认图 changedChannels=0，未更新 golden 或放宽断言。
正式 HomePage 动作覆盖 7 状态×3 平台配置×2 主题，另有重试/更改配置、正式路由点击、Tab/Enter、返回/Escape、表单校验/失败/清空密码、公告更新/已读、外观和系统亮度测试。
Windows 顶部 42 高；Android/iOS 配置至少48高；文字放大共同增长。Windows 实际 InkWell 悬停状态、完整区域上下留白点击导航已测试。平台配置测试不是实机认证验收。
安全区域、软键盘180、文字1.6、长文字、短窗口滚动有自动覆盖。Android/iOS 系统硬件返回、实机键盘及真实认证未验证。

Windows 原生 Release 命令（实际成功）：

```bat
cd /d D:\code\sidravia-demo-r4\gui
D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart
```

临时 web 拷贝实际执行 `flutter build web --release --no-web-resources-cdn -t lib/dev/interactive_demo_main.dart` 成功；没有将正式入口改为 mock。
浏览器录屏：30 个实际操作步骤、连续调整窗口、短窗滚动，38.28秒/1100×900/25fps，pageErrors=[]、blockedExternalRequests=[]。已查看解码帧；这只是 WSL Chromium 的交互证据。
Windows UI 工具两次初始化（含 reset）失败：`sandboxCwd is not a local file URI: file:///home/astesia/code/sidravia`。因此 Windows 窗口实际缩窄、主要导航/滚动及 DPI 交互均未验证。未采用其他系统 UI 自动化绕过工具限制。
未运行 Go 验证（受保护代码无修改）、Android/iOS 构建与实机、校园认证、真实 daemon 操作。

## 尺寸、字体与视觉

原始 HTML SHA256：53b0f460a6aba89ae1397747b89294a4f6055c9dde2d118055c2b34c54a4e813，未修改。
浏览器视口1280×1000/DPR1；HTML reference 外框400×690，实测业务区398×642（2px边框与46px顶部区域）。compact400×620→398×572；tall400×780→398×732；wide760×590→758×542。截图没有拉伸。
Windows 新尺寸按包含原生标题栏的外框逻辑像素：初始400×690，最小候选360×640；沿用 monitor DPI 比例换算，不锁比例。客户区/业务区小于外框，真实差值尚未测得，不能沿用 HTML 的46px扣减。

|关键元素|差异（Flutter－HTML）|
|---|---|
|默认头部、状态块44图标、320动作、分隔线、公告|整体位置/尺寸一致|
|默认配置按钮|x/y/高度一致，宽度296对306，窄10px；保留既有默认像素成果，列为未消除差异|
|设置按钮|42×42，坐标一致|
|上下文|顶部+0.359px，高92并可增长|
|失败卡|宽320一致，高+1px，顶部−0.141px|
|设置首组|高度+0.469px|
|配置首输入/更多组|约下移2/2.5px|
|诊断组|高度317一致，下移1px|
|公告弹窗|340×360一致，下移23px：HTML按整个外框居中、Flutter按业务区居中；不在页面硬编码标题栏补偿|

HarmonyOS Sans 及字重/MaterialIcons 已显式加载；本机截图另外显式加载 SegoeUISymbol 和 Consolas 修复符号/monospace 缺字，没有将系统字体打包。HTML实际Segoe/微软雅黑与Harmony的字形和栅格差异仍存在，Windows 原生字体回退未验证。新截图均为候选，未批准基准；关于等原稿占位部分复用现有能力，没有扩展功能。
持久精选图、全部关键坐标见 `evidence/general-frontend-20261005/`；完整24+11组参考/实现/叠加/差异及录屏在交付附件。

## Windows 可访问交付与启动

- ZIP：`D:\Downloads\Sidravia-Offline-Demo-R4.zip`，解压后双击 `Windows\打开离线 Demo.cmd`。
- 桌面：`C:\Users\EDKen\OneDrive\Desktop\Sidravia 可交互离线 Demo R4\打开离线 Demo.cmd`。
- 明细目录：`D:\Downloads\Sidravia-Offline-Demo-R4\delivery\`。
- 源码正式入口：gui目录 `flutter run -d windows -t lib/main.dart`，需要既有正式bootstrap/daemon环境，未运行真实账号；离线入口 `flutter run -d windows -t lib/dev/interactive_demo_main.dart`，无需daemon或凭据。固定截图入口 `lib/dev/preview_main.dart`，不是可交互正式演示。这些 run 命令本轮未实机执行，已验证的是上述 Release build 和浏览器录屏。

## 范围检查与停止

相对6adaf2d的文件清单仅 GUI 白名单。Go、IPC客户端/协议、GuiController、能力规则、凭据/daemon/bootstrap、托盘代码未改；runner差异仅3个尺寸数字及注释。主工作区用户修改保留。HTML与原golden未改，系统字体/真实凭据/用户数据/Go可执行文件不进入包。
本轮三批本地提交，不推送、不合并。Windows 原生验收与剩余视觉差异保留；停止等待确认，不进入自绘桌面窗框阶段。
