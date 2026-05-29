package main

import (
	"path/filepath"
	"strings"
)

// NormalizeFilePathForEvent converts a file path to project-relative,
// forward-slash format. Returns empty string if the path is outside the
// project root or if resolution fails.
//
// The function:
//   - Resolves symlinks via filepath.EvalSymlinks
//   - Cleans the path (removes .. segments)
//   - Converts Windows backslashes to forward slashes
//   - Returns a path relative to projectPath using forward slashes
//   - Returns "" if the resolved path does not start with the resolved project root
func NormalizeFilePathForEvent(filePath, projectPath string) string {
	if filePath == "" || projectPath == "" {
		return ""
	}

	// Resolve symlinks and clean both paths.
	resolvedFile, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		// If symlink resolution fails, fall back to cleaning the path.
		resolvedFile = filepath.Clean(filePath)
	}

	resolvedProject, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		resolvedProject = filepath.Clean(projectPath)
	}

	// Convert to absolute paths if not already.
	if !filepath.IsAbs(resolvedFile) {
		abs, err := filepath.Abs(resolvedFile)
		if err != nil {
			return ""
		}
		resolvedFile = abs
	}
	if !filepath.IsAbs(resolvedProject) {
		abs, err := filepath.Abs(resolvedProject)
		if err != nil {
			return ""
		}
		resolvedProject = abs
	}

	// Check that the file is within the project root.
	// pathHasPrefix (from ve_path_validator.go) handles case-insensitive
	// comparison on Windows and proper directory boundary checks.
	if !pathHasPrefix(resolvedFile, resolvedProject) {
		return ""
	}

	// Compute relative path.
	rel, err := filepath.Rel(resolvedProject, resolvedFile)
	if err != nil {
		return ""
	}

	// Reject if relative path escapes project root (starts with ..).
	if strings.HasPrefix(rel, "..") {
		return ""
	}

	// Convert to forward slashes.
	return filepath.ToSlash(rel)
}

// IsBinaryFile checks if the given content contains null bytes within the
// first 8192 bytes. If a null byte is found, the file is considered binary.
func IsBinaryFile(content []byte) bool {
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}
