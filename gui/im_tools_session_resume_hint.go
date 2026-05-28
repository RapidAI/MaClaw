package main

import (
	"fmt"
	"strings"
)

// writeAutoResumeHint appends guidance when a legacy structured session exits
// with a ResumeContext. New agent coding work must continue through the
// internal CodingSubAgent, not by creating another external session.
func writeAutoResumeHint(b *strings.Builder, rc *SessionResumeContext, reason string) {
	if rc == nil {
		b.WriteString("\nExternal coding session exited. Continue any remaining coding work through internal CodingSubAgent; do not create another external coding session.")
		return
	}
	if rc.ResumeCount >= 10 {
		b.WriteString("\nExternal coding session exited after repeated resume attempts. Summarize current progress and continue remaining coding work through internal CodingSubAgent.")
		return
	}

	b.WriteString(fmt.Sprintf("\n%s", strings.TrimSpace(reason)))
	b.WriteString(fmt.Sprintf("\nLegacy resume context available (attempt %d). Do not use external session tools for new agent work; route unfinished coding tasks through internal CodingSubAgent.", rc.ResumeCount+1))
	if rc.OriginalTask != "" {
		b.WriteString(fmt.Sprintf("\nOriginal task: %s", rc.OriginalTask))
	}
	if rc.LastProgress != "" {
		b.WriteString(fmt.Sprintf("\nLast progress: %s", rc.LastProgress))
	}
	if len(rc.CompletedFiles) > 0 {
		b.WriteString(fmt.Sprintf("\nCompleted files: %s", strings.Join(rc.CompletedFiles, ", ")))
	}
	resumeSessionID := strings.TrimSpace(rc.ResumeSessionID)
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(rc.ClaudeSessionID)
	}
	if resumeSessionID != "" {
		b.WriteString(fmt.Sprintf("\nLegacy resume_session_id for audit only: %s", resumeSessionID))
	}
}
