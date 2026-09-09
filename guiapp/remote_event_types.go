package guiapp

import (
	"fmt"
	"time"
)

func buildSessionInitEvent(session *RemoteSession) ImportantEvent {
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: session.ID,
		Type:      "session.init",
		Severity:  "info",
		Title:     "Session started",
		Summary:   fmt.Sprintf("Session started for %s in %s", session.Tool, session.ProjectPath),
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}

func buildSessionFailedEvent(session *RemoteSession, err error) ImportantEvent {
	summary := "Session failed before launch completed"
	if err != nil {
		summary = err.Error()
	}
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: session.ID,
		Type:      "session.failed",
		Severity:  "error",
		Title:     "Session failed",
		Summary:   summary,
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}

func buildSessionClosedEvent(session *RemoteSession, exit PTYExit) ImportantEvent {
	severity := "info"
	title := "Session finished"
	summary := "Session exited successfully"
	if exit.Code != nil {
		summary = fmt.Sprintf("Session exited with code %d", *exit.Code)
		if *exit.Code != 0 {
			severity = "warn"
		}
	}
	if exit.Err != nil {
		severity = "error"
		title = "Session crashed"
		summary = exit.Err.Error()
	}
	return ImportantEvent{
		EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		SessionID: session.ID,
		Type:      "session.closed",
		Severity:  severity,
		Title:     title,
		Summary:   summary,
		Count:     1,
		CreatedAt: time.Now().Unix(),
	}
}
