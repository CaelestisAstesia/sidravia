# R5.1 滚动、连续布局与公告弹层

起始 HEAD：`fe3da26d1b6a13d20f75ab0175b3fef51465fbc7`。分支 `codex/gui-platform-boundary`。结束 HEAD 为包含本报告的 R5.1 提交（交付包 BUILD-INFO.json 记录完整 SHA）。本地提交、不推送、不合并；候选图片不批准为 golden。

## 三项结果与修改范围

- `shared/widgets/design_widgets.dart`：继承现有 ScrollBehavior，仅关闭页面 scrollbar 装饰。连续插值 padding：父约束宽度620及以下24/12/26；620至758线性过渡；758以上36/16/32。页面620上限不变。
- `features/home/home_page.dart`：首页复用 DesignPage；操作区320上限、字号、控件尺度与生产动作不变。
- `dev/interactive_demo_main.dart`：开发工具改为48高横向滚动行，标题及反馈固定文本槽且提供完整 Tooltip；新增仅开发的公告展示策略。普通字号工具区稳定106.8高，正式程序不含该工具区。
- `features/announcements/announcement_widgets.dart`：PC保留根部340×360 Dialog；Android/iOS使用框架 Modal Bottom Sheet、拖动条、安全区域与键盘占位。共用 AnnouncementContent、原公告数据、markCurrentRead 与现有动作。正文使用 DraggableScrollableSheet 提供的 ScrollController：长正文先扩展/滚动；正文在顶部下拉可缩至最小高度，但不自动关闭。拖动条下拉、关闭按钮、遮罩和系统返回可关闭，避免阅读正文误关闭。
- `test/home_responsive_test.dart`：旧硬断点预期改为明确连续公式。
- `test/offline_demo_test.dart`：原PC Dialog测试显式Windows平台，保留原断言。
- `test/r51_adaptation_test.dart`：真实HomePage/Shell绑定、逐像素几何、滚动/键盘/焦点、已读、PC窄窗、Android/iOS面板、关闭路径、长文、横屏、文字放大、键盘、安全区域和Demo注入测试。
- 本报告、录屏工具与 `r51-evidence`：持久测量和候选图。部分原文件经 dart format 格式化，非新产品设计。

原生窗框、窗口Dart封装、Shell960上限、Go、IPC、GuiController、认证、权限、凭据、daemon、托盘业务语义未修改。主仓库既有3个Go未提交文件未触碰。没有显式Scrollbar/RawScrollbar待移除；业务页面和公告局部关闭自动装饰，不改变输入控件内部行为。

## 突变定位与测量

数据见 `r51-evidence/{baseline,updated}-geometry.json`。这些是Windows平台配置widget渲染、DPR1、文字缩放1的逻辑像素，**不是实际Windows外框物理宽度或实测DPI**。原生工具初始化报错：`sandboxCwd is not a local file URI: file:///home/astesia/code/sidravia`，因此外框物理宽度/DPI未知，不能把用户所说约960物理宽直接认定为源码960逻辑宽。

| 首页/DesignPage头部 | R5 x/y/w/h | R5.1 x/y/w/h |
|---|---|---|
| 757 | 92.5/12/572/42 | 104.413/15.971/548.174/42 |
| 758 | 105/16/548/42 | 105/16/548/42 |
| 759 | 105.5/16/548/42 | 105.5/16/548/42 |
| 959 | 205.5/16/548/42 | 同左 |
| 960 | 206/16/548/42 | 同左 |
| 961 | 206.5/16/548/42 | 同左 |

旧757→758产生x+12.5、y+4、宽−24跳变；新变化x+0.587、y+0.029、宽−0.174。960附近原本仅正常居中每像素x+0.5，无突变，未改Shell。
旧Demo工具区在320/322/458/522宽分别134/109/102/77高。新320..1100每逻辑像素采样均106.8，顶部业务偏移固定46+106.8（Windows平台widget包含自绘顶区）；浏览器无Windows顶区，只占工具区106.8。页面根据剩余父约束布局，不在页面扣标题栏。

