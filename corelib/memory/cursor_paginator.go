package memory

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// CursorPaginator manages cursor-based pagination state for recall results.
// Each user has a pool of at most maxCursorsPerUser cursors, evicted LRU by
// LastUsedAt. Cursors expire after cursorTTL (5 min).
// ---------------------------------------------------------------------------

// CursorPaginator manages the lifecycle of paginated recall sessions.
type CursorPaginator struct {
	mu      sync.Mutex
	cursors map[string]*userCursorPool // keyed by userID
}

// userCursorPool holds up to maxCursorsPerUser cursors per user, evicted LRU.
type userCursorPool struct {
	pool []*RecallCursor
}

// RecallCursor holds the full scored result set from the initial recall and
// tracks the current position for pagination.
type RecallCursor struct {
	ID          string
	UserID      string
	Query       string
	Category    Category
	ProjectPath string
	Candidates  []recallScored // full scored+sorted list from initial recall
	Position    int            // next entry index to return
	PageNum     int            // current page number (incremented by buildPage)
	CreatedAt   time.Time
	LastUsedAt  time.Time
}

// maxPageEntries limits entries per page (token budget is the primary limit).
const maxPageEntries = 15

// NewCursorPaginator creates a new CursorPaginator.
func NewCursorPaginator() *CursorPaginator {
	return &CursorPaginator{
		cursors: make(map[string]*userCursorPool),
	}
}

// FirstPage executes the full recall pipeline via the store and returns the
// first page of results along with a cursor token if more pages exist.
//
// Requirement 1.10: When a new recall action has a different query while a
// previous cursor for the same caller is still active, the previous cursor
// is invalidated. This ensures only the most recent query's pagination state
// is active per user, preventing stale result sets from persisting.
func (p *CursorPaginator) FirstPage(store *Store, query string, category Category, projectPath string, ownerID string) (*PaginatedResult, error) {
	// Execute the full scoring pipeline to get all scored candidates.
	candidates := store.recallScoredForPagination(query, category, projectPath, ownerID)

	// Generate unique cursor ID.
	cursorID := generateCursorID()

	cursor := &RecallCursor{
		ID:          cursorID,
		UserID:      ownerID,
		Query:       query,
		Category:    category,
		ProjectPath: projectPath,
		Candidates:  candidates,
		Position:    0,
		CreatedAt:   time.Now(),
		LastUsedAt:  time.Now(),
	}

	// Requirement 1.10: invalidate any existing cursors for this user that
	// have a different query. A new recall with a different query parameter
	// means the caller has moved on; stale cursors should not persist.
	p.mu.Lock()
	p.invalidateDifferentQueryCursorsLocked(ownerID, query)
	p.addCursorLocked(ownerID, cursor)
	p.mu.Unlock()

	// Build first page.
	return p.buildPage(cursor)
}

// invalidateDifferentQueryCursorsLocked removes all cursors for the user whose
// query differs from the given query. Implements Requirement 1.10.
// Caller must hold p.mu.
func (p *CursorPaginator) invalidateDifferentQueryCursorsLocked(userID, query string) {
	pool, ok := p.cursors[userID]
	if !ok {
		return
	}
	alive := pool.pool[:0]
	for _, c := range pool.pool {
		if c.Query == query {
			alive = append(alive, c)
		}
	}
	pool.pool = alive
	if len(pool.pool) == 0 {
		delete(p.cursors, userID)
	}
}

