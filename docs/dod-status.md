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

## 部分实现但尚未完整验证

| DoD 项 | 当前状态 | 后续动作 |
| --- | --- | --- |
| 五语言真实 definition 验证 | Go 已验证；Rust、Python、TypeScript、Shell 尚未在 CI 中端到端验证 | 增加可选多语言集成测试环境和 workflow |
| 响应时延 `<50ms` | 主路径已实现，未做稳定基准统计 | 增加 benchmark 或端到端耗时采样 |
| MCP Server 自身内存 `<64MB` | 未做自动化内存采样 | 增加进程内存采样测试或发布前手动验证脚本 |
| Windows 路径兼容 | URI 转换使用标准库实现，未在 Windows runner 验证 | 增加 Windows CI job |

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
