package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lsp-bridge/internal/lsp"
)

type Manager struct {
	logger      *log.Logger
	config      ConfigFile
	mu          sync.Mutex
	instances   map[string]*Instance
	activeKey   string
	starting    map[string]*startState
	defaultsNow func() time.Time
	stopReaper  chan struct{}
	closeOnce   sync.Once
}

type ConfigFile struct {
	Runtime     RuntimeConfig             `json:"runtime"`
	Performance PerformanceConfig         `json:"performance"`
	Languages   map[string]LanguageConfig `json:"languages"`
}

type RuntimeConfig struct {
	IdleTTLSec       int `json:"idle_ttl_sec"`
	MaxInstances     int `json:"max_instances"`
	MaxRestarts      int `json:"max_restarts"`
	RestartBackoffMs int `json:"restart_backoff_ms"`
}

type PerformanceConfig struct {
	DefaultTimeoutMs    int `json:"default_timeout_ms"`
	InitializeTimeoutMs int `json:"initialize_timeout_ms"`
	MaxReferences       int `json:"max_references"`
	MaxDiagnostics      int `json:"max_diagnostics"`
	MaxHoverChars       int `json:"max_hover_chars"`
}

type LanguageConfig struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

type Instance struct {
	RootDir      string
	LangID       string
	Command      []string
	Env          []string
	Client       *lsp.Client
	LastUsedAt   time.Time
	RestartCount int
	Active       int
	State        string
	LastError    string
}

type startResult struct {
	instance *Instance
	err      error
}

type startState struct {
	done   chan struct{}
	result startResult
}

type InstanceStatus struct {
	RootPath     string         `json:"root_path"`
	LangID       string         `json:"lang_id"`
	State        string         `json:"state"`
	PID          int            `json:"pid"`
	LastUsedAt   time.Time      `json:"last_used_at"`
	IdleSec      int64          `json:"idle_sec"`
	OpenFiles    int            `json:"open_files"`
	RestartCount int            `json:"restart_count"`
	Active       int            `json:"active_requests"`
	LastError    string         `json:"last_error,omitempty"`
	Server       ServerHealth   `json:"server"`
	Repair       []RepairAction `json:"repair,omitempty"`
}

type ServerHealth struct {
	Command []string `json:"command"`
	Found   bool     `json:"found"`
	Path    string   `json:"path,omitempty"`
	Running bool     `json:"running"`
	Healthy bool     `json:"healthy"`
	Error   string   `json:"error,omitempty"`
	Remedy  string   `json:"remedy,omitempty"`
}

type RepairAction struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Command     []string `json:"command,omitempty"`
	Automatic   bool     `json:"automatic"`
}

type RepairReport struct {
	Applied bool             `json:"applied"`
	Items   []InstanceStatus `json:"items"`
	Actions []RepairAction   `json:"actions"`
	Results []RepairResult   `json:"results,omitempty"`
}

type RepairResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

func NewManager(logger *log.Logger) *Manager {
	manager := &Manager{
		logger:      logger,
		config:      loadConfig(logger),
		instances:   make(map[string]*Instance),
		starting:    make(map[string]*startState),
		defaultsNow: time.Now,
		stopReaper:  make(chan struct{}),
	}
	go manager.reaperLoop()
	return manager
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

	instance, err := m.ensureInstance(ctx, key, rootDir, langID, command, env)
	if err != nil {
		return nil, err
	}
	m.reap()
	return instance, nil
}

