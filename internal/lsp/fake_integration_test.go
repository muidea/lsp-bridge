package lsp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lsp-bridge/internal/testlsp"
)

func TestHelperProcessFakeLSP(t *testing.T) {
	if os.Getenv("GO_WANT_FAKE_LSP") != "1" {
		return
	}
	exitAfterInitialize := os.Getenv("FAKE_LSP_EXIT_AFTER_INITIALIZE") == "1"
	if err := testlsp.Run(exitAfterInitialize); err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func TestClientSyncDiagnosticsReferencesAndConcurrency(t *testing.T) {
	client, filePath := newFakeClient(t, false)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.SyncContent(filePath, "package main\n\nvar value int\n"); err != nil {
		t.Fatalf("SyncContent int failed: %v", err)
	}
	hover, err := client.Hover(ctx, filePath, 2, 4)
	if err != nil {
		t.Fatalf("Hover int failed: %v", err)
	}
	if !strings.Contains(formatHoverForTest(hover), "value: int") {
		t.Fatalf("hover did not reflect int sync: %q", formatHoverForTest(hover))
	}

	if err := client.SyncContent(filePath, "package main\n\nvar value string\nSYNTAX_ERROR\n"); err != nil {
		t.Fatalf("SyncContent string failed: %v", err)
	}
	hover, err = client.Hover(ctx, filePath, 2, 4)
	if err != nil {
		t.Fatalf("Hover string failed: %v", err)
	}
	if !strings.Contains(formatHoverForTest(hover), "value: string") {
		t.Fatalf("hover did not reflect string sync: %q", formatHoverForTest(hover))
	}

	diagnostics := waitFakeDiagnostics(t, client, filePath)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics len = %d, want 1", len(diagnostics))
	}
	if diagnostics[0].Severity != 1 || diagnostics[0].Message != "synthetic syntax error" {
		t.Fatalf("unexpected diagnostic: %+v", diagnostics[0])
	}

	references, err := client.References(ctx, filePath, 2, 4)
	if err != nil {
		t.Fatalf("References failed: %v", err)
	}
	if len(references) != 12 {
		t.Fatalf("references len = %d, want 12", len(references))
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defs, err := client.Definition(ctx, filePath, 2, 4)
			if err != nil {
				errs <- err
				return
			}
			if len(defs) != 1 || defs[0].Range.Start.Line != 2 {
				errs <- errUnexpectedDefinition(defs)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestDefinitionLatencyEnvelopeWithFakeLSP(t *testing.T) {
	client, filePath := newFakeClient(t, false)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := client.SyncContent(filePath, "package main\n\nvar value int\n"); err != nil {
		t.Fatalf("SyncContent failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := client.Definition(ctx, filePath, 2, 4); err != nil {
			t.Fatalf("warmup definition failed: %v", err)
		}
	}

	const iterations = 50
	start := time.Now()
	for i := 0; i < iterations; i++ {
		if _, err := client.Definition(ctx, filePath, 2, 4); err != nil {
			t.Fatalf("definition failed: %v", err)
		}
	}
	avg := time.Since(start) / iterations
	if avg > 50*time.Millisecond {
		t.Fatalf("average definition latency = %s, want <= 50ms", avg)
	}
}

func TestMemoryEnvelopeWithThreeFakeLSPClients(t *testing.T) {
	clients := make([]*Client, 0, 3)
	defer func() {
		for _, client := range clients {
			_ = client.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		client, filePath := newFakeClient(t, false)
		clients = append(clients, client)
		if err := client.Initialize(ctx); err != nil {
			t.Fatalf("Initialize %d failed: %v", i, err)
		}
		if err := client.SyncContent(filePath, "package main\n\nvar value int\n"); err != nil {
			t.Fatalf("SyncContent %d failed: %v", i, err)
		}
	}

	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	const maxAlloc = 64 * 1024 * 1024
	if stats.Alloc > maxAlloc {
		t.Fatalf("heap allocation = %d bytes, want <= %d", stats.Alloc, maxAlloc)
	}
}

func newFakeClient(t *testing.T, exitAfterInitialize bool) (*Client, string) {
	t.Helper()

	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	writeFile(t, filePath, "package main\n\nvar value int\n")

	command := []string{os.Args[0], "-test.run", "TestHelperProcessFakeLSP", "--"}
	env := []string{"GO_WANT_FAKE_LSP=1"}
	if exitAfterInitialize {
		env = append(env, "FAKE_LSP_EXIT_AFTER_INITIALIZE=1")
	}

	client, err := NewClient(Config{
		Command: command,
		RootDir: root,
		Logger:  log.New(os.Stderr, "fake-lsp: ", log.LstdFlags),
		Env:     env,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client, filePath
}

func waitFakeDiagnostics(t *testing.T, client *Client, filePath string) []Diagnostic {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		items, err := client.Diagnostics(filePath)
		if err != nil {
			t.Fatalf("Diagnostics failed: %v", err)
		}
		if len(items) > 0 {
			return items
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for diagnostics")
		case <-ticker.C:
		}
	}
}

func errUnexpectedDefinition(defs []Location) error {
	return fmt.Errorf("unexpected definition response: %+v", defs)
}
