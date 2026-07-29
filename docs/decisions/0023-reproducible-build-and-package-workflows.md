# ADR 0023：单一可复现构建与打包入口

**状态：** Accepted

## 背景

仓库当前只有 README 中的手工 `go build` 命令和负责健康检查的公共 verifier，没有负责
版本注入、产物命名、便携标记、压缩包内容或校验文件的唯一构建入口，也没有 GitHub
Actions。手工命令足以开发，却容易让本地包、CI 包和 GitHub Release 使用不同参数或
遗漏 `sidravia.portable`。

ADR 0020 已经明确安装版与便携版的运行目录语义。下一步应把相同源码构造成可复查的
Windows 包，同时不把尚未实现的 Linux daemon 描述成可发布产品。

## 决定

在 `tools/build` 提供一个只使用 Go 标准库的构建工具。开发者和 GitHub Actions 都调用
这一个工具，不再各自复制 `go build`、zip 和 SHA-256 逻辑。

构建工具必须显式接收规范的 ProductVersion、BuildID 和输出目录，要求 Go 1.26.4，
使用 `CGO_ENABLED=0`、`GOOS=windows`、`GOARCH=amd64`、`-trimpath`、
`-buildvcs=false`、只读 module 模式和空 Go linker build ID。daemon 的
`main.ProductVersion` 与 `main.BuildID` 通过 linker flags 注入。构建环境不得从
`GOFLAGS`、Go workspace 或用户 GOENV 隐式改变参数。

同一调用生成：

```text
sidravia-v<version>-windows-amd64.zip
sidravia-v<version>-windows-amd64-portable.zip
SHA256SUMS.txt
```

两个 zip 共享同一对 PE 二进制、README、Windows 快速开始、LICENSE、内部
`SHA256SUMS` 和固定格式 `BUILD-INFO.txt`。普通包不含 marker，默认使用操作系统用户
目录；便携包额外包含空的普通文件 `sidravia.portable`。zip 内路径、顺序、权限和时间戳
固定；相同源码、Go 版本、ProductVersion 与 BuildID 必须得到逐字节相同的二进制、zip
和校验文件。

输出目录必须原先不存在，工具先在同一父目录的临时目录完整构建和打包，成功后再原子
发布；失败时不得覆盖旧产物或留下看似完成的输出目录。构建工具不读取 Git 来猜版本，
不签名、不上传、不创建 tag 或 GitHub Release。

GitHub Actions 分为两个窄工作流：

- push 和 pull request 运行现有公共 verifier；
- 手动 `workflow_dispatch` 接收版本，以当前 `github.sha` 作为 BuildID，调用同一个
  构建工具并上传三个生成文件作为 workflow artifact。

两个工作流只有 `contents: read` 权限。打包工作流不因 tag 自动发布，也不写仓库或
Release。官方 Action 的普通 major-version 更新是维护事项，不改变本 ADR。

## 结果

- 本地、CI 和未来 Release 使用相同的构建实现与产物契约。
- 普通包和便携包的唯一区别是运行模式元数据与 `sidravia.portable` marker，不重新编译
  两套二进制。
- Windows amd64 是当前唯一可发布 target。Linux 仍只在后续 WSL/Linux 切片实现和验证，
  本决定不生成会被误认为可用产品的 Linux 包。
- 代码签名、自动 Release、SBOM、安装器、多架构矩阵和包管理器继续留待有实际发布需求
  的独立切片。
- 本决定接受后才进入代码实施；文档接受不等于构建工具或 GitHub Actions 已经完成。