func (m *Manager) ensureInstance(ctx context.Context, key, rootDir, langID string, command, env []string) (*Instance, error) {
	for {
		now := m.now()

		m.mu.Lock()
		if current := m.instances[key]; current != nil && !current.Client.Exited() {
			current.LastUsedAt = now
			current.State = "ready"
			m.activeKey = key
			m.mu.Unlock()
			return current, nil
		}

		restartCount := 0
		if current := m.instances[key]; current != nil {
			restartCount = current.RestartCount + 1
			if max := m.runtimeConfig().MaxRestarts; max > 0 && restartCount > max {
				err := fmt.Errorf("lsp instance restart limit exceeded for %s", langID)
				current.State = "exited"
				current.LastError = err.Error()
				m.mu.Unlock()
				return nil, err
			}
			_ = current.Client.Close()
			delete(m.instances, key)
		}

		if state := m.starting[key]; state != nil {
			m.mu.Unlock()
			select {
			case <-state.done:
				result := state.result
				if result.err != nil {
					return nil, result.err
				}
				return result.instance, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		state := &startState{done: make(chan struct{})}
		m.starting[key] = state
		m.activeKey = key
		m.mu.Unlock()

		if restartCount > 0 {
			if backoff := m.restartBackoff(restartCount); backoff > 0 {
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					m.finishStart(key, state, nil, ctx.Err())
					return nil, ctx.Err()
				}
			}
		}

		instance, err := m.start(ctx, rootDir, langID, command, env, restartCount)
		m.finishStart(key, state, instance, err)
		if err != nil {
			return nil, err
		}
		return instance, nil
	}
}

func (m *Manager) finishStart(key string, state *startState, instance *Instance, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.starting, key)
	if err == nil && instance != nil {
		m.instances[key] = instance
		m.activeKey = key
	}
	state.result = startResult{instance: instance, err: err}
	close(state.done)
}

func (m *Manager) Begin(instance *Instance) func() {
	if instance == nil {
		return func() {}
	}

	m.mu.Lock()
	instance.Active++
	instance.LastUsedAt = m.now()
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		if instance.Active > 0 {
			instance.Active--
		}
		instance.LastUsedAt = m.now()
		m.mu.Unlock()
	}
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

	instance := m.bestInstanceLocked(abs, langID)
	if instance == nil && langID == "" {
		if m.activeKey != "" {
			instance = m.instances[m.activeKey]
		}
	}
	if instance == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("no active LSP instance for %s; call lsp_initialize first", langID)
	}
	if instance.Client.Exited() {
		rootDir := instance.RootDir
		instanceLangID := instance.LangID
		command := append([]string(nil), instance.Command...)
		env := append([]string(nil), instance.Env...)
		key := instanceKey(instance.RootDir, instance.LangID)
		m.mu.Unlock()
		return m.ensureInstance(ctx, key, rootDir, instanceLangID, command, env)
	}
	instance.LastUsedAt = m.now()
	m.activeKey = instanceKey(instance.RootDir, instance.LangID)
	m.mu.Unlock()
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
	m.closeOnce.Do(func() {
		close(m.stopReaper)
	})

	m.mu.Lock()

	var errs []error
	for key, instance := range m.instances {
		delete(m.instances, key)
		if err := instance.Client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	m.mu.Unlock()
	return joinErrors(errs)
}

func (m *Manager) Shutdown(rootDir, langID string, all bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if all {
		count := 0
		for key, instance := range m.instances {
			delete(m.instances, key)
			instance.State = "stopping"
			_ = instance.Client.Close()
			count++
		}
		m.activeKey = ""
		return count, nil
	}

	if strings.TrimSpace(rootDir) == "" {
		return 0, fmt.Errorf("root_path is required unless all is true")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return 0, fmt.Errorf("resolve root directory: %w", err)
	}
	key := instanceKey(abs, normalizeLanguage(langID))
	instance := m.instances[key]
	if instance == nil {
		return 0, nil
	}
	delete(m.instances, key)
	if m.activeKey == key {
		m.activeKey = ""
	}
	instance.State = "stopping"
	if instance.Client.Exited() {
		return 1, nil
	}
	return 1, instance.Client.Close()
}

func (m *Manager) Status() []InstanceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	items := make([]InstanceStatus, 0, len(m.instances))
	for _, instance := range m.instances {
		items = append(items, m.statusForInstanceLocked(instance, now))
	}
	return items
}

func (m *Manager) DependencyStatus(rootDir, langID string) (InstanceStatus, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return InstanceStatus{}, fmt.Errorf("resolve root directory: %w", err)
	}
	langID = normalizeLanguage(langID)
	command, env, err := m.commandForLanguage(rootDir, langID)
	if err != nil {
		return InstanceStatus{}, err
	}
	key := instanceKey(rootDir, langID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if instance := m.instances[key]; instance != nil {
		return m.statusForInstanceLocked(instance, m.now()), nil
	}
	health := serverHealth(command, env, false, false)
	status := InstanceStatus{
		RootPath: rootDir,
		LangID:   langID,
		State:    stateFromHealth(health),
		Server:   health,
	}
	status.Repair = repairActionsFor(langID, rootDir, command, health, false)
	return status, nil
}

