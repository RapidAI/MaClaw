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
	Source        string
	Summary       SessionSummary
	Preview       SessionPreview
	RecentEvents  []ImportantEvent
	HostOnline    bool
	UpdatedAt     time.Time
}

type Cache = ShardedCache

type Event struct {
	Type         string
	SessionID    string
	MachineID    string
	UserID       string
	Summary      *SessionSummary
	PreviewDelta *SessionPreviewDelta
	Important    *ImportantEvent
	ImageData    *SessionImage
	Payload      map[string]any
}

// SessionImage carries a base64-encoded image from a session for delivery
// to listeners (e.g. Feishu notifier).
type SessionImage struct {
	ImageID   string `json:"image_id"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"` // base64-encoded
}

type Listener func(Event)

type Service struct {
	cache     *ShardedCache
	sessions  store.SessionRepository
	mu        sync.RWMutex
	nextID    int
	listeners map[int]Listener
	stopReap  chan struct{}

	// previewDBThrottle tracks the last DB write time per session to avoid
	// writing preview text to the database on every delta. The in-memory
	// cache is always updated immediately; only the DB write is throttled.
	previewDBMu        sync.Mutex
	previewDBLastWrite map[string]time.Time

	// deltaDebounce tracks pending debounce timers per session. Multiple
	// preview deltas arriving within deltaDebounceInterval are merged into
	// a single emit to reduce WebSocket broadcast frequency.
	deltaDebounceMu     sync.Mutex
	deltaDebounceBuf    map[string]*SessionPreviewDelta // session_id → merged delta
	deltaDebounceTimers map[string]*time.Timer          // session_id → flush timer
}

// deltaDebounceInterval is the window within which consecutive preview deltas
// for the same session are merged before being emitted to listeners.
const deltaDebounceInterval = 150 * time.Millisecond

// previewDBWriteInterval is the minimum interval between DB writes for
// preview text of the same session. Viewers receive real-time updates via
// WebSocket from the in-memory cache, so the DB is only for persistence.
const previewDBWriteInterval = 2 * time.Second

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

func NewService(cache *ShardedCache, sessions store.SessionRepository) *Service {
	svc := &Service{
		cache:               cache,
		sessions:            sessions,
		listeners:           map[int]Listener{},
		stopReap:            make(chan struct{}),
		previewDBLastWrite:  make(map[string]time.Time),
		deltaDebounceBuf:    make(map[string]*SessionPreviewDelta),
		deltaDebounceTimers: make(map[string]*time.Timer),
	}
	go svc.reapLoop()
	return svc
}

