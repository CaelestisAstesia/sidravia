# Sidravia R5 · Windows 自绘窗框

本轮代码、自动验证及 Windows Release 构建完成；**Windows 原生验收未完成**。
起始：codex/gui-platform-boundary@4a8fdf1a9f4c65254dea6e2b9f307f9536989308。
结束 SHA 为包含本报告的本地提交，见交付包 BUILD-INFO.json。没有推送、合并或批准 golden。

## 实现与范围

沿用 sidravia/window，未引入窗口管理插件。Windows runner保留 WS_OVERLAPPEDWINDOW
与系统菜单/最小化/最大化/关闭样式，通过 WM_NCCALCSIZE 使客户区覆盖外框，Flutter绘制顶部。
DWM corner preference为DONOTROUND；旧系统不支持时返回 HRESULT，安全降级。
Dart在Windows绘制46逻辑像素顶部，按钮36×32、顶部10、右侧10、间隔2、圆角8及17px原稿SVG路径。
最大化/还原、悬停/按下、焦点边框、键盘激活、提示、操作失败反馈均有展示路径。
浅色/深色/系统模式沿用R4主题，DWM颜色变化保留前端选定外观。
Android/iOS及其他平台不绘制桌面顶部或控制按钮。

Win32拥有HTCAPTION双击/拖动、四边四角HT命中、鼠标指针及系统缩放。
Flutter子HWND在这些区域返回HTTRANSPARENT，交给同线程父窗口；业务区域仍HTCLIENT。
最大化按钮返回HTMAXBUTTON并传递DWM hover，保留Win11 Snap入口；鼠标激活由原生完成，
键盘/辅助功能激活使用既有通道。状态事件读取IsZoomed/IsIconic并在WM_SIZE/WM_DPICHANGED/WM_MOVE发布，
包括系统菜单、快捷键或系统分屏引起的变化，不使用点击后的乐观布尔翻转。
Alt+Space/Alt+F4从Flutter子窗口交给系统菜单/关闭；失活不重新抢业务焦点。
工作区最大尺寸逻辑沿用monitor rcWork，保留自由比例及跨屏建议DPI矩形。

根Navigator覆盖完整窗口，home内的窗框只消耗自己的46px，再把剩余约束交给页面。
现有公告Dialog未改内容或业务；现在以整窗中心定位，PopupRoute观察器使原生标题按钮/拖动区
尊重Flutter模态屏障。无页面标题栏padding或−23px补偿。Escape、原关闭与焦点路径保留。
正式版close仍PostMessage WM_CLOSE，原有desktop_presence_initialized隐藏到托盘及明确退出处理逐字保留。
离线Demo没有初始化DesktopPresence，因此原有WM_CLOSE销毁/退出路径仍有效，不隐藏后失去入口。

## 修改清单

|文件|原因|
|---|---|
|lib/window/sidravia_window_frame.dart|Windows窗框绘制、命令/原生状态接收、模态观察器与主题|
|lib/app/sidravia_app.dart|仅安装根Navigator窗口观察器|
|lib/dev/interactive_demo_main.dart|复用同一窗框；增加只读原生诊断按钮；假客户端不变|
|windows/runner/win32_window.{h,cpp}|移除标准caption占用、直角偏好、命中、子窗口转交、DPI最小尺寸、主题/失活处理|
|windows/runner/flutter_window.{h,cpp}|扩展现有window通道与状态发布；原有托盘/关闭/退出流程不改|
|windows/runner/frame_geometry.h|集中、可直接测试的逻辑/物理命中计算|
|windows/runner/CMakeLists.txt|链接系统comctl32用于子HWND subclass|
|test/window_frame_test.dart|平台/主题、按钮尺寸与命令、外部状态、失败、键盘、正式导航和公告根坐标|
|test/frame_geometry_test.cpp|直接执行生产命中算法及4档DPI最小尺寸|
|test/windows_runner_contract_test.dart|仅更新与自绘NCCALCSIZE/NCHITTEST冲突的两项旧断言；生命周期断言保留|
|tool/r5-window-report.md、tool/r5-evidence/|持久报告与少量候选对照；无批准基线覆盖|

## 原生测试条件与实际证据

任务开始即按computer-use技能初始化@oai/sky；工具失败：
`Mcp error: -32602: js: codex/sandbox-state-meta: sandboxCwd is not a local file URI: file:///home/astesia/code/sidravia`。
未采用PowerShell/其他UI自动化绕过，不启动真实认证、不读取真实账号/凭据。
因此本轮没有Windows原生窗口截图、录屏、实际尺寸或DPI记录；这些项均为待验。
没有用浏览器录屏代替原生验收。

|层级|结果|
|---|---|
|dart format|成功|
|flutter analyze|No issues found|
|全套flutter test|224通过，含R4正式绑定/页面/生命周期回归|
|新增窗框widget测试|11通过；Windows/Android/iOS配置与浅深色、命令、状态、键盘、模态屏障、正式导航|
|C++几何程序|g++ C++17编译并执行通过；96/120/144/192DPI，四边四角、按钮、业务区、模态及最小尺寸|
|Windows Release|3.47.0 SDK实际构建成功，正式生产窗框+内存Demo注入|
|Windows原生行为|未完成；构建成功不表示拖动/分屏/跨屏/托盘现场通过|
|移动端|配置模拟；无实机或真实认证验收|

