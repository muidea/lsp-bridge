package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
)

func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}
	return u.String()
}

func URIToPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse uri: %w", err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported uri scheme: %s", u.Scheme)
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", fmt.Errorf("unescape uri path: %w", err)
	}
	return filepath.Clean(path), nil
}
