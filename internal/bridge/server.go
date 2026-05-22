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

const maxReferences = 10

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
		mcp.WithDescription("返回指定符号的引用位置，最多返回 10 条并标记截断"),
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
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}

		locations, err := instance.Client.Definition(ctx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		return jsonResult(map[string]any{
			"items": formatLocations(locations, 0).Items,
		})
	}
}

func hoverHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}

		hover, err := instance.Client.Hover(ctx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		if hover == nil {
			return mcp.NewToolResultText(""), nil
		}

		return mcp.NewToolResultText(formatHover(hover)), nil
	}
}

func diagnosticsHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		if err := instance.Client.EnsureDidOpen(filePath); err != nil {
			return toolError(err), nil
		}

		items := waitDiagnostics(ctx, instance.Client, filePath)
		return jsonResult(map[string]any{
			"items": formatDiagnostics(items),
		})
	}
}

func referencesHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, line, col, langID, rootPath, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		instance, err := manager.ClientForPath(ctx, filePath, langID, rootPath)
		if err != nil {
			return toolError(err), nil
		}

		locations, err := instance.Client.References(ctx, filePath, line, col)
		if err != nil {
			return toolError(err), nil
		}
		result := formatLocations(locations, maxReferences)
		return jsonResult(map[string]any{
			"items":     result.Items,
			"total":     len(locations),
			"truncated": result.Truncated,
			"limit":     maxReferences,
		})
	}
}

func syncHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		if err := instance.Client.SyncContent(filePath, content); err != nil {
			return toolError(err), nil
		}

		return jsonResult(map[string]any{
			"path":    filePath,
			"lang_id": instance.LangID,
			"status":  "synced",
		})
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
