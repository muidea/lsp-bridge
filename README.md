# MCP LSP Bridge

[![CI](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml)
[![Release](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml)

一个 Go MCP Server，用于把 MCP tool 调用转换为本地 LSP 请求，并在 MCP 模式下复用后端 LSP server，提供状态检查、依赖检测和安全修复建议。

## 安装

默认安装到当前用户本地目录 `$HOME/.local`，二进制位于 `$HOME/.local/bin/lsp-bridge`：

```bash
curl -fsSL https://raw.githubusercontent.com/muidea/lsp-bridge/master/scripts/install.sh | bash
```

指定安装根目录：

```bash
curl -fsSL https://raw.githubusercontent.com/muidea/lsp-bridge/master/scripts/install.sh | LSP_BRIDGE_INSTALL_DIR="$HOME/lsp-bridge-install" bash
```

## MCP Client 配置

```json
{
  "mcpServers": {
    "lsp-bridge": {
      "command": "lsp-bridge",
      "env": {
        "LSP_BRIDGE_CONFIG": "./mcp-config.json"
      }
    }
  }
}
```

如果 `lsp-bridge` 不在 `PATH` 中，建议把 `command` 改为绝对路径，例如 `$HOME/.local/bin/lsp-bridge`。

项目级 LSP 命令、运行时和性能参数建议保存在 `mcp-config.json`，并通过 `LSP_BRIDGE_CONFIG` 显式传入。完整配置见 [docs/integration.md](docs/integration.md) 和 [docs/lsp-runtime-performance.md](docs/lsp-runtime-performance.md)。

```json
{
  "runtime": {
    "idle_ttl_sec": 1800,
    "max_instances": 8
  },
  "performance": {
    "default_timeout_ms": 5000,
    "max_references": 50
  }
}
```

## 语言服务器依赖

`lsp-bridge` 只负责桥接 MCP 与 LSP，每种语言仍需要对应的 LSP server。在线安装脚本默认会尝试安装 `pyright-langserver` 和 `gopls`；Rust、TypeScript/JavaScript、Shell 等语言依赖按项目需要安装。依赖列表和自定义命令见 [docs/integration.md#3-语言服务器依赖](docs/integration.md#3-语言服务器依赖)。

## 常用工具

- `lsp_initialize`
- `lsp_sync`
- `lsp_definition`
- `lsp_hover`
- `lsp_diagnostics`
- `lsp_references`
- `lsp_status`
- `lsp_shutdown`
- `lsp_repair`

兼容旧工具名：`initialize_lsp`、`get_definition`、`get_hover`。

## 状态与修复

- `lsp_status` 返回当前实例状态，并检查后端 LSP server 是否存在、是否正在运行。
- `lsp_shutdown` 关闭指定实例或全部实例，用于释放长期运行的 LSP server 资源。
- `lsp_repair` 默认只返回修复建议；`apply=true` 只执行安全的实例级修复，例如重启已退出实例。安装缺失依赖、修改 `PATH` 或改写配置不会自动执行。

## 文档

- 外部接入和使用：[docs/integration.md](docs/integration.md)
- LLM 工具调用建议：[docs/llm-system-prompt.md](docs/llm-system-prompt.md)
- LSP 运行时性能与生命周期：[docs/lsp-runtime-performance.md](docs/lsp-runtime-performance.md)
- 本地开发和验证：[docs/development.md](docs/development.md)
- 发布流程：[docs/release.md](docs/release.md)
- DoD 状态：[docs/dod-status.md](docs/dod-status.md)
