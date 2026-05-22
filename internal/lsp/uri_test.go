package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathToURIAndBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pkg with space", "main.py")

	uri := PathToURI(path)
	if !strings.HasPrefix(uri, "file:///") {
		t.Fatalf("expected file URI, got %q", uri)
	}
	if strings.Contains(uri, "%2F") {
		t.Fatalf("path separators must not be escaped: %q", uri)
	}
	if !strings.Contains(uri, "pkg%20with%20space") {
		t.Fatalf("expected escaped path segment, got %q", uri)
	}

	got, err := URIToPath(uri)
	if err != nil {
		t.Fatalf("URIToPath failed: %v", err)
	}
	if got != filepath.Clean(path) {
		t.Fatalf("got %q, want %q", got, filepath.Clean(path))
	}
}
