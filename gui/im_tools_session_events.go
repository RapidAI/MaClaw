package main

import (
	"fmt"
	"strings"
)

func snapshotSessionEvents(session *RemoteSession) []ImportantEvent {
	if session == nil {
		return nil
	}
	session.mu.RLock()
	defer session.mu.RUnlock()

	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	return events
}

func renderSessionEvents(sessionID string, events []ImportantEvent) string {
	if len(events) == 0 {
		return fmt.Sprintf("会话 %s 暂无重要事件。", sessionID)
	}
	var b strings.Builder
	for _, ev := range events {
		appendSessionEventLine(&b, ev)
	}
	return b.String()
}

func appendSessionEventLine(b *strings.Builder, ev ImportantEvent) {
	b.WriteString(fmt.Sprintf("- [%s] %s: %s", ev.Severity, ev.Type, ev.Title))
	if ev.Summary != "" {
		b.WriteString(fmt.Sprintf(" — %s", ev.Summary))
	}
	if ev.RelatedFile != "" {
		b.WriteString(fmt.Sprintf(" (文件: %s)", ev.RelatedFile))
	}
	b.WriteString("\n")
}
