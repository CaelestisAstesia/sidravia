# ADR 0020：显式区分安装版与便携版运行目录

**状态：** Accepted

## 背景

当前 Windows 实现把配置放在 `os.UserConfigDir()/Sidravia`，把运行信息和后台日志放在
`os.UserCacheDir()/Sidravia`。这适合普通用户安装，但不满足绿色版“解压、运行、数据随
目录移动”的预期。若根据目录是否可写自动猜测模式，同一份程序可能因为权限、启动位置
或升级方式变化而悄悄改用另一套配置，造成看似丢失配置或连接到错误 daemon。

持久 Configuration/Credentials 即将成为用户可见能力，因此必须先确定两种进程都能
独立得到同一结果的目录契约。

## 决定

Sidravia 支持两种显式运行目录模式：

- **安装版模式**是默认值。非秘密配置、机构 Profile 和凭据位于
  `os.UserConfigDir()/Sidravia`；运行信息与日志位于
  `os.UserCacheDir()/Sidravia`。
- **便携版模式**由可执行文件同目录的固定标记 `sidravia.portable` 显式启用。配置位于
  `<exe-dir>/config`，运行信息位于 `<exe-dir>/runtime`，日志位于
  `<exe-dir>/logs`。

便携版中的规范布局为：

```text
<exe-dir>/
  sidravia.exe
  sidraviad.exe
  sidravia.portable
  config/
    configurations.json
    credentials.json
    institution-profiles/
  runtime/
    runtime.json
  logs/
    sidraviad.log
    sidraviad.log.1
```

CLI 与 daemon 使用同一个平台边界解析这些路径。解析只依据自身可执行文件位置、标记
以及操作系统用户目录，不读取当前工作目录，不按可写性猜测，不扫描另一种模式，也不在
两种模式间自动回退或迁移。查询 `daemon status` 只解析和读取路径，不创建目录或文件。

标记缺失表示安装版；标记存在但不是预期的普通文件，或无法可靠检查时，启动相关操作
返回错误而不降级到另一种模式。创建和打开实际文件时，既有 SecureStore、ACL、原子
替换和日志轮转规则继续生效。

## 结果

- 用户可以明确选择随目录移动的绿色版，也不会因为权限变化悄悄切换数据源。
- 安装版保持当前操作系统目录行为；便携版不依赖 AppData。
- 路径解析先于持久 Configuration/Credentials 的用户入口实施，后续构建流程只需决定
  是否把空标记放入便携包。
- 本决定不实现安装器、数据迁移、自动探测、同步、多用户共享或跨模式合并。
- 本决定接受后才进入代码实施；文档接受不等于相应代码已经完成。
