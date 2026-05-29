package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"lsp-bridge/internal/lsp"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func Run(logger *log.Logger) error {
	manager := NewManager(logger)
	defer manager.Close()

	s := server.NewMCPServer(
		"MCP LSP Bridge",
		"0.2.0",
	)

	s.AddTool(initializeTool("lsp_initialize"), initializeHandler(manager))
	s.AddTool(definitionTool("lsp_definition"), definitionHandler(manager))
	s.AddTool(hoverTool("lsp_hover"), hoverHandler(manager))
	s.AddTool(diagnosticsTool(), diagnosticsHandler(manager))
	s.AddTool(referencesTool(), referencesHandler(manager))
	s.AddTool(syncTool(), syncHandler(manager))
	s.AddTool(statusTool(), statusHandler(manager))
	s.AddTool(shutdownTool(), shutdownHandler(manager))
	s.AddTool(repairTool(), repairHandler(manager))

	s.AddTool(initializeTool("initialize_lsp"), initializeHandler(manager))
	s.AddTool(definitionTool("get_definition"), definitionHandler(manager))
	s.AddTool(hoverTool("get_hover"), hoverHandler(manager))

	return server.ServeStdio(s)
}

func initializeTool(name string) mcp.Tool {
	return mcp.NewTool(
		name,
		mcp.WithDescription("初始化工作区，探测环境并启动指定语言的 LSP 子进程"),
		mcp.WithString("root_path", mcp.Description("项目根目录绝对路径或相对路径")),
		mcp.WithString("root_dir", mcp.Description("兼容旧接口的项目根目录参数")),
		mcp.WithString("lang_id", mcp.Description("语言 ID：go、rust、python、typescript、shell")),
		mcp.WithString("server", mcp.Description("兼容旧接口的语言或 LSP 服务名")),
	)
}

func definitionTool(name string) mcp.Tool {
	return mcp.NewTool(
		name,
		mcp.WithDescription("返回指定文件位置的定义源码位置"),
		mcp.WithString("path", mcp.Description("源码文件路径")),
		mcp.WithString("file_path", mcp.Description("兼容旧接口的源码文件路径")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("0-based 行号")),
		mcp.WithNumber("col", mcp.Description("0-based 列号")),
		mcp.WithNumber("character", mcp.Description("兼容旧接口的 0-based 列号")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，默认按文件扩展名推断")),
		mcp.WithString("root_path", mcp.Description("可选项目根目录；缺省时从已初始化实例中按路径匹配")),
	)
}

func hoverTool(name string) mcp.Tool {
	return mcp.NewTool(
		name,
		mcp.WithDescription("返回指定文件位置的类型签名、接口说明及文档注释"),
		mcp.WithString("path", mcp.Description("源码文件路径")),
		mcp.WithString("file_path", mcp.Description("兼容旧接口的源码文件路径")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("0-based 行号")),
		mcp.WithNumber("col", mcp.Description("0-based 列号")),
		mcp.WithNumber("character", mcp.Description("兼容旧接口的 0-based 列号")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，默认按文件扩展名推断")),
		mcp.WithString("root_path", mcp.Description("可选项目根目录；缺省时从已初始化实例中按路径匹配")),
	)
}

func diagnosticsTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_diagnostics",
		mcp.WithDescription("返回当前文件的 LSP 诊断信息"),
		mcp.WithString("path", mcp.Required(), mcp.Description("源码文件路径")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，默认按文件扩展名推断")),
		mcp.WithString("root_path", mcp.Description("可选项目根目录；缺省时从已初始化实例中按路径匹配")),
	)
}

func referencesTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_references",
		mcp.WithDescription("返回指定符号的引用位置，并按配置限制数量和标记截断"),
		mcp.WithString("path", mcp.Required(), mcp.Description("源码文件路径")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("0-based 行号")),
		mcp.WithNumber("col", mcp.Required(), mcp.Description("0-based 列号")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，默认按文件扩展名推断")),
		mcp.WithString("root_path", mcp.Description("可选项目根目录；缺省时从已初始化实例中按路径匹配")),
	)
}

func syncTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_sync",
		mcp.WithDescription("强制同步 LLM 内存中的文件内容到 LSP buffer"),
		mcp.WithString("path", mcp.Required(), mcp.Description("源码文件路径")),
		mcp.WithString("content", mcp.Required(), mcp.Description("完整文件内容")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，默认按文件扩展名推断")),
		mcp.WithString("root_path", mcp.Description("可选项目根目录；缺省时从已初始化实例中按路径匹配")),
	)
}

func statusTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_status",
		mcp.WithDescription("返回当前 LSP 实例和后端 LSP server 依赖状态；可传 root_path/lang_id 检查未启动实例的依赖"),
		mcp.WithString("root_path", mcp.Description("可选项目根目录，用于检查指定依赖")),
		mcp.WithString("lang_id", mcp.Description("可选语言 ID，用于检查指定依赖")),
	)
}

func shutdownTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_shutdown",
		mcp.WithDescription("关闭指定 LSP 实例，或使用 all=true 关闭全部实例"),
		mcp.WithBoolean("all", mcp.Description("关闭全部实例")),
		mcp.WithString("root_path", mcp.Description("要关闭的项目根目录")),
		mcp.WithString("lang_id", mcp.Description("要关闭的语言 ID")),
	)
}

func repairTool() mcp.Tool {
	return mcp.NewTool(
		"lsp_repair",
		mcp.WithDescription("生成 LSP 依赖和实例修复建议；apply=true 时仅执行安全的实例重启修复"),
		mcp.WithBoolean("apply", mcp.Description("是否执行自动修复；默认 false，只返回修复建议")),
		mcp.WithBoolean("all", mcp.Description("检查全部已知实例")),
		mcp.WithString("root_path", mcp.Description("要检查或修复的项目根目录")),
		mcp.WithString("lang_id", mcp.Description("要检查或修复的语言 ID")),
	)
}

func initializeHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rootDir, err := getFirstStringArg(request, "root_path", "root_dir")
		if err != nil {
			return toolError(err), nil
		}
		langID := getOptionalStringArg(request, "lang_id", "")
		if langID == "" {
			langID = getOptionalStringArg(request, "server", "python")
		}

		instance, err := manager.Initialize(ctx, rootDir, langID)
		if err != nil {
			return toolError(err), nil
		}

		return jsonResult(map[string]any{
			"root_path": instance.RootDir,
			"lang_id":   instance.LangID,
			"server":    strings.Join(instance.Command, " "),
			"pid":       instance.Client.PID(),
			"status":    "initialized",
		})
	}
}

func definitionHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}
		done := manager.Begin(instance)
		defer done()

		queryCtx, cancel := manager.toolContext(ctx)
		defer cancel()
		locations, err := instance.Client.Definition(queryCtx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		return jsonResult(map[string]any{
			"items":      formatLocations(locations, 0).Items,
			"complete":   true,
			"truncated":  false,
			"elapsed_ms": elapsedMs(start),
		})
	}
}

func hoverHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}
		done := manager.Begin(instance)
		defer done()

		queryCtx, cancel := manager.toolContext(ctx)
		defer cancel()
		hover, err := instance.Client.Hover(queryCtx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		if hover == nil {
			return mcp.NewToolResultText(""), nil
		}

		text, truncated := truncateString(formatHover(hover), manager.performanceConfig().MaxHoverChars)
		return jsonResult(map[string]any{
			"text":       text,
			"complete":   !truncated,
			"truncated":  truncated,
			"limit":      manager.performanceConfig().MaxHoverChars,
			"elapsed_ms": elapsedMs(start),
		})
	}
}

func diagnosticsHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		filePath, err := getFirstStringArg(request, "path", "file_path")
		if err != nil {
			return toolError(err), nil
		}
		langID := getOptionalStringArg(request, "lang_id", "")
		rootPath := getOptionalStringArg(request, "root_path", "")

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}
		done := manager.Begin(instance)
		defer done()
		if err := instance.Client.EnsureDidOpen(filePath); err != nil {
			return toolError(err), nil
		}

		queryCtx, cancel := manager.toolContext(ctx)
		defer cancel()
		items := waitDiagnostics(queryCtx, instance.Client, filePath)
		limit := manager.performanceConfig().MaxDiagnostics
		truncated := false
		if limit > 0 && len(items) > limit {
			items = items[:limit]
			truncated = true
		}
		return jsonResult(map[string]any{
			"items":      formatDiagnostics(items),
			"complete":   !truncated,
			"truncated":  truncated,
			"limit":      limit,
			"elapsed_ms": elapsedMs(start),
		})
	}
}

func referencesHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}
		done := manager.Begin(instance)
		defer done()

		queryCtx, cancel := manager.toolContext(ctx)
		defer cancel()
		locations, err := instance.Client.References(queryCtx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		limit := manager.performanceConfig().MaxReferences
		result := formatLocations(locations, limit)
		return jsonResult(map[string]any{
			"items":      result.Items,
			"total":      len(locations),
			"complete":   !result.Truncated,
			"truncated":  result.Truncated,
			"limit":      limit,
			"elapsed_ms": elapsedMs(start),
		})
	}
}

func syncHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		filePath, err := getFirstStringArg(request, "path", "file_path")
		if err != nil {
			return toolError(err), nil
		}
		content, err := getStringArg(request, "content")
		if err != nil {
			return toolError(err), nil
		}
		langID := getOptionalStringArg(request, "lang_id", "")
		rootPath := getOptionalStringArg(request, "root_path", "")

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}
		done := manager.Begin(instance)
		defer done()
		if err := instance.Client.SyncContent(filePath, content); err != nil {
			return toolError(err), nil
		}

		return jsonResult(map[string]any{
			"path":       filePath,
			"lang_id":    instance.LangID,
			"status":     "synced",
			"elapsed_ms": elapsedMs(start),
		})
	}
}

func statusHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rootPath := getOptionalStringArg(request, "root_path", "")
		langID := getOptionalStringArg(request, "lang_id", "")
		if strings.TrimSpace(rootPath) != "" || strings.TrimSpace(langID) != "" {
			if strings.TrimSpace(rootPath) == "" || strings.TrimSpace(langID) == "" {
				return toolError(fmt.Errorf("root_path and lang_id must be provided together")), nil
			}
			status, err := manager.DependencyStatus(rootPath, langID)
			if err != nil {
				return toolError(err), nil
			}
			return jsonResult(map[string]any{
				"instances": []InstanceStatus{status},
			})
		}
		return jsonResult(map[string]any{
			"instances": manager.Status(),
		})
	}
}

func shutdownHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		all := getOptionalBoolArg(request, "all", false)
		rootPath := getOptionalStringArg(request, "root_path", "")
		langID := getOptionalStringArg(request, "lang_id", "")
		count, err := manager.Shutdown(rootPath, langID, all)
		if err != nil {
			return toolError(err), nil
		}
		return jsonResult(map[string]any{
			"closed": count,
			"status": "shutdown",
		})
	}
}

func repairHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		apply := getOptionalBoolArg(request, "apply", false)
		all := getOptionalBoolArg(request, "all", false)
		rootPath := getOptionalStringArg(request, "root_path", "")
		langID := getOptionalStringArg(request, "lang_id", "")
		if !all && strings.TrimSpace(rootPath) == "" {
			return toolError(fmt.Errorf("root_path is required unless all is true")), nil
		}
		if !all && strings.TrimSpace(langID) == "" {
			return toolError(fmt.Errorf("lang_id is required unless all is true")), nil
		}
		report := manager.Repair(ctx, rootPath, langID, apply, all)
		return jsonResult(report)
	}
}

func positionArgs(request mcp.CallToolRequest) (string, int, int, string, string, error) {
	filePath, err := getFirstStringArg(request, "path", "file_path")
	if err != nil {
		return "", 0, 0, "", "", err
	}
	line, err := getIntArg(request, "line")
	if err != nil {
		return "", 0, 0, "", "", err
	}
	col, err := getFirstIntArg(request, "col", "character")
	if err != nil {
		return "", 0, 0, "", "", err
	}
	langID := getOptionalStringArg(request, "lang_id", "")
	rootPath := getOptionalStringArg(request, "root_path", "")
	return filePath, line, col, langID, rootPath, nil
}

