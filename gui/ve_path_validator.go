package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// VE Path Validator: validates that requested file paths fall within the
// configured allowed directories. Performs canonical path resolution and
// directory prefix containment checks with defense-in-depth.
//
// Two entry points:
//   - ValidateVEFilePath: for send_file / read_file — requires file to exist and not be a directory
//   - IsWithinAllowedDirs: for list_directory — does NOT require the path to exist
//
// Both resolve paths to canonical absolute form (filepath.EvalSymlinks + filepath.Abs)
// before performing the prefix containment check. On Windows, comparison is
// case-insensitive (strings.EqualFold on normalized paths).
//
// Requirements: 3.3, 3.4, 3.5, 3.6, 5.1, 5.2, 5.3, 5.4, 5.5, 5.6
// ---------------------------------------------------------------------------

// ValidateVEFilePath checks if the given file path is within any of the allowed
// directories. It requires the file to exist and not be a directory.
//
// Returns the canonical path on success, or an error with a Chinese message.
//
// Algorithm:
//  1. Check path is not empty
//  2. Resolve requestedPath to canonical absolute form (EvalSymlinks + Abs)
//  3. Check file exists (Stat) and is not a directory
//  4. For each allowedDir, resolve to canonical form and check prefix containment
//  5. If no match, return error
func ValidateVEFilePath(requestedPath string, allowedDirs []string) (string, error) {
	canonicalPath, _, err := ValidateVEFilePathWithInfo(requestedPath, allowedDirs)
	return canonicalPath, err
}

// ValidateVEFilePathWithInfo is like ValidateVEFilePath but also returns the
// os.FileInfo from the validation stat call. This eliminates the need for
// callers to stat the file again (e.g., for size checks), avoiding both a
// redundant syscall and a TOCTOU window.
func ValidateVEFilePathWithInfo(requestedPath string, allowedDirs []string) (string, os.FileInfo, error) {
	if strings.TrimSpace(requestedPath) == "" {
		return "", nil, fmt.Errorf("[error] path 参数不能为空")
	}

	// Resolve to canonical absolute path (resolves symlinks and .. segments)
	canonicalPath, err := resolveCanonicalPath(requestedPath)
	if err != nil {
		// If EvalSymlinks fails, the file likely doesn't exist or path is invalid
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("[error] 文件不存在: %s", requestedPath)
		}
		return "", nil, fmt.Errorf("[error] 无法解析文件路径: %v", err)
	}

	// Check file exists and is not a directory
	info, err := os.Stat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("[error] 文件不存在: %s", requestedPath)
		}
		return "", nil, fmt.Errorf("[error] 无法解析文件路径: %v", err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("[error] 路径是目录，请使用 list_directory")
	}

	// Check containment within allowed directories
	if !isPathWithinDirs(canonicalPath, allowedDirs) {
		return "", nil, fmt.Errorf("[error] 文件不在允许访问的目录中")
	}

	return canonicalPath, info, nil
}

// IsWithinAllowedDirs checks directory containment without requiring the file
// to exist. Used for list_directory validation where the target may be a
// directory that we want to list.
//
// Returns the canonical path on success, or an error with a Chinese message.
func IsWithinAllowedDirs(requestedPath string, allowedDirs []string) (string, error) {
	if strings.TrimSpace(requestedPath) == "" {
		return "", fmt.Errorf("[error] path 参数不能为空")
	}

	// Try full canonical resolution first (works if path exists).
	canonicalPath, err := resolveCanonicalPath(requestedPath)
	if err != nil {
		// A target may not exist yet (notably write_excel's output file), but
		// an existing ancestor can still be a symbolic link. Resolve the deepest
		// existing ancestor before comparing containment; falling back directly to
		// Abs+Clean would allow "allowed/link/new.xlsx" to escape through a link
		// whose target lies outside the profile boundary.
		canonicalPath, err = resolvePathThroughExistingAncestor(requestedPath)
		if err != nil {
			return "", fmt.Errorf("[error] 无法解析文件路径: %v", err)
		}

		// The target itself may remain absent, so compare against both the
		// canonical and apparent forms of allowed roots. The target path however
		// already incorporates every existing symlink ancestor.
		if !isPathWithinDirsFallback(canonicalPath, allowedDirs) {
			return "", fmt.Errorf("[error] 文件不在允许访问的目录中")
		}
		return canonicalPath, nil
	}

	// Full canonical resolution succeeded — use the standard comparison
	// (both file path and allowed dirs are symlink-resolved).
	if !isPathWithinDirs(canonicalPath, allowedDirs) {
		return "", fmt.Errorf("[error] 文件不在允许访问的目录中")
	}

	return canonicalPath, nil
}

// resolvePathThroughExistingAncestor canonicalizes a path whose leaf (or a
// suffix of it) does not exist. It resolves every symlink in the deepest
// existing ancestor and then appends the non-existent suffix. This preserves
// valid create semantics while preventing a symlinked parent directory from
// bypassing a lexical allowed-directory prefix check.
func resolvePathThroughExistingAncestor(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)
	for ancestor := absPath; ; ancestor = filepath.Dir(ancestor) {
		_, statErr := os.Lstat(ancestor)
		if statErr == nil {
			resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
			if resolveErr != nil {
				return "", resolveErr
			}
			suffix, relErr := filepath.Rel(ancestor, absPath)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Clean(filepath.Join(resolvedAncestor, suffix)), nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return absPath, nil
		}
	}
}

