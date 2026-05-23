# 本地开发和验证

本文说明如何在源码目录中运行、构建和验证 `lsp-bridge`。

## 本地运行

```bash
go run ./cmd/lsp-bridge
```

如果当前工作区存在不可用的 `.git` 元数据，可关闭 Go 的 VCS stamping：

```bash
go run -buildvcs=false ./cmd/lsp-bridge
```

## 基础验证

```bash
go test ./...
go build -buildvcs=false ./...
```

## 真实 LSP 集成测试

本机安装 `gopls` 后可以运行真实 LSP 集成测试：

```bash
LSP_BRIDGE_INTEGRATION=1 go test ./internal/lsp -run TestClientWithGopls -count=1
```

## 安装脚本语法检查

```bash
bash -n scripts/install.sh
```
