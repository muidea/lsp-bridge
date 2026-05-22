package bridge

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lsp-bridge/internal/lsp"
)

type Manager struct {
	logger *log.Logger
	mu     sync.Mutex
	active *Instance
}

type Instance struct {
	RootDir string
	Command []string
	Client  *lsp.Client
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) Initialize(ctx context.Context, rootDir, serverName string) (*Instance, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}

	command, err := commandForServer(serverName)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active != nil && sameInstance(m.active, rootDir, command) {
		return m.active, nil
	}

	if m.active != nil {
		_ = m.active.Client.Close()
		m.active = nil
	}

	client, err := lsp.NewClient(lsp.Config{
		Command: command,
		RootDir: rootDir,
		Logger:  m.logger,
	})
	if err != nil {
		return nil, err
	}

	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, err
	}

	m.active = &Instance{
		RootDir: rootDir,
		Command: command,
		Client:  client,
	}
	return m.active, nil
}

func (m *Manager) Current() (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == nil {
		return nil, fmt.Errorf("no active lsp instance; call initialize_lsp first")
	}
	return m.active, nil
}

func (m *Manager) RestartIfNeeded(ctx context.Context) (*Instance, error) {
	instance, err := m.Current()
	if err != nil {
		return nil, err
	}
	return m.Initialize(ctx, instance.RootDir, strings.Join(instance.Command, " "))
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil
	}
	err := m.active.Client.Close()
	m.active = nil
	return err
}

func sameInstance(instance *Instance, rootDir string, command []string) bool {
	if instance.RootDir != rootDir {
		return false
	}
	if len(instance.Command) != len(command) {
		return false
	}
	for i := range command {
		if instance.Command[i] != command[i] {
			return false
		}
	}
	return true
}

func commandForServer(serverName string) ([]string, error) {
	name := strings.TrimSpace(strings.ToLower(serverName))
	switch name {
	case "", "pyright", "pyright-langserver", "pyright-langserver --stdio":
		return []string{"pyright-langserver", "--stdio"}, nil
	case "gopls", "gopls serve":
		return []string{"gopls", "serve"}, nil
	default:
		parts := strings.Fields(serverName)
		if len(parts) == 0 {
			return nil, fmt.Errorf("invalid lsp server command")
		}
		return parts, nil
	}
}