func (m *Manager) Repair(ctx context.Context, rootDir, langID string, apply bool, all bool) RepairReport {
	items := m.Status()
	if !all && strings.TrimSpace(rootDir) != "" {
		if status, err := m.DependencyStatus(rootDir, langID); err == nil {
			items = []InstanceStatus{status}
		} else {
			items = []InstanceStatus{{
				RootPath: rootDir,
				LangID:   normalizeLanguage(langID),
				State:    "missing",
				Server: ServerHealth{
					Healthy: false,
					Error:   err.Error(),
				},
				Repair: []RepairAction{{
					ID:          "fix_config",
					Description: "修正 root_path、lang_id 或 mcp-config.json 后重试",
					Automatic:   false,
				}},
			}}
		}
	}

	report := RepairReport{
		Applied: apply,
		Items:   items,
	}
	for _, item := range items {
		report.Actions = append(report.Actions, item.Repair...)
	}
	if !apply {
		return report
	}

	for _, item := range items {
		for _, action := range item.Repair {
			if !action.Automatic {
				continue
			}
			switch action.ID {
			case "restart_instance":
				closed, err := m.Shutdown(item.RootPath, item.LangID, false)
				if err != nil {
					report.Results = append(report.Results, RepairResult{ID: action.ID, Success: false, Message: err.Error()})
					continue
				}
				if closed == 0 {
					report.Results = append(report.Results, RepairResult{ID: action.ID, Success: false, Message: "instance not found"})
					continue
				}
				if _, err := m.Initialize(ctx, item.RootPath, item.LangID); err != nil {
					report.Results = append(report.Results, RepairResult{ID: action.ID, Success: false, Message: err.Error()})
					continue
				}
				report.Results = append(report.Results, RepairResult{ID: action.ID, Success: true, Message: "instance restarted"})
			}
		}
	}
	return report
}

func (m *Manager) statusForInstanceLocked(instance *Instance, now time.Time) InstanceStatus {
	state := instance.State
	exited := instance.Client.Exited()
	if exited {
		state = "exited"
	}
	health := serverHealth(instance.Command, instance.Env, true, !exited)
	status := InstanceStatus{
		RootPath:     instance.RootDir,
		LangID:       instance.LangID,
		State:        state,
		PID:          instance.Client.PID(),
		LastUsedAt:   instance.LastUsedAt,
		IdleSec:      int64(now.Sub(instance.LastUsedAt).Seconds()),
		OpenFiles:    instance.Client.OpenFileCount(),
		RestartCount: instance.RestartCount,
		Active:       instance.Active,
		LastError:    instance.LastError,
		Server:       health,
	}
	status.Repair = repairActionsFor(instance.LangID, instance.RootDir, instance.Command, health, true)
	return status
}

func (m *Manager) start(ctx context.Context, rootDir, langID string, command, env []string, restartCount int) (*Instance, error) {
	client, err := lsp.NewClient(lsp.Config{
		Command: command,
		RootDir: rootDir,
		Logger:  m.logger,
		Env:     env,
	})
	if err != nil {
		return nil, err
	}

	initCtx, cancel := context.WithTimeout(ctx, time.Duration(m.performanceConfig().InitializeTimeoutMs)*time.Millisecond)
	defer cancel()
	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &Instance{
		RootDir:      rootDir,
		LangID:       langID,
		Command:      command,
		Env:          env,
		Client:       client,
		LastUsedAt:   m.now(),
		RestartCount: restartCount,
		State:        "ready",
	}, nil
}

func (m *Manager) reaperLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopReaper:
			return
		case <-ticker.C:
			m.reap()
		}
	}
}

func (m *Manager) reap() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reapLocked()
}

