# Windows 离线手动验收

仅由验收者在 Windows x64 桌面运行。工具不联网，不安装、不卸载、不修改 PATH/任务/注册表，也不删除系统数据。检查器只写新证据目录；启动辅助会创建全新隔离 namespace、复制包内官方 institution-profiles，然后启动 GUI。不要填写真实账号、密码或点击认证。所有安装器操作由你手动完成。

这次检查的版本、SHA、文件哈希见交付包来源说明。先核对 SHA256。安装目录与运行 namespace **不能隔离**固定 AppId、SidraviaDaemon 计划任务、快捷方式、用户 PATH 和默认数据删除。旧便携版（例如 D:\Software）自包含数据不自动构成冲突，工具不会扫描或修改它；旧用户 PATH 请由你手动撤销并在新终端确认。旧便携版留下的同名任务/快捷方式等仍是共享冲突。用户可直接在当前账户验收：用下文 KeepDataOnly 模式保留已有默认数据，先正常退出旧GUI/后台并自行撤销冲突集成。其他 BLOCK 仍要停止并核对，不能为通过检查清掉真实数据。需要测试“删除默认数据”时再单独安排，不作为本轮前置。

## 离线准备

将整套交付资料、便携 zip、安装 exe、检查脚本保存在本地可写目录，解压便携包到全新目录。不要从压缩文件预览直接运行。使用 Windows PowerShell 5.1，无需 Python、PowerShell 7 或网络。若执行策略禁止脚本，可仅在本次命令使用 `powershell.exe -NoProfile -ExecutionPolicy Bypass -File ...`；单位策略仍禁止则停止回报，不修改全局策略。

离线前确认 Windows x64、VC++ x64 运行库（System32 中 MSVCP140.dll、VCRUNTIME140.dll、VCRUNTIME140_1.dll）与 WebView2 Runtime。它们是外部前置，不随包完整提供；缺少时记环境 BLOCK，勿期待离线自动下载修复。文件存在仅是初筛，GUI 能启动仍需现场验证。WebView2 用 Windows“已安装的应用”查看 Microsoft Edge WebView2 Runtime；不要仅把 Edge 浏览器存在当运行库证明。

先断开网络（含 Wi-Fi/有线），记录方式。离线公告失败可以预期，不证明联网通知送达。当前候选 GUI 未签名；若系统安全软件或 SmartScreen 阻止，记录原文和文件 SHA，不关闭防护强行运行。

## 命令约定

在交付目录打开 PowerShell。以下路径是可复制的模板，先仅修改这一段的值（SHA 必须为来源说明中的完整 40 位 BuildID）：

```powershell
$sha = '替换为40位BuildID'
$manifest = Join-Path $PWD 'candidate-manifest.json'
$target = Join-Path $env:LOCALAPPDATA ('Programs\Sidravia-Acceptance-' + $sha.Substring(0,8))
$ns = 'accept-installed-' + $sha.Substring(0,8)
$portable = Join-Path $PWD 'portable'
$portableNs = 'accept-portable-' + $sha.Substring(0,8)
$evKeep = Join-Path $PWD 'evidence-keep'
$evDelete = Join-Path $PWD 'evidence-delete'
```

实际便携解压路径不同就修改 `$portable`。证据必须在产品、用户数据目录外。每轮证据 root 和 namespace 都要全新，已有阶段结果拒绝覆盖。JSON 保存路径、文件长度与 SHA256、PATH 原文和任务动作，不保存配置正文、进程命令行或密码；发回前仍应检查本机用户名、路径是否需要打码。

## 便携 GUI

```powershell
.\offline_launch.ps1 -Directory $portable -Namespace $portableNs
```

脚本拒绝未知已有 namespace 和符号链接/junction/reparse 路径；仅当自己的标记中 namespace、目录和 BuildID 均匹配时允许重复启动，不覆盖既有数据。官方 profiles 复制到 `portable\namespaces\<namespace>\institution-profiles`。便携和安装实例使用不同 namespace，避免桌面单实例相互激活。同一辅助脚本命令可再次打开自己的 namespace，以验证设置持久化；不要改全局环境变量。

