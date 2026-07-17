package lansengergroupsummary

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
)

// IsSummaryCommand reports whether cleaned user text is the bare /summary command
// (or Chinese alias). Extra arguments are not treated as the command.
func IsSummaryCommand(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// Normalize fullwidth solidus and case for Latin commands.
	t = strings.ReplaceAll(t, "／", "/")
	lower := strings.ToLower(t)
	return lower == "/summary" || t == "/摘要"
}

// FilterSummaryCommands removes pure /summary command lines from the set to
// summarize so the trigger itself does not pollute the discussion.
func FilterSummaryCommands(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if IsSummaryCommand(m.Text) {
			continue
		}
		// Also drop "@Bot /summary" style residual if strip failed upstream.
		if IsSummaryCommand(stripLeadingAtToken(m.Text)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func stripLeadingAtToken(text string) string {
	t := strings.TrimSpace(text)
	for strings.HasPrefix(t, "@") {
		// Drop first whitespace-delimited token.
		if i := strings.IndexFunc(t, unicode.IsSpace); i > 0 {
			t = strings.TrimSpace(t[i:])
			continue
		}
		return ""
	}
	return t
}

func speakerLabel(m Message) string {
	name := strings.TrimSpace(m.SpeakerName)
	if name == "" {
		name = strings.TrimSpace(m.SpeakerID)
	}
	if name == "" {
		return "成员"
	}
	return name
}

// formatMessageLine renders one transcript line (no trailing newline).
func formatMessageLine(m Message, perMsgMaxRunes int) string {
	if perMsgMaxRunes <= 0 {
		perMsgMaxRunes = DefaultPerMessageMaxRunes
	}
	ts := m.At.Local().Format("01-02 15:04")
	return fmt.Sprintf("[%s] %s: %s", ts, speakerLabel(m), truncateRunes(m.Text, perMsgMaxRunes))
}

// FormatTranscript renders messages as a dated speaker: text block.
func FormatTranscript(msgs []Message, perMsgMaxRunes int) string {
	if perMsgMaxRunes <= 0 {
		perMsgMaxRunes = DefaultPerMessageMaxRunes
	}
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(msgs) * 96)
	for _, m := range msgs {
		b.WriteString(formatMessageLine(m, perMsgMaxRunes))
		b.WriteByte('\n')
	}
	return b.String()
}

// MaxSeq returns the highest Seq in msgs (0 if empty).
func MaxSeq(msgs []Message) int64 {
	var max int64
	for _, m := range msgs {
		if m.Seq > max {
			max = m.Seq
		}
	}
	return max
}

// PreferOldest keeps the leading (older) messages whose formatted transcript
// fits within maxTokens. Newer trailing messages are dropped.
//
// This preserves seq-cursor correctness: after summarizing kept messages,
// MarkSummarized(MaxSeq(kept)) leaves the dropped tail as still-new for the
// next /summary. Dropping the head (low seq) and then marking MaxSeq(kept)
// would permanently skip unsummarized content — never do that.
func PreferOldest(msgs []Message, maxTokens, perMsgMaxRunes int) (kept []Message, dropped int) {
	if len(msgs) == 0 {
		return msgs, 0
	}
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTotalInputTokens
	}
	if perMsgMaxRunes <= 0 {
		perMsgMaxRunes = DefaultPerMessageMaxRunes
	}

	// Single forward pass: avoid building the full transcript just to re-walk it.
	var out []Message
	tokens := 0
	for _, m := range msgs {
		line := formatMessageLine(m, perMsgMaxRunes) + "\n"
		lt := corelib.EstimateTextTokens(line)
		if len(out) > 0 && tokens+lt > maxTokens {
			return out, len(msgs) - len(out)
		}
		// Always keep at least one message even if it alone exceeds budget.
		out = append(out, m)
		tokens += lt
	}
	return msgs, 0
}

// SplitWaves partitions messages into chronological waves that each fit
// approximately maxTokens. Oldest content is in wave 0.
// If more than maxWaves would be needed, the final wave absorbs the remainder
// (BuildChunks may PreferOldest-trim that last oversized wave).
func SplitWaves(msgs []Message, maxTokens, perMsgMaxRunes, maxWaves int) [][]Message {
	if len(msgs) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTotalInputTokens
	}
	if perMsgMaxRunes <= 0 {
		perMsgMaxRunes = DefaultPerMessageMaxRunes
	}
	if maxWaves <= 0 {
		maxWaves = DefaultMaxWaves
	}

	// Partition in one forward pass (no full-transcript pre-scan).
	var waves [][]Message
	var cur []Message
	tokens := 0
	for i, m := range msgs {
		line := formatMessageLine(m, perMsgMaxRunes) + "\n"
		lt := corelib.EstimateTextTokens(line)
		if len(cur) > 0 && tokens+lt > maxTokens {
			// Opening another wave would exceed maxWaves → last wave takes the rest.
			if len(waves)+1 >= maxWaves {
				cur = append(cur, msgs[i:]...)
				waves = append(waves, cur)
				return waves
			}
			waves = append(waves, cur)
			cur = nil
			tokens = 0
		}
		cur = append(cur, m)
		tokens += lt
	}
	if len(cur) > 0 {
		waves = append(waves, cur)
	}
	if len(waves) == 0 {
		return nil
	}
	return waves
}