func NewCache() *ShardedCache {
	return NewShardedCache()
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
	source, _ := payload["source"].(string)

	entry := &SessionCacheEntry{
		SessionID:     sessionID,
		MachineID:     machineID,
		UserID:        userID,
		ExecutionMode: executionMode,
		Source:        source,
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
	summary.SessionID = sessionID
	summary.MachineID = machineID

	var finalSummary SessionSummary
	s.cache.Modify(sessionID, func() *SessionCacheEntry {
		return &SessionCacheEntry{SessionID: sessionID, MachineID: machineID, UserID: userID, HostOnline: true, UpdatedAt: time.Now()}
	}, func(entry *SessionCacheEntry) {
		// Prevent a stale summary from reverting an already-exited session back to running/busy.
		if entry.Summary.Status == "exited" && summary.Status != "exited" && summary.Status != "error" {
			summary.Status = "exited"
		}
		entry.Summary = summary
		entry.HostOnline = true
		entry.UpdatedAt = time.Now()
		finalSummary = summary
	})

	if s.sessions != nil {
		data, err := json.Marshal(finalSummary)
		if err != nil {
			return err
		}
		if err := s.sessions.UpdateSummary(ctx, sessionID, string(data), finalSummary.Status, time.Now()); err != nil {
			return err
		}
	}

	s.emit(Event{Type: "session.summary", SessionID: sessionID, MachineID: machineID, UserID: userID, Summary: &finalSummary})
	return nil
}

func (s *Service) OnSessionPreviewDelta(ctx context.Context, machineID, userID, sessionID string, delta SessionPreviewDelta) error {
	// Atomically update the cache entry under the shard lock to prevent
	// concurrent delta appends from losing lines.
	var previewSnapshot string
	var outputSeq int64
	var shouldWriteDB bool

	s.cache.Modify(sessionID, func() *SessionCacheEntry {
		return &SessionCacheEntry{SessionID: sessionID, MachineID: machineID, UserID: userID, HostOnline: true, UpdatedAt: time.Now()}
	}, func(entry *SessionCacheEntry) {
		entry.Preview.SessionID = sessionID
		entry.Preview.OutputSeq = delta.OutputSeq
		entry.Preview.PreviewLines = append(entry.Preview.PreviewLines, delta.AppendLines...)
		if len(entry.Preview.PreviewLines) > 500 {
			entry.Preview.PreviewLines = entry.Preview.PreviewLines[len(entry.Preview.PreviewLines)-500:]
		}
		entry.Preview.UpdatedAt = time.Now().Unix()
		entry.HostOnline = true
		entry.UpdatedAt = time.Now()

		// Capture snapshot for potential DB write (outside shard lock).
		outputSeq = entry.Preview.OutputSeq
		previewSnapshot = strings.Join(entry.Preview.PreviewLines, "\n")
	})

	// Throttle DB writes: the in-memory cache is always up-to-date for
	// real-time WebSocket delivery. The DB write is only for persistence
	// and can be deferred.
	if s.sessions != nil {
		now := time.Now()
		s.previewDBMu.Lock()
		lastWrite := s.previewDBLastWrite[sessionID]
		shouldWriteDB = now.Sub(lastWrite) >= previewDBWriteInterval
		if shouldWriteDB {
			s.previewDBLastWrite[sessionID] = now
		}
		s.previewDBMu.Unlock()

		if shouldWriteDB {
			if err := s.sessions.UpdatePreview(ctx, sessionID, previewSnapshot, outputSeq, now); err != nil {
				return err
			}
		}
	}

	// Debounce: merge consecutive deltas within deltaDebounceInterval before
	// emitting to listeners. This reduces WebSocket broadcast frequency for
	// high-throughput sessions (e.g. streaming build output).
	s.deltaDebounceMu.Lock()
	existing := s.deltaDebounceBuf[sessionID]
	if existing == nil {
		merged := delta
		s.deltaDebounceBuf[sessionID] = &merged
	} else {
		existing.AppendLines = append(existing.AppendLines, delta.AppendLines...)
		existing.OutputSeq = delta.OutputSeq
		existing.UpdatedAt = delta.UpdatedAt
	}
	// Reset or start the debounce timer.
	if t, ok := s.deltaDebounceTimers[sessionID]; ok {
		t.Stop()
	}
	s.deltaDebounceTimers[sessionID] = time.AfterFunc(deltaDebounceInterval, func() {
		s.flushDebouncedDelta(sessionID, machineID, userID)
	})
	s.deltaDebounceMu.Unlock()

	return nil
}

// flushDebouncedDelta emits the merged preview delta for a session and clears
// the debounce buffer.
func (s *Service) flushDebouncedDelta(sessionID, machineID, userID string) {
	s.deltaDebounceMu.Lock()
	merged := s.deltaDebounceBuf[sessionID]
	delete(s.deltaDebounceBuf, sessionID)
	delete(s.deltaDebounceTimers, sessionID)
	s.deltaDebounceMu.Unlock()

	if merged == nil || len(merged.AppendLines) == 0 {
		return
	}
	s.emit(Event{Type: "session.preview_delta", SessionID: sessionID, MachineID: machineID, UserID: userID, PreviewDelta: merged})
}

func (s *Service) OnSessionImportantEvent(ctx context.Context, machineID, userID, sessionID string, event ImportantEvent) error {
	_ = ctx
	event.SessionID = sessionID
	event.MachineID = machineID

	s.cache.Modify(sessionID, func() *SessionCacheEntry {
		return &SessionCacheEntry{SessionID: sessionID, MachineID: machineID, UserID: userID, HostOnline: true, UpdatedAt: time.Now()}
	}, func(entry *SessionCacheEntry) {
		entry.RecentEvents = append(entry.RecentEvents, event)
		if len(entry.RecentEvents) > 50 {
			entry.RecentEvents = entry.RecentEvents[len(entry.RecentEvents)-50:]
		}
		entry.HostOnline = true
		entry.UpdatedAt = time.Now()
	})

	s.emit(Event{Type: "session.important_event", SessionID: sessionID, MachineID: machineID, UserID: userID, Important: &event})
	return nil
}

func (s *Service) OnSessionClosed(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error {
	status, _ := payload["status"].(string)
	exitCode := extractExitCode(payload["exit_code"])
	endedAt := extractUnixTime(payload["ended_at"], time.Now())

	var previewLines []string
	var outputSeq int64

	s.cache.Modify(sessionID, func() *SessionCacheEntry {
		return &SessionCacheEntry{SessionID: sessionID, MachineID: machineID, UserID: userID, HostOnline: true, UpdatedAt: time.Now()}
	}, func(entry *SessionCacheEntry) {
		entry.Summary.Status = status
		entry.Summary.UpdatedAt = endedAt.Unix()
		entry.UpdatedAt = endedAt
		// Capture preview snapshot for DB flush.
		previewLines = append([]string(nil), entry.Preview.PreviewLines...)
		outputSeq = entry.Preview.OutputSeq
	})

	if s.sessions != nil {
		// Flush any throttled preview data to DB before closing
		if len(previewLines) > 0 {
			_ = s.sessions.UpdatePreview(ctx, sessionID, strings.Join(previewLines, "\n"), outputSeq, endedAt)
		}
		if err := s.sessions.Close(ctx, sessionID, exitCode, endedAt, status); err != nil {
			return err
		}
	}

	// Clean up throttle tracking
	s.previewDBMu.Lock()
	delete(s.previewDBLastWrite, sessionID)
	s.previewDBMu.Unlock()

	// Flush any pending debounced delta before emitting the close event,
	// so listeners see the final output before the session ends.
	s.deltaDebounceMu.Lock()
	pendingDelta := s.deltaDebounceBuf[sessionID]
	delete(s.deltaDebounceBuf, sessionID)
	if t, ok := s.deltaDebounceTimers[sessionID]; ok {
		t.Stop()
		delete(s.deltaDebounceTimers, sessionID)
	}
	s.deltaDebounceMu.Unlock()
	if pendingDelta != nil && len(pendingDelta.AppendLines) > 0 {
		s.emit(Event{Type: "session.preview_delta", SessionID: sessionID, MachineID: machineID, UserID: userID, PreviewDelta: pendingDelta})
	}

	s.emit(Event{Type: "session.closed", SessionID: sessionID, MachineID: machineID, UserID: userID, Payload: payload})
	return nil
}

// OnSessionImage dispatches a session.image event to listeners. The image
// data is not persisted — it is only forwarded to real-time consumers such
// as the Feishu notifier.
func (s *Service) OnSessionImage(ctx context.Context, machineID, userID, sessionID string, img SessionImage) {
	s.emit(Event{
		Type:      "session.image",
		SessionID: sessionID,
		MachineID: machineID,
		UserID:    userID,
		ImageData: &img,
	})
}


func (s *Service) MarkMachineOffline(ctx context.Context, machineID string) error {
	// Collect session IDs belonging to this machine.
	type sessionMeta struct {
		sessionID string
		userID    string
	}
	var targets []sessionMeta
	s.cache.Range(func(_ string, entry *SessionCacheEntry) bool {
		if entry.MachineID == machineID {
			targets = append(targets, sessionMeta{sessionID: entry.SessionID, userID: entry.UserID})
		}
		return true
	})

	now := time.Now()
	for _, t := range targets {
		var summaryCopy SessionSummary
		s.cache.Modify(t.sessionID, func() *SessionCacheEntry {
			return &SessionCacheEntry{SessionID: t.sessionID, MachineID: machineID, UserID: t.userID, HostOnline: true, UpdatedAt: now}
		}, func(entry *SessionCacheEntry) {
			entry.HostOnline = false
			entry.UpdatedAt = now
			if entry.Summary.Status != "exited" {
				entry.Summary.Status = "unreachable"
			}
			entry.Summary.UpdatedAt = now.Unix()
			summaryCopy = entry.Summary
		})

		if s.sessions != nil {
			data, err := json.Marshal(summaryCopy)
			if err != nil {
				return err
			}
			if err := s.sessions.UpdateSummary(ctx, t.sessionID, string(data), summaryCopy.Status, now); err != nil {
				return err
			}
			if err := s.sessions.UpdateHostOnline(ctx, t.sessionID, false, now); err != nil {
				return err
			}
		}

		s.emit(Event{Type: "session.summary", SessionID: t.sessionID, MachineID: machineID, UserID: t.userID, Summary: &summaryCopy})
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

	out := make([]*SessionCacheEntry, 0)
	s.cache.Range(func(_ string, entry *SessionCacheEntry) bool {
		if entry.UserID == userID && entry.MachineID == machineID {
			if terminalStatuses[strings.ToLower(entry.Summary.Status)] {
				return true
			}
			out = append(out, cloneSessionCacheEntry(entry))
		}
		return true
	})
	return out, nil
}

func (s *Service) ListAll() []*SessionCacheEntry {
	out := make([]*SessionCacheEntry, 0)
	s.cache.Range(func(_ string, entry *SessionCacheEntry) bool {
		out = append(out, cloneSessionCacheEntry(entry))
		return true
	})
	return out
}

func (s *Service) get(sessionID string) (*SessionCacheEntry, bool) {
	return s.cache.Get(sessionID)
}

func (s *Service) set(sessionID string, entry *SessionCacheEntry) {
	s.cache.Set(sessionID, entry)
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
	s.cache.RangeWithDelete(func(_ string, entry *SessionCacheEntry) bool {
		return terminalStatuses[strings.ToLower(entry.Summary.Status)] && now.Sub(entry.UpdatedAt) > staleSessionTTL
	})
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
