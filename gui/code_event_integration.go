package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// maxCodeFileSize is the maximum file size (1 MB) for code event emission.
// Files larger than this are skipped to avoid performance issues.
const maxCodeFileSize = 1 << 20 // 1 MB

// emitCodeFileEvents processes ImportantEvents from a session and emits
// code:file_update events for file.change and file.read events.
// It reads file content from disk and, for file.change events, attempts
// to retrieve the original content via git show.
func (m *RemoteSessionManager) emitCodeFileEvents(s *RemoteSession, events []ImportantEvent) {
	if m.app == nil || m.app.codeEventEmitter == nil {
		return
	}
	emitter := m.app.codeEventEmitter

	for _, evt := range events {
		eventType := normalizeSummaryEventType(evt.Type)
		if !eventType.IsFileEvent() {
			continue
		}

		filePath := evt.RelatedFile
		if filePath == "" {
			// Try to extract file path from Summary for SDK events
			filePath = extractFilePathFromSummary(evt.Summary)
		}
		if filePath == "" {
			continue
		}

		// Resolve to absolute path using session's project path
		absPath := filePath
		if !filepath.IsAbs(filePath) && s.ProjectPath != "" {
			absPath = filepath.Join(s.ProjectPath, filePath)
		}

		// File size guard: skip files > 1MB
		info, err := os.Stat(absPath)
		if err != nil {
			log.Printf("[code-event] skip %s: stat error: %v", filePath, err)
			continue
		}
		if info.Size() > maxCodeFileSize {
			log.Printf("[code-event] skip %s: file too large (%d bytes)", filePath, info.Size())
			continue
		}

		// Read current file content
		content, err := os.ReadFile(absPath)
		if err != nil {
			log.Printf("[code-event] skip %s: read error: %v", filePath, err)
			continue
		}
		if !isCodePreviewTextContent(content) {
			log.Printf("[code-event] skip %s: binary or invalid UTF-8 content", filePath)
			continue
		}

		fileName := filepath.Base(filePath)
		language := detectLanguageFromExt(fileName)

		opType := "create"
		var original string

		if eventType.IsFileChange() {
			opType = "modify"
			// Attempt to read original content via git show
			original = gitShowOriginal(s.ProjectPath, absPath)
		}

		emitter.EmitCodeFileEvent(CodeFileEvent{
			SessionID:   s.ID,
			FilePath:    filePath,
			FileName:    fileName,
			AbsPath:     absPath,
			Content:     string(content),
			Original:    original,
			OpType:      opType,
			Language:    language,
			ProjectPath: s.ProjectPath,
		})
	}
}

func emitCodingSubAgentCodeFileEvents(app *App, sessionID, projectPath string, filesModified, filesCreated []string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	for _, evt := range buildCodingSubAgentCodeFileEvents(sessionID, projectPath, filesModified, filesCreated) {
		app.codeEventEmitter.EmitCodeFileEvent(evt)
	}
}

func emitCodingSubAgentCodeSessionStart(app *App, sessionID string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	app.codeEventEmitter.EmitSessionStart(sessionID)
}

func emitCodingSubAgentCodeSessionEnd(app *App, sessionID string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	app.codeEventEmitter.EmitSessionEnd(sessionID)
}

func codingSubAgentCodeSessionID(scope, userID string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "subagent"
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return scope
	}
	return scope + ":" + userID
}

var codingSubAgentCodeSessionSeq atomic.Uint64

func newCodingSubAgentCodeSessionID(scope, userID string) string {
	seq := codingSubAgentCodeSessionSeq.Add(1)
	return fmt.Sprintf("%s:%d:%d", codingSubAgentCodeSessionID(scope, userID), time.Now().UnixNano(), seq)
}

