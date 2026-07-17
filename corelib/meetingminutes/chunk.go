// Package meetingminutes helps build meeting minutes from long ASR transcripts
// without blowing the model context window (map-reduce over disk-backed text).
package meetingminutes

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	// DefaultChunkMaxTokens is the soft budget for one map-phase chunk.
	DefaultChunkMaxTokens = 4000
	// DefaultSinglePassMaxTokens: below this, skip map-reduce (single LLM call).
	DefaultSinglePassMaxTokens = 3500
	// DefaultMaxMapChunks caps map-phase LLM calls (cost + latency).
	DefaultMaxMapChunks = 10
	// DefaultMaxReduceInputTokens caps reduce-phase input size.
	DefaultMaxReduceInputTokens = 6000
	// DefaultMapOutputRunes is a soft target for each map partial.
	DefaultMapOutputRunes = 900
	// DefaultDraftMaxRunes caps the final draft body returned to the agent.
	DefaultDraftMaxRunes = 6000
)

// TextChunk is one map-phase slice of a plain transcript.
type TextChunk struct {
	Index         int
	Text          string
	TokenEstimate int
}

// SplitPlainText splits a transcript into token-budgeted chunks for map-reduce.
// Prefer paragraph/newline boundaries; falls back to rune slices for huge blobs.
func SplitPlainText(text string, chunkMaxTokens, singlePassMaxTokens, maxChunks int) []TextChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkMaxTokens <= 0 {
		chunkMaxTokens = DefaultChunkMaxTokens
	}
	if singlePassMaxTokens <= 0 {
		singlePassMaxTokens = DefaultSinglePassMaxTokens
	}
	if maxChunks <= 0 {
		maxChunks = DefaultMaxMapChunks
	}

	fullTokens := corelib.EstimateTextTokens(text)
	if fullTokens <= singlePassMaxTokens {
		return []TextChunk{{
			Index:         0,
			Text:          text,
			TokenEstimate: fullTokens,
		}}
	}

	units := splitTranscriptUnits(text)
	var chunks []TextChunk
	var cur strings.Builder
	curTokens := 0

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		body := strings.TrimSpace(cur.String())
		if body == "" {
			cur.Reset()
			curTokens = 0
			return
		}
		if len(chunks) >= maxChunks {
			// Fold into last chunk (may slightly exceed budget).
			last := &chunks[len(chunks)-1]
			last.Text = strings.TrimSpace(last.Text + "\n\n" + body)
			last.TokenEstimate = corelib.EstimateTextTokens(last.Text)
			cur.Reset()
			curTokens = 0
			return
		}
		chunks = append(chunks, TextChunk{
			Index:         len(chunks),
			Text:          body,
			TokenEstimate: corelib.EstimateTextTokens(body),
		})
		cur.Reset()
		curTokens = 0
	}

	for _, unit := range units {
		u := strings.TrimSpace(unit)
		if u == "" {
			continue
		}
		ut := corelib.EstimateTextTokens(u)
		if ut > chunkMaxTokens {
			// Hard-split oversized unit by runes.
			flush()
			for _, piece := range splitOversizedUnit(u, chunkMaxTokens) {
				if len(chunks) >= maxChunks {
					last := &chunks[len(chunks)-1]
					last.Text = strings.TrimSpace(last.Text + "\n\n" + piece)
					last.TokenEstimate = corelib.EstimateTextTokens(last.Text)
					continue
				}
				chunks = append(chunks, TextChunk{
					Index:         len(chunks),
					Text:          piece,
					TokenEstimate: corelib.EstimateTextTokens(piece),
				})
			}
			continue
		}
		if cur.Len() > 0 && curTokens+ut > chunkMaxTokens {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(u)
		curTokens += ut
	}
	flush()

	for i := range chunks {
		chunks[i].Index = i
	}
	return chunks
}

