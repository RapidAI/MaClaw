package main

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/acpagent"
)

func TestACPHostSessionsUseDistinctOwnersForSameWorkspace(t *testing.T) {
	app := &App{}
	host := newACPHostSession(app, "token", nil)
	workspace := filepath.Clean(t.TempDir())

	first, firstErr := host.onSessionNew(mustJSON(t, map[string]any{"cwd": workspace}))
	second, secondErr := host.onSessionNew(mustJSON(t, map[string]any{"cwd": workspace}))
	if firstErr != nil || secondErr != nil {
		t.Fatalf("session/new errors: first=%v second=%v", firstErr, secondErr)
	}
	firstID := first.(acpagent.SessionNewResult).SessionID
	secondID := second.(acpagent.SessionNewResult).SessionID
	firstOwner := host.sessions[firstID].UserID
	secondOwner := host.sessions[secondID].UserID
	if firstOwner == secondOwner {
		t.Fatalf("same workspace sessions shared owner %q", firstOwner)
	}
	if !isACPAssistantSessionUserID(firstOwner) || !isACPAssistantSessionUserID(secondOwner) {
		t.Fatalf("ACP owners must use ACP namespace: %q, %q", firstOwner, secondOwner)
	}
	if got := projectPathFromSessionOwnerID(firstOwner); got != "" {
		t.Fatalf("ACP owner was interpreted as project path %q", got)
	}
	if !isIsolatedAssistantSessionUserID(firstOwner) {
		t.Fatalf("ACP owner must be a strictly isolated memory session")
	}
	if got := app.EffectiveWorkingDirForOwner(firstOwner); got != workspace {
		t.Fatalf("first ACP working dir = %q, want %q", got, workspace)
	}
	if got := app.EffectiveWorkingDirForOwner(secondOwner); got != workspace {
		t.Fatalf("second ACP working dir = %q, want %q", got, workspace)
	}
	handler := &IMMessageHandler{app: app}
	if got := handler.resolveToolWorkDirForOwner("", firstOwner); got != workspace {
		t.Fatalf("first ACP tool working dir = %q, want %q", got, workspace)
	}
}

func TestNormalizeAIAssistantSessionUserIDAcceptsACPScopedOwner(t *testing.T) {
	owner := acpAssistantSessionOwnerID("acp_gui_123")
	if got, err := normalizeAIAssistantSessionUserID(owner); err != nil || got != owner {
		t.Fatalf("normalize ACP owner = %q, %v; want %q, nil", got, err, owner)
	}
}
