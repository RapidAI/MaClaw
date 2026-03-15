package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type SessionSummary struct {
	SessionID       string   `json:"session_id"`
	MachineID       string   `json:"machine_id"`
	Tool            string   `json:"tool"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	Severity        string   `json:"severity"`
	WaitingForUser  bool     `json:"waiting_for_user"`
	CurrentTask     string   `json:"current_task"`
	ProgressSummary string   `json:"progress_summary"`
	LastResult      string   `json:"last_result"`
	SuggestedAction string   `json:"suggested_action"`
	ImportantFiles  []string `json:"important_files"`
	LastCommand     string   `json:"last_command"`
	UpdatedAt       int64    `json:"updated_at"`
}

type SessionPreview struct {
	SessionID    string   `json:"session_id"`
	OutputSeq    int64    `json:"output_seq"`
	PreviewLines []string `json:"preview_lines"`
	UpdatedAt    int64    `json:"updated_at"`
}

type SessionPreviewDelta struct {
	SessionID   string   `json:"session_id"`
	OutputSeq   int64    `json:"output_seq"`
	AppendLines []string `json:"append_lines"`
	UpdatedAt   int64    `json:"updated_at"`
}

type ImportantEvent struct {
	EventID     string `json:"event_id"`
	SessionID   string `json:"session_id"`
	MachineID   string `json:"machine_id"`
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	RelatedFile string `json:"related_file,omitempty"`
	Command     string `json:"command,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type SessionCacheEntry struct {
	SessionID     string
	MachineID     string
	UserID        string
	ExecutionMode string
	Summary       SessionSummary
	Preview       SessionPreview
	RecentEvents  []ImportantEvent
	HostOnline    bool
	UpdatedAt     time.Time
}

type Cache struct {
	mu       sync.RWMutex
	sessions map[string]*SessionCacheEntry
}

type Event struct {
	Type         string
	SessionID    string
	MachineID    string
	UserID       string
	Summary      *SessionSummary
	PreviewDelta *SessionPreviewDelta
	Important    *ImportantEvent
	Payload      map[string]any
}

type Listener func(Event)

type Service struct {
	cache     *Cache
	sessions  store.SessionRepository
	mu        sync.RWMutex
	nextID    int
	listeners map[int]Listener
	stopReap  chan struct{}
}

// terminalStatuses lists session statuses that indicate the session is no
// longer running.  Sessions in these states are eligible for reaping after
// the stale-session TTL expires.
//
// IMPORTANT: keep in sync with the canonical list in
//   - frontend/src/components/remote/types.ts  → TERMINAL_SESSION_STATUSES
//   - hub/web/dist/_pwa_syntax_check.js        → sessionClosed array
var terminalStatuses = map[string]bool{
	"stopped":    true,
	"finished":   true,
	"failed":     true,
	"killed":     true,
	"exited":     true,
	"closed":     true,
	"done":       true,
	"error":      true,
	"completed":  true,
	"terminated": true,
}

// staleSessionTTL is the duration after which a terminal session is removed
// from the in-memory cache and excluded from list results.
const staleSessionTTL = 5 * time.Minute

func NewService(cache *Cache, sessions store.SessionRepository) *Service {
	svc := &Service{
		cache:     cache,
		sessions:  sessions,
		listeners: map[int]Listener{},
		stopReap:  make(chan struct{}),
	}
	go svc.reapLoop()
	return svc
}

func NewCache() *Cache {
	return &Cache{sessions: map[string]*SessionCacheEntry{}}
}

