# MCP LSP-Bridge MVP 开发文档

**版本:** 0.1 (MVP)  
**目标:** 实现 MCP 与 LSP 的基础双向通信，提供最核心的代码导航能力。

---

## 1. MVP 核心目标 (Goal)
构建一个基于 Golang 的桥接程序，能够启动本地 LSP 进程（以 Python Pyright 为例），并将 LLM 的“查看定义”和“获取信息”请求转换为标准 LSP 调用。

---

## 2. 最小功能范围 (Scope)

### 2.1 核心工具 (Tools)
MVP 阶段仅提供最基础、价值最高的三个接口：

1.  **`initialize_lsp`**: 
    *   功能：指定项目根目录，启动对应的 LSP 进程。
    *   DoD：成功收到 LSP 的 `initialize` 确认响应。
2.  **`get_definition`**: 
    *   功能：根据文件路径和行列号，获取符号定义的位置（文件、行、列）。
    *   DoD：LLM 发送请求后，能准确返回对应函数/类的源码位置。
3.  **`get_hover`**: 
    *   功能：获取指定位置的类型签名和文档注释。
    *   DoD：返回简洁的 Markdown 字符串，供 LLM 理解变量类型。

### 2.2 支持范围
*   **首选支持语言**: Python (需预装 `pyright`) 或 Go (`gopls`)。
*   **通信协议**: 基于 Stdio 的 JSON-RPC 2.0。
*   **文件同步**: 采用“全量简单同步”模式（即调用前通过 `didOpen` 发送文件全文，不处理复杂的增量更新）。

---

## 3. 关键技术方案

### 3.1 架构简图
`LLM (MCP Client)` <-> `MCP Server (Go)` <-> `LSP Server (Process)`

### 3.2 技术栈
*   **语言**: Golang 1.21+
*   **核心库**: 
    *   `github.com/mark3labs/mcp-go`: 用于构建 MCP Server 框架。
    *   `golang.org/x/tools/lsp/protocol`: 仅引用其结构体定义，确保协议标准。

### 3.3 状态管理
*   MVP 阶段仅维护一个 `LSPInstance` 映射表，记录当前活跃的 LSP 进程 PID 及其对应的 Stdio 管道。

---

## 4. 完成定义 (Definition of Done - DoD)

### 4.1 核心链路闭环
- [ ] **启动能力**: MCP Server 启动后，能通过工具调用成功拉起本地 `pyright-langserver --stdio` 进程。
- [ ] **协议转换**: 成功将 MCP 的 `call_tool` 参数转换为符合 LSP 规范的 JSON-RPC 报文。
- [ ] **响应处理**: 能够正确解析 LSP 返回的带有 `Content-Length` 头的 Stdio 流，并提取 `result` 字段。

### 4.2 交互测试用例
- [ ] **测试 1**: 在一个 Python 项目中，针对 `import os`，调用 `get_definition` 能够返回 `os.py` 的绝对路径。
- [ ] **测试 2**: 针对一个已知函数调用 `get_hover`，返回结果中包含该函数的 `def` 签名。

### 4.3 鲁棒性要求
- [ ] 如果 LSP 进程意外退出，MCP Server 不会崩溃，并能在下次请求时尝试重新启动或报错。
- [ ] 支持基本的路径转换（处理本地文件路径到 `file:///` URI 的转换）。

---

## 5. MVP 开发计划 (2-3天)

*   **Day 1**: 搭建 Go MCP 基础框架，实现 `exec.Command` 管理子进程，处理 Stdio 的异步读取。
*   **Day 2**: 实现 LSP Header 解析逻辑 (`Content-Length`)，封装 `initialize` 和 `get_definition` 请求。
*   **Day 3**: 编写简单的路径映射逻辑，进行 Python/Go 项目的端到端联调。

---

## 6. MVP 之后 (Post-MVP)
*   支持 Rust, TS 等更多语言。
*   实现 `textDocument/didChange` 增量同步以提升大型项目性能。
*   支持诊断信息（Diagnostics）主动推送。

---
