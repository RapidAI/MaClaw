package agentservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func secureMkdirAll(path string) error {
	return os.MkdirAll(path, 0o700)
}

func secureRemoveAllWithin(root, target string) error {
	root = strings.TrimSpace(root)
	target = strings.TrimSpace(target)
	if root == "" || target == "" {
		return fmt.Errorf("root and target are required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to remove path outside allowed root: %s", absTarget)
	}
	return os.RemoveAll(absTarget)
}