// NextPage returns the next slice from the cursor's candidate list.
// The paginator lock is held through buildPage to prevent data races when
// concurrent calls reference the same cursor (buildPage mutates Position/PageNum).
func (p *CursorPaginator) NextPage(cursorID string) (*PaginatedResult, error) {
	// Decode the cursor token to get cursor ID and user ID.
	payload, err := DecodeCursor(cursorID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	cursor := p.findCursorLocked(payload.UserID, payload.CursorID)
	if cursor == nil {
		p.mu.Unlock()
		return nil, ErrCursorNotFound
	}

	// Check TTL.
	if time.Since(cursor.CreatedAt) > cursorTTL {
		p.removeCursorLocked(payload.UserID, payload.CursorID)
		p.mu.Unlock()
		return nil, ErrCursorExpired
	}

	cursor.LastUsedAt = time.Now()
	// Hold lock through buildPage — it only operates on the cursor's in-memory
	// slice (no store calls, no deadlock risk) and mutates cursor.Position/PageNum.
	result, err := p.buildPage(cursor)
	p.mu.Unlock()
	return result, err
}

// Evict removes expired cursors for a given user (>5min TTL).
func (p *CursorPaginator) Evict(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pool, ok := p.cursors[userID]
	if !ok {
		return
	}

	now := time.Now()
	alive := pool.pool[:0]
	for _, c := range pool.pool {
		if now.Sub(c.CreatedAt) <= cursorTTL {
			alive = append(alive, c)
		}
	}
	pool.pool = alive

	if len(pool.pool) == 0 {
		delete(p.cursors, userID)
	}
}

// buildPage slices the cursor's candidates into a token-bounded page.
// Page number is tracked incrementally via cursor.PageNum to avoid an O(N²)
// re-simulation of previous page boundaries on every call.
func (p *CursorPaginator) buildPage(cursor *RecallCursor) (*PaginatedResult, error) {
	startPos := cursor.Position
	tokenBudget := perPageTokenBudget
	var entries []Entry

	for i := startPos; i < len(cursor.Candidates) && len(entries) < maxPageEntries; i++ {
		tokens := EstimateTextTokens(cursor.Candidates[i].entry.Content)
		if tokens > tokenBudget {
			// Entry doesn't fit in remaining budget. If we haven't added any
			// entries yet, include at least one to guarantee forward progress.
			if len(entries) == 0 {
				entries = append(entries, cursor.Candidates[i].entry)
				cursor.Position = i + 1
			}
			break
		}
		tokenBudget -= tokens
		entries = append(entries, cursor.Candidates[i].entry)
		cursor.Position = i + 1
	}

	// If we didn't advance position (no entries fit at all), force advance
	// past one entry to guarantee forward progress and avoid infinite loops.
	if cursor.Position == startPos && startPos < len(cursor.Candidates) {
		entries = append(entries, cursor.Candidates[startPos].entry)
		cursor.Position = startPos + 1
	}

	cursor.PageNum++
	hasMore := cursor.Position < len(cursor.Candidates)

	result := &PaginatedResult{
		Entries: entries,
		HasMore: hasMore,
		Page:    cursor.PageNum,
	}

	if hasMore {
		result.Cursor = EncodeCursor(cursor.ID, cursor.UserID, cursor.CreatedAt)
	}

	return result, nil
}

// addCursorLocked adds a cursor to the user's pool, evicting LRU if at capacity.
// Caller must hold p.mu.
func (p *CursorPaginator) addCursorLocked(userID string, cursor *RecallCursor) {
	pool, ok := p.cursors[userID]
	if !ok {
		pool = &userCursorPool{}
		p.cursors[userID] = pool
	}

	// Evict expired cursors first.
	now := time.Now()
	alive := pool.pool[:0]
	for _, c := range pool.pool {
		if now.Sub(c.CreatedAt) <= cursorTTL {
			alive = append(alive, c)
		}
	}
	pool.pool = alive

	// If still at capacity, evict oldest by LastUsedAt.
	if len(pool.pool) >= maxCursorsPerUser {
		sort.SliceStable(pool.pool, func(i, j int) bool {
			return pool.pool[i].LastUsedAt.Before(pool.pool[j].LastUsedAt)
		})
		pool.pool = pool.pool[1:] // remove oldest
	}

	pool.pool = append(pool.pool, cursor)
}

// findCursorLocked finds a cursor by ID in the user's pool.
// Caller must hold p.mu.
func (p *CursorPaginator) findCursorLocked(userID, cursorID string) *RecallCursor {
	pool, ok := p.cursors[userID]
	if !ok {
		return nil
	}
	for _, c := range pool.pool {
		if c.ID == cursorID {
			return c
		}
	}
	return nil
}

// removeCursorLocked removes a specific cursor from the user's pool.
// Caller must hold p.mu.
func (p *CursorPaginator) removeCursorLocked(userID, cursorID string) {
	pool, ok := p.cursors[userID]
	if !ok {
		return
	}
	for i, c := range pool.pool {
		if c.ID == cursorID {
			pool.pool = append(pool.pool[:i], pool.pool[i+1:]...)
			break
		}
	}
	if len(pool.pool) == 0 {
		delete(p.cursors, userID)
	}
}

// ActiveCursorsForUser returns the count of active cursors for a user.
// Useful for testing.
func (p *CursorPaginator) ActiveCursorsForUser(userID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	pool, ok := p.cursors[userID]
	if !ok {
		return 0
	}
	return len(pool.pool)
}

// generateCursorID creates a unique cursor identifier using crypto/rand.
func generateCursorID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
