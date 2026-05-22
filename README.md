# MCP LSP Bridge

[![CI](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/ci.yml)
[![Release](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/muidea/lsp-bridge/actions/workflows/release.yml)

一个最小可运行的 Go MCP Server，用于把 MCP tool 调用转换为本地 LSP 请求。

当前提供三个工具：

- `lsp_initialize`
- `lsp_sync`
- `lsp_definition`
- `lsp_hover`
- `lsp_diagnostics`
- `lsp_references`

兼容旧工具名：

- `initialize_lsp`
- `get_definition`
- `get_hover`

## 运行

```bash
go run ./cmd/lsp-bridge
```

如果当前工作区存在不可用的 `.git` 元数据，可关闭 Go 的 VCS stamping：

```bash
go run -buildvcs=false ./cmd/lsp-bridge
```

## 在线安装

安装脚本会自动检测 GitHub 最新发布版本，下载当前系统和架构匹配的二进制包，并安装到执行命令时的当前目录：

```bash
curl -fsSL https://raw.githubusercontent.com/muidea/lsp-bridge/master/scripts/install.sh | bash
```

安装后的目录结构：

```text
./bin/lsp-bridge
./bin/pyright-langserver
./bin/gopls
./.deps/node/
```

安装器会自动处理缺失依赖：

- 缺少 `curl`/`wget`、`tar` 等基础工具时，使用系统包管理器安装
- 缺少 `pyright-langserver` 时，安装 `node`/`npm` 并在当前目录下安装 `pyright`
- 缺少 `gopls` 时，安装 `go` 并把 `gopls` 安装到当前目录的 `bin`
- 自动写入 `LSP_BRIDGE_HOME` 和 `PATH` 到当前用户的 shell 配置文件

可选环境变量：

```bash
LSP_BRIDGE_INSTALL_DIR=/opt/lsp-bridge bash scripts/install.sh
LSP_BRIDGE_VERSION=v0.1.0 bash scripts/install.sh
INSTALL_PYRIGHT=0 INSTALL_GOPLS=0 bash scripts/install.sh
```

## 验证

```bash
go test ./...
go build -buildvcs=false ./...
```

本机安装 `gopls` 后可以运行真实 LSP 集成测试：

```bash
LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientWithGopls -count=1
```

## 发布

生成 release 产物：

```bash
scripts/release.sh v0.1.0
```

产物会输出到 `dist/`：

```text
lsp-bridge_v0.1.0_linux_amd64.tar.gz
lsp-bridge_v0.1.0_linux_arm64.tar.gz
lsp-bridge_v0.1.0_darwin_amd64.tar.gz
lsp-bridge_v0.1.0_darwin_arm64.tar.gz
checksums.txt
```

如果已安装并登录 GitHub CLI，可以直接发布：

```bash
PUBLISH=1 scripts/release.sh v0.1.0
```

GitHub Actions 已配置：

- `CI`: 推送到 `master`/`main` 或发起 PR 时，执行脚本语法检查、`go test ./...`、`go build -buildvcs=false ./...` 和 release 归档构建
- `Release`: 推送 `v*` tag 时，构建 release 归档并发布到 GitHub Releases

发布新版本：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 默认支持

- Python: `pyright-langserver --stdio`
- Go: `gopls serve`
- Rust: `rust-analyzer`
- TypeScript/JavaScript: `typescript-language-server --stdio`
- Shell: `bash-language-server start`

可通过 `mcp-config.json` 覆盖各语言命令：

```json
{
  "languages": {
    "python": {
      "command": ["pyright-langserver", "--stdio"]
    }
  }
}
```

## 工具参数

### `lsp_initialize`

```json
{
  "root_path": "/path/to/project",
  "lang_id": "python"
}
```

### `lsp_sync`

```json
{
  "path": "/path/to/file.py",
  "content": "print('hello')\n"
}
```

### `lsp_definition`

```json
{
  "path": "/path/to/file.py",
  "line": 0,
  "col": 0
}
```

### `lsp_hover`

```json
{
  "path": "/path/to/file.py",
  "line": 0,
  "col": 0
}
```

### `lsp_diagnostics`

```json
{
  "path": "/path/to/file.py"
}
```

### `lsp_references`

```json
{
  "path": "/path/to/file.py",
  "line": 0,
  "col": 0
}
```

行列号均为 0-based。更多 LLM 使用建议见 [docs/llm-system-prompt.md](docs/llm-system-prompt.md)。