// BuildChunks splits messages into token-budgeted chunks for map-reduce.
// When the full transcript fits in singlePassMaxTokens, a single chunk is returned.
// At most maxChunks chunks are produced.
func BuildChunks(msgs []Message, chunkMaxTokens, singlePassMaxTokens, perMsgMaxRunes int) []Chunk {
	return BuildChunksCapped(msgs, chunkMaxTokens, singlePassMaxTokens, perMsgMaxRunes, DefaultMaxMapChunks)
}

// BuildChunksCapped is BuildChunks with an explicit map-chunk cap.
func BuildChunksCapped(msgs []Message, chunkMaxTokens, singlePassMaxTokens, perMsgMaxRunes, maxChunks int) []Chunk {
	if len(msgs) == 0 {
		return nil
	}
	if chunkMaxTokens <= 0 {
		chunkMaxTokens = DefaultChunkMaxTokens
	}
	if singlePassMaxTokens <= 0 {
		singlePassMaxTokens = DefaultSinglePassMaxTokens
	}
	if perMsgMaxRunes <= 0 {
		perMsgMaxRunes = DefaultPerMessageMaxRunes
	}
	if maxChunks <= 0 {
		maxChunks = DefaultMaxMapChunks
	}

	// One wave should already be sized by SplitWaves; if a single wave is still
	// huge (e.g. last absorbing wave), keep the oldest prefix so MaxSeq(kept)
	// can advance the cursor without skipping unsummarized head content.
	budget := chunkMaxTokens * maxChunks
	if budget > DefaultMaxTotalInputTokens {
		budget = DefaultMaxTotalInputTokens
	}
	msgs, _ = PreferOldest(msgs, budget, perMsgMaxRunes)

	full := FormatTranscript(msgs, perMsgMaxRunes)
	fullTokens := corelib.EstimateTextTokens(full)
	if fullTokens <= singlePassMaxTokens {
		return []Chunk{{
			Index:         0,
			Messages:      append([]Message(nil), msgs...),
			Formatted:     full,
			TokenEstimate: fullTokens,
		}}
	}

	var chunks []Chunk
	var cur []Message
	var curFormatted strings.Builder
	curTokens := 0

	appendChunk := func(ms []Message, formatted string, tokens int) {
		if len(ms) == 0 {
			return
		}
		if len(chunks) >= maxChunks {
			// Strict cap: fold into the last chunk (may slightly exceed budget).
			last := &chunks[len(chunks)-1]
			last.Messages = append(last.Messages, ms...)
			last.Formatted += formatted
			last.TokenEstimate += tokens
			return
		}
		chunks = append(chunks, Chunk{
			Index:         len(chunks),
			Messages:      append([]Message(nil), ms...),
			Formatted:     formatted,
			TokenEstimate: tokens,
		})
	}

	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := curFormatted.String()
		appendChunk(cur, text, corelib.EstimateTextTokens(text))
		cur = cur[:0]
		curFormatted.Reset()
		curTokens = 0
	}

	for _, m := range msgs {
		line := formatMessageLine(m, perMsgMaxRunes) + "\n"
		lineTokens := corelib.EstimateTextTokens(line)
		// Oversized single message: its own chunk (already per-msg truncated).
		if lineTokens > chunkMaxTokens {
			flush()
			appendChunk([]Message{m}, line, lineTokens)
			continue
		}
		if len(cur) > 0 && curTokens+lineTokens > chunkMaxTokens {
			flush()
		}
		cur = append(cur, m)
		curFormatted.WriteString(line)
		curTokens += lineTokens
	}
	flush()

	for i := range chunks {
		chunks[i].Index = i
	}
	return chunks
}

// TruncateToTokenBudget shortens text from the start so the estimated token
// count is <= maxTokens. Keeps the tail (more recent partials).
func TruncateToTokenBudget(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return text
	}
	if corelib.EstimateTextTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	lo, hi := 0, len(runes)
	best := string(runes[len(runes)/2:])
	for lo < hi {
		mid := (lo + hi) / 2
		cand := string(runes[mid:])
		if corelib.EstimateTextTokens(cand) <= maxTokens {
			best = cand
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if best == text {
		return text
	}
	return "…(前文已省略)…\n" + strings.TrimLeft(best, "\n")
}

// TimeRangeLabel describes the covered window for display.
func TimeRangeLabel(msgs []Message) string {
	if len(msgs) == 0 {
		return ""
	}
	start := msgs[0].At
	end := msgs[0].At
	for _, m := range msgs[1:] {
		if !m.At.IsZero() && (start.IsZero() || m.At.Before(start)) {
			start = m.At
		}
		if m.At.After(end) {
			end = m.At
		}
	}
	if start.IsZero() || end.IsZero() {
		return ""
	}
	loc := time.Local
	if start.In(loc).Format("2006-01-02") == end.In(loc).Format("2006-01-02") {
		return fmt.Sprintf("%s – %s", start.In(loc).Format("01-02 15:04"), end.In(loc).Format("15:04"))
	}
	return fmt.Sprintf("%s – %s", start.In(loc).Format("01-02 15:04"), end.In(loc).Format("01-02 15:04"))
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
