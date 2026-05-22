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
