# 原生圆角与阴影适配（2026-10-06）

仓库 CaelestisAstesia/sidravia，分支 codex/gui-platform-boundary；起始干净 HEAD `cf5b6ad20d69fcc696272ee1ac8b142fd90f60ad`。以本地最新代码继续，未回退。结束 HEAD 为包含本报告的独立本地提交，准确 SHA 见交付 BUILD-INFO.json。本轮不推送、不合并、不批准 golden。

## 三项结果必须分别理解

| 项目 | 实现 | 验证边界 |
|---|---|---|
| 真正顶层窗口圆角 | EnableCustomFrame 对 HWND 请求 DWMWCP_ROUND；停用时恢复DEFAULT；保留WS_OVERLAPPEDWINDOW | 代码/构建完成；**原生圆角视觉未验证**，不以HRESULT成功作证明 |
| 连续细边线 | DWM BORDER_COLOR恢复正常颜色，原生处理浅深色/激活失焦；移除Flutter四条直线描边 | 自动验证无重复Flutter边线/内部裁角；DWM弧线连续性和黑白角仍待原生画面 |
| 桌面外阴影 | 显式DWMNCRP_ENABLED；普通窗口DwmExtendFrameIntoClientArea正1物理像素四边扩展，最大化0，组合变化重新应用 | 原生适配已实现；**外阴影是否实际出现未验证**，未声称阴影浓度增强或完成验收 |

当前会话可读取 computer-use 26.930.51102 技能，但要求的 node_repl 执行工具未暴露。初始化尝试得到 `TypeError: tools.mcp__node_repl__js is not a function`，随后检查完整工具元数据也无node_repl。与上轮的sandboxCwd初始化错误不同，本轮是执行工具缺失。未启动其他原生输入/截图自动化，未后期加工圆角或阴影，未用widget图替代桌面图。

## 原理、范围与降级

现有全客户区WM_NCCALCSIZE=0、子窗口铺满、尺寸/最小限制与命中计算均保持。先前设置DONOTROUND并以COLOR_NONE关闭原生边线，因此改为请求ROUND并恢复DWM独立绘制的细边线。颜色仍是浅色激活A6B1BF/失焦BEC6D0、深色激活5D6878/失焦465160，WM_ACTIVATE及现有Theme通道更新；没有第二套前端主题状态。

[微软圆角说明](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/ui/apply-rounded-corners)指出圆角请求只是系统提示，空非客户区等定制可能影响效果；最大化/系统分屏时系统本来就不圆角。因此不硬编码半径，不做分屏/贴边猜测，不用ClipRRect、逐像素透明或SetWindowRgn。系统不支持圆角/边框属性时调用失败被记录，应用继续使用系统的矩形行为和默认边框能力；旧系统无法保证本次自定义边框色可用。