默认业务内容398×642候选与R5 `candidates/content.png` 像素比较：同尺寸、RGBA不同通道 **0**。旧golden不改、不覆盖。620以下及758以上既有间距终点保留；中间宽度预期改变以消除硬切。

## 实际命令与验证

在仓库 `gui/`：

```sh
dart format lib/shared/widgets/design_widgets.dart lib/features/home/home_page.dart lib/features/announcements/announcement_widgets.dart lib/dev/interactive_demo_main.dart test/home_responsive_test.dart test/offline_demo_test.dart test/r51_adaptation_test.dart
flutter analyze
flutter test --dart-define=R51_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R51/evidence test/r51_adaptation_test.dart
flutter test
flutter test --dart-define=HOME_RESPONSIVE_OUTPUT=/mnt/d/Downloads/Sidravia-Offline-Demo-R51/responsive test/home_responsive_test.dart
```

Flutter3.47.0：analyze无问题；新增11项通过；完整235项通过；响应式候选11组生成。初次发现旧Android默认fixture断言Dialog，已按本地流程记录事故并执行R1恢复；之后修正连续采样的自然宽度增长模型、真实键盘padding/viewPadding关系及一个括号lint，保留业务与视觉断言。

Windows隔离源码副本 `D:/code/sidravia-demo-r51/gui`：`flutter build windows --release -t lib/dev/interactive_demo_main.dart` 成功。临时web副本：`flutter create --platforms=web .`、`flutter build web --release --no-web-resources-cdn -t lib/dev/interactive_demo_main.dart` 成功；未在仓库新增web平台文件。临时浏览器字体补充只用于截图渲染，交付Windows包不含系统字体。

录屏：`node gui/tool/record_r51_demo.cjs <临时web build/web> <交付recording目录>`。WSL Chromium153、Playwright1.63、DPR1，实际编译Flutter离线入口，阻止外部请求。包含首页和设置连续宽度调整、短窗滚动、PC公告/Escape、开发手机面板及关闭。浏览器viewport为逻辑内容尺寸，**不是Windows原生拖拽或移动触屏验收**。

## 候选、交付与待验

持久候选已亲自查看：PC360×640、Android/iOS360×640、780×320横屏且文字1.8倍/键盘占位。正文无滚动条，长文仍能滚动；安全/键盘区域由媒体约束提供，截图不绘制虚拟系统键盘。候选不是批准基线。

可获取位置：`D:/Downloads/Sidravia-Offline-Demo-R51.zip`，解压运行 `打开离线 Demo.cmd`；桌面独立R51文件夹同入口。详情与SHA见包中BUILD-INFO.json。录屏/完整11组截图、测量、日志和本报告位于包中review。没有真实账号、凭据、系统字体或后端程序。

用户报告R5缩窄、拖动、四边四角、窗口控制、主要按钮、弹窗、离线退出基本正常。本轮未重复宣称这些为R51实测。**Windows原生验收未完成**；Android/iOS仅widget平台模拟，未触屏/实机验收。没有实际跨屏DPI物理记录。

人工待验：双击R51；拖宽/缩窄经过原跳点并观察首页/设置无整片跳动；缩短后滚轮和Tab进入屏外内容；PC360宽公告仍居中Dialog；工具区横向移动查看完整控制，切换手机面板后测试拖动条下拉、正文滚动、关闭/遮罩；正式移动端另验系统返回、触屏与键盘。借助已有窗口诊断导出外框/客户区/逻辑内容/DPI/窗口状态，再关联用户原物理宽跳点。不要将鼠标预览称为触屏验收。

框架契约参考：[DraggableScrollableSheet](https://api.flutter.dev/flutter/widgets/DraggableScrollableSheet-class.html)、[enableDrag与拖动条](https://api.flutter.dev/flutter/material/BottomSheet/enableDrag.html)、[showModalBottomSheet](https://api.flutter.dev/flutter/material/showModalBottomSheet.html)。未新增网络架构或更改业务规则。
