package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Project instruction files loaded for pure-coding workbench (Codex/Claude-style).
// First non-empty match wins per priority group; multiple groups are concatenated.
var codingWorkbenchAgentFileCandidates = [][]string{
	{"AGENTS.md", "agents.md"},
	{"CLAUDE.md", "Claude.md", "claude.md"},
	{filepath.Join(".maclaw", "AGENTS.md"), filepath.Join(".maclaw", "agents.md")},
	{filepath.Join(".maclaw", "CLAUDE.md")},
	{"CODEX.md", "codex.md"},
	{".cursorrules"},
}

const (
	codingWorkbenchProjectInstructionsMaxRunes = 4000
	codingWorkbenchSingleAgentFileMaxRunes     = 2500
)

// loadCodingWorkbenchProjectInstructions reads AGENTS.md / CLAUDE.md / .maclaw
// project instructions from projectPath. Returns empty string when none found.
func loadCodingWorkbenchProjectInstructions(projectPath string) (content string, sources []string) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", nil
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return "", nil
	}

	var parts []string
	for _, group := range codingWorkbenchAgentFileCandidates {
		text, src := readFirstExistingAgentFile(projectPath, group)
		if text == "" {
			continue
		}
		sources = append(sources, src)
		parts = append(parts, fmt.Sprintf("### From %s\n%s", src, text))
		// Cap total parts to avoid blowing context.
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	joined := strings.Join(parts, "\n\n")
	if utf8.RuneCountInString(joined) > codingWorkbenchProjectInstructionsMaxRunes {
		joined = truncateRunesForSubAgent(joined, codingWorkbenchProjectInstructionsMaxRunes)
	}
	return strings.TrimSpace(joined), sources
}

func readFirstExistingAgentFile(projectPath string, names []string) (content, relative string) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		full := filepath.Join(projectPath, name)
		data, err := os.ReadFile(full)
		if err != nil || len(data) == 0 {
			continue
		}
		// Skip huge/binary-looking files.
		if len(data) > 256*1024 {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		if looksBinaryText(text) {
			continue
		}
		if utf8.RuneCountInString(text) > codingWorkbenchSingleAgentFileMaxRunes {
			text = truncateRunesForSubAgent(text, codingWorkbenchSingleAgentFileMaxRunes)
		}
		return text, filepath.ToSlash(name)
	}
	return "", ""
}

func looksBinaryText(s string) bool {
	// Heuristic: NUL or high ratio of non-printable bytes in first 512.
	n := len(s)
	if n > 512 {
		n = 512
	}
	nonPrint := 0
	for i := 0; i < n; i++ {
		c := s[i]
		if c == 0 {
			return true
		}
		if c < 9 || (c > 13 && c < 32) {
			nonPrint++
		}
	}
	return nonPrint > n/10
}

// formatProjectInstructionsForContext wraps instructions for prevOutputs/designCtx.
func formatProjectInstructionsForContext(content string, sources []string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Project instructions (must follow; from ")
	if len(sources) == 0 {
		b.WriteString("AGENTS.md/CLAUDE.md")
	} else {
		b.WriteString(strings.Join(sources, ", "))
	}
	b.WriteString("):\n")
	b.WriteString(content)
	return b.String()
}

// agentsInstructionsCache avoids re-reading AGENTS.md every turn when mtimes
// are unchanged (project path → {sig, content, sources, loadedAt}).
type agentsInstructionsCacheEntry struct {
	sig     string
	content string
	sources []string
}

var agentsInstructionsCache sync.Map // projectPath → agentsInstructionsCacheEntry

func agentsInstructionsSig(projectPath string) string {
	// Hash mtimes of candidate files (cheap Stat, no full read).
	var b strings.Builder
	for _, group := range codingWorkbenchAgentFileCandidates {
		for _, name := range group {
			full := filepath.Join(projectPath, name)
			fi, err := os.Stat(full)
			if err != nil {
				continue
			}
			b.WriteString(name)
			b.WriteByte(':')
			b.WriteString(fi.ModTime().UTC().Format(time.RFC3339Nano))
			b.WriteByte(';')
			b.WriteString(fmt.Sprintf("%d", fi.Size()))
			b.WriteByte('|')
		}
	}
	return b.String()
}

// ensureStickyProjectInstructions refreshes AGENTS.md content on sticky memory
// when project path is known. Cheap when file mtime/content unchanged.
func (h *IMMessageHandler) ensureStickyProjectInstructions(userID, projectPath string) string {
	if h == nil {
		return ""
	}
	userID = strings.TrimSpace(userID)
	projectPath = strings.TrimSpace(projectPath)
	if userID == "" || projectPath == "" {
		return ""
	}
	sig := agentsInstructionsSig(projectPath)
	var content string
	var sources []string
	cacheHit := false
	if raw, ok := agentsInstructionsCache.Load(projectPath); ok {
		if ent, ok := raw.(agentsInstructionsCacheEntry); ok && ent.sig == sig {
			content, sources = ent.content, ent.sources
			cacheHit = true
		}
	}
	if !cacheHit {
		content, sources = loadCodingWorkbenchProjectInstructions(projectPath)
		agentsInstructionsCache.Store(projectPath, agentsInstructionsCacheEntry{
			sig:     sig,
			content: content,
			sources: append([]string(nil), sources...),
		})
	}

	// Fast path: unchanged content → no sticky write.
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if content != "" {
		if mem.ProjectInstructions == content && strings.Join(mem.ProjectInstructionSources, ",") == strings.Join(sources, ",") {
			return content
		}
		h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
			m.ProjectInstructions = content
			m.ProjectInstructionSources = sources
			if m.ProjectPath == "" {
				m.ProjectPath = projectPath
			}
		})
		return content
	}
	// Clear stale instructions if files were removed.
	if strings.TrimSpace(mem.ProjectInstructions) != "" {
		h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
			m.ProjectInstructions = ""
			m.ProjectInstructionSources = nil
		})
	}
	return ""
}
