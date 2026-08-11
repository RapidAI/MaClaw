package main

// coding_subagent_project_path.go keeps the workflow project root immutable
// and extracts declared absolute paths solely for scope approval. Historical
// root expansion from model-generated task text is deliberately forbidden:
// task text must never widen a workflow security boundary.

import (
	"path/filepath"
	"regexp"
	"strings"
)

// resolveEffectiveProjectPathForTask returns the workflow's frozen project
// root. Model-generated task text must never widen that security boundary.
func resolveEffectiveProjectPathForTask(_ *TaskItem, declaredProjectPath string) string {
	return declaredProjectPath
}

// taskReferencesOutsideProjectPath reports whether a task names an absolute
// path outside the frozen root. Relative paths are not evidence for a root
// change.
func taskReferencesOutsideProjectPath(task *TaskItem, declaredProjectPath string) bool {
	absPaths := collectTaskAbsolutePaths(task)
	return len(absPaths) > 0 && !allPathsWithinDir(absPaths, declaredProjectPath)
}

// collectTaskAbsolutePaths extracts all absolute file paths from a task's
// Files field and Description text.
func collectTaskAbsolutePaths(task *TaskItem) []string {
	if task == nil {
		return nil
	}

	seen := make(map[string]bool)
	var result []string

	addIfAbsolute := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		// Remove common surrounding characters from markdown.
		path = strings.Trim(path, "`\"'")
		if !filepath.IsAbs(path) {
			return
		}
		clean := filepath.Clean(path)
		if !seen[strings.ToLower(clean)] {
			seen[strings.ToLower(clean)] = true
			result = append(result, clean)
		}
	}

	// From task.Files
	for _, f := range task.Files {
		addIfAbsolute(f)
	}

	// From task.Description — extract absolute paths using pattern matching.
	for _, path := range extractAbsolutePathsFromText(task.Description) {
		addIfAbsolute(path)
	}

	// From task.Title — may contain path reference.
	for _, path := range extractAbsolutePathsFromText(task.Title) {
		addIfAbsolute(path)
	}

	return result
}

// Windows path pattern: drive letter + colon + backslash/slash + path chars.
// Must handle paths with spaces (e.g. "D:\AI learning\AI coding\file.py").
// Strategy: match drive letter + path chars including spaces, then trim to
// the last path-valid segment (file extension or path separator boundary).
//
// Unix path pattern: starts with known top-level directory.
var taskAbsPathPatternWindows = regexp.MustCompile(`(?i)([a-z]:[\\/][^\r\n\t"'<>|*?]+)`)
var taskAbsPathPatternUnix = regexp.MustCompile(`(/(?:home|usr|opt|tmp|var|etc|mnt|media)/[^\s\r\n\t"'<>|*?]+)`)

// extractAbsolutePathsFromText finds all absolute file paths mentioned in text.
func extractAbsolutePathsFromText(text string) []string {
	if text == "" {
		return nil
	}
	var paths []string

	// Windows paths (may contain spaces).
	for _, match := range taskAbsPathPatternWindows.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		path := trimWindowsPathFromText(match[1])
		if path != "" {
			paths = append(paths, path)
		}
	}

	// Unix paths (no spaces in typical Unix paths).
	for _, match := range taskAbsPathPatternUnix.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		path := strings.TrimRight(match[1], " .,:;)]}。，；：、）】》")
		if path != "" {
			paths = append(paths, path)
		}
	}

	return paths
}

