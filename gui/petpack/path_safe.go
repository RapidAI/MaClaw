package petpack

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// safeRel cleans and rejects absolute or parent-escaping relative paths from manifests.
func safeRel(rel string) string {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "" || clean == "." {
		return ""
	}
	// Absolute, parent escape, or volume paths
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return ""
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return ""
		}
	}
	return clean
}

// pathUnderRoot reports whether full is root or a path strictly inside root.
// On Windows, comparison is case-insensitive (drive letter / user path casing).
func pathUnderRoot(root, full string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)
	fullAbs = filepath.Clean(fullAbs)
	if runtime.GOOS == "windows" {
		rootAbs = strings.ToLower(rootAbs)
		fullAbs = strings.ToLower(fullAbs)
	}
	sep := string(filepath.Separator)
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+sep) {
		return fmt.Errorf("path escapes root")
	}
	return nil
}
