# MCP LSP-Bridge MVP 开发文档

## 1. 项目目标 (Project Goals)

### 1.1 语义可信查询 (Trusted Semantic Queries)
*   **目标**：让 LLM 通过真实 LSP 结果完成定位、理解、诊断和重构分析，而不是依赖猜测。
*   **指标**：`lsp_definition`、`lsp_hover`、`lsp_diagnostics`、`lsp_references` 均返回结构化结果，至少包含路径、位置、文本/诊断、完整性等核心语义字段，并可被自动化测试覆盖。

### 1.2 多语言统一接入 (Unified Multi-language Support)
*   **目标**：单一 MCP Server 统一承载多语言工作区，并按 `(root_path, lang_id)` 复用后端 LSP 实例。
*   **指标**：支持 Go (`gopls`)、Rust (`rust-analyzer`)、Python (`pyright-langserver`)、TypeScript (`typescript-language-server`)、Shell (`bash-language-server`) 五种核心语言；同一工作区重复初始化必须幂等。

### 1.3 未保存编辑一致性 (Unsaved Edit Consistency)
*   **目标**：LLM 在未落盘的编辑场景下，仍可继续获得可信的语义查询结果。
*   **指标**：`lsp_sync` 可将内存内容同步给后端 LSP；同步后的 `hover`、`definition`、`diagnostics` 必须基于最新内容；内容未变化时应跳过无效 `didChange`。

### 1.4 有界输出与 Token 控制 (Bounded Output)
*   **目标**：让语义结果对 LLM 足够有用，同时避免单次响应挤爆上下文窗口。
*   **指标**：`hover`、`diagnostics`、`references` 支持可配置上限，并返回 `complete`、`truncated`、`limit`、`reason` 等元信息；默认输出仅保留决策必需字段。

### 1.5 可运维的运行时 (Operable Runtime)
*   **目标**：后端 LSP 实例需要长期复用、可观测、可恢复，便于 MCP Client 长时间运行。
*   **指标**：提供 `lsp_status`、`lsp_shutdown`、`lsp_repair`；支持空闲 TTL 回收、实例数量上限、依赖探测，以及子进程退出后的自动重启。

---

## 2. 完成定义 (Definition of Done - DoD)

只有满足以下所有清单要求，本项目才可宣布研发完成并交付：

### 2.1 功能验收 (Functional)
- [ ] **工具面完整**：对外提供并文档化 `lsp_initialize`、`lsp_sync`、`lsp_definition`、`lsp_hover`、`lsp_diagnostics`、`lsp_references`、`lsp_status`、`lsp_shutdown`、`lsp_repair` 九个工具。
- [ ] **多语言验证**：在测试套件或 CI 严格集成环境中，Go、Rust、Python、TypeScript、Shell 五种语言的 definition 查询全部通过，能够返回目标符号的源码路径与位置。
- [ ] **同步可靠性**：手动修改代码且不保存到磁盘后，通过 `lsp_sync` 同步，再发起 `lsp_hover` / `lsp_definition` / `lsp_diagnostics` 时能够反映更新后的内容。
- [ ] **错误反馈**：故意制造语法错误后，`lsp_diagnostics` 能准确返回错误描述、行号、列号和错误级别。
- [ ] **结果有界**：`lsp_references`、`lsp_hover`、`lsp_diagnostics` 的返回结果支持限制上限；超限时必须返回 `truncated` 或等价标识，而不是静默丢失上下文。

### 2.2 可运维验收 (Operability)
- [ ] **实例状态可观测**：`lsp_status` 能返回实例状态、活跃/空闲信息、重启次数，以及后端 LSP 依赖是否存在、是否正在运行。
- [ ] **实例可关闭**：`lsp_shutdown` 能关闭指定实例，也能按需关闭全部实例，不影响后续重新初始化。
- [ ] **修复建议可执行**：`lsp_repair` 默认仅返回修复建议；`apply=true` 仅允许执行安全的实例级恢复动作，例如重启已退出实例。

### 2.3 性能验收 (Performance)
- [ ] **响应时延**：在 LSP 预热完成后，基础查询（Definition/Hover）的 MCP 内部处理耗时（不含 LSP 本身计算）应小于 **50ms**。
- [ ] **内存控制**：同时开启 3 门语言支持时，MCP Server 程序自身（不含子进程）常驻内存占用应低于 **64MB**。
- [ ] **输出受控**：默认超时、最大引用数、最大诊断数、最大 Hover 字符数等性能参数可通过 `mcp-config.json` 配置，并在运行时生效。

### 2.4 稳定性验收 (Stability)
- [ ] **自动重启**：手动 `kill` 掉某个 LSP 子进程，Server 必须能在下次请求时自动重启该进程并完成初始化。
- [ ] **生命周期收敛**：实例空闲超过 TTL 后能被自动回收；实例数超过上限时能按 LRU 回收空闲实例。
- [ ] **路径兼容**：Linux 路径与 URI 转换验证通过；若声明支持 Windows，则需补齐 Windows 路径转换测试或 CI 验证，确保路径识别无歧义。
- [ ] **并发安全**：使用 10 个并发 Goroutine 模拟 LLM 同时发起的不同查询，Request ID 匹配无误，无数据竞争（通过 `go test -race` 检查）。

### 2.5 工程与交付物 (Engineering & Deliverables)
- [ ] **基础构建通过**：`go build -buildvcs=false ./...`、`go test ./...`、`go test -race ./...` 可稳定通过。
- [ ] **真实 LSP 集成验证**：至少具备 Go 真实 LSP 集成测试；多语言真实集成测试可在本地按已安装依赖尽力执行，并在 CI 严格环境中作为发布闸门。
- [ ] **配置样例齐全**：提供 `mcp-config.json` 示例，覆盖运行时参数、性能参数和多语言 LSP 命令配置。
- [ ] **使用文档齐全**：提供集成说明、开发说明、运行时性能说明、DoD 状态说明，以及面向 LLM 的 System Prompt 建议。
- [ ] **完整源码可交付**：核心 Go 源码、测试代码与文档一并入库，达到可复现构建、可复现验证的交付标准。

---