func (m *Manager) reapLocked() {
	runtime := m.runtimeConfig()
	now := m.now()
	idleTTL := time.Duration(runtime.IdleTTLSec) * time.Second

	for key, instance := range m.instances {
		if instance.Active > 0 {
			continue
		}
		if instance.Client.Exited() || (idleTTL > 0 && now.Sub(instance.LastUsedAt) > idleTTL) {
			delete(m.instances, key)
			if m.activeKey == key {
				m.activeKey = ""
			}
			_ = instance.Client.Close()
		}
	}

	for runtime.MaxInstances > 0 && len(m.instances) > runtime.MaxInstances {
		var oldestKey string
		var oldest time.Time
		for key, instance := range m.instances {
			if instance.Active > 0 {
				continue
			}
			if oldestKey == "" || instance.LastUsedAt.Before(oldest) {
				oldestKey = key
				oldest = instance.LastUsedAt
			}
		}
		if oldestKey == "" {
			return
		}
		instance := m.instances[oldestKey]
		delete(m.instances, oldestKey)
		if m.activeKey == oldestKey {
			m.activeKey = ""
		}
		_ = instance.Client.Close()
	}
}

func (m *Manager) now() time.Time {
	if m.defaultsNow == nil {
		return time.Now()
	}
	return m.defaultsNow()
}

func (m *Manager) runtimeConfig() RuntimeConfig {
	cfg := m.config.Runtime
	if cfg.IdleTTLSec <= 0 {
		cfg.IdleTTLSec = 1800
	}
	if cfg.MaxInstances <= 0 {
		cfg.MaxInstances = 8
	}
	if cfg.MaxRestarts <= 0 {
		cfg.MaxRestarts = 3
	}
	if cfg.RestartBackoffMs <= 0 {
		cfg.RestartBackoffMs = 1000
	}
	return cfg
}

func (m *Manager) performanceConfig() PerformanceConfig {
	cfg := m.config.Performance
	if cfg.DefaultTimeoutMs <= 0 {
		cfg.DefaultTimeoutMs = 5000
	}
	if cfg.InitializeTimeoutMs <= 0 {
		cfg.InitializeTimeoutMs = 30000
	}
	if cfg.MaxReferences <= 0 {
		cfg.MaxReferences = 50
	}
	if cfg.MaxDiagnostics <= 0 {
		cfg.MaxDiagnostics = 100
	}
	if cfg.MaxHoverChars <= 0 {
		cfg.MaxHoverChars = 4000
	}
	return cfg
}

func (m *Manager) restartBackoff(restartCount int) time.Duration {
	cfg := m.runtimeConfig()
	backoff := time.Duration(cfg.RestartBackoffMs) * time.Millisecond
	for i := 1; i < restartCount; i++ {
		backoff *= 2
	}
	if backoff > 30*time.Second {
		return 30 * time.Second
	}
	return backoff
}

func (m *Manager) toolContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, time.Duration(m.performanceConfig().DefaultTimeoutMs)*time.Millisecond)
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

func serverHealth(command, env []string, hasInstance, running bool) ServerHealth {
	health := ServerHealth{
		Command: command,
		Running: running,
	}
	if len(command) == 0 {
		health.Error = "lsp server command is empty"
		health.Remedy = "configure languages.<lang>.command in mcp-config.json"
		return health
	}

	path, err := lookPathWithEnv(command[0], env)
	if err != nil {
		health.Error = err.Error()
		health.Remedy = installHint(command[0])
		return health
	}

	health.Found = true
	health.Path = path
	health.Healthy = !hasInstance || running
	if hasInstance && !running {
		health.Error = "lsp server process is not running"
		health.Remedy = "run lsp_repair with apply=true to restart the instance"
	}
	return health
}

func lookPathWithEnv(name string, env []string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if stat, err := os.Stat(name); err == nil && !stat.IsDir() {
			return name, nil
		}
	}
	pathValue := os.Getenv("PATH")
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == "PATH" {
			pathValue = value
			break
		}
	}
	for _, dir := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(dir, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}
	return exec.LookPath(name)
}

func installHint(command string) string {
	switch command {
	case "gopls":
		return "install with: go install golang.org/x/tools/gopls@latest"
	case "pyright-langserver", "pyright":
		return "install with: npm install -g pyright or run the lsp-bridge installer"
	case "typescript-language-server":
		return "install with: npm install -g typescript typescript-language-server"
	case "rust-analyzer":
		return "install rust-analyzer with rustup or your system package manager"
	case "bash-language-server":
		return "install with: npm install -g bash-language-server"
	default:
		return "install the configured LSP server or update mcp-config.json"
	}
}

