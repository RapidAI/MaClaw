package main

import (
	"fmt"
	"strings"
)

const codingAgentDiffSummaryDetailMaxRunes = 240

type CodingDiffSnapshot struct {
	FilesModified []string
	FilesCreated  []string
	DiffSummary   string
}

func newCodingDiffSnapshot(filesModified, filesCreated []string, diffSummary string) CodingDiffSnapshot {
	return CodingDiffSnapshot{
		FilesModified: uniqueSortedSubAgentStrings(filesModified),
		FilesCreated:  uniqueSortedSubAgentStrings(filesCreated),
		DiffSummary:   strings.TrimSpace(diffSummary),
	}
}

func (s CodingDiffSnapshot) Files() []string {
	files := make([]string, 0, len(s.FilesModified)+len(s.FilesCreated))
	files = append(files, s.FilesModified...)
	files = append(files, s.FilesCreated...)
	return uniqueSortedSubAgentStrings(files)
}

func (s CodingDiffSnapshot) Count() int {
	return len(s.Files())
}

func (s CodingDiffSnapshot) Detail() string {
	count := s.Count()
	if count == 0 {
		return "no file changes"
	}
	parts := []string{fmt.Sprintf("%d files", count)}
	if len(s.FilesCreated) > 0 {
		parts = append(parts, fmt.Sprintf("%d created", len(s.FilesCreated)))
	}
	if strings.TrimSpace(s.DiffSummary) != "" {
		parts = append(parts, truncateRunesForSubAgent(firstLine(s.DiffSummary), codingAgentDiffSummaryDetailMaxRunes))
	}
	return strings.Join(parts, " | ")
}

func newCodingAgentDiffSummaryEvent(task *TaskItem, title string, snapshot CodingDiffSnapshot) CodingAgentEvent {
	event := newCodingAgentTaskEvent("result", task, title, "")
	event.Event = "diff_summary"
	event.Detail = snapshot.Detail()
	event.Count = snapshot.Count()
	event.Files = snapshot.Files()
	return event
}
