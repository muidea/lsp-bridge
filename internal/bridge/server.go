package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"lsp-bridge/internal/lsp"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func Run(logger *log.Logger) error {
	manager := NewManager(logger)
	defer manager.Close()

	s := server.NewMCPServer(
		"MCP LSP Bridge",
		"0.1.0",
	)

	s.AddTool(initializeTool(), initializeHandler(manager))
	s.AddTool(definitionTool(), definitionHandler(manager))
	s.AddTool(hoverTool(), hoverHandler(manager))

	return server.ServeStdio(s)
}

func initializeTool() mcp.Tool {
	return mcp.NewTool(
		"initialize_lsp",
		mcp.WithDescription("启动并初始化本地 LSP 进程，建立当前项目会话"),
		mcp.WithString("root_dir", mcp.Required(), mcp.Description("项目根目录绝对路径或相对路径")),
		mcp.WithString("server", mcp.Description("LSP 服务名或命令，默认 pyright")),
	)
}

func definitionTool() mcp.Tool {
	return mcp.NewTool(
		"get_definition",
		mcp.WithDescription("返回指定文件位置的定义源码位置"),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("源码文件路径")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("0-based 行号")),
		mcp.WithNumber("character", mcp.Required(), mcp.Description("0-based 列号")),
	)
}

func hoverTool() mcp.Tool {
	return mcp.NewTool(
		"get_hover",
		mcp.WithDescription("返回指定文件位置的悬浮信息"),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("源码文件路径")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("0-based 行号")),
		mcp.WithNumber("character", mcp.Required(), mcp.Description("0-based 列号")),
	)
}

func initializeHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rootDir, err := getStringArg(request, "root_dir")
		if err != nil {
			return toolError(err), nil
		}
		serverName := getOptionalStringArg(request, "server", "pyright")

		instance, err := manager.Initialize(ctx, rootDir, serverName)
		if err != nil {
			return toolError(err), nil
		}

		payload := map[string]any{
			"root_dir": instance.RootDir,
			"server":   strings.Join(instance.Command, " "),
			"pid":      instance.Client.PID(),
			"status":   "initialized",
		}
		return jsonResult(payload)
	}
}

func definitionHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		instance, err := manager.Current()
		if err != nil {
			return toolError(err), nil
		}

		filePath, line, character, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		locations, err := instance.Client.Definition(ctx, filePath, line, character)
		if err != nil {
			return toolError(err), nil
		}
		if len(locations) == 0 {
			return mcp.NewToolResultText("null"), nil
		}

		result := make([]map[string]any, 0, len(locations))
		for _, location := range locations {
			path, err := lsp.URIToPath(location.URI)
			if err != nil {
				path = location.URI
			}
			result = append(result, map[string]any{
				"file_path": path,
				"line":      location.Range.Start.Line,
				"character": location.Range.Start.Character,
				"end_line":  location.Range.End.Line,
				"end_char":  location.Range.End.Character,
			})
		}
		return jsonResult(result)
	}
}

func hoverHandler(manager *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		instance, err := manager.Current()
		if err != nil {
			return toolError(err), nil
		}

		filePath, line, character, err := positionArgs(request)
		if err != nil {
			return toolError(err), nil
		}

		hover, err := instance.Client.Hover(ctx, filePath, line, character)
		if err != nil {
			return toolError(err), nil
		}
		if hover == nil {
			return mcp.NewToolResultText(""), nil
		}

		return mcp.NewToolResultText(formatHover(hover)), nil
	}
}

func positionArgs(request mcp.CallToolRequest) (string, int, int, error) {
	filePath, err := getStringArg(request, "file_path")
	if err != nil {
		return "", 0, 0, err
	}
	line, err := getIntArg(request, "line")
	if err != nil {
		return "", 0, 0, err
	}
	character, err := getIntArg(request, "character")
	if err != nil {
		return "", 0, 0, err
	}
	return filePath, line, character, nil
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

func jsonResult(v any) (*mcp.CallToolResult, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
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
		return contents
	case map[string]any:
		if value, ok := contents["value"].(string); ok {
			return value
		}
		if kind, ok := contents["kind"].(string); ok {
			if value, ok := contents["value"].(string); ok {
				return fmt.Sprintf("%s\n\n%s", kind, value)
			}
		}
	case []any:
		parts := make([]string, 0, len(contents))
		for _, item := range contents {
			switch v := item.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if value, ok := v["value"].(string); ok {
					parts = append(parts, value)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}

	buf, err := json.MarshalIndent(hover.Contents, "", "  ")
	if err != nil {
		return ""
	}
	return string(buf)
}
