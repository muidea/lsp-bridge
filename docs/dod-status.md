# DoD 验证状态

本文记录当前实现相对最新开发文档 DoD 的验证状态。状态会随后续集成测试补齐而更新。

## 已验证

| DoD 项 | 状态 | 验证方式 |
| --- | --- | --- |
| 完整 Go 源代码 | 已完成 | `go build -buildvcs=false ./...` |
| `mcp-config.json` 示例 | 已完成 | 仓库根目录提供示例配置 |
| LLM System Prompt 建议 | 已完成 | `docs/llm-system-prompt.md` |
| `lsp_sync` 未落盘内容同步 | 已验证 | fake LSP 集成测试覆盖 sync 后 hover 结果变化 |
| `lsp_diagnostics` 错误反馈 | 已验证 | fake LSP 集成测试覆盖诊断发布与读取 |
| `lsp_references` 结果截断 | 已验证 | fake LSP 返回 12 条引用，工具层截断到 10 条 |
| 自动重启 | 已验证 | fake LSP 首次初始化后退出，Manager 下次请求自动重启 |
| 并发安全 | 已验证 | 10 个 goroutine 并发 definition 请求；`go test -race ./...` |
| Go 真实 LSP definition/hover | 已验证 | `LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientWithGopls -count=1` |
| 五语言真实 definition 验证 | CI 严格验证 | CI 安装 `gopls`、`rust-analyzer`、`pyright`、`typescript-language-server`、`bash-language-server` 后运行 `TestClientDefinitionAcrossInstalledLanguageServers` |
| 响应时延 `<50ms` | 已验证 | fake LSP latency envelope 测试，50 次 definition 平均耗时必须小于 50ms |
| MCP Server 自身内存 `<64MB` | 已验证 | 3 个 fake LSP client 同时初始化后采样 Go heap allocation，阈值 64MB |

## 部分实现但尚未完整验证

| DoD 项 | 当前状态 | 后续动作 |
| --- | --- | --- |
| Windows 路径兼容 | URI 转换使用标准库实现，但按当前计划暂不处理 Windows runner 验证 | 后续单独补 Windows CI job |

## 本地验证命令

```bash
go test ./...
go test -race ./...
go build -buildvcs=false ./...
```

真实 Go LSP 集成测试：

```bash
LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientWithGopls -count=1
```

本地按已安装语言服务器尽力执行多语言集成测试：

```bash
LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientDefinitionAcrossInstalledLanguageServers -count=1
```

CI 严格多语言验证会额外设置：

```bash
LSP_BRIDGE_STRICT_INTEGRATION=1
```
