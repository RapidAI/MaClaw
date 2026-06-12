package agent

// topic_detector.go — topic switch detection logic extracted from
// gui/im_topic_detector.go as part of the agent-unification plan.
//
// The entire struct and all methods are migrated here. gui/ will
// import and alias these types.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/viterin/vek/vek32"
)

// TopicDecision is the result of topic switch detection.
type TopicDecision int

const (
	// TopicSame means the new message continues the current conversation topic.
	TopicSame TopicDecision = iota
	// TopicNew means the new message starts a new topic; context should be cleared.
	TopicNew
)

// TopicSwitchDetector detects when a user's new message is about a different
// topic than the current conversation, enabling automatic context clearing.
//
// It uses a multi-signal voting approach:
//   - BM25 lexical scoring (word overlap)
//   - Cosine similarity via embedding vectors (semantic similarity)
//   - Conversation recency (active conversation protection)
//   - LLM confirmation (last resort for ambiguous cases)
//
// TopicNew is only returned when multiple signals agree.
type TopicSwitchDetector struct {
	// BM25SameThreshold: above this → BM25 votes "same".
	BM25SameThreshold float64
	// BM25NewThreshold: below this → BM25 votes "new".
	BM25NewThreshold float64
	// CosineSameThreshold: above this → embedding votes "same".
	CosineSameThreshold float64
	// CosineNewThreshold: below this → embedding votes "new".
	CosineNewThreshold float64
	// TimeDecayMinutes: idle time after which decay starts.
	TimeDecayMinutes float64
	// ActiveConversationMinutes: if last assistant reply is within this
	// window, the conversation is considered "active" and TopicNew requires
	// stronger evidence (all signals must agree).
	ActiveConversationMinutes float64
	// MinTurnsForDetection: don't run detection if fewer than this many
	// user turns exist.
	MinTurnsForDetection int
	// ShortMessageWords: messages with fewer than this many "words" skip
	// detection entirely. Word count is language-aware: CJK characters each
	// count as one word, non-CJK tokens are split by whitespace.
	ShortMessageWords int
	// LLMTimeout is the maximum time to wait for the LLM confirmation call.
	LLMTimeout time.Duration

	LLMClient func() (*http.Client, corelib.MaclawLLMConfig)
	Embedder  func() embedding.Embedder
}

// NewTopicSwitchDetector creates a TopicSwitchDetector with default thresholds.
func NewTopicSwitchDetector(llmClient func() (*http.Client, corelib.MaclawLLMConfig)) *TopicSwitchDetector {
	return &TopicSwitchDetector{
		BM25SameThreshold:         1.0,
		BM25NewThreshold:          0.3,
		CosineSameThreshold:       0.45,
		CosineNewThreshold:        0.25,
		TimeDecayMinutes:          30,
		ActiveConversationMinutes: 2,
		MinTurnsForDetection:      3,
		ShortMessageWords:         4,
		LLMTimeout:                30 * time.Second,
		LLMClient:                 llmClient,
	}
}

// SignalVote represents a single signal's opinion.
type SignalVote int

const (
	VoteSame    SignalVote = iota // signal says same topic
	VoteNew                       // signal says new topic
	VoteUnsure                    // signal is in the ambiguous zone
	VoteAbstain                   // signal unavailable (e.g. no embedder)
)

