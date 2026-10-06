# 外观选择器垂直居中修正（2026-10-06）

分支 codex/gui-platform-boundary；起始 e2fc93009aae7bc849d01ff356cd787ee3db767c。只修改 settings_page.dart 的局部容器 alignment，以及 settings_preferences_test.dart 的外框几何保护；本报告为第三个文件。文字、箭头、控件宽度、桌面最低30/移动最低48、设置行和动作保持既有实现。

根因：紧凑 Dropdown 的内部高度小于外框最小高度，旧的 selectedItemBuilder Center 只保证文字在内部居中。新增测试在修正前明确 RED：外框中心 y320，选中文字区域中心 y318，偏上2逻辑像素。容器 Alignment.center 修正后，文字区域及箭头中心相对外框误差均不超过0.01。测试实际加载 HarmonyOS Sans 字重及 MaterialIcons，覆盖三种模式、Windows/Android/iOS平台配置、文字1/2倍、浅深色、尺寸不变和选项点击；保留键盘测试。

实际命令与结果：
- dart format lib/features/settings/settings_page.dart test/settings_preferences_test.dart：成功。
- flutter test test/settings_preferences_test.dart：18项通过；新增断言修正前预期失败。
- flutter analyze：No issues found。
- flutter test：263项通过，包含R5/R5.1和偏好持久化回归。
- flutter test --dart-define=GEN_FRONTEND_OUTPUT=<Windows可访问输出目录> test/general_visual_test.dart --plain-name 'candidate settings'：前后各两张浅/深色候选成功，亲自查看。
- Windows Release 两种离线入口构建均成功；最终提交SHA见修正版包的 BUILD-INFO.json，实际命令见包内build日志。

截图 D:/Downloads/Sidravia-Selector-Center-Evidence/{before,after}/{light-settings,dark-settings}/flutter.png。398×642业务内容、DPR1、文字缩放1，Linux widget软件渲染、Windows平台配置；不等同于Windows原生GUI验收。正常字号控件内部整体下移2逻辑像素，外框及设置行未改变。不批准/覆盖golden。可选PIL差异图工具缺失（ModuleNotFoundError），未安装新依赖，提供实际前后候选图对照。

新版 Demo：D:/Downloads/Sidravia-Settings-Centered-Demo；桌面“Sidravia 设置居中修正版”。打开离线 Demo.cmd 为默认内存设置；打开持久化验收.cmd 仍只使用原隔离目录 D:/Downloads/Sidravia-Settings-Demo/isolated-preferences，既有设置文件不修改、不删除。没有真实后台或用户凭据。

相对起始HEAD：Go、IPC、GuiController、偏好存储、窗口runner、窗框、daemon和托盘全部未变。只本地提交，不推送、不合并。Windows原生实机交互未在本轮验证；请在修正版进入设置，切换三种选项观察文字和箭头相对外框，再放大字号检查。旧输出保留以免覆盖在用文件；未授权清理本轮不删除。
