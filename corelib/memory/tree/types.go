package tree

// Memory Tree: hierarchical summarization architecture.
// Inspired by OpenHuman's memory/tree/ module — organizes knowledge into
// concentric layers that get progressively more compressed over time.
//
// Architecture:
//   L0 (chunks)  → raw content chunks (≤500 tokens each)
//   L1 (daily)   → daily summaries (seal L0 chunks by day)
//   L2 (weekly)  → weekly summaries (seal L1 by week)
//   L3 (monthly) → monthly summaries (seal L2 by month)
//
// Retrieval: top-down expansion (L3 → L2 → L1 → L0 as needed).
// Storage: SQLite or JSON files per level.

import (
	"time"
)

// TreeLevel represents the compression level of a node.
type TreeLevel int

const (
	LevelChunk   TreeLevel = 0 // raw content chunk (≤500 tokens)
	LevelDaily   TreeLevel = 1 // daily summary
	LevelWeekly  TreeLevel = 2 // weekly summary
	LevelMonthly TreeLevel = 3 // monthly summary
)

// String returns a human-readable level name.
func (l TreeLevel) String() string {
	switch l {
	case LevelChunk:
		return "chunk"
	case LevelDaily:
		return "daily"
	case LevelWeekly:
		return "weekly"
	case LevelMonthly:
		return "monthly"
	default:
		return "unknown"
	}
}

// TreeNode represents a single node in the memory tree.
type TreeNode struct {
	ID         string    `json:"id"`
	Level      TreeLevel `json:"level"`
	Content    string    `json:"content"`     // summary text (≤3K tokens at higher levels)
	Source     string    `json:"source"`      // data source ("conversation" | "tool" | "workflow" | "external")
	Topic      string    `json:"topic"`       // topic cluster label
	Children   []string  `json:"children"`    // child node IDs
	ParentID   string    `json:"parent_id"`   // parent node ID (empty for roots)
	CreatedAt  time.Time `json:"created_at"`
	SealedAt   time.Time `json:"sealed_at"`   // when this node was sealed (summarized)
	TokenCount int       `json:"token_count"` // estimated token count of Content
	Tags       []string  `json:"tags"`        // searchable tags
}

// IsSealable returns true if this node has enough children to be sealed
// into a higher-level summary.
func (n *TreeNode) IsSealable(minChildren int) bool {
	return len(n.Children) >= minChildren
}

// SealConfig defines when and how nodes are sealed (summarized) to the next level.
type SealConfig struct {
	// MinChunksForDaily is the minimum L0 chunks needed to create an L1 daily summary.
	MinChunksForDaily int
	// MinDailiesForWeekly is the minimum L1 nodes needed to create an L2 weekly summary.
	MinDailiesForWeekly int
	// MinWeekliesForMonthly is the minimum L2 nodes needed to create an L3 monthly summary.
	MinWeekliesForMonthly int
	// MaxChunkTokens is the maximum token count for a single L0 chunk.
	MaxChunkTokens int
	// MaxSummaryTokens is the maximum token count for L1+ summaries.
	MaxSummaryTokens int
}

// DefaultSealConfig returns sensible defaults for the seal configuration.
func DefaultSealConfig() SealConfig {
	return SealConfig{
		MinChunksForDaily:     3,
		MinDailiesForWeekly:   5,
		MinWeekliesForMonthly: 3,
		MaxChunkTokens:        500,
		MaxSummaryTokens:      1500,
	}
}

// TreeStore is the interface for persisting tree nodes.
type TreeStore interface {
	// Save persists a node.
	Save(node *TreeNode) error
	// Get retrieves a node by ID.
	Get(id string) (*TreeNode, error)
	// ListByLevel returns all nodes at a given level.
	ListByLevel(level TreeLevel) ([]*TreeNode, error)
	// ListByLevelAndDate returns nodes at a level within a date range.
	ListByLevelAndDate(level TreeLevel, from, to time.Time) ([]*TreeNode, error)
	// ListChildren returns all children of a node.
	ListChildren(parentID string) ([]*TreeNode, error)
	// Delete removes a node.
	Delete(id string) error
	// Search finds nodes matching a query (across all levels).
	Search(query string, maxResults int) ([]*TreeNode, error)
}

// Summarizer is the function that compresses multiple chunks into a summary.
// It takes the concatenated content of child nodes and returns a summary.
type Summarizer func(content string) (string, error)