// Detect checks whether newMessage is a continuation of the user's current
// conversation or a new topic. Returns TopicNew if context should be cleared.
func (d *TopicSwitchDetector) Detect(newMessage string, userID string, mem *ConversationMemory) TopicDecision {
	entries := mem.Load(userID)
	if len(entries) == 0 {
		return TopicSame // first message, nothing to clear
	}

	// Collect recent user and assistant messages as context.
	var userTexts []string
	var allTexts []string // user + assistant for richer context
	for _, e := range entries {
		text, ok := e.Content.(string)
		if !ok || text == "" {
			continue
		}
		if e.Role == "user" {
			userTexts = append(userTexts, text)
		}
		if e.Role == "user" || e.Role == "assistant" {
			allTexts = append(allTexts, text)
		}
	}
	lastAccess := mem.LastAccessTime(userID)
	if len(userTexts) < d.MinTurnsForDetection {
		return TopicSame // too few turns to judge
	}

	if CountWords(newMessage) < d.ShortMessageWords {
		return TopicSame
	}

	// Determine if conversation is "active" (recent interaction).
	isActive := false
	if !lastAccess.IsZero() && d.ActiveConversationMinutes > 0 {
		if time.Since(lastAccess).Minutes() < d.ActiveConversationMinutes {
			isActive = true
		}
	}

	// Build context text from both user and assistant messages for richer signal.
	if len(allTexts) > 8 {
		allTexts = allTexts[len(allTexts)-8:]
	}
	contextText := strings.Join(allTexts, "\n")

	// --- Signal 1: BM25 lexical scoring ---
	bm25Vote := d.ScoreBM25(contextText, newMessage, lastAccess)

	// --- Signal 2: Embedding cosine similarity ---
	cosineVote := d.ScoreEmbedding(contextText, newMessage)

	// --- Voting logic ---
	return d.Vote(bm25Vote, cosineVote, isActive, contextText, newMessage)
}

// ScoreBM25 returns the BM25 signal vote with time decay applied.
func (d *TopicSwitchDetector) ScoreBM25(contextText, newMessage string, lastAccess time.Time) SignalVote {
	idx := bm25.New()
	idx.Rebuild([]bm25.Doc{{ID: "ctx", Text: contextText}})
	scores := idx.Score(newMessage)
	rawScore := scores["ctx"]

	// Apply time decay.
	decay := 1.0
	if !lastAccess.IsZero() && d.TimeDecayMinutes > 0 {
		elapsed := time.Since(lastAccess).Minutes()
		if elapsed > d.TimeDecayMinutes {
			excess := elapsed - d.TimeDecayMinutes
			decay = 1.0 - excess/d.TimeDecayMinutes
			if decay < 0 {
				decay = 0
			}
		}
	}
	adjusted := rawScore * decay

	log.Printf("[TopicDetector] bm25: raw=%.2f decay=%.2f adjusted=%.2f", rawScore, decay, adjusted)

	if adjusted >= d.BM25SameThreshold {
		return VoteSame
	}
	if adjusted <= d.BM25NewThreshold {
		return VoteNew
	}
	return VoteUnsure
}

// ScoreEmbedding returns the embedding cosine similarity vote.
// Returns VoteAbstain if no embedder is available.
func (d *TopicSwitchDetector) ScoreEmbedding(contextText, newMessage string) SignalVote {
	if d.Embedder == nil {
		return VoteAbstain
	}
	emb := d.Embedder()
	if emb == nil || embedding.IsNoop(emb) {
		return VoteAbstain
	}

	ctxVec, err := emb.Embed(TruncateRunes(contextText, 512))
	if err != nil || len(ctxVec) == 0 {
		return VoteAbstain
	}
	msgVec, err := emb.Embed(TruncateRunes(newMessage, 512))
	if err != nil || len(msgVec) == 0 {
		return VoteAbstain
	}

	if len(ctxVec) != len(msgVec) {
		return VoteAbstain
	}
	cosine := float64(vek32.Dot(ctxVec, msgVec))
	log.Printf("[TopicDetector] embedding cosine=%.3f", cosine)

	if cosine >= d.CosineSameThreshold {
		return VoteSame
	}
	if cosine <= d.CosineNewThreshold {
		return VoteNew
	}
	return VoteUnsure
}

