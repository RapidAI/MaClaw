package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type EventExtractor interface {
	Consume(session *RemoteSession, lines []string) []ImportantEvent
}

type ClaudeEventExtractor struct{}

func NewClaudeEventExtractor() *ClaudeEventExtractor {
	return &ClaudeEventExtractor{}
}

func (e *ClaudeEventExtractor) Consume(session *RemoteSession, lines []string) []ImportantEvent {
	events := make([]ImportantEvent, 0)
	for _, line := range lines {
		if evt := e.detectFileRead(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectFileChanged(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectCommandStarted(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectCommandResult(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectTaskCompleted(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectInputRequired(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
		if evt := e.detectError(session, line); evt != nil {
			events = append(events, *evt)
			continue
		}
	}
	return events
}

func (e *ClaudeEventExtractor) detectFileRead(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if !containsAny(lower, []string{"reading ", "read file", "inspecting ", "opened "}) {
		return nil
	}

	file := extractFilePath(line)
	return newEvent(session, "file.read", "info", "Inspected file", line, file, "")
}

func (e *ClaudeEventExtractor) detectFileChanged(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if !containsAny(lower, []string{"editing ", "modified ", "updated ", "patched ", "created ", "wrote ", "rewrote "}) {
		return nil
	}

	file := extractFilePath(line)
	return newEvent(session, "file.change", "info", "Changed file", line, file, "")
}

func (e *ClaudeEventExtractor) detectCommandStarted(session *RemoteSession, line string) *ImportantEvent {
	command, ok := extractCommand(line)
	if !ok {
		return nil
	}
	return newEvent(session, "command.started", "info", "Running command", line, "", command)
}

func (e *ClaudeEventExtractor) detectInputRequired(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	keywords := []string{
		"need your input",
		"waiting for input",
		"please confirm",
		"continue?",
		"choose an option",
		"approve",
		"yes/no",
		"y/n",
		"do you want to",
		"would you like to",
		"proceed?",
		"accept?",
		"allow?",
		"permission",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "input.required", "warn", "Waiting for your input", line, "", "")
		}
	}
	return nil
}

func (e *ClaudeEventExtractor) detectError(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)
	if containsAny(lower, []string{"0 errors", "without errors", "no error", "error count: 0"}) {
		return nil
	}

	keywords := []string{"error:", "failed", "panic:", "traceback", "exit status", "exception"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "session.error", "error", "Error detected", line, "", "")
		}
	}
	return nil
}

func (e *ClaudeEventExtractor) detectCommandResult(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)

	// If the line starts with an error indicator, let detectError handle it
	if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "error ") {
		return nil
	}

	// Detect successful command completion
	successKeywords := []string{
		"tests passed", "all tests pass", "test passed",
		"build succeeded", "build successful", "compiled successfully",
		"linting passed", "no issues found", "0 warnings",
		"exit code 0", "exited with 0",
	}
	for _, kw := range successKeywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "command.success", "info", "Command succeeded", line, "", "")
		}
	}

	// Detect failed command completion
	failKeywords := []string{
		"tests failed", "test failed", "build failed",
		"compilation failed", "lint failed", "linting failed",
		"exit code 1", "exited with 1", "non-zero exit",
	}
	for _, kw := range failKeywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "command.failed", "error", "Command failed", line, "", "")
		}
	}

	return nil
}

func (e *ClaudeEventExtractor) detectTaskCompleted(session *RemoteSession, line string) *ImportantEvent {
	lower := strings.ToLower(line)

	completionKeywords := []string{
		"task completed", "task complete", "task is complete",
		"i've completed", "i have completed", "i've finished", "i have finished",
		"all done", "changes are complete", "implementation is complete",
		"successfully completed", "done!", "that's done",
		"ready for review", "ready for your review",
		"let me know if", "let me know when",
		"is there anything else", "anything else you'd like",
		"shall i", "would you like me to",
		"what would you like", "what do you want me to",
		"how can i help", "what should i do next",
		"what's next", "next steps",
		"i'm done", "i am done",
		"changes have been", "updates have been",
		"i've made the", "i have made the",
		"i've updated", "i have updated",
		"i've added", "i have added",
		"i've fixed", "i have fixed",
		"i've created", "i have created",
		"i've implemented", "i have implemented",
	}
	for _, kw := range completionKeywords {
		if strings.Contains(lower, kw) {
			return newEvent(session, "task.completed", "info", "Task completed", line, "", "")
		}
	}

	return nil
}

func newEvent(session *RemoteSession, typ, severity, title, summary, relatedFile, command string) *ImportantEvent {
	return &ImportantEvent{
		EventID:     fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID:   session.ID,
		MachineID:   session.Summary.MachineID,
		Type:        typ,
		Severity:    severity,
		Title:       title,
		Summary:     summary,
		Count:       1,
		RelatedFile: relatedFile,
		Command:     command,
		CreatedAt:   time.Now().Unix(),
	}
}

func containsAny(value string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(value, kw) {
			return true
		}
	}
	return false
}

func extractCommand(line string) (string, bool) {
	lower := strings.ToLower(line)

	for _, prefix := range []string{"running ", "executing ", "command: "} {
		if idx := strings.Index(lower, prefix); idx >= 0 {
			command := strings.TrimSpace(line[idx+len(prefix):])
			if command != "" {
				return trimCommandMarker(command), true
			}
		}
	}

	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"$ ", "> ", "# "} {
		if strings.HasPrefix(trimmed, marker) {
			command := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
			if command != "" {
				return command, true
			}
		}
	}

	knownCommands := []string{"go test", "go build", "pytest", "npm test", "pnpm test", "cargo test", "cargo build", "python ", "node "}
	for _, command := range knownCommands {
		if strings.HasPrefix(lower, command) {
			return trimmed, true
		}
	}

	return "", false
}

func trimCommandMarker(command string) string {
	command = strings.TrimSpace(command)
	command = strings.TrimPrefix(command, "$ ")
	command = strings.TrimPrefix(command, "> ")
	command = strings.TrimPrefix(command, "# ")
	return strings.TrimSpace(command)
}

func extractFilePath(line string) string {
	for _, token := range strings.Fields(line) {
		candidate := strings.Trim(token, "\"'`()[]{}")
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, "/") || strings.Contains(candidate, "\\") {
			return filepath.Clean(candidate)
		}
		if hasLikelyFileExtension(candidate) {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

func hasLikelyFileExtension(value string) bool {
	ext := strings.ToLower(filepath.Ext(value))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".md", ".yaml", ".yml", ".py", ".java", ".rs", ".sh", ".css", ".html":
		return true
	default:
		return false
	}
}
