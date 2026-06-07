package memory

import (
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// ScrollSessionManager — per-loop scroll-through recall sessions.
// ---------------------------------------------------------------------------

// ScrollSession holds a cached, pre-scored candidate list for iterative
// deepening within a single agent loop execution.
type ScrollSession struct {
	LoopID     string         // identifies the owning agent loop
	Query      string         // normalized lowercase query that produced Candidates
	Candidates []recallScored // up to scrollSessionMaxCache (200) scored entries
	Position   int            // next slice start index
	CreatedAt  time.Time
}

// ScrollSessionManager tracks per-loop scroll sessions. Sessions are keyed by
// (OwnerID, LoopID) tuple to prevent cross-user or cross-loop cache leakage
// (Requirement 4.8). The composite key ensures isolation even when different
// users share the same loopID value (e.g., "default").
type ScrollSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*ScrollSession // keyed by sessionKey(ownerID, loopID)
}

// sessionKey returns the composite key for session isolation.
// Requirement 4.8: each Scroll_Session is isolated by (OwnerID, LoopID) tuple.
func sessionKey(ownerID, loopID string) string {
	if ownerID == "" {
		return loopID
	}
	return ownerID + "\x00" + loopID
}

// NewScrollSessionManager creates a new manager with an empty session map.
func NewScrollSessionManager() *ScrollSessionManager {
	return &ScrollSessionManager{
		sessions: make(map[string]*ScrollSession),
	}
}

// GetOrCreate returns the existing scroll session for the given loopID, or
// creates a new one by executing RecallDynamicForTool and caching up to 200
// scored candidates. If the query (normalized lowercase) differs from the
// cached session's query, the session is discarded and re-created with the
// new query.
func (m *ScrollSessionManager) GetOrCreate(
	loopID string,
	store *Store,
	query string,
	category Category,
	projectPath string,
	ownerID string,
) *ScrollSession {
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	key := sessionKey(ownerID, loopID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if sess, ok := m.sessions[key]; ok {
		// Query unchanged — return existing session.
		if sess.Query == normalizedQuery {
			return sess
		}
		// Query changed — discard cached candidates, re-score with new query.
		delete(m.sessions, key)
	}

	// Execute full recall pipeline to produce scored candidates.
	candidates := m.buildCandidates(store, query, category, projectPath, ownerID)

	sess := &ScrollSession{
		LoopID:     loopID,
		Query:      normalizedQuery,
		Candidates: candidates,
		Position:   0,
		CreatedAt:  time.Now(),
	}
	m.sessions[key] = sess
	return sess
}

// buildCandidates executes the store's recall pipeline and returns up to
// scrollSessionMaxCache (200) scored candidates sorted by score descending.
//
// LOCK ORDERING NOTE: The manager lock (m.mu) is released before calling into
// the store because store methods acquire store.mu internally. Holding both
// locks simultaneously would create a deadlock risk (lock ordering: store.mu
// must never be acquired while m.mu is held). After relock, the caller
// (GetOrCreate) assigns the result to a new session, so concurrent map
// modifications during the unlocked window are safe — any stale session that
// was replaced won't be referenced.
func (m *ScrollSessionManager) buildCandidates(
	store *Store,
	query string,
	category Category,
	projectPath string,
	ownerID string,
) []recallScored {
	// Release manager lock while calling store (avoids deadlock with store.mu).
	m.mu.Unlock()

	// recallScoredForPagination executes the full multi-signal fusion pipeline
	// and returns all scored candidates without the standard 15-entry/2500-token
	// limits. Same method used by CursorPaginator.
	candidates := store.recallScoredForPagination(query, category, projectPath, ownerID)

	m.mu.Lock()

	// Cap at scrollSessionMaxCache (200).
	if len(candidates) > scrollSessionMaxCache {
		candidates = candidates[:scrollSessionMaxCache]
	}
	return candidates
}

// scrollSessionTTL is the maximum lifetime of a scroll session before it's
// considered stale and eligible for eviction. Scroll sessions span multiple
// agent loop iterations, so the TTL is longer than cursor TTL (5min).
const scrollSessionTTL = 10 * time.Minute

// Advance returns the next slice of entries from the session's cached
// candidates. Each page contains up to 15 entries or pageTokenBudget tokens,
// whichever limit is reached first. When the cache is exhausted, returns
// SessionExhausted: true with empty entries.
//
// As a side effect, evicts any sessions that have exceeded scrollSessionTTL
// to prevent unbounded accumulation if Destroy is never called (e.g., process crash).
func (m *ScrollSessionManager) Advance(loopID string, pageTokenBudget int, ownerID ...string) (*ScrollResult, error) {
	if pageTokenBudget <= 0 {
		pageTokenBudget = perPageTokenBudget
	}

	owner := ""
	if len(ownerID) > 0 {
		owner = ownerID[0]
	}
	key := sessionKey(owner, loopID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Periodic TTL eviction — remove stale sessions to prevent memory leaks
	// when Destroy is never called (process crash, unrecovered panic).
	m.evictExpiredLocked()

	sess, ok := m.sessions[key]
	if !ok {
		return &ScrollResult{
			Entries:          nil,
			SessionExhausted: true,
		}, nil
	}

	// Cache exhausted.
	if sess.Position >= len(sess.Candidates) {
		return &ScrollResult{
			Entries:          nil,
			SessionExhausted: true,
		}, nil
	}

	const maxEntriesPerPage = 15
	var entries []Entry
	tokenBudget := pageTokenBudget

	for sess.Position < len(sess.Candidates) && len(entries) < maxEntriesPerPage {
		candidate := sess.Candidates[sess.Position]
		tokens := EstimateTextTokens(candidate.entry.Content)
		if tokens > tokenBudget && len(entries) > 0 {
			// Would exceed budget and we already have some entries.
			break
		}
		tokenBudget -= tokens
		entries = append(entries, candidate.entry)
		sess.Position++
		if tokenBudget <= 0 {
			break
		}
	}

	return &ScrollResult{
		Entries:          entries,
		SessionExhausted: false,
	}, nil
}

// evictExpiredLocked removes scroll sessions that have exceeded scrollSessionTTL.
// Called during Advance to piggyback cleanup on regular operations (no background goroutine needed).
// Caller must hold m.mu.
func (m *ScrollSessionManager) evictExpiredLocked() {
	now := time.Now()
	for key, sess := range m.sessions {
		if now.Sub(sess.CreatedAt) > scrollSessionTTL {
			delete(m.sessions, key)
		}
	}
}

// Destroy removes the scroll session for the given loopID. Called when the
// agent loop execution completes (normal exit, cancel, or error).
func (m *ScrollSessionManager) Destroy(loopID string, ownerID ...string) {
	owner := ""
	if len(ownerID) > 0 {
		owner = ownerID[0]
	}
	key := sessionKey(owner, loopID)

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, key)
}
