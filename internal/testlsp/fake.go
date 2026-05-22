package testlsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

var errExitAfterInitialized = errors.New("exit after initialized")

type Server struct {
	in      *bufio.Reader
	out     io.Writer
	mu      sync.Mutex
	textBy  map[string]string
	exitNow bool
}

func Run(exitAfterInitialize bool) error {
	server := &Server{
		in:      bufio.NewReader(os.Stdin),
		out:     os.Stdout,
		textBy:  make(map[string]string),
		exitNow: exitAfterInitialize,
	}
	return server.loop()
}

func (s *Server) loop() error {
	for {
		body, err := readFrame(s.in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		var req struct {
			ID     *int64          `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return err
		}

		if req.ID == nil {
			if err := s.handleNotification(req.Method, req.Params); err != nil {
				if errors.Is(err, errExitAfterInitialized) {
					return nil
				}
				return err
			}
			continue
		}

		if err := s.handleRequest(*req.ID, req.Method, req.Params); err != nil {
			return err
		}
	}
}

func (s *Server) handleNotification(method string, params json.RawMessage) error {
	switch method {
	case "initialized":
		if s.exitNow {
			return errExitAfterInitialized
		}
		return nil
	case "textDocument/didOpen":
		var req struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return err
		}
		s.setText(req.TextDocument.URI, req.TextDocument.Text)
		return s.publishDiagnostics(req.TextDocument.URI)
	case "textDocument/didChange":
		var req struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return err
		}
		if len(req.ContentChanges) > 0 {
			s.setText(req.TextDocument.URI, req.ContentChanges[len(req.ContentChanges)-1].Text)
		}
		return s.publishDiagnostics(req.TextDocument.URI)
	default:
		return nil
	}
}

func (s *Server) handleRequest(id int64, method string, params json.RawMessage) error {
	switch method {
	case "initialize":
		return s.respond(id, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":   1,
				"definitionProvider": true,
				"hoverProvider":      true,
				"referencesProvider": true,
			},
		})
	case "textDocument/definition":
		uri := documentURI(params)
		return s.respond(id, []map[string]any{location(uri, 2, 4)})
	case "textDocument/hover":
		uri := documentURI(params)
		text := s.text(uri)
		value := "fake-symbol: unknown"
		if strings.Contains(text, "value int") {
			value = "value: int"
		}
		if strings.Contains(text, "value string") {
			value = "value: string"
		}
		return s.respond(id, map[string]any{
			"contents": map[string]any{
				"kind":  "markdown",
				"value": value,
			},
		})
	case "textDocument/references":
		uri := documentURI(params)
		items := make([]map[string]any, 0, 12)
		for i := 0; i < 12; i++ {
			items = append(items, location(uri, i, 1))
		}
		return s.respond(id, items)
	default:
		return s.respondError(id, -32601, "method not found")
	}
}

func (s *Server) publishDiagnostics(uri string) error {
	text := s.text(uri)
	diagnostics := []map[string]any{}
	if strings.Contains(text, "SYNTAX_ERROR") {
		diagnostics = append(diagnostics, map[string]any{
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 1},
				"end":   map[string]any{"line": 0, "character": 13},
			},
			"severity": 1,
			"source":   "fake-lsp",
			"message":  "synthetic syntax error",
		})
	}
	return writeMessage(s.out, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params": map[string]any{
			"uri":         uri,
			"diagnostics": diagnostics,
		},
	})
}

func (s *Server) setText(uri, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.textBy[uri] = text
}

func (s *Server) text(uri string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.textBy[uri]
}

func (s *Server) respond(id int64, result any) error {
	return writeMessage(s.out, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func (s *Server) respondError(id int64, code int, message string) error {
	return writeMessage(s.out, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func documentURI(params json.RawMessage) string {
	var req struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	_ = json.Unmarshal(params, &req)
	return req.TextDocument.URI
}

func location(uri string, line, col int) map[string]any {
	return map[string]any{
		"uri": uri,
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": col},
			"end":   map[string]any{"line": line, "character": col + 1},
		},
	}
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, value, _ := strings.Cut(line, ":")
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeMessage(writer io.Writer, msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
