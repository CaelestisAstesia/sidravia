# Sidravia

Sidravia 是一个面向 Windows 的 Dr.COM 网络认证客户端。产品由短生命周期 CLI
`sidravia` 和长期运行的本地 daemon `sidraviad` 组成：

```text
sidravia CLI -> loopback WebSocket IPC -> sidraviad -> Dr.COM -> network
```

## 当前状态

仓库已经实现并自动验证：

- CLI/daemon 状态通信骨架；
- typed 一次性 Session 启动、停止和查询的应用边界；
- Session、Supervisor、网络快照分发与单活动 Session 规则；
- Dr.COM 5.2.0(D) 报文、Factory 和阻塞式认证 Run；
- Windows JSON 文件 ACL 与原子持久化基础。

真实 Windows Environment Detector、D520/Profile 的生产注册、认证命令行入口和
`cmd/sidraviad` 完整装配尚未完成。自动测试也不等同于真实校园网络验证。

长期进度见 [产品路线图](docs/roadmap.md)，模块关系见
[当前架构](docs/architecture.md)。

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

## License

Sidravia 使用 [GNU Affero General Public License v3.0](LICENSE)。
