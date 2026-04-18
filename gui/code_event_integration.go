package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
		if evt.Type != "file.change" && evt.Type != "file.read" {
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

		fileName := filepath.Base(filePath)
		language := detectLanguageFromExt(fileName)

		opType := "create"
		var original string

		if evt.Type == "file.change" {
			opType = "modify"
			// Attempt to read original content via git show
			original = gitShowOriginal(s.ProjectPath, absPath)
		}

		emitter.EmitCodeFileEvent(CodeFileEvent{
			SessionID: s.ID,
			FilePath:  filePath,
			FileName:  fileName,
			Content:   string(content),
			Original:  original,
			OpType:    opType,
			Language:  language,
		})
	}
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
