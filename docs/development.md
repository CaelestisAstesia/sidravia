# 开发指南

本文面向修改代码、验证和构建发行包的开发者。日常 GUI 使用见[使用指南](guide.md)，ctl 操作见[高级使用](advance.md)。

后台、ctl 和 Windows 维护工具使用 Go，GUI 使用 Flutter。Windows 是主要交付平台，Linux 提供共享核心的命令行与便携归档。先理解[架构中的状态归属](architecture.md)，再按改动范围选择验证。

## 工具与验证入口

当前仓库固定 Go **1.26.4**、Flutter **3.47.0**，CI 使用 Python **3.14.4**。工具版本与完整参数见 [developer 工具说明](../tools/developer/README.md)和[验证工作流](../.github/workflows/verify.yml)。不需要为每次修改创建新编译环境或清理缓存。

在仓库根目录使用统一入口：

```bash
python3 tools/developer/dev.py verify --scope go --go-checks native
python3 tools/developer/dev.py verify --scope go --go-checks cross
python3 tools/developer/dev.py verify --scope python
```

根据改动选择检查范围；局部 Go 修改也可以直接运行对应包的 `go test -count=1`。交叉编译只证明可编译，不能证明目标平台运行行为。真实认证、安装卸载和系统集成验证需要对应环境与明确操作范围。

## Windows GUI

Windows GUI 需要原生 Windows 工具链。可从 Windows 或具备 PowerShell 互操作的 WSL 运行：

```bash
python3 tools/developer/dev.py windows --check-gui
```

该入口复用受管理的 Windows 工作区与缓存，执行 Flutter 分析、测试和 Release 编译。Flutter、Go 和工作区位置可显式指定，参数见工具说明。同步源码前需处理未跟踪文件；工具不会自动把文件暂存到 Git。

需要热重载时，在 Windows 的 `gui` 目录运行 `flutter run -d windows`；它会使用实际后台与用户数据。只做视觉开发可使用隔离的预览入口，见 [GUI 开发说明](../gui/README.md)。

## 构建发行包

发行包构建要求干净的源码提交与尚不存在的输出目录。以下 Windows 路径为示例，须替换为实际安装的工具路径：

```bash
python3 tools/developer/dev.py windows --package --windows-python 'C:\Python314\python.exe' --iscc 'C:\Tools\Inno Setup\ISCC.exe'
```

该入口编译 Windows GUI、核心与维护工具，并构建和检查便携包及安装器。安装器使用固定的 Inno Setup **7.1.0**。在干净的 Linux amd64 检出中构建核心便携包：

```bash
python3 tools/developer/dev.py package --version 0.3.0-alpha.1 --output dist
```

输出目录的父目录需存在。以上入口不会安装程序、启动 GUI、真实认证、签名或发布。包内容、哈希与 help smoke 检查也不能代替实际安装和网络验证。

手动 Package 工作流见 [package.yml](../.github/workflows/package.yml)。早期 `tools/build` 的 legacy 模式为历史 CLI 包保留，现代 GUI 发行使用 `distribution` 路径；不要混用其安装集成说明。

## 代码入口

| 路径 | 职责 |
| --- | --- |
| `cmd/sidravia`、`internal/cli` | 命令行入口与呈现 |
| `cmd/sidraviad`、`internal/daemon` | 后台组合、配置、会话、网络与协议 |
| `internal/ipc` | 本机通信契约、客户端与服务端 |
| `internal/clientbootstrap` | 后台探测、启动与连接 |
| `cmd/sidraviamgr`、`internal/installationmanager` | Windows 当前用户安装集成 |
| `gui/lib` | Flutter 应用、通信与界面 |
| `tools/developer`、`tools/build` | 固定验证和发行工具 |

继续阅读[架构概览](architecture.md)与[协议适配](protocols.md)。

## 提交改动

说明实际问题、改动后的行为和验证结果。测试优先覆盖领域规则、公共契约与真实调用链；避免只重复实现逻辑。涉及凭据、协议、并发生命周期或发行流程时，需要特别核对取消、清理、错误处理和敏感信息边界。

不要提交账号、密码、原始现场日志、用户数据或生成的发行产物。涉及协议兼容性时，分别说明来源、确定性测试和真实环境结果；不要将 mock 通过写成网络现场验证。