func buildCodingSubAgentCodeFileEvents(sessionID, projectPath string, filesModified, filesCreated []string) []CodeFileEvent {
	created := make(map[string]bool, len(filesCreated))
	for _, filePath := range filesCreated {
		if normalized := normalizeSubAgentCodeEventPath(filePath, projectPath); normalized.displayPath != "" {
			created[normalized.displayPath] = true
		}
	}

	inputFiles := append(append([]string{}, filesModified...), filesCreated...)
	seen := make(map[string]bool, len(inputFiles))
	events := make([]CodeFileEvent, 0, len(inputFiles))
	for _, filePath := range inputFiles {
		normalized := normalizeSubAgentCodeEventPath(filePath, projectPath)
		if normalized.displayPath == "" || normalized.absPath == "" || seen[normalized.displayPath] {
			continue
		}
		seen[normalized.displayPath] = true
		if !isSubAgentCodeEventPathInProject(normalized.absPath, projectPath) {
			log.Printf("[code-event] skip subagent file %s: outside project path", normalized.displayPath)
			continue
		}

		info, err := os.Stat(normalized.absPath)
		if err != nil || info.IsDir() {
			if err != nil {
				log.Printf("[code-event] skip subagent file %s: stat error: %v", normalized.displayPath, err)
			}
			continue
		}
		if info.Size() > maxCodeFileSize {
			log.Printf("[code-event] skip subagent file %s: file too large (%d bytes)", normalized.displayPath, info.Size())
			continue
		}

		content, err := os.ReadFile(normalized.absPath)
		if err != nil {
			log.Printf("[code-event] skip subagent file %s: read error: %v", normalized.displayPath, err)
			continue
		}
		if !isCodePreviewTextContent(content) {
			log.Printf("[code-event] skip subagent file %s: binary or invalid UTF-8 content", normalized.displayPath)
			continue
		}

		opType := "modify"
		var original string
		if created[normalized.displayPath] {
			opType = "create"
		} else {
			original = gitShowOriginal(projectPath, normalized.absPath)
		}

		fileName := filepath.Base(normalized.displayPath)
		events = append(events, CodeFileEvent{
			SessionID:   sessionID,
			FilePath:    normalized.displayPath,
			FileName:    fileName,
			AbsPath:     normalized.absPath,
			Content:     string(content),
			Original:    original,
			OpType:      opType,
			Language:    detectLanguageFromExt(fileName),
			ForceOpen:   true,
			ProjectPath: projectPath,
		})
	}
	return events
}

type subAgentCodeEventPath struct {
	displayPath string
	absPath     string
}

func normalizeSubAgentCodeEventPath(filePath, projectPath string) subAgentCodeEventPath {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return subAgentCodeEventPath{}
	}

	absPath := filePath
	if !filepath.IsAbs(absPath) && strings.TrimSpace(projectPath) != "" {
		absPath = filepath.Join(projectPath, filePath)
	}
	absPath = filepath.Clean(absPath)

	displayPath := filepath.Clean(filePath)
	if strings.TrimSpace(projectPath) != "" && filepath.IsAbs(absPath) {
		if rel, err := filepath.Rel(projectPath, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			displayPath = rel
		}
	}
	return subAgentCodeEventPath{
		displayPath: filepath.ToSlash(displayPath),
		absPath:     absPath,
	}
}

func isSubAgentCodeEventPathInProject(absPath, projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return true
	}
	ok, err := isPathWithinDir(absPath, projectPath)
	return err == nil && ok
}

func isCodePreviewTextContent(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	if !utf8.Valid(content) {
		return false
	}
	for _, b := range content {
		if b == 0 {
			return false
		}
	}
	return true
}

// gitShowTimeout is the maximum time allowed for a git show command.
// Prevents blocking the event processing goroutine on slow repos.
const gitShowTimeout = 5 * time.Second

// gitShowOriginal attempts to read the original file content from git HEAD.
// Returns empty string if git is unavailable, the file is new, or the
// command times out.
func gitShowOriginal(projectPath, absPath string) string {
	if projectPath == "" {
		return ""
	}

	relPath, err := filepath.Rel(projectPath, absPath)
	if err != nil {
		return ""
	}
	// Normalize to forward slashes for git
	relPath = filepath.ToSlash(relPath)

	ctx, cancel := context.WithTimeout(context.Background(), gitShowTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "show", fmt.Sprintf("HEAD:%s", relPath))
	cmd.Dir = projectPath
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		// git show fails for new files, non-git repos, or timeout — expected
		return ""
	}
	return string(out)
}

// extractFilePathFromSummary extracts a file path from an event summary string.
// SDK events store the full path in Summary like "Inspected /path/to/file" or
// "Modified /path/to/file".
func extractFilePathFromSummary(summary string) string {
	prefixes := []string{"Inspected ", "Modified ", "Read ", "Edited ", "Created ", "Wrote "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(summary, prefix) {
			path := strings.TrimSpace(strings.TrimPrefix(summary, prefix))
			if path != "" {
				return path
			}
		}
	}
	return ""
}