func (s *Service) RegisterListener(listener Listener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.listeners[id] = listener

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

func (s *Service) OnSessionCreated(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error {
	tool, _ := payload["tool"].(string)
	title, _ := payload["title"].(string)
	projectPath, _ := payload["project_path"].(string)
	status, _ := payload["status"].(string)
	executionMode, _ := payload["execution_mode"].(string)

	entry := &SessionCacheEntry{
		SessionID:     sessionID,
		MachineID:     machineID,
		UserID:        userID,
		ExecutionMode: executionMode,
		Summary: SessionSummary{
			SessionID: sessionID,
			MachineID: machineID,
			Tool:      tool,
			Title:     title,
			Status:    status,
			Severity:  "info",
			UpdatedAt: time.Now().Unix(),
		},
		Preview:    SessionPreview{SessionID: sessionID, PreviewLines: []string{}, UpdatedAt: time.Now().Unix()},
		HostOnline: true,
		UpdatedAt:  time.Now(),
	}
	s.set(sessionID, entry)

	if s.sessions != nil {
		if err := s.sessions.Create(ctx, &store.Session{
			ID:          sessionID,
			MachineID:   machineID,
			UserID:      userID,
			Tool:        tool,
			Title:       title,
			ProjectPath: projectPath,
			Status:      status,
			SummaryJSON: "{}",
			PreviewText: "",
			OutputSeq:   0,
			HostOnline:  true,
			StartedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}); err != nil {
			return err
		}
	}

	s.emit(Event{Type: "session.created", SessionID: sessionID, MachineID: machineID, UserID: userID, Payload: payload})
	return nil
}

func (s *Service) OnSessionSummary(ctx context.Context, machineID, userID, sessionID string, summary SessionSummary) error {
	entry := s.ensureEntry(machineID, userID, sessionID)
	summary.SessionID = sessionID
	summary.MachineID = machineID
	// Prevent a stale summary from reverting an already-exited session back to running/busy.
	if entry.Summary.Status == "exited" && summary.Status != "exited" && summary.Status != "error" {
		summary.Status = "exited"
	}
	entry.Summary = summary
	entry.HostOnline = true
	entry.UpdatedAt = time.Now()
	s.set(sessionID, entry)

	if s.sessions != nil {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		if err := s.sessions.UpdateSummary(ctx, sessionID, string(data), summary.Status, time.Now()); err != nil {
			return err
		}
	}

	s.emit(Event{Type: "session.summary", SessionID: sessionID, MachineID: machineID, UserID: userID, Summary: &summary})
	return nil
}

func (s *Service) OnSessionPreviewDelta(ctx context.Context, machineID, userID, sessionID string, delta SessionPreviewDelta) error {
	entry := s.ensureEntry(machineID, userID, sessionID)
	entry.Preview.SessionID = sessionID
	entry.Preview.OutputSeq = delta.OutputSeq
	entry.Preview.PreviewLines = append(entry.Preview.PreviewLines, delta.AppendLines...)
	if len(entry.Preview.PreviewLines) > 500 {
		entry.Preview.PreviewLines = entry.Preview.PreviewLines[len(entry.Preview.PreviewLines)-500:]
	}
	entry.Preview.UpdatedAt = time.Now().Unix()
	entry.HostOnline = true
	entry.UpdatedAt = time.Now()
	s.set(sessionID, entry)

	if s.sessions != nil {
		if err := s.sessions.UpdatePreview(ctx, sessionID, strings.Join(entry.Preview.PreviewLines, "\n"), entry.Preview.OutputSeq, time.Now()); err != nil {
			return err
		}
	}

	s.emit(Event{Type: "session.preview_delta", SessionID: sessionID, MachineID: machineID, UserID: userID, PreviewDelta: &delta})
	return nil
}

func (s *Service) OnSessionImportantEvent(ctx context.Context, machineID, userID, sessionID string, event ImportantEvent) error {
	_ = ctx
	entry := s.ensureEntry(machineID, userID, sessionID)
	event.SessionID = sessionID
	event.MachineID = machineID
	entry.RecentEvents = append(entry.RecentEvents, event)
	if len(entry.RecentEvents) > 50 {
		entry.RecentEvents = entry.RecentEvents[len(entry.RecentEvents)-50:]
	}
	entry.HostOnline = true
	entry.UpdatedAt = time.Now()
	s.set(sessionID, entry)
	s.emit(Event{Type: "session.important_event", SessionID: sessionID, MachineID: machineID, UserID: userID, Important: &event})
	return nil
}

func (s *Service) OnSessionClosed(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error {
	entry := s.ensureEntry(machineID, userID, sessionID)
	status, _ := payload["status"].(string)
	exitCode := extractExitCode(payload["exit_code"])
	endedAt := extractUnixTime(payload["ended_at"], time.Now())

	entry.Summary.Status = status
	entry.Summary.UpdatedAt = endedAt.Unix()
	entry.UpdatedAt = endedAt
	s.set(sessionID, entry)

	if s.sessions != nil {
		if err := s.sessions.Close(ctx, sessionID, exitCode, endedAt, status); err != nil {
			return err
		}
	}

	s.emit(Event{Type: "session.closed", SessionID: sessionID, MachineID: machineID, UserID: userID, Payload: payload})
	return nil
}

func (s *Service) MarkMachineOffline(ctx context.Context, machineID string) error {
	s.cache.mu.RLock()
	affected := make([]*SessionCacheEntry, 0)
	for _, entry := range s.cache.sessions {
		if entry.MachineID == machineID {
			copyEntry := *entry
			affected = append(affected, &copyEntry)
		}
	}
	s.cache.mu.RUnlock()

	now := time.Now()
	for _, entry := range affected {
		current := s.ensureEntry(entry.MachineID, entry.UserID, entry.SessionID)
		current.HostOnline = false
		current.UpdatedAt = now
		if current.Summary.Status != "exited" {
			current.Summary.Status = "unreachable"
		}
		current.Summary.UpdatedAt = now.Unix()
		s.set(current.SessionID, current)

		if s.sessions != nil {
			data, err := json.Marshal(current.Summary)
			if err != nil {
				return err
			}
			if err := s.sessions.UpdateSummary(ctx, current.SessionID, string(data), current.Summary.Status, now); err != nil {
				return err
			}
			if err := s.sessions.UpdateHostOnline(ctx, current.SessionID, false, now); err != nil {
				return err
			}
		}

		summaryCopy := current.Summary
		s.emit(Event{Type: "session.summary", SessionID: current.SessionID, MachineID: current.MachineID, UserID: current.UserID, Summary: &summaryCopy})
	}

	return nil
}

func (s *Service) GetSnapshot(userID, machineID, sessionID string) (*SessionCacheEntry, bool) {
	entry, ok := s.get(sessionID)
	if !ok {
		return nil, false
	}
	if entry.UserID != userID || entry.MachineID != machineID {
		return nil, false
	}
	return cloneSessionCacheEntry(entry), true
}

func (s *Service) ListByMachine(ctx context.Context, userID, machineID string) ([]*SessionCacheEntry, error) {
	_ = ctx

	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()

	out := make([]*SessionCacheEntry, 0)
	for _, entry := range s.cache.sessions {
		if entry.UserID == userID && entry.MachineID == machineID {
			// Skip terminal (exited) sessions entirely – they are no longer
			// useful in the machine session list, hub admin, or PWA.
			if terminalStatuses[strings.ToLower(entry.Summary.Status)] {
				continue
			}
			out = append(out, cloneSessionCacheEntry(entry))
		}
	}
	return out, nil
}

func (s *Service) ensureEntry(machineID, userID, sessionID string) *SessionCacheEntry {
	entry, ok := s.get(sessionID)
	if ok {
		return entry
	}
	entry = &SessionCacheEntry{SessionID: sessionID, MachineID: machineID, UserID: userID, HostOnline: true, UpdatedAt: time.Now()}
	s.set(sessionID, entry)
	return entry
}

func (s *Service) get(sessionID string) (*SessionCacheEntry, bool) {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	entry, ok := s.cache.sessions[sessionID]
	return entry, ok
}

func (s *Service) set(sessionID string, entry *SessionCacheEntry) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.sessions[sessionID] = entry
}

func cloneSessionCacheEntry(entry *SessionCacheEntry) *SessionCacheEntry {
	if entry == nil {
		return nil
	}

	cloned := *entry
	cloned.Summary = cloneSessionSummary(entry.Summary)
	cloned.Preview = cloneSessionPreview(entry.Preview)
	if len(entry.RecentEvents) > 0 {
		cloned.RecentEvents = make([]ImportantEvent, len(entry.RecentEvents))
		copy(cloned.RecentEvents, entry.RecentEvents)
	}
	return &cloned
}

func cloneSessionSummary(summary SessionSummary) SessionSummary {
	cloned := summary
	if len(summary.ImportantFiles) > 0 {
		cloned.ImportantFiles = append([]string(nil), summary.ImportantFiles...)
	}
	return cloned
}

func cloneSessionPreview(preview SessionPreview) SessionPreview {
	cloned := preview
	if len(preview.PreviewLines) > 0 {
		cloned.PreviewLines = append([]string(nil), preview.PreviewLines...)
	}
	return cloned
}

func (s *Service) emit(event Event) {
	s.mu.RLock()
	listeners := make([]Listener, 0, len(s.listeners))
	for _, listener := range s.listeners {
		listeners = append(listeners, listener)
	}
	s.mu.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}

func extractExitCode(v any) *int {
	switch val := v.(type) {
	case int:
		code := val
		return &code
	case int32:
		code := int(val)
		return &code
	case int64:
		code := int(val)
		return &code
	case float64:
		code := int(val)
		return &code
	default:
		return nil
	}
}

func extractUnixTime(v any, fallback time.Time) time.Time {
	switch val := v.(type) {
	case int64:
		return time.Unix(val, 0)
	case int:
		return time.Unix(int64(val), 0)
	case float64:
		return time.Unix(int64(val), 0)
	default:
		return fallback
	}
}

// reapLoop periodically removes terminal sessions that have been idle longer
// than staleSessionTTL from the in-memory cache.
func (s *Service) reapLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.reapStaleSessions()
		case <-s.stopReap:
			return
		}
	}
}

// reapStaleSessions removes closed/exited sessions from the cache once they
// exceed the stale-session TTL.
func (s *Service) reapStaleSessions() {
	now := time.Now()
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()

	for id, entry := range s.cache.sessions {
		if terminalStatuses[strings.ToLower(entry.Summary.Status)] && now.Sub(entry.UpdatedAt) > staleSessionTTL {
			delete(s.cache.sessions, id)
		}
	}
}

// StopReaper stops the background reaper goroutine.  Safe to call multiple
// times; only the first call has an effect.
func (s *Service) StopReaper() {
	select {
	case <-s.stopReap:
		// already closed
	default:
		close(s.stopReap)
	}
}
