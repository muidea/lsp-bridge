package bridge

import (
	"strings"
	"testing"

	"lsp-bridge/internal/lsp"
)

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
