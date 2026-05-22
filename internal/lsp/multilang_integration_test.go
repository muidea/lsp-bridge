package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientDefinitionAcrossInstalledLanguageServers(t *testing.T) {
	if os.Getenv("LSP_BRIDGE_INTEGRATION") != "1" {
		t.Skip("set LSP_BRIDGE_INTEGRATION=1 to run integration tests")
	}
	strict := os.Getenv("LSP_BRIDGE_STRICT_INTEGRATION") == "1"

	cases := []struct {
		name    string
		command []string
		setup   func(t *testing.T, root string) (path string, line int, col int, wantFiles []string)
	}{
		{
			name:    "go",
			command: commandFromPath(t, "gopls", "serve"),
			setup: func(t *testing.T, root string) (string, int, int, []string) {
				writeFile(t, filepath.Join(root, "go.mod"), "module example.com/integration\n\ngo 1.24.0\n")
				writeFile(t, filepath.Join(root, "lib.go"), "package main\n\nfunc greet(name string) string { return name }\n")
				mainPath := filepath.Join(root, "main.go")
				writeFile(t, mainPath, "package main\n\nfunc main() {\n\t_ = greet(\"world\")\n}\n")
				return mainPath, 3, 6, []string{"lib.go"}
			},
		},
		{
			name:    "python",
			command: commandFromPath(t, "pyright-langserver", "--stdio"),
			setup: func(t *testing.T, root string) (string, int, int, []string) {
				writeFile(t, filepath.Join(root, "lib.py"), "def greet(name: str) -> str:\n    return name\n")
				mainPath := filepath.Join(root, "main.py")
				writeFile(t, mainPath, "from lib import greet\n\nprint(greet('world'))\n")
				return mainPath, 2, 7, []string{"lib.py"}
			},
		},
		{
			name:    "typescript",
			command: commandFromPath(t, "typescript-language-server", "--stdio"),
			setup: func(t *testing.T, root string) (string, int, int, []string) {
				writeFile(t, filepath.Join(root, "package.json"), `{"type":"module","devDependencies":{"typescript":"latest"}}`+"\n")
				writeFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"module":"esnext","target":"es2020","strict":true}}`+"\n")
				writeFile(t, filepath.Join(root, "util.ts"), "export function greet(name: string): string { return name }\n")
				mainPath := filepath.Join(root, "index.ts")
				writeFile(t, mainPath, "import { greet } from './util'\n\ngreet('world')\n")
				return mainPath, 2, 2, []string{"util.ts", "index.ts"}
			},
		},
		{
			name:    "rust",
			command: commandFromPath(t, "rust-analyzer"),
			setup: func(t *testing.T, root string) (string, int, int, []string) {
				writeFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"integration\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
				if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
					t.Fatalf("mkdir src: %v", err)
				}
				writeFile(t, filepath.Join(root, "src", "lib.rs"), "pub fn greet(name: &str) -> String { name.to_string() }\n")
				mainPath := filepath.Join(root, "src", "main.rs")
				writeFile(t, mainPath, "use integration::greet;\n\nfn main() {\n    let _ = greet(\"world\");\n}\n")
				return mainPath, 3, 12, []string{"lib.rs"}
			},
		},
		{
			name:    "shell",
			command: commandFromPath(t, "bash-language-server", "start"),
			setup: func(t *testing.T, root string) (string, int, int, []string) {
				writeFile(t, filepath.Join(root, "lib.sh"), "greet() {\n  echo \"$1\"\n}\n")
				mainPath := filepath.Join(root, "main.sh")
				writeFile(t, mainPath, "source ./lib.sh\n\ngreet world\n")
				return mainPath, 2, 2, []string{"lib.sh", "main.sh"}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.command) == 0 {
				if strict {
					t.Fatalf("%s language server not available", tc.name)
				}
				t.Skip("language server not available")
			}

			root := t.TempDir()
			path, line, col, wantFiles := tc.setup(t, root)
			client, err := NewClient(Config{Command: tc.command, RootDir: root})
			if err != nil {
				if strict {
					t.Fatalf("NewClient failed: %v", err)
				}
				t.Skipf("language server could not start: %v", err)
			}
			defer client.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := client.Initialize(ctx); err != nil {
				if strict {
					t.Fatalf("Initialize failed: %v", err)
				}
				t.Skipf("language server could not initialize: %v", err)
			}

			defs := waitDefinition(t, ctx, client, path, line, col)
			if !locationsContainAnyFile(defs, wantFiles) {
				t.Fatalf("definition did not include one of %v: %+v", wantFiles, defs)
			}
		})
	}
}

func commandFromPath(t *testing.T, name string, args ...string) []string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		return nil
	}
	if name == "rust-analyzer" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, path, "--version").Run(); err != nil {
			return nil
		}
	}
	return append([]string{path}, args...)
}

func waitDefinition(t *testing.T, ctx context.Context, client *Client, path string, line, col int) []Location {
	t.Helper()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		defs, err := client.Definition(ctx, path, line, col)
		if err == nil && len(defs) > 0 {
			return defs
		}
		lastErr = err
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for definition, last error: %v", lastErr)
		case <-ticker.C:
		}
	}
}

func locationsContainAnyFile(locations []Location, suffixes []string) bool {
	for _, location := range locations {
		path, err := URIToPath(location.URI)
		if err != nil {
			path = location.URI
		}
		for _, suffix := range suffixes {
			if strings.HasSuffix(filepath.ToSlash(path), suffix) {
				return true
			}
		}
	}
	return false
}
