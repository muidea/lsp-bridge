package lsp

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientWithGopls(t *testing.T) {
	if os.Getenv("LSP_BRIDGE_INTEGRATION") != "1" {
		t.Skip("set LSP_BRIDGE_INTEGRATION=1 to run integration tests")
	}

	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls not available")
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/integration\n\ngo 1.24.0\n")
	mainFile := filepath.Join(root, "main.go")
	writeFile(t, mainFile, `package main

import "fmt"

func greet(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func main() {
	println(greet("world"))
}
`)

	client, err := NewClient(Config{
		Command: []string{goplsPath, "serve"},
		RootDir: root,
		Logger:  log.New(os.Stderr, "test-lsp: ", log.LstdFlags),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	defs, err := client.Definition(ctx, mainFile, 9, 10)
	if err != nil {
		t.Fatalf("Definition failed: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("expected at least one definition")
	}
	if defs[0].Range.Start.Line != 4 {
		t.Fatalf("definition line = %d, want 4", defs[0].Range.Start.Line)
	}

	hover, err := client.Hover(ctx, mainFile, 9, 10)
	if err != nil {
		t.Fatalf("Hover failed: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover result")
	}

	buf := formatHoverForTest(hover)
	if !strings.Contains(buf, "func greet(name string) string") {
		t.Fatalf("hover = %q", buf)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func formatHoverForTest(hover *Hover) string {
	switch contents := hover.Contents.(type) {
	case string:
		return contents
	case map[string]any:
		if value, ok := contents["value"].(string); ok {
			return value
		}
	case []any:
		parts := make([]string, 0, len(contents))
		for _, item := range contents {
			if value, ok := item.(string); ok {
				parts = append(parts, value)
			}
			if valueMap, ok := item.(map[string]any); ok {
				if value, ok := valueMap["value"].(string); ok {
					parts = append(parts, value)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}
