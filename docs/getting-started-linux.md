# Sidravia Linux/WSL 快速开始

Linux/WSL 与 Windows 共享 domain core、typed IPC、Configuration、Session、运行目录与日志
契约。它是原生开发/运行时基线，不是首版产品包，也不证明 WSL 能认证 Windows 校园接口。
macOS 当前明确 Unsupported。

## 前置条件和构建

需要 Go 1.26.4，并允许普通用户回环监听与本地进程启动：

```bash
go build -trimpath -buildvcs=false -o build/sidravia ./cmd/sidravia
go build -trimpath -buildvcs=false \
  -ldflags '-X main.ProductVersion=0.1.0-dev -X main.BuildID=linux-dev' \
  -o build/sidraviad ./cmd/sidraviad
```

CLI 从 owner-only `runtime.json` 读取 endpoint、token 与 daemon BuildID；CLI 自身没有独立
BuildID linker 变量。

## 运行目录

官方机构 Profile 是随版本程序内容，两种模式都从程序根读取：

```text
<exe-dir>/institution-profiles/<InstitutionProfileID>.json
```

安装版：

- `configurations.json`：`os.UserConfigDir()/Sidravia`，通常是
  `~/.config/Sidravia/configurations.json`；
- runtime 与日志：`os.UserCacheDir()/Sidravia`，通常是 `~/.cache/Sidravia`；
- 官方 Profile：`<exe-dir>/institution-profiles/`。

便携版：

```text
<exe-dir>/
  sidravia
  sidraviad
  sidravia.portable
  institution-profiles/
    jlu.json
  config/
    configurations.json
  runtime/
    runtime.json
    runtime.json.lock
  logs/
    sidraviad.log
    sidraviad.log.1
```

daemon 直接读取 Profile，无配置目录复制或 seed。目录可临时用于调试 Profile，但不保证
跨升级保留；正式新增机构走上游提交。增删 marker 前必须先停止 daemon。

## daemon 与配置

```bash
./sidravia daemon status
./sidravia daemon start
./sidravia daemon restart
./sidravia daemon stop

./sidravia config list
./sidravia config create
./sidravia config set-password <configuration-id>
./sidravia auth start --config <configuration-id>
./sidravia auth status <session-id>
./sidravia auth stop <session-id>
./sidravia profile list
```

`daemon status` 严格只读。Linux launcher 使用 `setsid` 启动兄弟 daemon，stdin 为 nil，
stdout/stderr 写入当前日志；随后 `Process.Release`，不持有 Windows 式子进程 Wait。
daemon 用 `flock` 在 `runtime.json.lock` 上维持同用户单实例。

Configuration schema 3 编码 `AutoLogin` 与 `AutoReconnect`。严格 schema 2 仍可读取，
使用 `false/true` 默认值并且仅打开不会重写。密码交互通过 termios 关闭回显；非交互必须
显式 `--password-stdin`。

## 权限、日志和 WSL 边界

运行目录使用 owner-only 权限：目录 0700，runtime、锁与日志 0600。便携文件系统明确不支持
权限模型时可以报告 `unprotected`；新增或替换秘密需要当次明确授权。

Info/Debug 日志不含密码、token、Profile JSON、MAC、DNS/DHCP、网关、接口 ID、报文字节或
原始 error。Trace 含完整 D520 数据报，显式敏感。

WSL 虚拟接口会被保守分类为非 hardware-backed/endpoint，不能通过自动物理绑定谓词。当前
基线不提供面向认证服务器的完整 Windows route/metric 事实，也不发布 Linux 包或 service
manager 单元。WSL 生命周期或交叉编译不能替代 Windows-native 与校园现场验证。
