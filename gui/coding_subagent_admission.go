package main

import (
	"os"
	"path/filepath"
	"strings"

	coreintent "github.com/RapidAI/CodeClaw/corelib/intent"
)

type codingSubAgentAdmissionInput struct {
	Text                        string
	OwnerID                     string
	ProjectPath                 string
	InteractionContinuation     bool
	RequireExistingCodeEvidence bool
}

func (h *IMMessageHandler) codingSubAgentAdmissionBlockReason(input codingSubAgentAdmissionInput) string {
	text := strings.TrimSpace(input.Text)
	if input.InteractionContinuation {
		return "interaction continuations must stay in the current task context"
	}
	hasCodeContext := hasExplicitCodingImplementationContext(text)
	if browserPublicationExecutionContext(text) && !hasCodeContext {
		return "current request is a browser publication/login/submit task"
	}
	if looksLikeBrowserPublicationFollowUp(text) && !hasCodeContext {
		if h.recentConversationHasBrowserPublicationContext(input.OwnerID) {
			return "follow-up belongs to a browser publication task"
		}
	}
	if input.RequireExistingCodeEvidence && !projectPathHasExistingCodeEvidence(input.ProjectPath) {
		return "no existing code project evidence at " + strings.TrimSpace(input.ProjectPath)
	}
	return ""
}

func browserPublicationExecutionContext(text string) bool {
	return coreintent.BrowserPublicationAffordance(text)
}

func looksLikeBrowserPublicationFollowUp(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}
	markers := []string{
		"submit", "submitted", "publish", "published", "post", "posted", "login", "sign in", "disappear", "disappeared", "vanish", "vanished",
		"\u63d0\u4ea4", "\u53d1\u5e03", "\u53d1\u8868", "\u767b\u5f55", "\u767b\u5165", "\u6d88\u5931", "\u6ca1\u6709\u6210\u529f", "\u6ca1\u6210\u529f", "\u5931\u8d25", "\u5931\u6548",
	}
	for _, marker := range markers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	if strings.Contains(msg, "\u586b\u5145") && (strings.Contains(msg, "\u5185\u5bb9") || strings.Contains(msg, "\u8868\u5355")) {
		return true
	}
	return false
}

func hasExplicitCodingImplementationContext(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}
	englishMarkers := []string{
		"code", "coding", "repo", "repository", "project", "source", "implementation", "app", "frontend", "backend", "api", "build", "compile",
	}
	for _, marker := range englishMarkers {
		if containsASCIITerm(msg, marker) {
			return true
		}
	}
	chineseMarkers := []string{
		"\u4ee3\u7801", "\u9879\u76ee", "\u4ed3\u5e93", "\u6e90\u7801", "\u5b9e\u73b0", "\u524d\u7aef", "\u540e\u7aef", "\u63a5\u53e3", "\u6539\u4ee3\u7801", "\u6784\u5efa", "\u7f16\u8bd1",
	}
	for _, marker := range chineseMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func containsASCIITerm(text, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(text[start:], term)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isASCIIWordByte(text[idx-1])
		after := idx + len(term)
		afterOK := after >= len(text) || !isASCIIWordByte(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

func isASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

func (h *IMMessageHandler) recentConversationHasBrowserPublicationContext(ownerID string) bool {
	if h == nil || h.memory == nil || strings.TrimSpace(ownerID) == "" {
		return false
	}
	entries := h.memory.Load(ownerID)
	seen := 0
	for i := len(entries) - 1; i >= 0 && seen < 8; i-- {
		if entries[i].Role != "user" {
			continue
		}
		seen++
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		if browserPublicationExecutionContext(text) {
			return true
		}
	}
	return false
}

func projectPathHasExistingCodeEvidence(projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return false
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(projectPath)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		if projectEvidenceName(entry.Name()) {
			return true
		}
	}
	found := false
	visited := 0
	_ = filepath.WalkDir(projectPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if path == projectPath {
			return nil
		}
		rel, relErr := filepath.Rel(projectPath, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".maclaw" {
				if name == ".git" {
					found = true
				}
				return filepath.SkipDir
			}
			if depth >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		visited++
		if visited > 300 {
			return filepath.SkipAll
		}
		if projectEvidenceName(d.Name()) || sourceFileExtension(filepath.Ext(d.Name())) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func projectEvidenceName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", "go.mod", "package.json", "pyproject.toml", "requirements.txt", "cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts", "src", "app", "cmd", "pkg", "internal":
		return true
	default:
		return false
	}
}

func sourceFileExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".kt", ".cs", ".cpp", ".cc", ".c", ".h", ".hpp", ".swift", ".php", ".rb":
		return true
	default:
		return false
	}
}
