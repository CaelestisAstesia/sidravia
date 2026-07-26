# Sidravia

Sidravia 是一个面向 Windows 的 Dr.COM 网络认证客户端。产品由短生命周期 CLI
`sidravia` 和长期运行的本地 daemon `sidraviad` 组成：

```text
sidravia CLI -> loopback WebSocket IPC -> sidraviad -> Dr.COM -> network
```

## 当前状态

`v0.1.0-alpha.1` 是面向 Windows amd64 的首个 Alpha。仓库已经实现并自动验证：

- CLI/daemon 状态通信骨架；
- `auth start/status/stop` 一次性认证命令、Windows 隐藏密码输入和
  `--password-stdin`；
- typed 一次性 Session 启动、停止和查询的应用边界；
- Session、Supervisor、网络快照分发与单活动 Session 规则；
- Dr.COM 5.2.0(D) 报文、Factory 和阻塞式认证 Run；
- 本地可编辑机构 Profile 的严格一次性加载；
- 生产 daemon 的 D520 注册、Profile 加载、Windows Environment Observer、typed IPC
  和统一生命周期；
- Windows JSON 文件 ACL 与原子持久化基础。

提交 `508197d` 已在 Windows 11 与吉林大学校园网完成首次现场验证：原生 CLI/daemon
选择物理以太网，完成 D520 登录、持续心跳和主动 Logout。Clash TUN 在场但未被选中。
这是单台机器、单个网络环境的 Alpha 证据，不代表已经覆盖所有 Windows 版本、网卡或
校园网络变体。机构 Profile 仍由用户放入本地配置目录，仓库不包含个人配置或凭据。

长期进度见 [产品路线图](docs/roadmap.md)，模块关系见
[当前架构](docs/architecture.md)。

## Windows Alpha 使用

安装、创建本地 Profile、启动认证与停止认证见
[Windows Alpha 快速开始](docs/getting-started-windows.md)。本版本仍有明确限制：
没有 GUI、安装器、Windows Service、自动更新、持久认证配置管理或多活动 Session；
它适合愿意使用 PowerShell 并能自行保留原网络客户端作为回退的测试者。

## 构建

需要 Go 1.26.4：

```bash
go build -o build/sidravia ./cmd/sidravia
go build -o build/sidraviad ./cmd/sidraviad
```

Windows amd64 交叉构建：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/sidravia.exe ./cmd/sidravia
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/sidraviad.exe ./cmd/sidraviad
```

## 验证

运行完整的公开仓库检查：

```bash
python3 tools/developer/verify_repository.py --scope all
```

它检查 Go 格式、测试、vet、Windows 交叉构建，以及 Python mock/验收工具测试。
涉及本地 UDP 或 HTTP 测试时，运行环境必须允许回环监听。

## 文档

- [文档入口](docs/README.md)
- [当前架构](docs/architecture.md)
- [工程实践](docs/engineering.md)
- [产品路线图](docs/roadmap.md)
- [架构决策](docs/decisions/README.md)
- [Dr.COM 5.2.0(D) 协议规范](docs/protocols/drcom-5.2.0-d.md)
- [验收证据](docs/evidence/README.md)
- [Windows Alpha 快速开始](docs/getting-started-windows.md)

## License

Sidravia 使用 [GNU Affero General Public License v3.0](LICENSE)。
