package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// DiffComputer computes unified diffs for file changes.
type DiffComputer struct {
	contextLines int // default 3
	maxDiffLines int // default 500
}

// NewDiffComputer creates a DiffComputer with default settings.
func NewDiffComputer() *DiffComputer {
	return &DiffComputer{
		contextLines: 3,
		maxDiffLines: 500,
	}
}

// FileChangeDiff is the diff result for a single file.
type FileChangeDiff struct {
	Path       string // project-relative, forward slashes
	ChangeType string // "added", "modified", "deleted"
	Diff       string // unified diff content
	Language   string // inferred from extension
	Truncated  bool   // true if diff exceeded maxDiffLines
	TotalLines int    // total diff lines before truncation
	Error      string // non-empty if diff computation failed
}

// ComputeFileDiffs generates diffs for all files in a task result.
// Uses snapshots for modified files, full-content for created/deleted files.
// Files in snapshots but missing on disk are treated as deleted.
func (dc *DiffComputer) ComputeFileDiffs(
	projectPath string,
	snapshots *FileSnapshotStore,
	filesModified []string,
	filesCreated []string,
) []FileChangeDiff {
	var results []FileChangeDiff

	// Process modified files.
	for _, fp := range filesModified {
		diff := dc.computeModifiedDiff(projectPath, snapshots, fp)
		if diff != nil {
			results = append(results, *diff)
		}
	}

	// Process created files.
	for _, fp := range filesCreated {
		diff := dc.computeCreatedDiff(projectPath, fp)
		if diff != nil {
			results = append(results, *diff)
		}
	}

	// Check for files in snapshot that are missing on disk (deleted).
	dc.appendDeletedFiles(projectPath, snapshots, filesModified, filesCreated, &results)

	return results
}

// computeModifiedDiff computes a diff for a modified file by comparing
// snapshot content with current disk content.
func (dc *DiffComputer) computeModifiedDiff(projectPath string, snapshots *FileSnapshotStore, fp string) *FileChangeDiff {
	absPath := fp
	if !filepath.IsAbs(fp) {
		absPath = filepath.Join(projectPath, fp)
	}
	absPath = filepath.Clean(absPath)

	relPath := NormalizeFilePathForEvent(absPath, projectPath)
	if relPath == "" {
		return nil
	}

	// Get pre-modification snapshot.
	snap, ok := snapshots.GetSnapshot(absPath)
	if !ok {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "modified",
			Language:   inferLanguage(relPath),
			Error:      "no snapshot available",
		}
	}

	// Handle snapshot errors.
	if snap.Error != "" {
		if snap.Error == "binary" {
			return &FileChangeDiff{
				Path:       relPath,
				ChangeType: "modified",
				Diff:       "Binary file changed",
				Language:   inferLanguage(relPath),
			}
		}
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "modified",
			Language:   inferLanguage(relPath),
			Error:      fmt.Sprintf("snapshot error: %s", snap.Error),
		}
	}

	// Read current file content.
	currentContent, err := os.ReadFile(absPath)
	if err != nil {
		// File no longer exists — treat as deleted (requirement 2.4).
		return dc.buildFullDeletionDiff(relPath, snap.Content)
	}

	// Check if current content is binary.
	if IsBinaryFile(currentContent) {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "modified",
			Diff:       "Binary file changed",
			Language:   inferLanguage(relPath),
		}
	}

	currentStr := string(currentContent)

	// No change.
	if snap.Content == currentStr {
		return nil
	}

	// Compute unified diff.
	diff, err := dc.computeUnifiedDiff(relPath, snap.Content, currentStr)
	if err != nil {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "modified",
			Language:   inferLanguage(relPath),
			Error:      fmt.Sprintf("diff computation failed: %v", err),
		}
	}

	result := &FileChangeDiff{
		Path:       relPath,
		ChangeType: "modified",
		Diff:       diff,
		Language:   inferLanguage(relPath),
	}

	dc.applyTruncation(result)
	return result
}

// computeCreatedDiff generates a full-addition diff for a newly created file.
func (dc *DiffComputer) computeCreatedDiff(projectPath string, fp string) *FileChangeDiff {
	absPath := fp
	if !filepath.IsAbs(fp) {
		absPath = filepath.Join(projectPath, fp)
	}
	absPath = filepath.Clean(absPath)

	relPath := NormalizeFilePathForEvent(absPath, projectPath)
	if relPath == "" {
		return nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "added",
			Language:   inferLanguage(relPath),
			Error:      fmt.Sprintf("unable to read file: %v", err),
		}
	}

	// Check if binary.
	if IsBinaryFile(content) {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "added",
			Diff:       "Binary file changed",
			Language:   inferLanguage(relPath),
		}
	}

	// Generate full-addition diff.
	diff, err := dc.computeUnifiedDiff(relPath, "", string(content))
	if err != nil {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "added",
			Language:   inferLanguage(relPath),
			Error:      fmt.Sprintf("diff computation failed: %v", err),
		}
	}

	result := &FileChangeDiff{
		Path:       relPath,
		ChangeType: "added",
		Diff:       diff,
		Language:   inferLanguage(relPath),
	}

	dc.applyTruncation(result)
	return result
}

