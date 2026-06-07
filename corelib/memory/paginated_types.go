package memory

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// ---------------------------------------------------------------------------
// Constants for multi-page recall mechanisms.
// ---------------------------------------------------------------------------

const (
	// defaultMaxEntries is the standard proactive recall entry count.
	defaultMaxEntries = 12
	// expandedMaxEntries is the upper bound when adaptive expansion triggers.
	expandedMaxEntries = 24

	// defaultMaxTokens is the standard proactive recall token budget.
	defaultMaxTokens = 2500
	// expandedMaxTokens is the upper bound when adaptive expansion triggers.
	expandedMaxTokens = 5000

	// topicDensityThreshold triggers adaptive expansion when exceeded.
	topicDensityThreshold = 0.15
	// expansionFactor scales matchingEntries to compute expanded count.
	expansionFactor = 0.4

	// exhaustiveMaxEntries caps entries in exhaustive recall mode.
	exhaustiveMaxEntries = 100
	// exhaustiveMaxTokens caps token budget in exhaustive recall mode.
	exhaustiveMaxTokens = 15000

	// cursorTTL is how long a recall cursor stays valid after creation.
	cursorTTL = 5 * time.Minute
	// maxCursorsPerUser limits active cursors per user (LRU eviction).
	maxCursorsPerUser = 10

	// scrollSessionMaxCache bounds scored candidates cached per scroll session.
	scrollSessionMaxCache = 200
	// perPageTokenBudget limits tokens returned per paginated page.
	perPageTokenBudget = 2500
)

// ---------------------------------------------------------------------------
// Result types for multi-page recall.
// ---------------------------------------------------------------------------

// PaginatedResult is returned by cursor-based paginated recall.
type PaginatedResult struct {
	Entries []Entry `json:"entries"`
	Cursor  string  `json:"cursor,omitempty"`  // opaque token; empty when no more pages
	HasMore bool    `json:"has_more"`
	Page    int     `json:"page"` // 1-indexed
}

// ScrollResult is returned by scroll-through recall within an agent loop.
type ScrollResult struct {
	Entries          []Entry `json:"entries"`
	SessionExhausted bool    `json:"session_exhausted"`
}

// ExhaustiveResult is returned by exhaustive recall mode.
type ExhaustiveResult struct {
	Entries       []Entry `json:"entries"`
	Truncated     bool    `json:"truncated"`
	TotalMatching int     `json:"total_matching"`
}

// AdaptiveBudgetResult holds the computed budget for proactive recall.
type AdaptiveBudgetResult struct {
	MaxEntries   int     `json:"max_entries"`
	MaxTokens    int     `json:"max_tokens"`
	Expanded     bool    `json:"expanded"`
	TopicDensity float64 `json:"topic_density"`
}

// StagedRecallResult holds results from the staged recall pipeline.
type StagedRecallResult struct {
	Entries      []Entry       `json:"entries"`
	StageReached string        `json:"stage_reached"` // "bm25_only" | "bm25_vec" | "full"
	Elapsed      time.Duration `json:"elapsed"`
	Partial      bool          `json:"partial"` // true if not all stages completed
}

// ---------------------------------------------------------------------------
// Cursor token encoding / decoding.
// ---------------------------------------------------------------------------

// cursorPayload is the internal structure encoded into the opaque cursor token.
// The actual recall state (candidates, position) lives server-side in
// CursorPaginator; this payload is just a lookup key with embedded expiry hint.
type cursorPayload struct {
	CursorID  string `json:"c"`
	UserID    string `json:"u"`
	CreatedAt int64  `json:"t"` // unix timestamp for quick client-side expiry check
}

// ErrCursorExpired is returned when a cursor token has exceeded its TTL.
var ErrCursorExpired = errors.New("cursor expired or invalid, please start a new recall")

// ErrCursorNotFound is returned when a cursor ID does not match any active cursor.
var ErrCursorNotFound = errors.New("cursor not found")

// EncodeCursor encodes a cursorPayload into a base64 URL-safe opaque token.
func EncodeCursor(cursorID, userID string, createdAt time.Time) string {
	p := cursorPayload{
		CursorID:  cursorID,
		UserID:    userID,
		CreatedAt: createdAt.Unix(),
	}
	data, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor decodes a base64 URL-safe token back into a cursorPayload.
// Returns an error if the token is malformed or expired (based on cursorTTL).
func DecodeCursor(token string) (*cursorPayload, error) {
	if token == "" {
		return nil, ErrCursorNotFound
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrCursorExpired
	}
	var p cursorPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, ErrCursorExpired
	}
	if p.CursorID == "" || p.UserID == "" {
		return nil, ErrCursorExpired
	}
	// Quick expiry check based on embedded timestamp.
	if time.Since(time.Unix(p.CreatedAt, 0)) > cursorTTL {
		return nil, ErrCursorExpired
	}
	return &p, nil
}
