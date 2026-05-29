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

func TestConfigDefaultsAndOverrides(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "mcp-config.json")
	content := []byte(`{
		"runtime": {
			"idle_ttl_sec": 7,
			"max_instances": 2
		},
		"performance": {
			"default_timeout_ms": 99,
			"max_references": 3
		},
		"languages": {
			"gopls": {
				"command": ["fake-gopls"]
			}
		}
	}`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("LSP_BRIDGE_CONFIG", configPath)
	manager := NewManager(nil)
	defer manager.Close()

	if manager.runtimeConfig().IdleTTLSec != 7 {
		t.Fatalf("idle ttl = %d, want 7", manager.runtimeConfig().IdleTTLSec)
	}
	if manager.runtimeConfig().MaxInstances != 2 {
		t.Fatalf("max instances = %d, want 2", manager.runtimeConfig().MaxInstances)
	}
	if manager.runtimeConfig().MaxRestarts != 3 {
		t.Fatalf("max restarts default = %d, want 3", manager.runtimeConfig().MaxRestarts)
	}
	if manager.performanceConfig().DefaultTimeoutMs != 99 {
		t.Fatalf("default timeout = %d, want 99", manager.performanceConfig().DefaultTimeoutMs)
	}
	if manager.performanceConfig().MaxReferences != 3 {
		t.Fatalf("max references = %d, want 3", manager.performanceConfig().MaxReferences)
	}
	if _, ok := manager.config.Languages["go"]; !ok {
		t.Fatalf("expected normalized go language config")
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

func TestManagerStatusShutdownAndReap(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nvar value int\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	configPath := filepath.Join(root, "mcp-config.json")
	config := ConfigFile{
		Runtime: RuntimeConfig{
			IdleTTLSec:   1,
			MaxInstances: 1,
		},
		Languages: map[string]LanguageConfig{
			"go": {
				Command: []string{os.Args[0], "-test.run", "TestHelperProcessFakeLSPBridge", "--"},
				Env: map[string]string{
					"GO_WANT_FAKE_LSP_BRIDGE": "1",
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

	now := time.Now()
	manager.defaultsNow = func() time.Time { return now }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	instance, err := manager.Initialize(ctx, root, "go")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := instance.Client.EnsureDidOpen(filePath); err != nil {
		t.Fatalf("didOpen failed: %v", err)
	}

	status := manager.Status()
	if len(status) != 1 {
		t.Fatalf("status len = %d, want 1", len(status))
	}
	if status[0].OpenFiles != 1 {
		t.Fatalf("open files = %d, want 1", status[0].OpenFiles)
	}
	if !status[0].Server.Found {
		t.Fatalf("expected fake lsp command to be found: %+v", status[0].Server)
	}
	if !status[0].Server.Running || !status[0].Server.Healthy {
		t.Fatalf("expected running healthy server: %+v", status[0].Server)
	}

	now = now.Add(2 * time.Second)
	manager.reap()
	if got := manager.Status(); len(got) != 0 {
		t.Fatalf("expected idle instance to be reaped, got %d", len(got))
	}

	if _, err := manager.Initialize(ctx, root, "go"); err != nil {
		t.Fatalf("Initialize after reap failed: %v", err)
	}
	closed, err := manager.Shutdown(root, "go", false)
	if err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	if got := manager.Status(); len(got) != 0 {
		t.Fatalf("expected no instances after shutdown, got %d", len(got))
	}
}

func TestDependencyStatusReportsMissingServerAndRepairAction(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "mcp-config.json")
	config := ConfigFile{
		Languages: map[string]LanguageConfig{
			"go": {
				Command: []string{"definitely-missing-lsp-bridge-test-server"},
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

	status, err := manager.DependencyStatus(root, "go")
	if err != nil {
		t.Fatalf("dependency status failed: %v", err)
	}
	if status.State != "missing" {
		t.Fatalf("state = %q, want missing", status.State)
	}
	if status.Server.Found {
		t.Fatalf("server unexpectedly found: %+v", status.Server)
	}
	if len(status.Repair) == 0 {
		t.Fatal("expected repair actions")
	}
	if status.Repair[0].Automatic {
		t.Fatalf("install repair must not be automatic: %+v", status.Repair[0])
	}
}

func TestRepairRestartsExitedInstance(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nvar value int\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	marker := filepath.Join(root, "exit-once-marker")
	configPath := filepath.Join(root, "mcp-config.json")
	config := ConfigFile{
		Runtime: RuntimeConfig{
			RestartBackoffMs: 1,
		},
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

	instance, err := manager.Initialize(ctx, root, "go")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	waitExited(t, instance.Client)

	plan := manager.Repair(ctx, root, "go", false, false)
	if len(plan.Actions) == 0 {
		t.Fatal("expected repair actions")
	}
	foundRestart := false
	for _, action := range plan.Actions {
		if action.ID == "restart_instance" && action.Automatic {
			foundRestart = true
		}
	}
	if !foundRestart {
		t.Fatalf("expected automatic restart action: %+v", plan.Actions)
	}

	report := manager.Repair(ctx, root, "go", true, false)
	if len(report.Results) == 0 || !report.Results[0].Success {
		t.Fatalf("expected successful repair result: %+v", report.Results)
	}
	restarted, err := manager.ClientForPath(ctx, filePath, "go", "")
	if err != nil {
		t.Fatalf("ClientForPath after repair failed: %v", err)
	}
	if restarted.Client.Exited() {
		t.Fatal("expected repaired instance to be running")
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
