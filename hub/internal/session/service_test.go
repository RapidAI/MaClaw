package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type sessionRepoStub struct {
	created         *store.Session
	summaryID       string
	summaryJSON     string
	summaryStatus   string
	previewID       string
	previewText     string
	previewSeq      int64
	hostOnlineID    string
	hostOnlineValue bool
	hostOnlineAt    time.Time
	closedID        string
	closedCode      *int
	closedStatus    string
	closedEndedAt   time.Time
}

func (s *sessionRepoStub) Create(ctx context.Context, session *store.Session) error {
	s.created = session
	return nil
}

func (s *sessionRepoStub) UpdateSummary(ctx context.Context, sessionID string, summaryJSON string, status string, updatedAt time.Time) error {
	s.summaryID = sessionID
	s.summaryJSON = summaryJSON
	s.summaryStatus = status
	return nil
}

func (s *sessionRepoStub) UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error {
	s.previewID = sessionID
	s.previewText = previewText
	s.previewSeq = outputSeq
	return nil
}

func (s *sessionRepoStub) UpdateHostOnline(ctx context.Context, sessionID string, hostOnline bool, updatedAt time.Time) error {
	s.hostOnlineID = sessionID
	s.hostOnlineValue = hostOnline
	s.hostOnlineAt = updatedAt
	return nil
}

func (s *sessionRepoStub) Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error {
	s.closedID = sessionID
	s.closedCode = exitCode
	s.closedEndedAt = endedAt
	s.closedStatus = status
	return nil
}

func TestSessionServiceLifecycleUpdatesCacheAndRepository(t *testing.T) {
	repo := &sessionRepoStub{}
	svc := NewService(NewCache(), repo)

	err := svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
		"tool":         "claude",
		"title":        "demo",
		"project_path": "D:/workprj/demo",
		"status":       "starting",
	})
	if err != nil {
		t.Fatalf("OnSessionCreated error: %v", err)
	}

	if repo.created == nil || repo.created.ID != "session-1" {
		t.Fatalf("expected created session to be persisted")
	}

	summary := SessionSummary{
		Status:          "busy",
		Severity:        "info",
		CurrentTask:     "Running validation command",
		ProgressSummary: "Running go test ./...",
		TokenUsage: &SessionTokenUsage{
			InputTokens:       1200,
			OutputTokens:      80,
			CachedInputTokens: 768,
			CacheWriteTokens:  128,
		},
	}
	err = svc.OnSessionSummary(context.Background(), "machine-1", "user-1", "session-1", summary)
	if err != nil {
		t.Fatalf("OnSessionSummary error: %v", err)
	}

	err = svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"Running go test ./...", "FAIL retry policy"},
	})
	if err != nil {
		t.Fatalf("OnSessionPreviewDelta error: %v", err)
	}

	err = svc.OnSessionImportantEvent(context.Background(), "machine-1", "user-1", "session-1", ImportantEvent{
		Type:      "session.error",
		Severity:  "error",
		Title:     "Error detected",
		Summary:   "FAIL retry policy",
		CreatedAt: 123,
	})
	if err != nil {
		t.Fatalf("OnSessionImportantEvent error: %v", err)
	}

	err = svc.OnSessionClosed(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
		"status":    "exited",
		"exit_code": float64(2),
		"ended_at":  float64(1700000000),
	})
	if err != nil {
		t.Fatalf("OnSessionClosed error: %v", err)
	}

	entry, ok := svc.GetSnapshot("user-1", "machine-1", "session-1")
	if !ok {
		t.Fatalf("expected snapshot to exist")
	}
	if entry.Summary.Status != "exited" {
		t.Fatalf("expected status exited, got %q", entry.Summary.Status)
	}
	if len(entry.Preview.PreviewLines) != 2 {
		t.Fatalf("expected 2 preview lines, got %d", len(entry.Preview.PreviewLines))
	}
	if len(entry.RecentEvents) != 1 || entry.RecentEvents[0].Type != "session.error" {
		t.Fatalf("expected recent error event to be recorded")
	}
	if entry.Summary.TokenUsage == nil || entry.Summary.TokenUsage.CachedInputTokens != 768 {
		t.Fatalf("expected diagnostic token usage to stay on session summary, got %#v", entry.Summary.TokenUsage)
	}
	if !strings.Contains(repo.summaryJSON, `"token_usage"`) || !strings.Contains(repo.summaryJSON, `"cached_input_tokens":768`) {
		t.Fatalf("expected summary JSON to persist diagnostic token usage, got %s", repo.summaryJSON)
	}
	if repo.closedID != "session-1" || repo.closedStatus != "exited" {
		t.Fatalf("expected close to be persisted")
	}
	if repo.closedCode == nil || *repo.closedCode != 2 {
		t.Fatalf("expected exit code 2, got %#v", repo.closedCode)
	}
	if repo.closedEndedAt.Unix() != 1700000000 {
		t.Fatalf("expected ended_at to be persisted from payload")
	}
}

