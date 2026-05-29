# LSP 运行时性能与生命周期方案

本文定义 `lsp-bridge` 在 MCP 模式下管理后端 LSP server 的最终方案和收口清单。目标是在大项目中避免频繁启停、控制资源占用，并保证语义查询结果可信。

## 目标

- 长生命周期复用 LSP server，避免每次 MCP tool 调用都重新启动和索引。
- 让后端 LSP server 负责索引，`lsp-bridge` 只做轻量网关和有界查询。
- 对大项目提供超时、限量、截断标记、状态查询和显式关闭。
- 检测后端 LSP server 是否缺失、是否正在运行，并提供修复建议。
- 通过空闲 TTL、实例上限和 LRU 回收控制资源占用。
- 对不完整结果明确返回 `complete`、`truncated`、`reason` 等元信息，避免误报完整性。

## 实例模型

使用 `(root_path, lang_id)` 作为实例 key：

```text
/repo/backend + go        -> gopls
/repo/web + typescript    -> typescript-language-server
/repo/tools + python      -> pyright-langserver
```

每个实例维护：

- `root_path`
- `lang_id`
- LSP command/env
- 子进程 pid
- JSON-RPC client
- 状态：`starting` / `ready` / `exited` / `stopping`
- `last_used_at`
- open files 数量
- restart count
- 当前活跃请求数
- 最近错误
- 后端 LSP server 依赖状态

## 生命周期规则

1. `lsp_initialize` 必须幂等。
   - 已存在健康实例时直接复用。
   - 已退出实例按原配置重启。
   - 不存在实例时懒启动。
2. 同一 `(root_path, lang_id)` 的并发启动应合并，避免重复拉起同一个 LSP server。
3. `initialize` 只等待 LSP 握手成功，不等待全仓索引完成。
4. 查询工具不关闭实例，只刷新 `last_used_at`。
5. 空闲超过 `idle_ttl_sec` 且无活跃请求时自动关闭实例。
6. 实例数超过 `max_instances` 时按 LRU 回收空闲实例。
7. 子进程退出后标记 `exited`，下次请求自动重启。
8. 连续失败使用退避策略，避免 crash loop。

## 查询策略

默认所有 MCP tool 都应是有界查询：

- `lsp_definition` / `lsp_hover`: 默认超时 5 秒。
- `lsp_hover`: 限制最大输出字符数。
- `lsp_references`: 默认只返回前 N 条，并返回 `total`、`truncated`、`limit`。
- `lsp_diagnostics`: 默认只查指定文件，并限制最大条数。
- `lsp_sync`: 内容未变化时跳过 `didChange`。

返回结果应携带必要元信息：

```json
{
  "items": [],
  "complete": false,
  "truncated": true,
  "limit": 50,
  "reason": "result limit reached",
  "elapsed_ms": 123
}
```

## 配置

`mcp-config.json` 支持运行时和性能配置：

```json
{
  "runtime": {
    "idle_ttl_sec": 1800,
    "max_instances": 8,
    "max_restarts": 3,
    "restart_backoff_ms": 1000
  },
  "performance": {
    "default_timeout_ms": 5000,
    "initialize_timeout_ms": 30000,
    "max_references": 50,
    "max_diagnostics": 100,
    "max_hover_chars": 4000
  },
  "languages": {
    "go": {
      "command": ["gopls", "serve"]
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

默认值：

| 配置 | 默认值 |
| --- | --- |
| `runtime.idle_ttl_sec` | `1800` |
| `runtime.max_instances` | `8` |
| `runtime.max_restarts` | `3` |
| `runtime.restart_backoff_ms` | `1000` |
| `performance.default_timeout_ms` | `5000` |
| `performance.initialize_timeout_ms` | `30000` |
| `performance.max_references` | `50` |
| `performance.max_diagnostics` | `100` |
| `performance.max_hover_chars` | `4000` |

## 管理工具

### `lsp_status`

返回当前实例状态和后端 LSP server 状态：

```json
{
  "instances": [
    {
      "root_path": "/repo/backend",
      "lang_id": "go",
      "state": "ready",
      "pid": 12345,
      "last_used_at": "2026-05-29T10:00:00+08:00",
      "idle_sec": 120,
      "open_files": 3,
      "restart_count": 0,
      "server": {
        "command": ["gopls", "serve"],
        "found": true,
        "path": "/home/user/go/bin/gopls",
        "running": true,
        "healthy": true
      }
    }
  ]
}
```

也可以传入 `root_path` 和 `lang_id`，检查尚未启动实例的依赖是否存在：

```json
{
  "root_path": "/repo/backend",
  "lang_id": "go"
}
```

### `lsp_shutdown`

关闭指定实例：

```json
{
  "root_path": "/repo/backend",
  "lang_id": "go"
}
```

关闭全部实例：

```json
{
  "all": true
}
```

### `lsp_repair`

返回修复建议；默认不执行任何修改：

```json
{
  "root_path": "/repo/backend",
  "lang_id": "go"
}
```

返回示例：

```json
{
  "applied": false,
  "actions": [
    {
      "id": "install_lsp_server",
      "description": "install with: go install golang.org/x/tools/gopls@latest",
      "command": ["go", "install", "golang.org/x/tools/gopls@latest"],
      "automatic": false
    }
  ]
}
```

`apply=true` 只执行安全的实例级修复，例如重启已经存在但进程退出的实例。安装缺失依赖、修改 PATH 或改写配置只返回建议，不自动执行。

## 收口清单

- [x] 按 `(root_path, lang_id)` 复用 LSP 实例。
- [x] `lsp_initialize` 已存在健康实例时直接复用。
- [x] 子进程退出后下次请求自动重启。
- [x] `lsp_sync` 内容未变化时跳过 `didChange`。
- [x] `lsp_references` 返回截断标记。
- [x] 支持 `runtime` / `performance` 配置。
- [x] 支持请求级默认超时。
- [x] 支持可配置的 `max_references`、`max_diagnostics`、`max_hover_chars`。
- [x] 支持空闲 TTL 回收。
- [x] 支持最大实例数和 LRU 回收。
- [x] 提供 `lsp_status`。
- [x] 提供 `lsp_shutdown`。
- [x] `lsp_status` 检测后端 LSP server 依赖存在性和进程运行状态。
- [x] 提供 `lsp_repair` 修复建议和安全自动修复入口。
- [x] 增加相关单元测试和集成测试。
