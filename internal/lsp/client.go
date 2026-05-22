package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var errClientClosed = errors.New("lsp client closed")

type Config struct {
	Command []string
	RootDir string
	Logger  *log.Logger
}

type Client struct {
	cmd       *exec.Cmd
	rootDir   string
	logger    *log.Logger
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan ResponseMessage
	nextID    atomic.Int64
	openedMu  sync.Mutex
	opened    map[string]openedDocument
	closed    atomic.Bool
	waitErr   chan error
}

type openedDocument struct {
	Text    string
	Version int
}

func NewClient(cfg Config) (*Client, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("lsp command is required")
	}
	if cfg.RootDir == "" {
		return nil, errors.New("root directory is required")
	}

	rootDir, err := filepath.Abs(cfg.RootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}

	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = rootDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open stderr pipe: %w", err)
	}

	client := &Client{
		cmd:     cmd,
		rootDir: rootDir,
		logger:  cfg.Logger,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[int64]chan ResponseMessage),
		opened:  make(map[string]openedDocument),
		waitErr: make(chan error, 1),
	}

	if client.logger == nil {
		client.logger = log.New(io.Discard, "", 0)
	}

	if err := client.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lsp server: %w", err)
	}

	go client.readLoop()
	go client.stderrLoop()
	go client.waitLoop()

	return client, nil
}

func (c *Client) PID() int {
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *Client) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   PathToURI(c.rootDir),
		Capabilities: map[string]any{
			"textDocument": map[string]any{
				"definition": map[string]any{
					"linkSupport": false,
				},
				"hover": map[string]any{
					"contentFormat": []string{"markdown", "plaintext"},
				},
			},
			"workspace": map[string]any{},
		},
		ClientInfo: map[string]string{
			"name":    "mcp-lsp-bridge",
			"version": "0.1.0",
		},
		WorkspaceFolders: []WorkspaceFolder{
			{
				URI:  PathToURI(c.rootDir),
				Name: filepath.Base(c.rootDir),
			},
		},
	}

	if _, err := c.request(ctx, "initialize", params); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := c.notify("initialized", InitializedParams{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	return nil
}

func (c *Client) EnsureDidOpen(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve file path: %w", err)
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	text := string(content)

	uri := PathToURI(abs)

	c.openedMu.Lock()
	prev, ok := c.opened[abs]
	if ok && prev.Text == text {
		c.openedMu.Unlock()
		return nil
	}
	version := 1
	if ok {
		version = prev.Version + 1
	}
	c.opened[abs] = openedDocument{Text: text, Version: version}
	c.openedMu.Unlock()

	if !ok {
		params := DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: detectLanguage(abs),
				Version:    version,
				Text:       text,
			},
		}

		if err := c.notify("textDocument/didOpen", params); err != nil {
			return fmt.Errorf("didOpen: %w", err)
		}
		return nil
	}

	params := DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: text},
		},
	}
	if err := c.notify("textDocument/didChange", params); err != nil {
		return fmt.Errorf("didChange: %w", err)
	}
	return nil
}

func (c *Client) Definition(ctx context.Context, path string, line, character int) ([]Location, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve file path: %w", err)
	}
	if err := c.EnsureDidOpen(abs); err != nil {
		return nil, err
	}

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: PathToURI(abs)},
		Position: Position{
			Line:      line,
			Character: character,
		},
	}

	raw, err := c.request(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var locations []Location
	if err := json.Unmarshal(raw, &locations); err == nil {
		return locations, nil
	}

	var single Location
	if err := json.Unmarshal(raw, &single); err == nil {
		return []Location{single}, nil
	}

	return nil, fmt.Errorf("decode definition response: %s", string(raw))
}

func (c *Client) Hover(ctx context.Context, path string, line, character int) (*Hover, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve file path: %w", err)
	}
	if err := c.EnsureDidOpen(abs); err != nil {
		return nil, err
	}

	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: PathToURI(abs)},
		Position: Position{
			Line:      line,
			Character: character,
		},
	}

	raw, err := c.request(ctx, "textDocument/hover", params)
	if err != nil {
		return nil, err
	}

	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var hover Hover
	if err := json.Unmarshal(raw, &hover); err != nil {
		return nil, fmt.Errorf("decode hover response: %w", err)
	}
	return &hover, nil
}

func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	var errs []error

	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	select {
	case <-c.waitErr:
	case <-time.After(2 * time.Second):
		if c.cmd != nil && c.cmd.Process != nil {
			if err := c.cmd.Process.Kill(); err != nil {
				errs = append(errs, err)
			}
		}
		<-c.waitErr
	}

	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()

	return errors.Join(errs...)
}

func (c *Client) waitLoop() {
	err := c.cmd.Wait()
	select {
	case c.waitErr <- err:
	default:
	}
	if err != nil {
		c.logger.Printf("lsp process exited: %v", err)
	}
	c.failPending(err)
}

func (c *Client) stderrLoop() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		c.logger.Printf("lsp stderr: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		c.logger.Printf("read lsp stderr: %v", err)
	}
}

func (c *Client) readLoop() {
	reader := bufio.NewReader(c.stdout)
	for {
		body, err := readFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !c.closed.Load() {
				c.logger.Printf("read lsp frame: %v", err)
			}
			c.failPending(err)
			return
		}

		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *RespError      `json:"error,omitempty"`
			Method  string          `json:"method,omitempty"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			c.logger.Printf("decode lsp response: %v", err)
			continue
		}

		if envelope.ID == nil {
			if envelope.Method != "" {
				c.logger.Printf("ignore lsp notification: %s", envelope.Method)
			}
			continue
		}

		c.pendingMu.Lock()
		ch := c.pending[*envelope.ID]
		delete(c.pending, *envelope.ID)
		c.pendingMu.Unlock()
		if ch == nil {
			continue
		}

		ch <- ResponseMessage{
			JSONRPC: envelope.JSONRPC,
			ID:      *envelope.ID,
			Result:  envelope.Result,
			Error:   envelope.Error,
			Method:  envelope.Method,
			Params:  envelope.Params,
		}
		close(ch)
	}
}

func (c *Client) failPending(err error) {
	if err == nil {
		err = errClientClosed
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- ResponseMessage{
			Error: &RespError{
				Code:    -32000,
				Message: err.Error(),
			},
		}
		close(ch)
	}
}

func (c *Client) request(ctx context.Context, method string, params interface{}) ([]byte, error) {
	if c.closed.Load() {
		return nil, errClientClosed
	}

	id := c.nextID.Add(1)
	respCh := make(chan ResponseMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	req := RequestMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.writeMessage(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-respCh:
		if !ok {
			return nil, errClientClosed
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *Client) notify(method string, params interface{}) error {
	if c.closed.Load() {
		return errClientClosed
	}
	req := RequestMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(req)
}

func (c *Client) writeMessage(msg interface{}) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
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
			value := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			if value == line {
				value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			}
			contentLength, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse content length %q: %w", value, err)
			}
		}
	}

	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func detectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	default:
		return "plaintext"
	}
}