func TestSessionServicePropagatesTenantToCacheRepositoryAndEvents(t *testing.T) {
	repo := &sessionRepoStub{}
	svc := NewService(NewCache(), repo)
	var events []Event
	svc.RegisterListener(func(event Event) {
		events = append(events, event)
	})
	ctx := store.WithTenant(context.Background(), "tenant_a")

	if err := svc.OnSessionCreated(ctx, "machine-1", "user-1", "session-tenant", map[string]any{
		"tool":      "claude",
		"status":    "running",
		"tenant_id": "tenant_a",
	}); err != nil {
		t.Fatalf("OnSessionCreated error: %v", err)
	}
	if err := svc.OnSessionSummary(ctx, "machine-1", "user-1", "session-tenant", SessionSummary{Status: "busy"}); err != nil {
		t.Fatalf("OnSessionSummary error: %v", err)
	}
	if err := svc.OnSessionClosed(ctx, "machine-1", "user-1", "session-tenant", map[string]any{"status": "exited"}); err != nil {
		t.Fatalf("OnSessionClosed error: %v", err)
	}

	if repo.created == nil || repo.created.TenantID != "tenant_a" {
		t.Fatalf("expected persisted tenant tenant_a, got %#v", repo.created)
	}
	entry, ok := svc.GetSnapshot("user-1", "machine-1", "session-tenant")
	if !ok || entry.TenantID != "tenant_a" {
		t.Fatalf("expected cached tenant tenant_a, got %#v", entry)
	}
	if len(events) < 3 {
		t.Fatalf("expected lifecycle events, got %d", len(events))
	}
	for _, event := range events {
		if event.TenantID != "tenant_a" {
			t.Fatalf("expected tenant_a event, got %#v", event)
		}
	}
}

func TestSessionServiceListByMachineFiltersByUserAndMachine(t *testing.T) {
	svc := NewService(NewCache(), nil)

	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{"tool": "claude"})
	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-2", "session-2", map[string]any{"tool": "claude"})
	_ = svc.OnSessionCreated(context.Background(), "machine-2", "user-1", "session-3", map[string]any{"tool": "claude"})

	items, err := svc.ListByMachine(context.Background(), "user-1", "machine-1")
	if err != nil {
		t.Fatalf("ListByMachine error: %v", err)
	}
	if len(items) != 1 || items[0].SessionID != "session-1" {
		t.Fatalf("expected only session-1, got %#v", items)
	}
}

func TestSessionServiceListByMachineFiltersByTenant(t *testing.T) {
	svc := NewService(NewCache(), nil)
	ctxA := store.WithTenant(context.Background(), "tenant_a")
	ctxB := store.WithTenant(context.Background(), "tenant_b")
	_ = svc.OnSessionCreated(ctxA, "machine-1", "user-1", "session-a", map[string]any{"tool": "claude"})
	_ = svc.OnSessionCreated(ctxB, "machine-1", "user-1", "session-b", map[string]any{"tool": "claude"})

	items, err := svc.ListByMachine(ctxA, "user-1", "machine-1")
	if err != nil {
		t.Fatalf("ListByMachine tenant A error: %v", err)
	}
	if len(items) != 1 || items[0].SessionID != "session-a" {
		t.Fatalf("tenant A list leaked sessions: %#v", items)
	}
	if _, ok := svc.GetSnapshotForTenant("tenant_a", "user-1", "machine-1", "session-b"); ok {
		t.Fatalf("tenant A should not read tenant B snapshot")
	}
}

