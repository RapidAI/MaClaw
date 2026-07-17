// Package lansengergroupsummary buffers Lansenger (蓝信) group chat messages
// so the bot can produce on-demand discussion summaries via /summary.
package lansengergroupsummary

import "time"

const (
	// StoreDirName is the sub-directory under the MaClaw base dir.
	StoreDirName = "lansenger_group_summary"
	// MessagesDirName holds per-group append-only JSONL message logs.
	MessagesDirName = "groups"
	// StateDirName holds per-group summary cursor state.
	StateDirName = "state"

	// DefaultMaxMessagesPerGroup caps retained messages per group (FIFO).
	DefaultMaxMessagesPerGroup = 5000
	// DefaultMaxMessageAge is how long messages are kept before prune.
	DefaultMaxMessageAge = 7 * 24 * time.Hour
	// DefaultPruneEveryNAppends avoids rewriting JSONL on every hot-path write.
	DefaultPruneEveryNAppends = 64

	// DefaultChunkMaxTokens is the soft input budget for one map-phase LLM call
	// (system prompt + transcript). Conservative so primary models with 8k–32k
	// windows stay safe; long histories use map-reduce.
	DefaultChunkMaxTokens = 6000
	// DefaultSinglePassMaxTokens: below this, skip map-reduce and summarize once.
	DefaultSinglePassMaxTokens = 5500
	// DefaultMaxMapChunks caps map-phase LLM calls (cost + latency guard).
	DefaultMaxMapChunks = 12
	// DefaultMaxTotalInputTokens is one map-reduce wave budget
	// (chunkMax * maxMapChunks). Longer histories are split into sequential waves.
	DefaultMaxTotalInputTokens = DefaultChunkMaxTokens * DefaultMaxMapChunks
	// DefaultMaxWaves caps sequential waves so /summary cannot run unbounded LLM calls.
	// Worst case ≈ MaxWaves * (MaxMapChunks + 1) + 1 reduce-of-waves calls.
	DefaultMaxWaves = 4
	// DefaultMaxReduceInputTokens caps the reduce-phase user prompt size.
	DefaultMaxReduceInputTokens = 6000
	// DefaultMaxOutputRunes caps the final summary text returned to the group.
	DefaultMaxOutputRunes = 2500
	// DefaultPerMessageMaxRunes truncates a single chat line in the prompt.
	DefaultPerMessageMaxRunes = 800
)

// Message is one buffered group chat line.
type Message struct {
	Seq         int64     `json:"seq"`
	MessageID   string    `json:"message_id,omitempty"`
	SpeakerID   string    `json:"speaker_id,omitempty"`
	SpeakerName string    `json:"speaker_name,omitempty"`
	Text        string    `json:"text"`
	At          time.Time `json:"at"`
}

// GroupState tracks the summary cursor for one group.
type GroupState struct {
	GroupID        string    `json:"group_id"`
	GroupName      string    `json:"group_name,omitempty"`
	LastSummaryAt  time.Time `json:"last_summary_at,omitempty"`
	LastSummarySeq int64     `json:"last_summary_seq"` // messages with Seq > this are "new"
	NextSeq        int64     `json:"next_seq"`
	AppendsSince   int       `json:"appends_since_prune,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

// Chunk is a contiguous slice of messages for one map-phase call.
type Chunk struct {
	Index    int
	Messages []Message
	// Formatted is the ready-to-send transcript block for this chunk.
	Formatted string
	// TokenEstimate is EstimateTextTokens(Formatted).
	TokenEstimate int
}
