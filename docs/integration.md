# 集成使用说明

本文面向外部使用者，说明如何把 MCP LSP Bridge 接入 MCP Client，并在项目中使用 LSP 语义工具。

## 1. 安装

推荐在一个固定目录执行在线安装脚本。安装器会下载 GitHub 最新发布版本，并把二进制安装到当前目录的 `bin/` 下。

```bash
mkdir -p ./lsp-bridge-install
cd ./lsp-bridge-install
curl -fsSL https://raw.githubusercontent.com/muidea/lsp-bridge/master/scripts/install.sh | bash
```

安装完成后确认命令可用：

```bash
./bin/lsp-bridge
```

如果当前 shell 还没有加载安装脚本写入的环境变量，可以临时执行：

```bash
export LSP_BRIDGE_HOME="$(pwd)"
export PATH="$LSP_BRIDGE_HOME/bin:$PATH"
```

## 2. MCP Client 配置

MCP LSP Bridge 使用 stdio transport。多数 MCP Client 都需要配置一个 server command。

通用配置示例：

```json
{
  "mcpServers": {
    "lsp-bridge": {
      "command": "lsp-bridge",
      "args": [],
      "env": {
        "LSP_BRIDGE_CONFIG": "./mcp-config.json"
      }
    }
  }
}
```

如果 `lsp-bridge` 已经在 `PATH` 中：

```json
{
  "mcpServers": {
    "lsp-bridge": {
      "command": "lsp-bridge"
    }
  }
}
```

## 3. 语言服务器依赖

`lsp-bridge` 本身只负责桥接 MCP 与 LSP。每种语言仍需要对应的 LSP server。

默认命令：

| 语言 | 默认 LSP 命令 | 安装建议 |
| --- | --- | --- |
| Go | `gopls serve` | `go install golang.org/x/tools/gopls@latest` |
| Rust | `rust-analyzer` | 使用 rustup/component 或系统包管理器安装 |
| Python | `pyright-langserver --stdio` | `npm install -g pyright` |
| TypeScript/JavaScript | `typescript-language-server --stdio` | `npm install -g typescript typescript-language-server` |
| Shell | `bash-language-server start` | `npm install -g bash-language-server` |

在线安装脚本默认会尝试安装 `pyright-langserver` 和 `gopls`。Rust、TypeScript 和 Shell 的 LSP 依赖建议按项目需要安装。

## 4. 自定义 LSP 配置

可以通过 `mcp-config.json` 覆盖默认命令：

```json
{
  "languages": {
    "go": {
      "command": ["gopls", "serve"]
    },
    "python": {
      "command": ["pyright-langserver", "--stdio"]
    },
    "typescript": {
      "command": ["typescript-language-server", "--stdio"],
      "env": {
        "NODE_OPTIONS": "--max-old-space-size=4096"
      }
    }
  }
}
```

启动 server 时指定配置：

```bash
LSP_BRIDGE_CONFIG=./mcp-config.json lsp-bridge
```

## 5. 推荐调用流程

进入项目后，先初始化项目语言：

```json
{
  "root_path": "<project-root>",
  "lang_id": "go"
}
```

工具名：`lsp_initialize`

多语言仓库需要分别初始化：

```json
{"root_path": "<project-root>", "lang_id": "go"}
{"root_path": "<project-root>", "lang_id": "python"}
{"root_path": "<project-root>", "lang_id": "typescript"}
```

查询定义：

```json
{
  "path": "<project-root>/main.go",
  "line": 12,
  "col": 8
}
```

工具名：`lsp_definition`

查询 hover：

```json
{
  "path": "<project-root>/main.go",
  "line": 12,
  "col": 8
}
```

工具名：`lsp_hover`

同步未保存内容：

```json
{
  "path": "<project-root>/main.go",
  "content": "package main\n\nfunc main() {}\n"
}
```

工具名：`lsp_sync`

查询诊断：

```json
{
  "path": "<project-root>/main.go"
}
```

工具名：`lsp_diagnostics`

查询引用：

```json
{
  "path": "<project-root>/main.go",
  "line": 12,
  "col": 8
}
```

工具名：`lsp_references`

## 6. 输出格式

`lsp_definition` 返回紧凑 JSON：

```json
{
  "items": [
    {
      "path": "<project-root>/pkg/service.go",
      "line": 10,
      "col": 5
    }
  ]
}
```

`lsp_references` 最多返回 10 条：

```json
{
  "items": [],
  "total": 18,
  "truncated": true,
  "limit": 10
}
```

`lsp_diagnostics` 返回：

```json
{
  "items": [
    {
      "line": 3,
      "col": 12,
      "severity": "error",
      "source": "compiler",
      "message": "expected ';'"
    }
  ]
}
```

## 7. 常见问题

### 找不到 LSP server

确认对应命令在 `PATH` 中：

```bash
command -v gopls
command -v pyright-langserver
command -v typescript-language-server
command -v rust-analyzer
command -v bash-language-server
```

如果命令不在 `PATH`，可以把 LSP server 所在目录加入 `PATH`，或在 `mcp-config.json` 中写可移植的包装脚本命令。

### 初始化成功但查询为空

常见原因：

- LSP 仍在索引项目，稍后重试。
- 查询行列号不在符号上，确认行列号是 0-based。
- 项目依赖未安装，先执行项目自己的依赖安装命令。

### 修改代码后结果还是旧的

如果修改内容尚未保存到磁盘，必须调用 `lsp_sync` 并传入完整文件内容。后续查询才会基于 LSP 内存 buffer。

### 多语言项目切换错误

每种语言都先调用一次 `lsp_initialize`。查询时可以显式传 `lang_id`：

```json
{
  "path": "<project-root>/app.py",
  "line": 1,
  "col": 4,
  "lang_id": "python"
}
```

## 8. 与 LLM 配合

面向 LLM 的工具使用建议见 [llm-system-prompt.md](llm-system-prompt.md)。