// splitTranscriptUnits prefers blank-line paragraphs, then lines, then sentences.
func splitTranscriptUnits(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	paras := strings.Split(text, "\n\n")
	var units []string
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Very long paragraph: split by lines first.
		if corelib.EstimateTextTokens(p) > DefaultChunkMaxTokens/2 {
			lines := strings.Split(p, "\n")
			var buf strings.Builder
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if buf.Len() == 0 {
					buf.WriteString(line)
					continue
				}
				// Keep short consecutive lines together.
				if corelib.EstimateTextTokens(buf.String())+corelib.EstimateTextTokens(line) < DefaultChunkMaxTokens/3 {
					buf.WriteByte('\n')
					buf.WriteString(line)
					continue
				}
				units = append(units, buf.String())
				buf.Reset()
				buf.WriteString(line)
			}
			if buf.Len() > 0 {
				units = append(units, buf.String())
			}
			continue
		}
		units = append(units, p)
	}
	if len(units) == 0 && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	return units
}

func splitOversizedUnit(text string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = DefaultChunkMaxTokens
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	// Approximate runes per token for mixed CJK/ASCII (~1.2 runes/token average).
	approxRunes := maxTokens
	if approxRunes < 200 {
		approxRunes = 200
	}
	var out []string
	for start := 0; start < len(runes); {
		end := start + approxRunes
		if end > len(runes) {
			end = len(runes)
		}
		// Prefer breaking at whitespace / punctuation.
		if end < len(runes) {
			for i := end; i > start+approxRunes/2; i-- {
				if unicode.IsSpace(runes[i-1]) || isSentenceEnd(runes[i-1]) {
					end = i
					break
				}
			}
		}
		piece := strings.TrimSpace(string(runes[start:end]))
		if piece != "" {
			// If still over budget, shrink.
			for corelib.EstimateTextTokens(piece) > maxTokens && end > start+50 {
				end -= 50
				piece = strings.TrimSpace(string(runes[start:end]))
			}
			out = append(out, piece)
		}
		if end <= start {
			end = start + 1
		}
		start = end
	}
	return out
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '.', '!', '?', ';', '…':
		return true
	default:
		return false
	}
}

// TruncateToTokenBudget keeps the tail of text within maxTokens (like group summary).
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
	return strings.TrimSpace(best)
}

// TruncateRunes caps a string by rune count with an ellipsis.
func TruncateRunes(s string, max int) string {
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

// MapSystemPrompt is the map-phase system prompt for meeting chunk notes.
const MapSystemPrompt = `你是会议转写分段提炼助手。下面是整场会议转写中的一段，请提炼本段要点。

要求：
1. 用简短条目列出：议题、关键发言/观点、决议、待办（事项/责任人/截止若可知）、风险或未决问题。
2. 不要编造转写中不存在的内容；不确定就写「未明确」。
3. 控制在 400 字以内；使用简洁中文要点，勿用复杂表格。
4. 忽略明显的口头禅与无意义寒暄。`

// ReduceSystemPrompt merges map partials into a minutes draft body.
const ReduceSystemPrompt = `你是会议纪要汇总助手。下面是同一场会议的多段分段要点，请合并为一份结构化会议纪要正文（不含完整转写原文）。

要求：
1. 结构（有内容的节才写）：
   - 摘要
   - 关键讨论
   - 决议
   - 待办事项（事项 / 责任人 / 截止 / 依赖）
   - 待确认问题
2. 合并重复点，保留分歧与未决项。
3. 不要编造分段要点中没有的信息。
4. 控制在 1200 字以内；用 Markdown 小标题与列表，便于写入纪要文件。
5. 不要输出「完整转写」专节（完整转写将从磁盘文件原样组装）。`

// SinglePassSystemPrompt is used when the whole transcript fits one call.
const SinglePassSystemPrompt = `你是会议纪要助手。根据完整转写生成结构化会议纪要正文（不含完整转写原文）。

要求：
1. 结构：摘要 → 关键讨论 → 决议 → 待办事项（事项/责任人/截止/依赖）→ 待确认问题。
2. 不要编造；不确定写「未明确」。
3. 控制在 1200 字以内；Markdown 小标题与列表。
4. 不要输出「完整转写」专节（完整转写将从磁盘文件原样组装）。`

// BuildMapUserPrompt formats one map-phase user message.
func BuildMapUserPrompt(title, purpose string, chunk TextChunk, total int) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("会议标题：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if p := strings.TrimSpace(purpose); p != "" {
		b.WriteString("用途：")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("分段：%d/%d\n估计 tokens：%d\n\n转写片段：\n", chunk.Index+1, total, chunk.TokenEstimate))
	b.WriteString(chunk.Text)
	return b.String()
}