[微软自绘窗框说明](https://learn.microsoft.com/en-us/windows/win32/dwm/customframe)和[边缘扩展API](https://learn.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmextendframeintoclientarea)提供保留DWM合成的原生入口。本轮仅启用非客户区渲染并向客户区内扩展正1物理像素，未创建窗口外空白、未Inset/移动Flutter子窗口，未使用负边距整窗玻璃。最大化时扩展为0，恢复为1；像素值变化才更新扩展，DPI或组合变化强制重设。没有在装饰刷新中新增SetWindowPos，没有新增阴影宿主/窗口管理框架/BoxShadow。

这项适配让DWM具备绘制条件，但当前无法观察不透明Flutter子窗口与目标系统合成后的真实效果。若人工检查仍没有阴影或边线，需记录截图、Windows版本及诊断；不扩大为透明窗重构，也不以圆角已出现推断阴影存在。

## 修改文件与保护

| 文件 | 改动 |
|---|---|
| gui/windows/runner/win32_window.cpp | ROUND、DWM边线颜色、NC渲染与1物理像素扩展；现有主题/激活/大小/DPI事件中刷新 |
| gui/windows/runner/win32_window.h | 最小装饰更新函数与HRESULT/请求量诊断状态 |
| gui/windows/runner/flutter_window.cpp | 在原有窗口诊断中增加四个字段 |
| gui/windows/runner/frame_decoration.h | 小型纯装饰策略：圆角请求、扩展量、边线RGB；不含尺寸/命中逻辑 |
| gui/lib/window/sidravia_window_frame.dart | 删除直线_WindowOutline及只用于它的_active展示状态；保留顶部/控件/页面组装 |
| gui/test/window_frame_test.dart | 替换与新要求冲突的直线像素断言，检查Flutter没有重复线/裁角、内容和点击不变 |
| gui/test/frame_decoration_test.cpp | 直接执行生产装饰策略，覆盖普通/最大化/恢复/停用/主题/激活 |
| gui/tool/window-round-report.md | 本报告 |

删除旧自绘描边的理由是让原生圆角与边线由同一DWM机制拥有，避免裁掉直线端点后弧线断开。替代保护是四边四角完整不透明像素、原生策略测试、保留的窗口/焦点/弹窗/移动平台回归，不是放宽旧阈值或批准新golden。

相对起始HEAD仅上述8文件变化。Go、业务IPC/客户端、GuiController、设置/选择器、偏好持久化、自动重连、认证/凭据、daemon和托盘语义无改动。frame_geometry.h、窗口初始400×690/最小360×640、46逻辑像素顶部、业务padding及窗口按钮位置不变。

## 实际验证

- `boundary_probe.py --only coreutils python windows-interop`、`agent_check.py`：改动前通过。
- `dart format gui/lib/window/sidravia_window_frame.dart gui/test/window_frame_test.dart`：通过。
- GUI目录 `flutter analyze`：No issues found。
- `flutter test test/window_frame_test.dart`：19项通过，包括Windows/Android/iOS平台配置、正式导航/公告、Escape、按钮、模拟DPR1/1.25/1.5/2的内容几何与边角像素。
- `flutter test`：全套271项通过，未跳过既有回归。
- `g++ -std=c++17 -Wall -Wextra -Werror gui/test/frame_geometry_test.cpp -o /tmp/sidravia-round-geometry-test` 后运行：96/120/144/192 DPI算法检查通过。
- 同参数编译并执行 `gui/test/frame_decoration_test.cpp` 至 `/tmp/sidravia-round-decoration-test`：通过；它是生产策略测试，不是DWM实机测试。
- Windows隔离目录 `D:/code/sidravia-round-demo/gui`，Flutter3.47.0：`flutter.bat build windows --release -t lib/main.dart`、`flutter.bat build windows --release -t lib/dev/interactive_demo_main.dart` 均通过。正式入口只编译、不启动；Demo后台完全模拟。
- 基线隔离副本取git show cf5b6ad的窗框及测试。前后同一 `same business-size frame candidate` 测试捕获：398×642业务内容PNG字节一致，SHA256均为 `43d1acb8cecae7583038dc5f0d4af06036e3ba686d358c36efa2a6664f120a8c`。两张widget图已亲自查看，仅证明业务渲染未变与旧Flutter线已移除。

实际捕获命令含 `--dart-define=R5_OUTPUT=/mnt/d/Downloads/Sidravia-Window-Round-Evidence/{before,after}`；其他模拟图由 `EDGE_OUTPUT` 输出widget子目录。Linux widget软件渲染、固定虚构数据、HarmonyOS Sans/Material字体现有加载器；不包含DWM或真实桌面。原生实际DPI、正常/最大化/分屏/恢复、失焦/主题、四边四角拖动、顶部拖动、按钮和公告角部遮罩本轮均未实测。

## Demo与人工验收

新版：`D:/Downloads/Sidravia-Window-Round-Demo`，ZIP同名；桌面“Sidravia 原生圆角 Demo”，双击“打开离线 Demo.cmd”。默认内存偏好与假后台，正式设置/账号/凭据不会被读取或写入。保留旧输出和隔离偏好文件，包内无系统字体文件。

Demo原有“窗口诊断”新增：
- `cornerPreferenceRequested`：2表示ROUND请求，0表示DEFAULT；不是实际圆角半径或生效结果。
- `renderingPolicyHRESULT`：非客户区渲染策略设置调用结果。
- `frameExtensionHRESULT`：边缘扩展调用结果。
- `frameExtensionPhysicalPixels`：请求的物理像素扩展量；普通1/最大化0，需结合HRESULT，不表示实际阴影宽度。

继续保留cornerHRESULT、borderHRESULT、dpi、outerPhysical、visiblePhysical、clientPhysical、clientLogicalWidth/Height、topLogical、contentLogicalHeight、active、maximized、darkFrame、dwmComposition与nonClientRendering等。HRESULT成功、非客户区渲染开启都不是视觉证据。

人工检查建议：
1. 新旧Demo同尺寸同位置，分别截取带四角及四边外至少30～50物理像素桌面的原始画面；浅/深界面各在浅/深背景检查。
2. **单独看圆角**：普通窗口四角曲线和边线连续，无遮挡的黑角/白角；点击桌面失焦后再激活、切换浅深色；打开公告检查遮罩不越过真正窗口外轮廓。
3. **单独看阴影**：观察桌面上窗口四周软阴影是否存在，与新旧Demo比较；记录“有/无/不确定”，不要因细边线明显就记为阴影通过。
4. 最大化、系统分屏、恢复：遵循Windows角部策略，无双边框/额外留白/布局跳动；记录实际状态和诊断。系统允许的不同DPI/跨屏条件逐项记实测，不能用模拟DPR代替。
5. 缩窄至360×640候选，四边四角缩放、顶部拖动/双击、最小化/恢复/关闭、主要按钮和公告Escape分别检查。只用离线Demo，不操作真实认证。

完成代码和交付后停止。圆角、细边线与外阴影的原生视觉仍待用户检查；这不是Windows实机验收通过声明。
