# MCP LSP Bridge

[![CI](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml)
[![Release](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml)

一个 Go MCP Server，用于把 MCP tool 调用转换为本地 LSP 请求。

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

## 常用工具

- `lsp_initialize`
- `lsp_sync`
- `lsp_definition`
- `lsp_hover`
- `lsp_diagnostics`
- `lsp_references`

兼容旧工具名：`initialize_lsp`、`get_definition`、`get_hover`。

## 文档

- 外部接入和使用：[docs/integration.md](docs/integration.md)
- LLM 工具调用建议：[docs/llm-system-prompt.md](docs/llm-system-prompt.md)
- 本地开发和验证：[docs/development.md](docs/development.md)
- 发布流程：[docs/release.md](docs/release.md)
- DoD 状态：[docs/dod-status.md](docs/dod-status.md)