// BuildReduceUserPrompt formats the reduce-phase user message from map partials.
func BuildReduceUserPrompt(title, purpose string, partials []string) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("会议标题：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if p := strings.TrimSpace(purpose); p != "" {
		b.WriteString("用途：")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("共 %d 段分段要点，请合并为会议纪要正文（不要完整转写）：\n\n", len(partials)))
	b.WriteString(strings.Join(partials, "\n\n"))
	return TruncateToTokenBudget(b.String(), DefaultMaxReduceInputTokens)
}

// BuildSinglePassUserPrompt formats a single-pass minutes request.
func BuildSinglePassUserPrompt(title, purpose, transcript string) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("会议标题：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if p := strings.TrimSpace(purpose); p != "" {
		b.WriteString("用途：")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\n完整转写：\n")
	b.WriteString(transcript)
	return b.String()
}

// ExtractiveDraft builds a non-LLM fallback draft from head/mid/tail samples.
// Used when the model is unavailable or map-reduce fails.
func ExtractiveDraft(title, purpose, transcript string) string {
	transcript = strings.TrimSpace(transcript)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "会议纪要"
	}
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	if p := strings.TrimSpace(purpose); p != "" {
		b.WriteString("**用途**：")
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	b.WriteString(fmt.Sprintf("**转写规模**：约 %d 字 / est_tokens=%d\n\n", utf8.RuneCountInString(transcript), corelib.EstimateTextTokens(transcript)))
	b.WriteString("> 自动 map-reduce 不可用或失败时的抽取草稿：请结合 transcript_file 核对后完善决议与待办。\n\n")
	b.WriteString("## 摘要\n\n")
	b.WriteString("（待完善）长转写已落盘；以下为开头/中部/结尾抽样，勿当作完整结论。\n\n")
	head, mid, tail := sampleThirds(transcript, 350)
	b.WriteString("## 关键讨论（抽样）\n\n")
	b.WriteString("### 开头\n\n")
	b.WriteString(head)
	b.WriteString("\n\n### 中部\n\n")
	b.WriteString(mid)
	b.WriteString("\n\n### 结尾\n\n")
	b.WriteString(tail)
	b.WriteString("\n\n## 决议\n\n- （待从 transcript_file 核对补充）\n\n")
	b.WriteString("## 待办事项\n\n| 事项 | 责任人 | 截止 | 依赖 |\n| --- | --- | --- | --- |\n| （待补充） |  |  |  |\n\n")
	b.WriteString("## 待确认问题\n\n- （待补充）\n")
	return TruncateRunes(b.String(), DefaultDraftMaxRunes)
}

func sampleThirds(text string, eachRunes int) (head, mid, tail string) {
	runes := []rune(strings.TrimSpace(text))
	n := len(runes)
	if n == 0 {
		return "（空）", "（空）", "（空）"
	}
	if eachRunes < 40 {
		eachRunes = 40
	}
	if n <= eachRunes*3 {
		s := string(runes)
		return s, "（全文较短，见开头）", "（全文较短，见开头）"
	}
	head = string(runes[:eachRunes]) + "…"
	midStart := n/2 - eachRunes/2
	if midStart < 0 {
		midStart = 0
	}
	midEnd := midStart + eachRunes
	if midEnd > n {
		midEnd = n
	}
	mid = "…" + string(runes[midStart:midEnd]) + "…"
	tail = "…" + string(runes[n-eachRunes:])
	return head, mid, tail
}
