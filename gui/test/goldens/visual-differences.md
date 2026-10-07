# 首轮视觉对照记录

- Flutter 候选图：`home-connected-400x644.png`，由同一生产 `HomeView` 和固定 fixture 生成。
- 参考 HTML 的设计窗口仍为 400×690；其 `.view` 主内容区应按实测尺寸与 Flutter 画布对齐，本次未把浏览器视口缩成 400×690。
- 当前 WSL 环境没有 Chromium、Edge 或 Playwright，因此没有生成原 HTML 参考图，也没有生成叠加/差异图。
- Flutter widget 图已生成并查看，但 HarmonyOS Sans 在 Linux widget rasterizer 中的中文/符号字形显示为方框；该图只能作为候选布局产物，不能作为视觉验收证据。
- Windows 原生标题栏、客户区、DPI、托盘和真实 daemon/IPC 未运行，均未宣称通过。
