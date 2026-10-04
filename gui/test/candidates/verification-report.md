# GUI 首轮修复 R2 交付记录

分支 `codex/gui-platform-boundary`。起始 HEAD 为 `2216e7e2444c6de2fe16523d1dbbb2d3324f3004`；入口工作区干净，remote 为 `git@github.com:CaelestisAstesia/sidravia.git`。`git ls-remote` 确认远端目标分支仍指向起始 HEAD，无后续提交。本轮不推送、不合并。

## 三项修复

1. `HomePage` 在绑定层监听现有 GUI 与公告控制器，提取公告并接入原有已读/弹窗动作。`HomeView` 只接收 `HomeViewData` 与回调；正式和 fixture 都使用 `HomeNoticeView`。无公告或功能禁用时不显示入口与分隔线。
2. `controller.notice` 映射为独立 `actionError`，显示/清除完全跟随原控制器。正式 `HomePage` 配合真实 `GuiController` 和假 IPC 客户端测试错误出现、成功后的清除与 `sessionStop` 参数；没有真实进程、IPC 或凭据访问。
3. 修复 24px 页面 padding、42px 头部区域、14px 操作区顶间距、可增长的 92px 上下文、按钮最小高度、公告 30/20px 分隔留白、网格间距、文字颜色及 Material 隐含尺寸。全局主题未改。

## 截图条件与结果

原 HTML 字节未修改，SHA-256 与交接文件一致。参考在 1280×1000 浏览器视口、DPR 1、light/reference/connected/active 下生成。完整窗口 400×690，裁剪 `.view` 为 `(311,111,398,642)`；外框 1px 边框和 46px 顶部预留不进入本轮 Flutter 画布。Flutter 使用同一生产组件、398×642 逻辑画布、文本缩放 1、像素比 1，背景也在真实 Flutter 渲染边界内。

有效产物：`reference-window.png`、`reference-content.png`、`flutter-content.png`、`overlay.png`、`difference.png`。两端完整内容图均为 398×642，无拉伸。所有五张图都已查看。叠加为 50/50；差异图为逐通道绝对差，没有阈值或相似度通过声明。详细测量见 `reference-measurements.json`、`flutter-measurements.json` 和 `geometry-comparison.md`。

头部、状态块、44px 标记、操作区、分隔线、公告外框和公告三行网格的坐标尺寸已一致。上下文顶坐标差 0.359px；状态说明比参考宽 7.04px、居中后左移 3.52px，行盒顶坐标差 0.5px。账户 Latin 字形、✓、设置与箭头绘制仍有差异。参考实际字体为 Microsoft YaHei UI/Segoe UI，Flutter 继续用已存在的 HarmonyOS Sans；未换字体或下载/重新分发字体来掩盖差异。

上一轮缺字原因是测试未实际加载字体：本轮显式加载生产字体资源及三个字重、Material 图标字体与本机已有 Segoe UI Symbol，候选图已无中文、按钮、图标和 ✓ 缺字。原缺字 golden 字节保留用于追溯，已退出有效基线比较。测试改为有语义/几何断言的回归测试，截图仅在 `GENERATE_HOME_CANDIDATE=true` 时生成；没有自动批准任何 golden。

## 实际命令与结果

工作目录为仓库 `gui/`，除下面有路径前缀的 Git/工具命令外：

- `dart format lib/features/home/home_page.dart test/home_visual_test.dart`：通过。
- `flutter analyze`：通过，No issues found。
- `flutter test --dart-define=GENERATE_HOME_CANDIDATE=true test/home_visual_test.dart`：4 项通过，生成候选图与坐标。
- `flutter test --reporter compact`：113 项通过。包含正式绑定、公告异步更新/消失/已读/弹窗、错误出现/清除与回调测试。
- `flutter build bundle --debug`：通过。编译证据独立于视觉对照与 Windows 验收。
- `git diff --check`：通过。
- `git diff 2216e7e --name-only`：生产变更只有首页展示/绑定文件，另有首页测试、截图、测量、记录与截图工具。

截图工具依赖装在临时目录 `/tmp/sidravia-visual-tools`，未改变 GUI dependencies、SDK 或系统字体。初次 Chromium 启动失败为缺少 `libnspr4.so`；通过 `apt-get download libnspr4 libnss3 libasound2t64`，将 deb 只解压到该临时目录，配置 `LD_LIBRARY_PATH` 后成功。早期 widget 字体文件读取和截图等待在测试假时钟中悬停，停止后改为 `tester.runAsync`，最终命令通过。早期 400×644 与缺字候选已移出当前截图集，未批准或覆盖正式基线。

从仓库根复现参考与对照（Node packages 仅供本地工具）：

```bash
NODE_PATH=/tmp/sidravia-visual-tools/node_modules \
FONTCONFIG_FILE=/tmp/sidravia-visual-tools/fonts.conf \
LD_LIBRARY_PATH=/tmp/sidravia-visual-tools/libs/usr/lib/x86_64-linux-gnu \
node gui/tool/capture_home_reference.cjs /tmp/sidravia-reference-r2/Sidravia_Final.html gui/test/candidates
NODE_PATH=/tmp/sidravia-visual-tools/node_modules \
node gui/tool/compare_home_capture.cjs gui/test/candidates
```

`fonts.conf` 是只含 Windows 现有字体路径与临时缓存的独立 Fontconfig 配置，未改系统配置。不同机器可在 widget 候选命令中指定 `--dart-define=HOME_SYMBOL_FONT=/path/to/existing/seguisym.ttf`。普通语义/几何测试不要求此 Windows 字体存在；候选生成缺少该文件时明确报错。

## 范围与验收状态

Go、IPC 客户端与协议、GuiController、状态机、权限、凭据、bootstrap、daemon 生命周期、Windows runner、标题栏、托盘、全局主题及其他页面均未修改。参考图展示的模拟标题栏不纳入内容比较。

代码修复与自动验证完成；有效视觉对照已生成，仍等待用户视觉确认。不是 Windows 实机验收，未运行真实后端或校园验证。本轮后停止，不进入七状态、其他页面、深色或其他尺寸。

首次逐像素对照因 Flutter RepaintBoundary 只捕获了内容高度 544px 而被工具拒绝（canvas mismatch）；之后让真实绘制边界填满 398×642，再重新渲染和比较，没有缩放或补图对齐。
