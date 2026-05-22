# MCP LSP-Bridge MVP 开发文档

## 1. 项目目标 (Project Goals)

### 1.1 语义级代码感知 (Semantic Awareness)
*   **目标**：消除 LLM 对代码理解的“幻觉”。
*   **指标**：LLM 必须能够通过工具获取 100% 准确的符号定义位置、变量类型签名和全项目引用关系。

### 1.2 工业级多语言覆盖 (Multi-language Support)
*   **目标**：单一 MCP Server 实例无缝切换支持 5 种核心语言。
*   **指标**：支持 Go (`gopls`), Rust (`rust-analyzer`), Python (`pyright`), TypeScript (`tsserver`), Shell (`bash-lsp`)，且各语言切换延迟小于 1s。

### 1.3 强实时一致性 (Real-time Consistency)
*   **目标**：确保 LSP 状态与 LLM 编辑内容高度同步。
*   **指标**：在 LLM 执行 `write_file` 操作后，任何紧随其后的 LSP 查询请求必须基于更新后的代码内容，状态滞后率为 0。

### 1.4 高效 Token 利用 (Token Efficiency)
*   **目标**：防止 LSP 冗长响应撑爆上下文。
*   **指标**：对 LSP 返回的复杂 JSON 进行极致压缩，将原始数据量缩减 70% 以上，仅保留核心语义信息。

---

## 2. 核心工具定义 (MCP Tools)

| 工具名称 | 核心参数 | 目标说明 |
| :--- | :--- | :--- |
| `lsp_initialize` | `root_path`, `lang_id` | 初始化工作区，探测环境，启动对应语言的子进程。 |
| `lsp_definition` | `path`, `line`, `col` | 精确跳转。支持跨文件跳转，返回文件路径与行列号。 |
| `lsp_hover` | `path`, `line`, `col` | 获取类型签名、接口说明及文档注释。 |
| `lsp_diagnostics`| `path` | 获取当前文件的语法/逻辑错误，支持 LLM 自我修复 Bug。 |
| `lsp_references` | `path`, `line`, `col` | 寻找引用点，帮助 LLM 进行重构分析。 |
| `lsp_sync` | `path`, `content` | **强制同步**。向 LSP 发送 `didChange` 通知，更新服务器内存 Buffer。 |

---

## 3. 系统架构与技术实现

### 3.1 核心架构：双向协议异步桥接器
*   **Transport 层**：基于 Go `os/exec` 管理子进程 Stdio。
*   **JSON-RPC 层**：实现 `Header + Content` 解析协议。使用 `sync.Map` 映射 Request ID 与 Response Channel。
*   **VFS 层**：维护一个 `Map[FilePath]Hash`。每次执行 LSP 查询前，对比磁盘/内存 Hash，若不一致则自动调用 `lsp_sync`。

### 3.2 路径与环境自适应
*   **URI 转换**：自动处理 Windows 与 Unix 的 `file://` URI 路径差异。
*   **环境探测**：支持自动读取项目中的 `venv` (Python), `node_modules` (TS), `go.mod` (Go) 以配置 LSP 运行参数。

---

## 4. 关键技术难点解决方案

1.  **慢速启动 (Rust-analyzer)**：采用“预加载+异步阻塞”策略。在项目启动时立即拉起进程，若查询时索引未完成，返回特定错误码引导 LLM 稍后重试。
2.  **结果截断**：对于 `references` 返回过多的情况，仅提取前 10 个核心引用并附加“结果已截断”标识，保护 Context Window。
3.  **僵尸进程预防**：利用 Go 的 `context` 机制和 `defer cmd.Process.Kill()`，确保 MCP Server 退出时所有 LSP 实例被强制清理。

---

## 5. 完成定义 (Definition of Done - DoD)

只有满足以下所有清单要求，本项目才可宣布研发完成并交付：

### 5.1 功能验收 (Functional)
- [ ] **多语言验证**：在测试套件中，Go、Rust、Python、TS、Shell 五种语言的 `get_definition` 全部通过，能准确返回对应库函数的源码路径。
- [ ] **同步可靠性**：手动修改代码（不保存磁盘）后，通过 `lsp_sync` 接口同步，随后发起的 `lsp_hover` 能准确反映出修改后的新类型。
- [ ] **错误反馈**：故意制造语法错误，`lsp_diagnostics` 能够准确捕获错误描述、行号和错误级别。

### 5.2 性能验收 (Performance)
- [ ] **响应时延**：在 LSP 预热完成后，基础查询（Definition/Hover）的 MCP 内部处理耗时（不含 LSP 本身计算）应小于 **50ms**。
- [ ] **内存控制**：同时开启 3 门语言支持时，MCP Server 程序自身（不含子进程）常驻内存占用应低于 **64MB**。

### 5.3 稳定性验收 (Stability)
- [ ] **自动重启**：手动 `kill` 掉某个 LSP 子进程，Server 必须能在下次请求时自动重启该进程并完成初始化。
- [ ] **路径兼容**：在 Windows、Linux 两个平台上测试通过，路径识别无歧义。
- [ ] **并发安全**：使用 10 个并发 Goroutine 模拟 LLM 同时发起的不同查询，Request ID 匹配无误，无数据竞争（通过 `go test -race` 检查）。

### 5.4 交付物清单 (Deliverables)
- [ ] 完整的 Go 源代码。
- [ ] `mcp-config.json` 示例文件（包含各语言 LSP 路径配置）。
- [ ] 针对 LLM 的 System Prompt 建议（指导 AI 如何高效组合使用这些 LSP Tool）。

---
