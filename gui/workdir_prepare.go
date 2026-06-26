package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func ensureAbsoluteDirectoryPath(rawPath, label string) (string, bool, error) {
	path := normalizeProjectSessionPath(rawPath)
	if path == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(path) {
		return path, false, fmt.Errorf("%s is not absolute: %s", label, path)
	}
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return path, false, fmt.Errorf("%s is not a directory: %s", label, path)
		}
		return path, false, nil
	}
	if !os.IsNotExist(err) {
		return path, false, fmt.Errorf("inspect %s %s: %w", label, path, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return path, false, fmt.Errorf("create %s %s: %w", label, path, err)
	}
	return path, true, nil
}