// Vote combines signals to produce a final decision.
//
// Rules:
//   - If any available signal says "same" → TopicSame (conservative).
//   - If conversation is active, require ALL available signals to say "new".
//   - If both BM25 and embedding say "new" → TopicNew.
//   - If one says "new" and the other is unsure/abstain → ask LLM.
//   - Otherwise → TopicSame.
func (d *TopicSwitchDetector) Vote(bm25Vote, cosineVote SignalVote, isActive bool, contextText, newMessage string) TopicDecision {
	// Conservative: any "same" vote blocks topic switch.
	if bm25Vote == VoteSame || cosineVote == VoteSame {
		return TopicSame
	}

	// Active conversation protection.
	if isActive {
		newCount := 0
		availCount := 0
		for _, v := range []SignalVote{bm25Vote, cosineVote} {
			if v != VoteAbstain {
				availCount++
				if v == VoteNew {
					newCount++
				}
			}
		}
		if availCount < 2 || newCount < availCount {
			return TopicSame
		}
		log.Printf("[TopicDetector] active conversation: all %d signals say new → TopicNew", newCount)
		return TopicNew
	}

	// Non-active: both signals agree on "new" → clear.
	if bm25Vote == VoteNew && cosineVote == VoteNew {
		log.Printf("[TopicDetector] both signals say new → TopicNew")
		return TopicNew
	}

	// One says "new", other is unsure or abstain → ask LLM as tiebreaker.
	if bm25Vote == VoteNew || cosineVote == VoteNew {
		if d.LLMClient == nil {
			return TopicSame // conservative fallback
		}
		decision := d.ConfirmWithLLM(contextText, newMessage)
		log.Printf("[TopicDetector] llm tiebreaker → %v", decision)
		return decision
	}

	// Both unsure/abstain → conservative.
	return TopicSame
}

// ConfirmWithLLM makes a very short LLM call (~50-100 tokens) to determine
// if the new message is a topic switch. Returns TopicSame on any error.
func (d *TopicSwitchDetector) ConfirmWithLLM(contextText, newMessage string) TopicDecision {
	httpClient, cfg := d.LLMClient()
	if cfg.URL == "" || cfg.Model == "" {
		return TopicSame
	}

	contextText = TruncateRunes(contextText, 200)
	newMessage = TruncateRunes(newMessage, 200)

	messages := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "判断用户的新消息是否延续之前的对话话题。只回答 same 或 new，不要解释。",
		},
		map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("之前的话题:\n%s\n\n新消息:\n%s", contextText, newMessage),
		},
	}

	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: d.LLMTimeout}
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.LLMTimeout)
	defer cancel()

	var req *http.Request
	var err error
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		req, _, _, err = llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{
			Stream:    false,
			ExtraBody: map[string]interface{}{"max_tokens": 10},
		})
	} else {
		req, _, _, err = llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
			Stream: false,
			ExtraBody: map[string]interface{}{
				"max_tokens": 10,
			},
		})
	}
	if err != nil {
		return TopicSame
	}

	resp, err := client.Do(req)
	if err != nil {
		return TopicSame
	}
	defer resp.Body.Close()

	var parsed *llm.Response
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		parsed, err = llm.ParseNonStreamResponsesAPIResponse(resp)
	} else {
		parsed, err = llm.ParseNonStreamOpenAIResponse(resp)
	}
	if err != nil || len(parsed.Choices) == 0 {
		return TopicSame
	}

	answer := strings.TrimSpace(strings.ToLower(parsed.Choices[0].Message.Content))
	if answer == "" {
		answer = strings.TrimSpace(strings.ToLower(parsed.Choices[0].Message.ReasoningContent))
	}
	if strings.Contains(answer, "new") {
		return TopicNew
	}
	return TopicSame
}

// ---------------------------------------------------------------------------
// Standalone utility functions (exported)
// ---------------------------------------------------------------------------

// CountWords returns a language-aware word count for a message.
func CountWords(s string) int {
	count := 0
	inLatinWord := false
	for _, r := range s {
		if IsCJK(r) {
			if inLatinWord {
				count++
				inLatinWord = false
			}
			count++
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if inLatinWord {
				count++
				inLatinWord = false
			}
		} else {
			if !inLatinWord {
				inLatinWord = true
			}
		}
	}
	if inLatinWord {
		count++
	}
	return count
}

// IsCJK returns true if the rune is a CJK ideograph, Hiragana, Katakana, or Hangul.
func IsCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// TruncateRunes truncates a string to at most n runes, preserving
// multi-byte UTF-8 characters.
func TruncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// BuildQuickSummary creates a one-line summary from conversation entries
// for archival before auto-clearing.
func BuildQuickSummary(entries []ConversationEntry) string {
	var lastUserText string
	for _, e := range entries {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && text != "" {
				lastUserText = text
			}
		}
	}
	if lastUserText == "" {
		return ""
	}
	runes := []rune(lastUserText)
	if len(runes) > 100 {
		lastUserText = string(runes[:100]) + "..."
	}
	return "对话话题: " + lastUserText
}