// isPathWithinDirsFallback checks containment when the requested path could not
// be fully canonicalized (file doesn't exist). It compares against both the
// canonical form AND the Abs+Clean form of each allowed directory, ensuring
// that symlinked allowed dirs still match.
func isPathWithinDirsFallback(cleanAbsPath string, allowedDirs []string) bool {
	for _, dir := range allowedDirs {
		if dir == "" {
			continue
		}
		// Try canonical resolution of the allowed dir
		canonicalDir, err := resolveCanonicalPath(dir)
		if err == nil && pathHasPrefix(cleanAbsPath, canonicalDir) {
			return true
		}
		// Also try Abs+Clean of the allowed dir (matches when both sides
		// are unresolved — e.g., allowed dir is a symlink and the requested
		// path is under the symlink's apparent location)
		absDir, absErr := filepath.Abs(dir)
		if absErr == nil {
			cleanDir := filepath.Clean(absDir)
			if pathHasPrefix(cleanAbsPath, cleanDir) {
				return true
			}
		}
	}
	return false
}

// resolveCanonicalPath resolves a path to its canonical absolute form by
// evaluating symlinks and converting to absolute path.
func resolveCanonicalPath(path string) (string, error) {
	// EvalSymlinks resolves all symlinks and also cleans the path.
	// It returns an error if the path doesn't exist.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// Ensure the result is absolute
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// isPathWithinDirs checks if canonicalPath is within any of the allowed directories.
// Each allowed directory is also resolved to canonical form before comparison.
// On Windows, comparison is case-insensitive.
func isPathWithinDirs(canonicalPath string, allowedDirs []string) bool {
	for _, dir := range allowedDirs {
		if dir == "" {
			continue
		}
		// Resolve the allowed directory to canonical form
		canonicalDir, err := resolveCanonicalPath(dir)
		if err != nil {
			// If the allowed dir can't be resolved (e.g., disconnected drive),
			// skip it — we can't verify containment against a non-resolvable dir
			continue
		}

		if pathHasPrefix(canonicalPath, canonicalDir) {
			return true
		}
	}
	return false
}

// CheckVEPathSensitive checks if a file path matches sensitive file patterns.
// This is the integration point between the path validator (directory containment)
// and the sensitive file detection (vePathIsSensitive from app_ve_tools.go).
//
// In the defense-in-depth chain (task 6.4 ExecuteTool), this is called AFTER
// ValidateVEFilePath passes (directory containment confirmed), providing the
// second layer of protection:
//
//  1. Path parameter check (empty/missing) → "[error] path 参数不能为空"
//  2. Directory containment (ValidateVEFilePath) → "[error] 文件不在允许访问的目录中"
//  3. Sensitive file check (CheckVEPathSensitive) → "[error] 该文件包含敏感信息，无法发送"
//  4. File size check (> 50 MB) → "[error] 文件过大"
//  5. Actual file read + send → success or OS error
//
// The check is case-insensitive: .ENV, .Pem, .KEY are all blocked.
//
// Requirements: 8.1, 8.2, 8.3, 8.4
func CheckVEPathSensitive(canonicalPath string) error {
	if vePathIsSensitive(canonicalPath) {
		return fmt.Errorf("[error] 该文件包含敏感信息，无法发送")
	}
	return nil
}

// pathHasPrefix checks if filePath starts with dirPath as a directory prefix.
// On Windows, comparison is case-insensitive.
// Ensures proper directory boundary (e.g., /tmp/evil doesn't match /tmp/ev).
func pathHasPrefix(filePath, dirPath string) bool {
	// Normalize separators to OS-specific
	filePath = filepath.Clean(filePath)
	dirPath = filepath.Clean(dirPath)

	// Ensure dirPath ends with separator for proper prefix matching
	// (prevents /tmp/evil matching /tmp/ev)
	dirWithSep := dirPath
	if !strings.HasSuffix(dirWithSep, string(filepath.Separator)) {
		dirWithSep += string(filepath.Separator)
	}

	if runtime.GOOS == "windows" {
		// Case-insensitive comparison on Windows.
		// Use strings.ToLower for prefix check instead of byte-slicing + EqualFold,
		// because byte-slicing can split multi-byte UTF-8 characters (e.g., Chinese
		// path components like C:\用户\文档\) producing invalid UTF-8 for EqualFold.
		lowerFile := strings.ToLower(filePath)
		lowerDir := strings.ToLower(dirPath)
		lowerDirSep := strings.ToLower(dirWithSep)

		// Check exact match (file is the dir itself) or prefix with separator
		if lowerFile == lowerDir {
			return true
		}
		return strings.HasPrefix(lowerFile, lowerDirSep)
	}

	// Case-sensitive comparison on other platforms
	if filePath == dirPath {
		return true
	}
	return strings.HasPrefix(filePath, dirWithSep)
}
