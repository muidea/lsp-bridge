package lsp

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadFrame(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Length: 15\r\n\r\n{\"jsonrpc\":\"2\"}"))

	body, err := readFrame(reader)
	if err != nil {
		t.Fatalf("readFrame failed: %v", err)
	}
	if string(body) != `{"jsonrpc":"2"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestReadFrameRejectsMissingContentLength(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}"))

	_, err := readFrame(reader)
	if err == nil {
		t.Fatal("expected error")
	}
}