// appendDeletedFiles checks for files in the snapshot store that are missing
// on disk (not in filesModified or filesCreated) and generates deletion diffs.
func (dc *DiffComputer) appendDeletedFiles(
	projectPath string,
	snapshots *FileSnapshotStore,
	filesModified []string,
	filesCreated []string,
	results *[]FileChangeDiff,
) {
	if snapshots == nil {
		return
	}

	// Build a set of files already processed.
	processed := make(map[string]bool)
	for _, fp := range filesModified {
		absPath := fp
		if !filepath.IsAbs(fp) {
			absPath = filepath.Join(projectPath, fp)
		}
		processed[filepath.Clean(absPath)] = true
	}
	for _, fp := range filesCreated {
		absPath := fp
		if !filepath.IsAbs(fp) {
			absPath = filepath.Join(projectPath, fp)
		}
		processed[filepath.Clean(absPath)] = true
	}

	// Iterate over snapshots to find files that no longer exist on disk.
	snapshots.mu.RLock()
	defer snapshots.mu.RUnlock()

	for absPath, snap := range snapshots.snapshots {
		if processed[absPath] {
			continue
		}
		// Skip snapshots that had errors during capture.
		if snap.Error != "" {
			continue
		}

		// Check if file still exists on disk.
		if _, err := os.Stat(absPath); err == nil {
			continue // File still exists, not deleted.
		}

		relPath := NormalizeFilePathForEvent(absPath, projectPath)
		if relPath == "" {
			continue
		}

		diff := dc.buildFullDeletionDiff(relPath, snap.Content)
		*results = append(*results, *diff)
	}
}

// buildFullDeletionDiff generates a full-deletion diff from the given content.
func (dc *DiffComputer) buildFullDeletionDiff(relPath string, content string) *FileChangeDiff {
	diff, err := dc.computeUnifiedDiff(relPath, content, "")
	if err != nil {
		return &FileChangeDiff{
			Path:       relPath,
			ChangeType: "deleted",
			Language:   inferLanguage(relPath),
			Error:      fmt.Sprintf("diff computation failed: %v", err),
		}
	}

	result := &FileChangeDiff{
		Path:       relPath,
		ChangeType: "deleted",
		Diff:       diff,
		Language:   inferLanguage(relPath),
	}

	dc.applyTruncation(result)
	return result
}

// computeUnifiedDiff generates a unified diff string between two text contents.
func (dc *DiffComputer) computeUnifiedDiff(filePath, original, modified string) (string, error) {
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: "a/" + filePath,
		ToFile:   "b/" + filePath,
		Context:  dc.contextLines,
	}

	return difflib.GetUnifiedDiffString(ud)
}

// applyTruncation truncates the diff if it exceeds maxDiffLines.
// Sets Truncated=true and TotalLines when truncation occurs.
func (dc *DiffComputer) applyTruncation(result *FileChangeDiff) {
	if result.Diff == "" {
		return
	}

	lines := strings.Split(result.Diff, "\n")
	totalLines := len(lines)

	// Remove trailing empty line from split (if diff ends with \n).
	if totalLines > 0 && lines[totalLines-1] == "" {
		totalLines--
	}

	result.TotalLines = totalLines

	if totalLines > dc.maxDiffLines {
		result.Truncated = true
		truncated := lines[:dc.maxDiffLines]
		result.Diff = strings.Join(truncated, "\n") + fmt.Sprintf("\n... [truncated: showing %d of %d lines]", dc.maxDiffLines, totalLines)
	}
}

// inferLanguage maps file extensions to language identifiers.
func inferLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return "plaintext"
	}
	// Remove leading dot.
	ext = ext[1:]

	langMap := map[string]string{
		"go":     "go",
		"ts":     "typescript",
		"tsx":    "typescriptreact",
		"js":     "javascript",
		"jsx":    "javascriptreact",
		"py":     "python",
		"rb":     "ruby",
		"rs":     "rust",
		"java":   "java",
		"kt":     "kotlin",
		"kts":    "kotlin",
		"swift":  "swift",
		"c":      "c",
		"h":      "c",
		"cpp":    "cpp",
		"cc":     "cpp",
		"cxx":    "cpp",
		"hpp":    "cpp",
		"cs":     "csharp",
		"php":    "php",
		"html":   "html",
		"htm":    "html",
		"css":    "css",
		"scss":   "scss",
		"sass":   "sass",
		"less":   "less",
		"json":   "json",
		"xml":    "xml",
		"yaml":   "yaml",
		"yml":    "yaml",
		"toml":   "toml",
		"md":     "markdown",
		"sql":    "sql",
		"sh":     "shellscript",
		"bash":   "shellscript",
		"zsh":    "shellscript",
		"bat":    "bat",
		"cmd":    "bat",
		"ps1":    "powershell",
		"vue":    "vue",
		"svelte": "svelte",
		"dart":   "dart",
		"lua":    "lua",
		"r":      "r",
		"scala":  "scala",
		"ex":     "elixir",
		"exs":    "elixir",
		"erl":    "erlang",
		"hs":     "haskell",
		"tf":     "terraform",
		"proto":  "protobuf",
		"graphql":"graphql",
		"gql":    "graphql",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "plaintext"
}
