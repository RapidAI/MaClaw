package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	tokenSourceID   string
	tokenUsage      store.UserTokenUsage
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

// previewFailRepo fails the first failUntil preview writes, then records them.
type previewFailRepo struct {
	sessionRepoStub
	failUntil int
	calls     int
}

func (r *previewFailRepo) UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error {
	r.calls++
	if r.calls <= r.failUntil {
		return errors.New("disk full")
	}
	return r.sessionRepoStub.UpdatePreview(ctx, sessionID, previewText, outputSeq, updatedAt)
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

func (s *sessionRepoStub) RecordUserTokenUsageSnapshot(ctx context.Context, tenantID, sourceID, userID string, usage store.UserTokenUsage, observedAt time.Time) error {
	s.tokenSourceID = sourceID
	s.tokenUsage = usage
	return nil
}

func (s *sessionRepoStub) RecordHeartbeat(ctx context.Context, tenantID, machineID, userID string, at time.Time) error {
	return nil
}

func (s *sessionRepoStub) SummarizeUserTokenUsage(ctx context.Context, tenantID string, start, end time.Time) ([]store.UserTokenSummary, error) {
	return nil, nil
}

func (s *sessionRepoStub) SummarizeUserDurations(ctx context.Context, tenantID string, start, end, now time.Time) ([]store.UserDurationSummary, error) {
	return nil, nil
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
	if repo.tokenSourceID != "" || repo.tokenUsage.TotalTokens() != 0 {
		t.Fatalf("expected diagnostic session token usage to be ignored, got source=%q usage=%#v", repo.tokenSourceID, repo.tokenUsage)
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

func TestSessionServiceRecordsUserTokenUsageSnapshot(t *testing.T) {
	repo := &sessionRepoStub{}
	svc := NewService(NewCache(), repo)

	err := svc.RecordUserTokenUsageSnapshot(store.WithTenant(context.Background(), "tenant_a"), "tenant_a", "gui:machine-1", "user-1", store.UserTokenUsage{
		InputTokens:       1200,
		OutputTokens:      80,
		CachedInputTokens: 768,
		CacheWriteTokens:  128,
	}, time.Now())
	if err != nil {
		t.Fatalf("RecordUserTokenUsageSnapshot error: %v", err)
	}
	if repo.tokenSourceID != "gui:machine-1" || repo.tokenUsage.InputTokens != 1200 || repo.tokenUsage.TotalTokens() != 1280 {
		t.Fatalf("expected GUI token usage snapshot to be recorded, got source=%q usage=%#v", repo.tokenSourceID, repo.tokenUsage)
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

func TestPreviewDeltaWindowMatchesTheDesktopBurst(t *testing.T) {
	if deltaDebounceInterval != 16*time.Millisecond {
		t.Fatalf("viewer merge window = %s, want 16ms", deltaDebounceInterval)
	}
}

func TestPreviewDeltaPublishesTheFirstLineImmediately(t *testing.T) {
	prev := deltaDebounceInterval
	deltaDebounceInterval = 80 * time.Millisecond
	t.Cleanup(func() { deltaDebounceInterval = prev })

	svc := NewService(NewCache(), nil)
	t.Cleanup(svc.StopReaper)

	got := make(chan []string, 4)
	var published *SessionPreviewDelta
	svc.RegisterListener(func(ev Event) {
		if ev.Type != "session.preview_delta" || ev.PreviewDelta == nil {
			return
		}
		published = ev.PreviewDelta
		got <- append([]string(nil), ev.PreviewDelta.AppendLines...)
	})

	first := []string{"first"}
	start := time.Now()
	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: first,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case lines := <-got:
		if time.Since(start) > deltaDebounceInterval/2 {
			t.Fatalf("first preview line waited %s", time.Since(start))
		}
		if len(lines) != 1 || lines[0] != "first" {
			t.Fatalf("first lines = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("first preview line was not published")
	}
	first[0] = "mutated"
	if published == nil || len(published.AppendLines) != 1 || published.AppendLines[0] != "first" {
		t.Fatalf("published preview aliases the caller buffer: %#v", published)
	}

	follow := []string{"second"}
	followStart := time.Now()
	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: follow,
	}); err != nil {
		t.Fatal(err)
	}
	follow[0] = "mutated"
	select {
	case lines := <-got:
		t.Fatalf("follow-up published before the open window: %#v", lines)
	case <-time.After(deltaDebounceInterval / 4):
	}
	select {
	case lines := <-got:
		// The window was armed on the lead, so this line is due with that
		// timer. A second full period means the viewer hold stacked again.
		if waited := time.Since(followStart); waited > deltaDebounceInterval+100*time.Millisecond {
			t.Fatalf("follow-up waited %s", waited)
		}
		if len(lines) != 1 || lines[0] != "second" {
			t.Fatalf("follow-up lines = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("follow-up preview line was not published")
	}
}

func TestPreviewDeltaKeepsPublishingWhileLinesKeepArriving(t *testing.T) {
	svc := NewService(NewCache(), nil)
	t.Cleanup(svc.StopReaper)

	got := make(chan []string, 32)
	svc.RegisterListener(func(ev Event) {
		if ev.Type != "session.preview_delta" || ev.PreviewDelta == nil {
			return
		}
		got <- append([]string(nil), ev.PreviewDelta.AppendLines...)
	})

	send := func(seq int, line string) {
		t.Helper()
		if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-live", SessionPreviewDelta{
			OutputSeq:   int64(seq),
			AppendLines: []string{line},
		}); err != nil {
			t.Fatal(err)
		}
	}

	var want []string
	send(1, "line-0")
	want = append(want, "line-0")
	select {
	case lines := <-got:
		if len(lines) != 1 || lines[0] != "line-0" {
			t.Fatalf("first = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("first line was not published")
	}

	secondArrived := false
	var gotLines []string
	gotLines = append(gotLines, "line-0")
	deadline := time.Now().Add(400 * time.Millisecond)
	seq := 2
	for time.Now().Before(deadline) {
		line := fmt.Sprintf("line-%d", seq-1)
		send(seq, line)
		want = append(want, line)
		seq++
		select {
		case lines := <-got:
			secondArrived = true
			gotLines = append(gotLines, lines...)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !secondArrived {
		t.Fatal("steady preview stream published nothing until the sender paused")
	}

	quiet := time.NewTimer(deltaDebounceInterval + 250*time.Millisecond)
	defer quiet.Stop()
	for {
		select {
		case lines := <-got:
			gotLines = append(gotLines, lines...)
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(deltaDebounceInterval + 250*time.Millisecond)
		case <-quiet.C:
			if strings.Join(gotLines, "\n") != strings.Join(want, "\n") {
				t.Fatalf("published lines = %q, want %q", gotLines, want)
			}
			return
		}
	}
}

func TestPreviewDeltaCloseFlushesTheMergedTail(t *testing.T) {
	svc := NewService(NewCache(), nil)
	t.Cleanup(svc.StopReaper)

	type note struct {
		typ   string
		lines []string
	}
	got := make(chan note, 4)
	svc.RegisterListener(func(ev Event) {
		n := note{typ: ev.Type}
		if ev.PreviewDelta != nil {
			n.lines = append([]string(nil), ev.PreviewDelta.AppendLines...)
		}
		got <- n
	})

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"first"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-got:
		if n.typ != "session.preview_delta" || len(n.lines) != 1 || n.lines[0] != "first" {
			t.Fatalf("first = %#v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("first preview line was not published")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"tail"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnSessionClosed(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
		"status": "exited",
	}); err != nil {
		t.Fatal(err)
	}

	var notes []note
	for i := 0; i < 2; i++ {
		select {
		case n := <-got:
			notes = append(notes, n)
		case <-time.After(deltaDebounceInterval / 4):
			t.Fatalf("close did not flush the open tail, got %#v", notes)
		}
	}
	if notes[0].typ != "session.preview_delta" || len(notes[0].lines) != 1 || notes[0].lines[0] != "tail" {
		t.Fatalf("tail = %#v", notes[0])
	}
	if notes[1].typ != "session.closed" {
		t.Fatalf("expected close after the tail, got %#v", notes[1])
	}
	select {
	case n := <-got:
		t.Fatalf("extra event after close: %#v", n)
	case <-time.After(deltaDebounceInterval + 50*time.Millisecond):
	}
}

func TestPreviewDeltaPersistFailureStillPublishesAndRetries(t *testing.T) {
	repo := &previewFailRepo{failUntil: 1}
	svc := NewService(NewCache(), repo)
	t.Cleanup(svc.StopReaper)

	got := make(chan []string, 2)
	svc.RegisterListener(func(ev Event) {
		if ev.Type != "session.preview_delta" || ev.PreviewDelta == nil {
			return
		}
		got <- append([]string(nil), ev.PreviewDelta.AppendLines...)
	})

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"kept"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case lines := <-got:
		if len(lines) != 1 || lines[0] != "kept" {
			t.Fatalf("published = %#v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("persist failure dropped the live preview")
	}
	if repo.calls != 1 {
		t.Fatalf("persist calls = %d, want 1", repo.calls)
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"retry"},
	}); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 2 {
		t.Fatalf("failed persist did not retry, calls = %d", repo.calls)
	}
	if repo.previewText != "kept\nretry" {
		t.Fatalf("retried preview = %q", repo.previewText)
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   3,
		AppendLines: []string{"later"},
	}); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 2 {
		t.Fatalf("successful persist did not keep the throttle, calls = %d", repo.calls)
	}
}

func TestPreviewDeltaCloseWaitsForTheInFlightFlush(t *testing.T) {
	svc := NewService(NewCache(), nil)
	t.Cleanup(svc.StopReaper)

	type note struct {
		typ   string
		lines []string
	}
	got := make(chan note, 4)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFlush := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFlush)
	svc.RegisterListener(func(ev Event) {
		n := note{typ: ev.Type}
		if ev.PreviewDelta != nil {
			n.lines = append([]string(nil), ev.PreviewDelta.AppendLines...)
		}
		if n.typ == "session.preview_delta" && len(n.lines) == 1 && n.lines[0] == "slow" {
			entered <- struct{}{}
			<-release
		}
		got <- n
	})

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"first"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-got:
		if n.typ != "session.preview_delta" || len(n.lines) != 1 || n.lines[0] != "first" {
			t.Fatalf("first = %#v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("first preview line was not published")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"slow"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("merged preview did not reach the listener")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   3,
		AppendLines: []string{"after"},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(deltaDebounceInterval + 20*time.Millisecond)
	select {
	case n := <-got:
		t.Fatalf("line published during the in-flight emit: %#v", n)
	default:
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- svc.OnSessionClosed(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
			"status": "exited",
		})
	}()
	time.Sleep(30 * time.Millisecond)
	select {
	case err := <-closeDone:
		t.Fatalf("close finished while the preview emit was still running: %v", err)
	default:
	}

	releaseFlush()
	var notes []note
	deadline := time.After(time.Second)
	for len(notes) < 3 {
		select {
		case n := <-got:
			notes = append(notes, n)
		case <-deadline:
			t.Fatalf("events after release = %#v", notes)
		}
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if notes[0].typ != "session.preview_delta" || len(notes[0].lines) != 1 || notes[0].lines[0] != "slow" {
		t.Fatalf("in-flight preview = %#v", notes[0])
	}
	if notes[1].typ != "session.preview_delta" || len(notes[1].lines) != 1 || notes[1].lines[0] != "after" {
		t.Fatalf("line queued during emit = %#v", notes[1])
	}
	if notes[2].typ != "session.closed" {
		t.Fatalf("close = %#v", notes[2])
	}
}

// closeFailRepo records the close, then fails the row update.
type closeFailRepo struct {
	sessionRepoStub
}

func (r *closeFailRepo) Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error {
	_ = r.sessionRepoStub.Close(ctx, sessionID, exitCode, endedAt, status)
	return errors.New("close failed")
}

func TestPreviewDeltaCloseStillPublishesWhenTheRowUpdateFails(t *testing.T) {
	repo := &closeFailRepo{}
	svc := NewService(NewCache(), repo)
	t.Cleanup(svc.StopReaper)

	type note struct {
		typ   string
		lines []string
	}
	got := make(chan note, 4)
	svc.RegisterListener(func(ev Event) {
		n := note{typ: ev.Type}
		if ev.PreviewDelta != nil {
			n.lines = append([]string(nil), ev.PreviewDelta.AppendLines...)
		}
		got <- n
	})

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"first"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-got:
		if n.typ != "session.preview_delta" || len(n.lines) != 1 || n.lines[0] != "first" {
			t.Fatalf("first = %#v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("first preview line was not published")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"tail"},
	}); err != nil {
		t.Fatal(err)
	}
	err := svc.OnSessionClosed(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
		"status": "exited",
	})
	if err == nil || err.Error() != "close failed" {
		t.Fatalf("close err = %v", err)
	}
	if repo.closedID != "session-1" || repo.closedStatus != "exited" {
		t.Fatalf("close row = id %q status %q", repo.closedID, repo.closedStatus)
	}
	svc.previewDBMu.Lock()
	_, stamped := svc.previewDBLastWrite["session-1"]
	svc.previewDBMu.Unlock()
	if stamped {
		t.Fatal("failed close left the preview throttle stamp")
	}

	var notes []note
	for i := 0; i < 2; i++ {
		select {
		case n := <-got:
			notes = append(notes, n)
		default:
			t.Fatalf("viewer event missing when close returned, got %#v", notes)
		}
	}
	if notes[0].typ != "session.preview_delta" || len(notes[0].lines) != 1 || notes[0].lines[0] != "tail" {
		t.Fatalf("tail = %#v", notes[0])
	}
	if notes[1].typ != "session.closed" {
		t.Fatalf("expected close after the tail, got %#v", notes[1])
	}
}

func TestPreviewDeltaCloseKeepsALineThatArrivesWhileTheTailIsEmitted(t *testing.T) {
	svc := NewService(NewCache(), nil)
	t.Cleanup(svc.StopReaper)

	type note struct {
		typ   string
		lines []string
	}
	got := make(chan note, 4)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFlush := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFlush)
	svc.RegisterListener(func(ev Event) {
		n := note{typ: ev.Type}
		if ev.PreviewDelta != nil {
			n.lines = append([]string(nil), ev.PreviewDelta.AppendLines...)
		}
		if n.typ == "session.preview_delta" && len(n.lines) == 1 && n.lines[0] == "tail" {
			entered <- struct{}{}
			<-release
		}
		got <- n
	})

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   1,
		AppendLines: []string{"first"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-got:
		if n.typ != "session.preview_delta" || len(n.lines) != 1 || n.lines[0] != "first" {
			t.Fatalf("first = %#v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("first preview line was not published")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   2,
		AppendLines: []string{"tail"},
	}); err != nil {
		t.Fatal(err)
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- svc.OnSessionClosed(context.Background(), "machine-1", "user-1", "session-1", map[string]any{
			"status": "exited",
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("tail was not emitted")
	}

	if err := svc.OnSessionPreviewDelta(context.Background(), "machine-1", "user-1", "session-1", SessionPreviewDelta{
		OutputSeq:   3,
		AppendLines: []string{"during"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-got:
		t.Fatalf("line published while the tail emit was blocked: %#v", n)
	default:
	}

	releaseFlush()
	var notes []note
	deadline := time.After(time.Second)
	for len(notes) < 3 {
		select {
		case n := <-got:
			notes = append(notes, n)
		case <-deadline:
			t.Fatalf("events after release = %#v", notes)
		}
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if notes[0].typ != "session.preview_delta" || len(notes[0].lines) != 1 || notes[0].lines[0] != "tail" {
		t.Fatalf("tail = %#v", notes[0])
	}
	if notes[1].typ != "session.preview_delta" || len(notes[1].lines) != 1 || notes[1].lines[0] != "during" {
		t.Fatalf("line queued during the tail emit = %#v", notes[1])
	}
	if notes[2].typ != "session.closed" {
		t.Fatalf("close = %#v", notes[2])
	}
}
