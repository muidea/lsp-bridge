package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lsp-bridge/internal/lsp"
)

type Manager struct {
	logger    *log.Logger
	config    ConfigFile
	mu        sync.Mutex
	instances map[string]*Instance
	activeKey string
}

type ConfigFile struct {
	Languages map[string]LanguageConfig `json:"languages"`
}

type LanguageConfig struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

type Instance struct {
	RootDir string
	LangID  string
	Command []string
	Env     []string
	Client  *lsp.Client
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{
		logger:    logger,
		config:    loadConfig(logger),
		instances: make(map[string]*Instance),
	}
}

func (m *Manager) Initialize(ctx context.Context, rootDir, langID string) (*Instance, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}

	langID = normalizeLanguage(langID)
	command, env, err := m.commandForLanguage(rootDir, langID)
	if err != nil {
		return nil, err
	}

	key := instanceKey(rootDir, langID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if current := m.instances[key]; current != nil && !current.Client.Exited() {
		m.activeKey = key
		return current, nil
	}

	if current := m.instances[key]; current != nil {
		_ = current.Client.Close()
		delete(m.instances, key)
	}

	instance, err := m.startLocked(ctx, rootDir, langID, command, env)
	if err != nil {
		return nil, err
	}
	m.instances[key] = instance
	m.activeKey = key
	return instance, nil
}