// trimWindowsPathFromText trims a Windows path extracted by the greedy regex.
// The regex captures everything after "X:\" until end of line, so we need to
// find where the actual path ends and natural language continues.
//
// Heuristic: Find the last file extension (.py, .go, etc.) or path separator
// in the captured string. Content after a file extension + whitespace is
// natural language.
func trimWindowsPathFromText(raw string) string {
	// First, strip trailing punctuation/Chinese characters that are clearly not path.
	path := strings.TrimRight(raw, " \t.,:;)]}"+"\u3002\uff0c\uff1b\uff1a\u3001\uff09\u3011\u300b\u201c\u201d\u2018\u2019")

	// Look for file extension followed by whitespace — that's where the path ends.
	// Common pattern: "D:\dir\file.py 中的检测逻辑"
	extEndIdx := findFileExtensionEnd(path)
	if extEndIdx > 0 {
		return filepath.Clean(path[:extEndIdx])
	}

	// If path ends with a separator, it's a directory reference.
	if strings.HasSuffix(path, `\`) || strings.HasSuffix(path, `/`) {
		return filepath.Clean(path)
	}

	// No file extension found — look for natural language continuation.
	lastSep := strings.LastIndexAny(path, `\/`)
	if lastSep < 0 {
		return filepath.Clean(path)
	}

	lastComponent := path[lastSep+1:]
	if containsNaturalLanguageContinuation(lastComponent) {
		path = trimToLastValidPathComponent(path)
	}

	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// findFileExtensionEnd finds the end position of a file extension in the path.
// Returns the position right after the extension (e.g., for "file.py rest",
// returns the index of the space after ".py"). Returns 0 if no extension found.
func findFileExtensionEnd(path string) int {
	// Scan for patterns like ".ext" followed by non-path character or end.
	for i := len(path) - 1; i > 0; i-- {
		if path[i] == '.' {
			// Found a dot — check if what follows is a valid extension.
			extStart := i
			extEnd := i + 1
			for extEnd < len(path) && isExtChar(path[extEnd]) {
				extEnd++
			}
			ext := strings.ToLower(path[extStart:extEnd])
			if isKnownFileExtension(ext) {
				return extEnd
			}
		}
	}
	return 0
}

func isExtChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isKnownFileExtension(ext string) bool {
	switch ext {
	case ".py", ".go", ".js", ".ts", ".jsx", ".tsx", ".java", ".c", ".cpp", ".h",
		".rs", ".rb", ".php", ".swift", ".kt", ".cs", ".r", ".m", ".sh", ".bat",
		".cmd", ".ps1", ".yaml", ".yml", ".json", ".xml", ".toml", ".ini", ".cfg",
		".conf", ".md", ".txt", ".log", ".csv", ".html", ".css", ".scss", ".sql",
		".dockerfile", ".proto", ".graphql", ".vue", ".svelte", ".pdf", ".docx",
		".xlsx", ".pptx", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".mp3", ".mp4":
		return true
	}
	return false
}

// containsNaturalLanguageContinuation checks if text contains patterns
// suggesting it's natural language rather than a path component.
func containsNaturalLanguageContinuation(text string) bool {
	// Chinese sentence-continuation markers.
	nlMarkers := []string{"的", "中", "文件", "进行", "修改", "优化", "添加", "删除", "更新", "查看"}
	lower := strings.ToLower(text)
	for _, marker := range nlMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// trimToLastValidPathComponent walks backward through the path to find
// the last segment that looks like a valid filename or directory name.
func trimToLastValidPathComponent(path string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '\\' || r == '/'
	})
	// Walk backward, keep segments until we find one with NL markers.
	for i := len(parts) - 1; i >= 1; i-- {
		if containsNaturalLanguageContinuation(parts[i]) {
			// Reconstruct path without this and subsequent segments.
			// Find the position of this segment in the original path.
			idx := strings.LastIndex(path, parts[i])
			if idx > 0 {
				// Go back to the separator before this segment.
				candidate := strings.TrimRight(path[:idx], `\/`)
				if candidate != "" {
					return candidate
				}
			}
		} else {
			// This segment looks like a valid path component — keep everything up to here.
			break
		}
	}
	return path
}

// allPathsWithinDir checks if all paths are within the given directory.
// Uses simple string prefix comparison on cleaned absolute paths (no I/O).
func allPathsWithinDir(paths []string, dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absDir = filepath.Clean(absDir)
	dirLower := strings.ToLower(absDir)

	for _, p := range paths {
		absP, err := filepath.Abs(p)
		if err != nil {
			return false
		}
		absP = filepath.Clean(absP)
		pLower := strings.ToLower(absP)

		// Check prefix relationship.
		if !strings.HasPrefix(pLower, dirLower+string(filepath.Separator)) && pLower != dirLower {
			return false
		}
	}
	return true
}

// commonAncestorDir finds the deepest common parent directory of all paths.
func commonAncestorDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	// Normalize all paths to absolute cleaned form.
	var cleaned []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Use the directory of the path (not the file itself).
		cleaned = append(cleaned, filepath.Dir(filepath.Clean(abs)))
	}
	if len(cleaned) == 0 {
		return ""
	}

	// Start with the first path's components, progressively narrow.
	ancestor := cleaned[0]
	for _, p := range cleaned[1:] {
		ancestor = commonPrefix(ancestor, p)
		if ancestor == "" {
			return ""
		}
	}
	return ancestor
}

// commonPrefix finds the longest common directory prefix between two paths.
// Returns the common ancestor directory (not a partial path component).
func commonPrefix(a, b string) string {
	// On Windows, comparison must be case-insensitive.
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)

	aParts := strings.Split(filepath.ToSlash(aLower), "/")
	bParts := strings.Split(filepath.ToSlash(bLower), "/")

	// Use original case from 'a' for the result.
	aOrigParts := strings.Split(filepath.ToSlash(a), "/")

	n := len(aParts)
	if len(bParts) < n {
		n = len(bParts)
	}

	var commonParts []string
	for i := 0; i < n; i++ {
		if aParts[i] != bParts[i] {
			break
		}
		commonParts = append(commonParts, aOrigParts[i])
	}

	if len(commonParts) == 0 {
		return ""
	}
	return filepath.FromSlash(strings.Join(commonParts, "/"))
}

// isRootOrNearRoot checks if a path is a filesystem root or just one level deep.
// Examples of paths that return true: "C:\", "/", "D:\Users", "/home"
func isRootOrNearRoot(path string) bool {
	cleaned := filepath.Clean(path)
	vol := filepath.VolumeName(cleaned)

	// Remove volume prefix for depth counting.
	rest := cleaned[len(vol):]
	rest = strings.TrimPrefix(rest, string(filepath.Separator))

	if rest == "" {
		return true // root itself
	}

	// Count path separators to determine depth.
	parts := strings.Split(rest, string(filepath.Separator))
	return len(parts) <= 1
}