func stateFromHealth(health ServerHealth) string {
	if health.Healthy {
		return "available"
	}
	if !health.Found {
		return "missing"
	}
	return "degraded"
}

func repairActionsFor(langID, rootDir string, command []string, health ServerHealth, hasInstance bool) []RepairAction {
	actions := []RepairAction{}
	if hasInstance && !health.Running {
		actions = append(actions, RepairAction{
			ID:          "restart_instance",
			Description: "重启已存在但不可用的 LSP 实例",
			Automatic:   true,
		})
	}
	if !health.Found {
		actions = append(actions, RepairAction{
			ID:          "install_lsp_server",
			Description: health.Remedy,
			Command:     installCommandFor(langID, command),
			Automatic:   false,
		})
	}
	if !health.Healthy {
		actions = append(actions, RepairAction{
			ID:          "check_config",
			Description: "确认 root_path、lang_id、PATH 和 languages.<lang>.command 配置正确",
			Automatic:   false,
		})
	}
	return actions
}

func installCommandFor(langID string, command []string) []string {
	name := ""
	if len(command) > 0 {
		name = command[0]
	}
	switch name {
	case "gopls":
		return []string{"go", "install", "golang.org/x/tools/gopls@latest"}
	case "pyright-langserver", "pyright":
		return []string{"npm", "install", "-g", "pyright"}
	case "typescript-language-server":
		return []string{"npm", "install", "-g", "typescript", "typescript-language-server"}
	case "bash-language-server":
		return []string{"npm", "install", "-g", "bash-language-server"}
	}
	switch normalizeLanguage(langID) {
	case "go":
		return []string{"go", "install", "golang.org/x/tools/gopls@latest"}
	case "python":
		return []string{"npm", "install", "-g", "pyright"}
	case "typescript":
		return []string{"npm", "install", "-g", "typescript", "typescript-language-server"}
	case "shell":
		return []string{"npm", "install", "-g", "bash-language-server"}
	default:
		return nil
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
		return defaultConfig()
	}

	var config ConfigFile
	if err := json.Unmarshal(content, &config); err != nil {
		if logger != nil {
			logger.Printf("read config %s: %v", path, err)
		}
		return defaultConfig()
	}

	if config.Languages == nil {
		config.Languages = map[string]LanguageConfig{}
	}

	normalized := make(map[string]LanguageConfig, len(config.Languages))
	for langID, langConfig := range config.Languages {
		normalized[normalizeLanguage(langID)] = langConfig
	}
	config.Languages = normalized
	config = applyConfigDefaults(config)
	return config
}

func defaultConfig() ConfigFile {
	return applyConfigDefaults(ConfigFile{Languages: map[string]LanguageConfig{}})
}

func applyConfigDefaults(config ConfigFile) ConfigFile {
	if config.Runtime.IdleTTLSec <= 0 {
		config.Runtime.IdleTTLSec = 1800
	}
	if config.Runtime.MaxInstances <= 0 {
		config.Runtime.MaxInstances = 8
	}
	if config.Runtime.MaxRestarts <= 0 {
		config.Runtime.MaxRestarts = 3
	}
	if config.Runtime.RestartBackoffMs <= 0 {
		config.Runtime.RestartBackoffMs = 1000
	}
	if config.Performance.DefaultTimeoutMs <= 0 {
		config.Performance.DefaultTimeoutMs = 5000
	}
	if config.Performance.InitializeTimeoutMs <= 0 {
		config.Performance.InitializeTimeoutMs = 30000
	}
	if config.Performance.MaxReferences <= 0 {
		config.Performance.MaxReferences = 50
	}
	if config.Performance.MaxDiagnostics <= 0 {
		config.Performance.MaxDiagnostics = 100
	}
	if config.Performance.MaxHoverChars <= 0 {
		config.Performance.MaxHoverChars = 4000
	}
	if config.Languages == nil {
		config.Languages = map[string]LanguageConfig{}
	}
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