func (m *Manager) ClientForPath(ctx context.Context, path, langHint, rootHint string) (*Instance, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve file path: %w", err)
	}

	langID := normalizeLanguage(langHint)
	if langID == "" {
		langID = inferLanguage(abs)
	}

	if rootHint != "" {
		return m.Initialize(ctx, rootHint, langID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	instance := m.bestInstanceLocked(abs, langID)
	if instance == nil && langID == "" {
		if m.activeKey != "" {
			instance = m.instances[m.activeKey]
		}
	}
	if instance == nil {
		return nil, fmt.Errorf("no active LSP instance for %s; call lsp_initialize first", langID)
	}
	if instance.Client.Exited() {
		restarted, err := m.startLocked(ctx, instance.RootDir, instance.LangID, instance.Command, instance.Env)
		if err != nil {
			return nil, fmt.Errorf("restart %s LSP: %w", instance.LangID, err)
		}
		key := instanceKey(instance.RootDir, instance.LangID)
		m.instances[key] = restarted
		m.activeKey = key
		instance = restarted
	}
	return instance, nil
}

func (m *Manager) Current() (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeKey == "" || m.instances[m.activeKey] == nil {
		return nil, fmt.Errorf("no active lsp instance; call lsp_initialize first")
	}
	return m.instances[m.activeKey], nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for key, instance := range m.instances {
		delete(m.instances, key)
		if err := instance.Client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return joinErrors(errs)
}

func (m *Manager) startLocked(ctx context.Context, rootDir, langID string, command, env []string) (*Instance, error) {
	client, err := lsp.NewClient(lsp.Config{
		Command: command,
		RootDir: rootDir,
		Logger:  m.logger,
		Env:     env,
	})
	if err != nil {
		return nil, err
	}

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &Instance{
		RootDir: rootDir,
		LangID:  langID,
		Command: command,
		Env:     env,
		Client:  client,
	}, nil
}

func (m *Manager) bestInstanceLocked(path, langID string) *Instance {
	var best *Instance
	bestLen := -1
	for _, instance := range m.instances {
		if langID != "" && instance.LangID != langID {
			continue
		}
		rel, err := filepath.Rel(instance.RootDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(instance.RootDir) > bestLen {
			best = instance
			bestLen = len(instance.RootDir)
		}
	}

	if best != nil {
		return best
	}
	for _, instance := range m.instances {
		if langID == "" || instance.LangID == langID {
			return instance
		}
	}
	return nil
}

func (m *Manager) commandForLanguage(rootDir, langID string) ([]string, []string, error) {
	if cfg, ok := m.config.Languages[langID]; ok && len(cfg.Command) > 0 {
		return cfg.Command, mergeEnv(detectEnv(rootDir, langID), cfg.Env), nil
	}

	command, err := defaultCommandForLanguage(langID)
	if err != nil {
		return nil, nil, err
	}
	return command, mergeEnv(detectEnv(rootDir, langID), nil), nil
}

func defaultCommandForLanguage(langID string) ([]string, error) {
	switch normalizeLanguage(langID) {
	case "go":
		return []string{"gopls", "serve"}, nil
	case "rust":
		return []string{"rust-analyzer"}, nil
	case "python":
		return []string{"pyright-langserver", "--stdio"}, nil
	case "typescript":
		return []string{"typescript-language-server", "--stdio"}, nil
	case "shell":
		return []string{"bash-language-server", "start"}, nil
	default:
		return nil, fmt.Errorf("unsupported language %q", langID)
	}
}

func commandForServer(serverName string) ([]string, error) {
	name := strings.TrimSpace(strings.ToLower(serverName))
	switch name {
	case "", "pyright", "pyright-langserver", "pyright-langserver --stdio", "python":
		return []string{"pyright-langserver", "--stdio"}, nil
	case "gopls", "gopls serve", "go":
		return []string{"gopls", "serve"}, nil
	case "rust", "rust-analyzer":
		return []string{"rust-analyzer"}, nil
	case "typescript", "ts", "typescript-language-server", "typescript-language-server --stdio":
		return []string{"typescript-language-server", "--stdio"}, nil
	case "shell", "bash", "bash-lsp", "bash-language-server", "bash-language-server start":
		return []string{"bash-language-server", "start"}, nil
	default:
		parts := strings.Fields(serverName)
		if len(parts) == 0 {
			return nil, fmt.Errorf("invalid lsp server command")
		}
		return parts, nil
	}
}

func normalizeLanguage(langID string) string {
	switch strings.ToLower(strings.TrimSpace(langID)) {
	case "", "auto":
		return ""
	case "go", "golang", "gopls":
		return "go"
	case "rs", "rust", "rust-analyzer":
		return "rust"
	case "py", "python", "pyright", "pyright-langserver":
		return "python"
	case "ts", "tsx", "js", "jsx", "javascript", "typescript", "typescriptreact", "javascriptreact", "tsserver":
		return "typescript"
	case "sh", "bash", "shell", "shellscript", "bash-lsp", "bash-language-server":
		return "shell"
	default:
		return strings.ToLower(strings.TrimSpace(langID))
	}
}

func inferLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".sh", ".bash":
		return "shell"
	default:
		return ""
	}
}

func instanceKey(rootDir, langID string) string {
	return filepath.Clean(rootDir) + "\x00" + normalizeLanguage(langID)
}

func loadConfig(logger *log.Logger) ConfigFile {
	path := os.Getenv("LSP_BRIDGE_CONFIG")
	if path == "" {
		path = "mcp-config.json"
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return ConfigFile{Languages: map[string]LanguageConfig{}}
	}

	var config ConfigFile
	if err := json.Unmarshal(content, &config); err != nil {
		if logger != nil {
			logger.Printf("read config %s: %v", path, err)
		}
		return ConfigFile{Languages: map[string]LanguageConfig{}}
	}

	if config.Languages == nil {
		config.Languages = map[string]LanguageConfig{}
	}

	normalized := make(map[string]LanguageConfig, len(config.Languages))
	for langID, langConfig := range config.Languages {
		normalized[normalizeLanguage(langID)] = langConfig
	}
	config.Languages = normalized
	return config
}

func detectEnv(rootDir, langID string) map[string]string {
	env := map[string]string{}
	pathParts := []string{}

	if langID == "python" {
		for _, name := range []string{".venv", "venv"} {
			venv := filepath.Join(rootDir, name)
			if stat, err := os.Stat(venv); err == nil && stat.IsDir() {
				env["VIRTUAL_ENV"] = venv
				pathParts = append(pathParts, filepath.Join(venv, "bin"), filepath.Join(venv, "Scripts"))
				break
			}
		}
	}

	if langID == "typescript" {
		nodeBin := filepath.Join(rootDir, "node_modules", ".bin")
		if stat, err := os.Stat(nodeBin); err == nil && stat.IsDir() {
			pathParts = append(pathParts, nodeBin)
		}
	}

	if len(pathParts) > 0 {
		pathParts = append(pathParts, os.Getenv("PATH"))
		env["PATH"] = strings.Join(pathParts, string(os.PathListSeparator))
	}

	return env
}

func mergeEnv(base map[string]string, override map[string]string) []string {
	for key, value := range override {
		base[key] = value
	}
	out := make([]string, 0, len(base))
	for key, value := range base {
		out = append(out, key+"="+value)
	}
	return out
}

func joinErrors(errs []error) error {
	var parts []string
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}