人工检查：断网下窗口可打开，已有设计和中文正常；100%/125%/150%（设备支持范围内）无重要裁切；主窗口、菜单、托盘显示和打开/隐藏行为；退出应结束 GUI 和它拥有的后台。记录实际做过的 DPI，其余写未验。不得输入真实认证信息。

便携预期只产生自身 namespace 内配置/GUI 设置/runtime/logs；不应新增安装注册项、共享任务、快捷方式或 PATH。可在便携运行前后用独立 Before 证据记录，再人工比较 JSON；安装 Before 要在退出便携之后另开全新证据轮次，不能复用。

## 当前账户：只验收保留数据（本轮默认）

不需要另建用户、虚拟机或删除现有数据。先从托盘明确退出旧版及其后台，用户自己撤销旧PATH/自启（若有）；脚本不会代替你停止进程或修改集成。首个Before命令增加 `-KeepDataOnly`，后续Installed/AfterKeep自动继承该基线模式。

```powershell
.\offline_acceptance.ps1 -Stage Before -Directory $target -Namespace $ns -EvidenceRoot $evKeep -ExpectedBuildID $sha -CandidateManifest $manifest -KeepDataOnly
```

此模式允许已有默认数据，保存原文件路径/hash；安装后允许新增测试namespace，但原有文件不得改变。卸载后再次核对原有数据和整个卸载前数据清单。记录hash不等于备份，脚本也无法替你控制卸载器选项：卸载时必须选“不删除数据/否”。该基线拒绝AfterDelete；误选删除即使后续清理检查通过也会报告数据变化。

当前桌面属于OneDrive重解析路径，首轮不要勾“桌面快捷方式”，先验证安装、开始菜单、PATH及保留数据卸载。实际桌面图标检查另记未验，不能因此声称已通过；不会放宽链接保护。安装时自启也可先不勾，不在本轮验证实际登录自启。

然后按下面第一轮从手动安装步骤继续，**不要再执行其中不带KeepDataOnly的Before模板**。已有阶段结果不覆盖；每次新验收另用新证据目录和未用namespace。

## 第一轮：安装 → 保留数据卸载

以下是无默认数据账户的严格模板；当前账户本轮已执行上面的KeepDataOnly Before，跳过此重复步骤。严格模板先退出所有 Sidravia 进程，再执行：

```powershell
.\offline_acceptance.ps1 -Stage Before -Directory $target -Namespace $ns -EvidenceRoot $evKeep -ExpectedBuildID $sha -CandidateManifest $manifest
```

只在无 BLOCK 时手动运行候选安装 exe，安装目录明确填 `$target`。记录安装类型、GUI 组件、自启选择（GUI 或后台）、用户 PATH、桌面图标。建议完整 GUI，勾用户 PATH 和桌面图标，自启可以关闭；实际选择与 JSON 对照。安装器结束出现“系统集成未完成”即失败，不能仅凭文件存在判通过。

```powershell
.\offline_launch.ps1 -Directory $target -Namespace $ns
```

完成上面的窗口/托盘/缩放检查。GUI 仍运行时尝试卸载，应明确拒绝且程序文件仍在；不要强杀或删除文件来绕过。随后从托盘正常退出并等待后台退出。若仍有进程，停止并记录。此时可在安装目录手动新建无秘密的 `keep-me.txt`，观察未知文件保留；不要修改 payload。然后采集稳定的卸载前状态：

```powershell
.\offline_acceptance.ps1 -Stage Installed -Directory $target -Namespace $ns -EvidenceRoot $evKeep -ExpectedBuildID $sha -CandidateManifest $manifest
```

Installed 自动核对 BUILD-INFO BuildID。手工确认任务动作指向本安装、快捷方式目标正确、PATH 仅新增本安装目录（JSON 同时给出 Before/Current 的精确 token 序列，保留空项、重复项和顺序）。任务是否实际在下次登录运行、当前用户 PATH 是否在新终端生效均需人工，不由快照自动证明。真实登录自启通常运行默认 namespace；隔离 GUI 启动成功不能证明它安全，干净验收账户且不含真实配置才做这项。

手动卸载，删除数据问题选“否”。

```powershell
.\offline_acceptance.ps1 -Stage AfterKeep -Directory $target -Namespace $ns -EvidenceRoot $evKeep -ExpectedBuildID $sha -CandidateManifest $manifest
```