func getStringArg(request mcp.CallToolRequest, name string) (string, error) {
	raw, ok := request.Params.Arguments[name]
	if !ok {
		return "", fmt.Errorf("missing argument %q", name)
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("argument %q must be a non-empty string", name)
	}
	return value, nil
}

func getFirstStringArg(request mcp.CallToolRequest, names ...string) (string, error) {
	for _, name := range names {
		raw, ok := request.Params.Arguments[name]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if ok && strings.TrimSpace(value) != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("missing argument %q", strings.Join(names, " or "))
}

func getOptionalStringArg(request mcp.CallToolRequest, name, fallback string) string {
	raw, ok := request.Params.Arguments[name]
	if !ok {
		return fallback
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func getOptionalBoolArg(request mcp.CallToolRequest, name string, fallback bool) bool {
	raw, ok := request.Params.Arguments[name]
	if !ok {
		return fallback
	}
	value, ok := raw.(bool)
	if !ok {
		return fallback
	}
	return value
}

func getIntArg(request mcp.CallToolRequest, name string) (int, error) {
	raw, ok := request.Params.Arguments[name]
	if !ok {
		return 0, fmt.Errorf("missing argument %q", name)
	}
	value, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("argument %q must be a number", name)
	}
	return int(value), nil
}

func getFirstIntArg(request mcp.CallToolRequest, names ...string) (int, error) {
	for _, name := range names {
		raw, ok := request.Params.Arguments[name]
		if !ok {
			continue
		}
		value, ok := raw.(float64)
		if ok {
			return int(value), nil
		}
	}
	return 0, fmt.Errorf("missing argument %q", strings.Join(names, " or "))
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(buf)), nil
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: err.Error(),
			},
		},
		IsError: true,
	}
}

func formatHover(hover *lsp.Hover) string {
	switch contents := hover.Contents.(type) {
	case string:
		return strings.TrimSpace(contents)
	case map[string]any:
		if value, ok := contents["value"].(string); ok {
			return strings.TrimSpace(value)
		}
	case []any:
		parts := make([]string, 0, len(contents))
		for _, item := range contents {
			switch v := item.(type) {
			case string:
				parts = append(parts, strings.TrimSpace(v))
			case map[string]any:
				if value, ok := v["value"].(string); ok {
					parts = append(parts, strings.TrimSpace(value))
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	}

	buf, err := json.Marshal(hover.Contents)
	if err != nil {
		return ""
	}
	return string(buf)
}

func truncateString(value string, limit int) (string, bool) {
	if limit <= 0 || len(value) <= limit {
		return value, false
	}
	return value[:limit], true
}

func elapsedMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

type formattedLocations struct {
	Items     []map[string]any
	Truncated bool
}

func formatLocations(locations []lsp.Location, limit int) formattedLocations {
	count := len(locations)
	truncated := false
	if limit > 0 && count > limit {
		count = limit
		truncated = true
	}

	items := make([]map[string]any, 0, count)
	for _, location := range locations[:count] {
		path, err := lsp.URIToPath(location.URI)
		if err != nil {
			path = location.URI
		}
		items = append(items, map[string]any{
			"path": path,
			"line": location.Range.Start.Line,
			"col":  location.Range.Start.Character,
		})
	}
	return formattedLocations{Items: items, Truncated: truncated}
}

func formatDiagnostics(items []lsp.Diagnostic) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"line":     item.Range.Start.Line,
			"col":      item.Range.Start.Character,
			"severity": severityName(item.Severity),
			"source":   item.Source,
			"message":  item.Message,
		})
	}
	return out
}

func waitDiagnostics(ctx context.Context, client *lsp.Client, path string) []lsp.Diagnostic {
	deadline := time.NewTimer(800 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var last []lsp.Diagnostic
	for {
		items, err := client.Diagnostics(path)
		if err == nil {
			last = items
			if len(items) > 0 {
				return items
			}
		}
		select {
		case <-ctx.Done():
			return last
		case <-deadline.C:
			return last
		case <-ticker.C:
		}
	}
}

func severityName(severity int) string {
	switch severity {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "information"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}