首次测试诊断：字体helper名字拼写、手动platform override的测试生命周期、误放variant参数均在计划允许的局部修正中解决；未跳过测试或放宽断言。像素比较首次使用错误R4文件名，修正为flutter-content.png；最终比较成功。详细成功日志随包，未修改系统/SDK。

实际命令（gui目录）：

```sh
dart format lib/window/sidravia_window_frame.dart lib/app/sidravia_app.dart lib/dev/interactive_demo_main.dart test/window_frame_test.dart test/windows_runner_contract_test.dart
flutter analyze
flutter test --dart-define=R5_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R5/candidates test/window_frame_test.dart
flutter test
```

仓库根：

```sh
g++ -std=c++17 -Wall -Wextra -Werror gui/test/frame_geometry_test.cpp -o /tmp/sidravia-r5-frame-test
/tmp/sidravia-r5-frame-test
python3 gui/tool/prepare_demo_build.py /mnt/d/code/sidravia-demo-r5/gui
git diff --check 4a8fdf1
```

新frame_geometry.h及最终源码补入隔离构建副本；交付时逐字节核验全部lib及runner源码。
Windows实际最终构建：

```bat
cd /d D:\code\sidravia-demo-r5\gui
D:\Tools\Flutter\3.47.0\flutter\bin\flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart
```

Go、移动构建/实机、真实daemon/认证、原生交互与DPI现场没有执行。

## 尺寸、候选与缺口

初始400×690、最小360×640仍按外框逻辑尺寸；没有锁比例、整页缩放或旧标题栏二次扣减。
GetDpiForWindow确定物理/逻辑换算，最小尺寸向上取整；最大化使用当前monitor工作区。
真实可见DWM外框、GetWindowRect、客户区、DPI和剩余业务区须由运行诊断采集，不能宣称预期值已实测。
诊断命令返回outerPhysical/visiblePhysical/clientPhysical（x,y,w,h）、clientLogicalWidth/Height、topLogical、
contentLogicalHeight、dpi、maximized/minimized、cornerHRESULT/visibleHRESULT、closeToTray及darkFrame。
visibleHRESULT失败时visiblePhysical退回outerPhysical，必须连同HRESULT解读。

|对象|自动渲染或要求|Windows实测|
|---|---|---|
|默认外框|约400×690逻辑像素|未测|
|最小外框|360×640逻辑候选|未测|
|客户区|NCCALCSIZE全外框；实际以诊断为准|未测|
|自绘顶部|widget实测46逻辑像素|未测|
|比较业务区|398×642，DPR1、文字缩放1、Windows widget平台|仅widget实测|
|比较画布|398×688=46+642；用于公平像素对照，不是生产窗口固定尺寸|仅widget实测|

R5内容候选与R4 responsive/reference/flutter-content.png像素changedChannels=0。
对应图与JSON在r5-evidence；已查看完整窗框候选与差异图。这证明业务页面保持不变，不能证明DWM外框正确。
HTML400×690仍含1px边框和46顶部的原稿口径，未改HTML。HarmonyOS/图标字体显式加载，
仅本机读SegoeUISymbol做缺字检查；没有分发系统字体。候选没有批准golden。
R4配置按钮宽10px差异、表单/诊断微小偏移、字体差异继续保留。
旧公告居中口径通过根宿主调整，自动测试以整窗中心345验证；原生视觉仍待确认。

## 获取与人工验收

ZIP：D:\Downloads\Sidravia-Offline-Demo-R5.zip，解压后双击Windows\打开离线 Demo.cmd。
桌面：C:\Users\EDKen\OneDrive\Desktop\Sidravia 可交互离线 Demo R5\打开离线 Demo.cmd。
无daemon前提，不输入真实账号/密码；黄色条的所有业务操作都是内存模拟。
点击“窗口诊断”→“复制诊断”，在每个条件下保存JSON，并用系统截图工具保存整窗截图/录屏。
复制完成关闭诊断弹窗再测窗口，避免模态屏障影响拖动/按钮。

1. 启动后采集默认JSON与整窗截图：DPI、outer/visible/client、top46、业务区、closeToTray应为false。
2. 四边和四角分别拖动，缩到最小竖窗；采集JSON验证visible外框与360×640候选口径。任一不符标失败，不用页面缩放补偿。
3. 顶部左侧空白拖动、双击最大化/还原；确认最大化不遮任务栏，还原位置/尺寸合理；鼠标悬停/按下最大化图标与状态一致。
4. 最小化后从任务栏恢复；Alt+Tab切出/切回，Alt+Space操作还原/最大化；Win+方向键和贴边拖动；Win11且系统启用时悬停最大化检查Snap菜单。
5. 点击配置上下留白、设置、输入框、详情、公告；Tab/Enter/Space激活；公告整窗居中、Escape和关闭按钮有效，短内容区可以滚动，弹窗期间顶部按钮不穿透。
6. 系统/浅色/深色分别检查窗框、控件焦点和关闭悬停；用快捷键/系统菜单改变状态检查还原图标同步。
7. 如有不同DPI屏幕，在现有显示设置下跨屏拖动，每屏重复采集JSON和点击/缩放；仅记录实际测试档位。只有单屏时将跨屏标未测。
8. 离线Demo分别使用自绘关闭、Alt+F4、系统菜单关闭，确认进程退出且无隐藏窗口。正式版关闭到托盘及明确退出断开流程必须在独立正式现场验证；本包不运行真实认证。

每一步记录通过/失败/未测与对应JSON/截图，附Windows版本和DPI；有问题保留现场，不调用真实认证。
本轮结束停止，等待原生及视觉确认；不推送或进入其他阶段。