预期程序 payload、注册项、任务和快捷方式移除；自动 Installed 按交付 candidate-manifest.json 核对完整 payload/hash，卸载后按同清单及实际观测的 uninsNNN 文件检查消失；清单 SHA256 固定在 Before，后续阶段不允许换清单；用户 PATH 与 Before 原文及注册值类型一致；namespace 文件与 Installed 相同。未知 keep-me.txt/未知目录和 namespace 数据应保留；安装目录非空不自动等于卸载失败。无观测数据时 KeepData 为 SKIP，不能说保留已证明。卸载后不要删除保留现场。

## 第二轮：安装 → 删除默认白名单数据（本轮暂缓）

**仅在干净测试用户做**。namespace 数据从来不属于删除对象。为本轮使用不同目标目录、namespace 和全新证据 root：

```powershell
$target = $target + '-delete'
$ns = $ns + '-delete'
.\offline_acceptance.ps1 -Stage Before -Directory $target -Namespace $ns -EvidenceRoot $evDelete -ExpectedBuildID $sha -CandidateManifest $manifest
```

若第一轮留下默认数据/其他共享状态导致 BLOCK，换另一个干净 Windows 用户，不自动清理。重复安装与隔离 GUI 检查，退出所有实例。要证明默认数据删除，必须人工在此干净用户用默认实例创建**假测试数据**（无真实账号/凭据、不认证），然后退出；同时可以放一个不在白名单的无害测试文件，证明它保留。使用默认实例会写默认用户目录，这是此轮现场人工操作，辅助脚本不会替你做。没有观察到默认白名单文件，可以继续卸载但该证明只能 SKIP。

```powershell
.\offline_acceptance.ps1 -Stage Installed -Directory $target -Namespace $ns -EvidenceRoot $evDelete -ExpectedBuildID $sha -CandidateManifest $manifest
```

手动卸载，删除数据问题选“是”，再执行：

```powershell
.\offline_acceptance.ps1 -Stage AfterDelete -Directory $target -Namespace $ns -EvidenceRoot $evDelete -ExpectedBuildID $sha -CandidateManifest $manifest
```

自动比较 Installed 中观测到的默认白名单：`%APPDATA%\Sidravia\configurations.json`、`gui-settings.json`，`%LOCALAPPDATA%\Sidravia\runtime.json`，logs 中 sidraviad.log、sidraviad.log.1、sidraviad-YYYY-MM-DD-HHMMSS.log。它们应消失；其他文件和所有 namespace 文件应保留。检查器不生成默认配置、也不证明未观测文件曾被删除。SKIP/MANUAL 不是 PASS。

## 可选与未验项

组件取消/恢复、同版本覆盖安装、版本升级、后台自启/GUI 自启各模式可单独开轮次验收，不要和以上两轮混用证据。升级保留配置需要明确旧包来源和人工计划。实际登录自启、通知送达、联网公告、真实认证、校园连接、不同 Windows 版本和签名可信度并非离线脚本通过就已验收。不要为了补齐这些项目启用真实凭据或校园认证。

出错立即停止受影响操作，保留原包、证据和现场；不要运行修复、清数据、覆盖阶段 JSON。脚本异常退出且没有 JSON 表示该阶段**未完成**，不是通过；记下报错。检查器拒绝链接、超大目录、访问被拒绝时也应留证回报。不要直接回传完整配置/原始敏感日志。

## 恢复联网后的回报模板

```text
候选 BuildID / 两包 SHA256：
Windows 版本 / x64 / 缩放：
用户是否专用干净账户 / VC++ 与 WebView2：
断网方式及各检查时网络状态：
便携路径 / namespace / GUI、托盘、退出结果：
安装两轮目标 / namespace / evidence root：
安装组件 / PATH / 桌面图标 / 自启选择：
运行中拒绝卸载：通过 / 失败 / 未验
保留卸载：实际结果、SKIP/MANUAL 项
删除卸载：观测了哪些假默认数据、白名单删除、namespace/未知文件保留
PATH 精确恢复 / 共享任务 / 注册项 / 快捷方式 / payload 残留：
实际登录自启 / 新终端 PATH / 通知 / 校园：未验或具体证据
异常操作与原文（先打码）：
附：Before/Installed/After*.json（审核隐私后）、必要截图；不附配置正文或真实凭据
```
