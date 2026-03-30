package configfile

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ContextLayer represents one layer of a context file hierarchy.
type ContextLayer struct {
	Level   string // "ROOT", "MODULE: xxx", "LOCAL"
	Path    string // AGENTS.md file path
	Content string // file content
}

// ContextInjector collects and concatenates layered context files (AGENTS.md)
// from the project directory hierarchy.
type ContextInjector struct {
	maxTokens int // total token limit (default 8000)
}

// NewContextInjector creates a new ContextInjector with the given token limit.
// If maxTokens <= 0, defaults to 8000.
func NewContextInjector(maxTokens int) *ContextInjector {
	if maxTokens <= 0 {
		maxTokens = 8000
	}
	return &ContextInjector{maxTokens: maxTokens}
}

// Collect walks from workDir up to projectRoot, collecting AGENTS.md files
// at each directory level. At projectRoot it also merges CLAUDE.md for
// backward compatibility. Returns layers sorted root-to-leaf.
func (c *ContextInjector) Collect(workDir, projectRoot string) []ContextLayer {
	workDir = filepath.Clean(workDir)
	projectRoot = filepath.Clean(projectRoot)

	// Gather directories from workDir up to projectRoot (inclusive).
	var dirs []string
	cur := workDir
	for {
		dirs = append(dirs, cur)
		if pathEqual(cur, projectRoot) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without hitting projectRoot.
			// Append projectRoot so we still check it.
			dirs = append(dirs, projectRoot)
			break
		}
		cur = parent
	}

	// dirs is leaf-to-root; reverse to root-to-leaf.
	reverse(dirs)

	var layers []ContextLayer
	for i, dir := range dirs {
		isRoot := pathEqual(dir, projectRoot)
		isLocal := pathEqual(dir, workDir)

		var content string

		// At root level, merge CLAUDE.md first (backward compat), then AGENTS.md.
		if isRoot {
			claudeContent := readFileContent(filepath.Join(dir, "CLAUDE.md"))
			agentsContent := readFileContent(filepath.Join(dir, "AGENTS.md"))
			content = mergeContent(claudeContent, agentsContent)
		} else {
			content = readFileContent(filepath.Join(dir, "AGENTS.md"))
		}

		if content == "" {
			continue
		}

		level := c.classifyLevel(dir, projectRoot, isRoot, isLocal, i == 0 && isRoot && isLocal)
		var path string
		if isRoot {
			// Prefer AGENTS.md path if it exists, else CLAUDE.md.
			if fileExists(filepath.Join(dir, "AGENTS.md")) {
				path = filepath.Join(dir, "AGENTS.md")
			} else {
				path = filepath.Join(dir, "CLAUDE.md")
			}
		} else {
			path = filepath.Join(dir, "AGENTS.md")
		}

		layers = append(layers, ContextLayer{
			Level:   level,
			Path:    path,
			Content: content,
		})
	}

	return layers
}

// classifyLevel determines the layer label for a directory.
func (c *ContextInjector) classifyLevel(dir, projectRoot string, isRoot, isLocal, isSameDir bool) string {
	if isSameDir {
		// workDir == projectRoot: label as ROOT
		return "ROOT"
	}
	if isRoot {
		return "ROOT"
	}
	if isLocal {
		return "LOCAL"
	}
	// Middle layer: MODULE with relative path from projectRoot.
	rel, err := filepath.Rel(projectRoot, dir)
	if err != nil {
		rel = filepath.Base(dir)
	}
	return "MODULE: " + filepath.ToSlash(rel)
}

// Build concatenates layers with markers into a single context string.
// If the result exceeds the token limit, it keeps ROOT and LOCAL layers
// and truncates middle layers one by one from the innermost.
func (c *ContextInjector) Build(layers []ContextLayer) string {
	if len(layers) == 0 {
		return ""
	}

	// Try building all layers first.
	result := buildAll(layers)
	if estimateTokens(result) <= c.maxTokens {
		return result
	}

	// Exceeds limit — separate into root, middle, local.
	var root, local *ContextLayer
	var middle []ContextLayer

	for i := range layers {
		switch {
		case layers[i].Level == "ROOT":
			root = &layers[i]
		case layers[i].Level == "LOCAL":
			local = &layers[i]
		default:
			middle = append(middle, layers[i])
		}
	}

	// Remove middle layers from innermost (last) to outermost (first)
	// until we fit within the token budget.
	for len(middle) > 0 {
		kept := rebuildWithMiddle(root, middle, local)
		if estimateTokens(kept) <= c.maxTokens {
			return kept
		}
		// Drop the innermost (last) middle layer.
		middle = middle[:len(middle)-1]
	}

	// Only ROOT + LOCAL remain.
	return rebuildWithMiddle(root, nil, local)
}

// buildAll concatenates all layers with markers.
func buildAll(layers []ContextLayer) string {
	var sb strings.Builder
	for i, l := range layers {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "--- [%s] ---\n%s\n", l.Level, l.Content)
	}
	return sb.String()
}

// rebuildWithMiddle builds the context string from root + middle + local.
func rebuildWithMiddle(root *ContextLayer, middle []ContextLayer, local *ContextLayer) string {
	var parts []ContextLayer
	if root != nil {
		parts = append(parts, *root)
	}
	parts = append(parts, middle...)
	if local != nil {
		parts = append(parts, *local)
	}
	return buildAll(parts)
}

// estimateTokens estimates the token count of a string.
// Uses the simple heuristic: len(content) / 2.
func estimateTokens(s string) int {
	return len(s) / 2
}

// readFileContent reads a file and returns its content as a string.
// Returns empty string if the file doesn't exist or can't be read.
func readFileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[context-injector] warning: failed to read %s: %v", path, err)
		}
		return ""
	}
	return strings.TrimSpace(string(data))
}

// mergeContent merges two content strings, skipping empty ones.
func mergeContent(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n\n" + b
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pathEqual compares two paths after cleaning.
func pathEqual(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// reverse reverses a string slice in place.
func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
