package main

import (
	"fmt"
	"strings"
)

type screenshotSessionSelectionKind int

const (
	screenshotSessionSelectionMissing screenshotSessionSelectionKind = iota
	screenshotSessionSelectionSelected
	screenshotSessionSelectionMultiple
	screenshotSessionSelectionNone
)

type screenshotSessionSelection struct {
	Kind      screenshotSessionSelectionKind
	SessionID string
	Message   string
}

func selectScreenshotSession(sessionID string, sessions []*RemoteSession) screenshotSessionSelection {
	if sessionID != "" {
		return screenshotSessionSelection{Kind: screenshotSessionSelectionSelected, SessionID: sessionID}
	}
	switch len(sessions) {
	case 0:
		return screenshotSessionSelection{Kind: screenshotSessionSelectionNone}
	case 1:
		return screenshotSessionSelection{Kind: screenshotSessionSelectionSelected, SessionID: sessions[0].ID}
	default:
		return screenshotSessionSelection{
			Kind:    screenshotSessionSelectionMultiple,
			Message: renderMultipleScreenshotSessions(sessions),
		}
	}
}

func renderMultipleScreenshotSessions(sessions []*RemoteSession) string {
	var lines []string
	lines = append(lines, "有多个活跃会话，请指定 session_id：")
	for _, s := range sessions {
		if s == nil {
			continue
		}
		s.mu.RLock()
		status := string(s.Status)
		s.mu.RUnlock()
		lines = append(lines, fmt.Sprintf("- %s (工具=%s, 状态=%s)", s.ID, s.Tool, status))
	}
	return strings.Join(lines, "\n")
}