func TestSessionServiceGetSnapshotRejectsMismatchedIdentity(t *testing.T) {
	svc := NewService(NewCache(), nil)
	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{"tool": "claude"})

	if _, ok := svc.GetSnapshot("user-2", "machine-1", "session-1"); ok {
		t.Fatalf("expected mismatched user to be rejected")
	}
	if _, ok := svc.GetSnapshot("user-1", "machine-2", "session-1"); ok {
		t.Fatalf("expected mismatched machine to be rejected")
	}
}

func TestSessionServiceGetSnapshotReturnsDefensiveCopy(t *testing.T) {
	svc := NewService(NewCache(), nil)
	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{"tool": "claude", "title": "demo"})
	_ = svc.OnSessionSummary(context.Background(), "machine-1", "user-1", "session-1", SessionSummary{
		Status:         "busy",
		ImportantFiles: []string{"a.go"},
	})
	_ = svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"line-1"},
	})
	_ = svc.OnSessionImportantEvent(context.Background(), "machine-1", "user-1", "session-1", ImportantEvent{
		Type:    "file.modified",
		Summary: "changed a.go",
	})

	entry, ok := svc.GetSnapshot("user-1", "machine-1", "session-1")
	if !ok {
		t.Fatalf("expected snapshot to exist")
	}

	entry.Summary.Status = "corrupted"
	entry.Summary.ImportantFiles[0] = "mutated.go"
	entry.Preview.PreviewLines[0] = "mutated"
	entry.RecentEvents[0].Summary = "mutated event"

	refetched, ok := svc.GetSnapshot("user-1", "machine-1", "session-1")
	if !ok {
		t.Fatalf("expected snapshot to exist after refetch")
	}
	if refetched.Summary.Status != "busy" {
		t.Fatalf("expected internal summary status to stay busy, got %q", refetched.Summary.Status)
	}
	if refetched.Summary.ImportantFiles[0] != "a.go" {
		t.Fatalf("expected important files to stay intact, got %#v", refetched.Summary.ImportantFiles)
	}
	if refetched.Preview.PreviewLines[0] != "line-1" {
		t.Fatalf("expected preview lines to stay intact, got %#v", refetched.Preview.PreviewLines)
	}
	if refetched.RecentEvents[0].Summary != "changed a.go" {
		t.Fatalf("expected recent event summary to stay intact, got %q", refetched.RecentEvents[0].Summary)
	}
}

func TestSessionServiceListByMachineReturnsDefensiveCopies(t *testing.T) {
	svc := NewService(NewCache(), nil)
	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{"tool": "claude", "title": "demo"})

	items, err := svc.ListByMachine(context.Background(), "user-1", "machine-1")
	if err != nil {
		t.Fatalf("ListByMachine error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 session, got %d", len(items))
	}

	items[0].Summary.Title = "mutated"

	refetched, err := svc.ListByMachine(context.Background(), "user-1", "machine-1")
	if err != nil {
		t.Fatalf("ListByMachine refetch error: %v", err)
	}
	if refetched[0].Summary.Title != "demo" {
		t.Fatalf("expected internal title to stay demo, got %q", refetched[0].Summary.Title)
	}
}

func TestSessionServiceMarkMachineOfflineMarksSessionsUnreachable(t *testing.T) {
	repo := &sessionRepoStub{}
	svc := NewService(NewCache(), repo)

	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-1", map[string]any{"tool": "claude", "status": "running"})
	_ = svc.OnSessionCreated(context.Background(), "machine-1", "user-1", "session-2", map[string]any{"tool": "claude", "status": "busy"})
	_ = svc.OnSessionCreated(context.Background(), "machine-2", "user-1", "session-3", map[string]any{"tool": "claude", "status": "running"})

	if err := svc.MarkMachineOffline(context.Background(), "machine-1"); err != nil {
		t.Fatalf("MarkMachineOffline error: %v", err)
	}

	entry1, ok := svc.GetSnapshot("user-1", "machine-1", "session-1")
	if !ok || entry1.HostOnline {
		t.Fatalf("expected session-1 to be offline")
	}
	if entry1.Summary.Status != "unreachable" {
		t.Fatalf("expected session-1 status unreachable, got %q", entry1.Summary.Status)
	}

	entry3, ok := svc.GetSnapshot("user-1", "machine-2", "session-3")
	if !ok || !entry3.HostOnline {
		t.Fatalf("expected session-3 to stay online")
	}

	if repo.hostOnlineID == "" || repo.hostOnlineValue {
		t.Fatalf("expected host_online persistence to be updated")
	}
}
