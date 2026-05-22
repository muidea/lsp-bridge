package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lsp-bridge/internal/lsp"
	"lsp-bridge/internal/testlsp"
)

func TestHelperProcessFakeLSPBridge(t *testing.T) {
	if os.Getenv("GO_WANT_FAKE_LSP_BRIDGE") != "1" {
		return
	}

	exitAfterInitialized := false
	if marker := os.Getenv("FAKE_LSP_EXIT_ONCE_MARKER"); marker != "" {
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			exitAfterInitialized = true
			_ = os.WriteFile(marker, []byte("used"), 0o644)
		}
	}

	if err := testlsp.Run(exitAfterInitialized); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func TestFormatHoverMarkupContent(t *testing.T) {
	got := formatHover(&lsp.Hover{
		Contents: map[string]any{
			"kind":  "markdown",
			"value": "```python\ndef foo() -> str\n```",
		},
	})

	if !strings.Contains(got, "def foo() -> str") {
		t.Fatalf("hover did not include signature: %q", got)
	}
}

func TestCommandForServer(t *testing.T) {
	command, err := commandForServer("pyright")
	if err != nil {
		t.Fatalf("commandForServer failed: %v", err)
	}
	got := strings.Join(command, " ")
	if got != "pyright-langserver --stdio" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"gopls":         "go",
		"rust-analyzer": "rust",
		"pyright":       "python",
		"tsserver":      "typescript",
		"bash-lsp":      "shell",
	}

	for input, want := range cases {
		if got := normalizeLanguage(input); got != want {
			t.Fatalf("normalizeLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFormatLocationsTruncates(t *testing.T) {
	locations := make([]lsp.Location, 12)
	for i := range locations {
		locations[i] = lsp.Location{
			URI: "file:///tmp/example.go",
			Range: lsp.Range{
				Start: lsp.Position{Line: i, Character: 2},
			},
		}
	}

	got := formatLocations(locations, 10)
	if !got.Truncated {
		t.Fatal("expected truncated result")
	}
	if len(got.Items) != 10 {
		t.Fatalf("len = %d, want 10", len(got.Items))
	}
}

func TestManagerRestartsExitedLSP(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nvar value int\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	marker := filepath.Join(root, "exit-once-marker")
	configPath := filepath.Join(root, "mcp-config.json")
	config := ConfigFile{
		Languages: map[string]LanguageConfig{
			"go": {
				Command: []string{os.Args[0], "-test.run", "TestHelperProcessFakeLSPBridge", "--"},
				Env: map[string]string{
					"GO_WANT_FAKE_LSP_BRIDGE":   "1",
					"FAKE_LSP_EXIT_ONCE_MARKER": marker,
				},
			},
		},
	}
	content, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("LSP_BRIDGE_CONFIG", configPath)
	manager := NewManager(nil)
	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := manager.Initialize(ctx, root, "go")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	firstPID := first.Client.PID()

	waitExited(t, first.Client)

	second, err := manager.ClientForPath(ctx, filePath, "go", "")
	if err != nil {
		t.Fatalf("ClientForPath restart failed: %v", err)
	}
	if second.Client.PID() == firstPID {
		t.Fatalf("expected restarted process, pid stayed %d", firstPID)
	}

	defs, err := second.Client.Definition(ctx, filePath, 2, 4)
	if err != nil {
		t.Fatalf("Definition after restart failed: %v", err)
	}
	if len(defs) != 1 || defs[0].Range.Start.Line != 2 {
		t.Fatalf("unexpected definition after restart: %+v", defs)
	}
}

func waitExited(t *testing.T, client *lsp.Client) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		if client.Exited() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fake LSP exit")
		case <-ticker.C:
		}
	}
}
