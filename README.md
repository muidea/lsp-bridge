# MCP LSP Bridge

一个最小可运行的 Go MCP Server，用于把 MCP tool 调用转换为本地 LSP 请求。

当前提供三个工具：

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

## 验证

```bash
go test ./...
go build -buildvcs=false ./...
```

本机安装 `gopls` 后可以运行真实 LSP 集成测试：

```bash
LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientWithGopls -count=1
```

## 默认支持

- Python: `pyright-langserver --stdio`
- Go: `gopls serve`

## 工具参数

### `initialize_lsp`

```json
{
  "root_dir": "/path/to/project",
  "server": "pyright"
}
```

### `get_definition`

```json
{
  "file_path": "/path/to/file.py",
  "line": 0,
  "character": 0
}
```

### `get_hover`

```json
{
  "file_path": "/path/to/file.py",
  "line": 0,
  "character": 0
}
```
