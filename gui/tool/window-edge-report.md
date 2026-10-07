# Windows 细轮廓交付（2026-10-06）

仓库 CaelestisAstesia/sidravia，origin git@github.com:CaelestisAstesia/sidravia.git；分支 codex/gui-platform-boundary；起始 HEAD `36d472513d37431ace8266d82c26465fde175cad`，工作区干净。结束 HEAD 为包含本报告的独立提交，准确值见交付包 BUILD-INFO.json 和推送核验。之前设置、持久化、重连及选择器修正均保留。

## 已检查现状与方案

原生窗口仍使用 WS_OVERLAPPEDWINDOW，WM_NCCALCSIZE 将整个窗口作为客户区；已有主题和 DONOTROUND 设置。原代码没有 DwmExtendFrameIntoClientArea、没有显式非客户区渲染禁用、没有 Flutter 外阴影，也没有显式 DWM 边框颜色设置。没有证据证明已有原生外阴影正确显示，也不能仅凭未设置阴影属性宣称它被禁用。

优先评估了原生能力：[微软 DWMWINDOWATTRIBUTE](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/ne-dwmapi-dwmwindowattribute) 的 BORDER_COLOR 从 Windows 11 build22000 支持；它控制原生边框颜色，但不构成当前全客户区、Flutter 不透明子窗口能够稳定显示原生边线的证据。[微软自定义窗框说明](https://learn.microsoft.com/en-us/windows/win32/dwm/customframe) 涉及 DWM 扩展客户区。为本轮细轮廓而引入玻璃扩展或重调已正常的客户区有不必要风险，因此选择已有窗框绘制层的细边线，不改变非客户区几何。

- Windows 共用 SidraviaWindowFrame 的 foregroundPainter 绘制内侧 **1物理像素**四条不透明填充条；宽度1/DPR，关闭抗锯齿，直角、不扩窗、不增加padding/透明空隙。绘制层不拦截鼠标。
- 浅色激活 #A6B1BF、失焦 #BEC6D0；深色激活 #5D6878、失焦 #465160。窗口 WM_ACTIVATE 通过现有 sidravia/window 通道发布真实前台状态；已有焦点操作保留。主题直接跟随现有Theme，未增加主题状态或用户设置。
- 支持系统调用 DWMWA_BORDER_COLOR=COLOR_NONE，关闭原生边线，避免与自绘叠加；停用自绘窗框时恢复COLOR_DEFAULT。旧系统属性不支持时保留原有全客户区机制并记录HRESULT，不崩溃。不修改原生命中、尺寸、工作区、最大化还原和托盘流程。
- 最大化时不画细线，系统还原事件恢复。分屏保留一像素轮廓，不探测“靠近边缘”、不创建系统外留白；原生分屏/贴边效果待实测。
- **DWM 阴影策略保持原样**：未叠加BoxShadow、未新建阴影窗口、未改变非客户区渲染策略或玻璃边距。现有阴影若系统产生则保留；本轮不宣称恢复/增强外阴影。移动端和web不绘制桌面轮廓。

## 修改文件与保护检查

| 文件 | 原因 |
|---|---|
| gui/lib/window/sidravia_window_frame.dart | 唯一Windows绘制层细轮廓、DPR、激活/主题/最大化展示 |
| gui/windows/runner/win32_window.cpp | 只在已有configureFrame处抑制/恢复原生边线 |
| gui/windows/runner/win32_window.h | 保存边线HRESULT，只读诊断 |
| gui/windows/runner/flutter_window.cpp | 现有通道增加激活事件及DWM只读诊断 |
| gui/test/window_frame_test.dart | 实际栅格一像素检查、颜色/还原/点击及平台保护 |
| gui/tool/window-edge-report.md | 本报告 |

相对起始HEAD仅以上6文件；Go、IPC认证接口/客户端、GuiController、权限、凭据、偏好存储、设置页/选择器/自动重连接线、daemon及托盘逻辑均无改动。原生geometry.h、main.cpp、最小/初始尺寸、四边四角命中和46逻辑像素顶部均未改。正式与Demo复用同一窗框，没有另一套页面或窗管库。

## 命令与证据分层

- boundary_probe.py --only coreutils python windows-interop、agent_check.py：改动前通过。
- dart format gui/lib/window/sidravia_window_frame.dart gui/test/window_frame_test.dart：通过。
- flutter analyze：No issues found。
- flutter test test/window_frame_test.dart：19项通过，包括Windows、Android、iOS配置；EDGE_OUTPUT/R5_OUTPUT候选输出通过。
- flutter test：全套271项通过，保留R5/R5.1、绑定、偏好与重连回归。
- g++ -std=c++17 -Wall -Wextra -Werror gui/test/frame_geometry_test.cpp -o /tmp/sidravia-edge-frame-test；运行该文件：96/120/144/192DPI命中与最小尺寸算法通过。
- 基线捕获：隔离副本使用git show 36d4725的原窗框和原测试；flutter test --dart-define=R5_OUTPUT=/mnt/d/Downloads/Sidravia-Window-Edge-Evidence/before test/window_frame_test.dart --plain-name 'same business-size frame candidate'，1项通过。新代码同样捕获after；398×642业务区域PNG字节与SHA256完全一致（43d1acb8cecae7583038dc5f0d4af06036e3ba686d358c36efa2a6664f120a8c）。没有改页面布局。
- Windows Flutter3.47.0隔离副本 D:/code/sidravia-edge-demo/gui：flutter.bat build windows --release -t lib/main.dart、-t lib/dev/interactive_demo_main.dart：均成功。正式入口仅编译、不启动。实际日志包含完整结果。
- 测试模拟DPR1/1.25/1.5/2、浅深色、真实Harmony/Material字体候选；RGBA检查四边最外一像素及紧邻内侧像素，验证不模糊/不增厚，激活/失焦/最大化/还原尺寸不跳动；业务内边缘点击仍到原组件。所有平台模拟均不是原生行为证据。早期测试截图Future进入FakeAsync导致等待，限定夹具修正为runAsync；未放宽断言。

**Windows 原生视觉和交互验收未完成。** computer-use初始化实际报错 `-32602 sandboxCwd is not a local file URI: file:///home/astesia/code/sidravia`。未绕过工具权限，未使用其他输入/截图自动化。没有包含桌面外围的原生前后截图，也没有实测桌面背景矩阵、DPI跨屏、分屏、激活失焦外阴影、缩窄/缩放/拖动/按钮。本次只新增自动层回归；用户前轮已测的R5原生基本行为是历史证据，不冒充本轮验证。

候选图片亲自查看：D:/Downloads/Sidravia-Window-Edge-Evidence/before/frame.png、after/frame.png及widget/{light,dark}-{active,inactive,maximized,restored}.png；Linux widget软件渲染，仅证明内部绘制。没有给图片后期添加阴影，没有批准/覆盖golden；外阴影与真实桌面分界仍未验证。

## 原生诊断与人工验收

新版离线Demo：D:/Downloads/Sidravia-Window-Edge-Demo；桌面“Sidravia 窗口细轮廓 Demo”，双击“打开离线 Demo.cmd”。只有模拟后台和内存偏好，不启动daemon、不读写正式凭据或设置；旧输出和隔离偏好文件保留。系统字体文件未随包分发。

Demo原有“窗口诊断”可复制以下数据：dpi、outerPhysical、visiblePhysical、clientPhysical、clientLogicalWidth/Height、topLogical、contentLogicalHeight、maximized、active、darkFrame、cornerHRESULT、borderHRESULT、dwmComposition/compositionHRESULT、nonClientRendering/nonClientHRESULT。HRESULT=0为调用成功；非零应记录具体值。后两组只说明DWM能力/非客户区渲染状态，**不证明阴影已出现**。borderHRESULT非零时重点检查是否仍有原生边线；不要以widget测试消除双边框疑虑。

建议人工顺序：
1. 用原“设置居中修正版”和新Demo分别截图，保留四边外至少30～50物理像素桌面背景；两者尺寸、位置、主题一致。浅/深界面各在浅/深桌面背景检查，勿修改截图阴影。
2. 点击桌面失焦，再激活窗口；轮廓应略减弱/恢复，切换主题后边线应跟随。记录Windows版本和显示缩放，并复制窗口诊断；不假定默认就是96DPI。
3. 最大化/还原、系统菜单或系统提供的分屏分别检查：最大化无内侧线，正常还原细线恢复；无双边框、黑边、透明留白或页面位置跳动。分屏仅人工使用系统能力，本轮工具没有执行Win快捷键。
4. 缩窄至360×640候选；逐边逐角拖动、顶部拖动/双击、最小化恢复、窗口按钮与主要业务按钮/弹窗均检查。Demo关闭应退出；不要启动正式后台认证。
5. 可用时在100/125/150/200%显示缩放及跨屏下检查，记录实际执行项。出现边框问题时附原始外围截图和诊断，不补画阴影。

## 完成和推送边界

代码、自动验证和Windows构建完成；原生视觉/阴影待人工核验，不宣称全面验收通过。按用户最后明确授权，仅任务交付后推送本独立提交，先核对远端快进、只推指定分支，不强推、不合并；最终提交链接与远端核验在交付消息中。完成后停止，不继续窗框工程。
