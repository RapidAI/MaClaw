package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
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

// topicSwitchDetector detects when a user's new message is about a different
// topic than the current conversation, enabling automatic context clearing.
//
// It uses a multi-signal voting approach:
//   - BM25 lexical scoring (word overlap)
//   - Cosine similarity via embedding vectors (semantic similarity)
//   - Conversation recency (active conversation protection)
//   - LLM confirmation (last resort for ambiguous cases)
//
// TopicNew is only returned when multiple signals agree.
type topicSwitchDetector struct {
	// bm25SameThreshold: above this → BM25 votes "same".
	bm25SameThreshold float64
	// bm25NewThreshold: below this → BM25 votes "new".
	bm25NewThreshold float64
	// cosineSameThreshold: above this → embedding votes "same".
	cosineSameThreshold float64
	// cosineNewThreshold: below this → embedding votes "new".
	cosineNewThreshold float64
	// timeDecayMinutes: idle time after which decay starts.
	timeDecayMinutes float64
	// activeConversationMinutes: if last assistant reply is within this
	// window, the conversation is considered "active" and TopicNew requires
	// stronger evidence (all signals must agree).
	activeConversationMinutes float64
	// minTurnsForDetection: don't run detection if fewer than this many
	// user turns exist.
	minTurnsForDetection int
	// shortMessageWords: messages with fewer than this many "words" skip
	// detection entirely. Word count is language-aware: CJK characters each
	// count as one word, non-CJK tokens are split by whitespace.
	shortMessageWords int
	// llmTimeout is the maximum time to wait for the LLM confirmation call.
	llmTimeout time.Duration

	llmClient func() (*http.Client, MaclawLLMConfig)
	embedder  func() embedding.Embedder
}

func newTopicSwitchDetector(llmClient func() (*http.Client, MaclawLLMConfig)) *topicSwitchDetector {
	return &topicSwitchDetector{
		bm25SameThreshold:         1.0,
		bm25NewThreshold:          0.3,
		cosineSameThreshold:       0.45,
		cosineNewThreshold:        0.25,
		timeDecayMinutes:          30,
		activeConversationMinutes: 2,
		minTurnsForDetection:      3,
		shortMessageWords:         4,
		llmTimeout:                5 * time.Second,
		llmClient:                 llmClient,
	}
}

// detect checks whether newMessage is a continuation of the user's current
// conversation or a new topic. Returns TopicNew if context should be cleared.
func (d *topicSwitchDetector) detect(newMessage string, userID string, mem *conversationMemory) TopicDecision {
	entries := mem.load(userID)
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
	// Use the memory's lastAccessTime as a proxy for conversation recency,
	// since the handler calls save() after each assistant response.
	lastAccess := mem.lastAccessTime(userID)
	if len(userTexts) < d.minTurnsForDetection {
		return TopicSame // too few turns to judge
	}

	// Guard: skip topic detection for very short messages.
	// Uses language-aware word counting so the threshold works consistently
	// across Chinese ("重启下" = 3 words), English ("restart" = 1 word),
	// and mixed text.
	if countWords(newMessage) < d.shortMessageWords {
		return TopicSame
	}

	// Determine if conversation is "active" (recent interaction).
	isActive := false
	if !lastAccess.IsZero() && d.activeConversationMinutes > 0 {
		if time.Since(lastAccess).Minutes() < d.activeConversationMinutes {
			isActive = true
		}
	}

	// Build context text from both user and assistant messages for richer signal.
	if len(allTexts) > 8 {
		allTexts = allTexts[len(allTexts)-8:]
	}
	contextText := strings.Join(allTexts, "\n")

	// --- Signal 1: BM25 lexical scoring ---
	bm25Vote := d.scoreBM25(contextText, newMessage, lastAccess)

	// --- Signal 2: Embedding cosine similarity ---
	cosineVote := d.scoreEmbedding(contextText, newMessage)

	// --- Voting logic ---
	return d.vote(bm25Vote, cosineVote, isActive, contextText, newMessage)
}

// signalVote represents a single signal's opinion.
type signalVote int

const (
	voteSame    signalVote = iota // signal says same topic
	voteNew                      // signal says new topic
	voteUnsure                   // signal is in the ambiguous zone
	voteAbstain                  // signal unavailable (e.g. no embedder)
)

// scoreBM25 returns the BM25 signal vote with time decay applied.
func (d *topicSwitchDetector) scoreBM25(contextText, newMessage string, lastAccess time.Time) signalVote {
	idx := bm25.New()
	idx.Rebuild([]bm25.Doc{{ID: "ctx", Text: contextText}})
	scores := idx.Score(newMessage)
	rawScore := scores["ctx"]

	// Apply time decay.
	decay := 1.0
	if !lastAccess.IsZero() && d.timeDecayMinutes > 0 {
		elapsed := time.Since(lastAccess).Minutes()
		if elapsed > d.timeDecayMinutes {
			excess := elapsed - d.timeDecayMinutes
			decay = 1.0 - excess/d.timeDecayMinutes
			if decay < 0 {
				decay = 0
			}
		}
	}
	adjusted := rawScore * decay

	log.Printf("[TopicDetector] bm25: raw=%.2f decay=%.2f adjusted=%.2f", rawScore, decay, adjusted)

	if adjusted >= d.bm25SameThreshold {
		return voteSame
	}
	if adjusted <= d.bm25NewThreshold {
		return voteNew
	}
	return voteUnsure
}

// scoreEmbedding returns the embedding cosine similarity vote.
// Returns voteAbstain if no embedder is available.
func (d *topicSwitchDetector) scoreEmbedding(contextText, newMessage string) signalVote {
	if d.embedder == nil {
		return voteAbstain
	}
	emb := d.embedder()
	if emb == nil || embedding.IsNoop(emb) {
		return voteAbstain
	}

	ctxVec, err := emb.Embed(truncateRunes(contextText, 512))
	if err != nil || len(ctxVec) == 0 {
		return voteAbstain
	}
	msgVec, err := emb.Embed(truncateRunes(newMessage, 512))
	if err != nil || len(msgVec) == 0 {
		return voteAbstain
	}

	// Vectors are L2-normalized by the embedder, so dot product = cosine similarity.
	// Guard against mismatched dimensions (shouldn't happen, but be safe).
	if len(ctxVec) != len(msgVec) {
		return voteAbstain
	}
	cosine := float64(vek32.Dot(ctxVec, msgVec))
	log.Printf("[TopicDetector] embedding cosine=%.3f", cosine)

	if cosine >= d.cosineSameThreshold {
		return voteSame
	}
	if cosine <= d.cosineNewThreshold {
		return voteNew
	}
	return voteUnsure
}

// vote combines signals to produce a final decision.
//
// Rules:
//   - If any available signal says "same" → TopicSame (conservative).
//   - If conversation is active, require ALL available signals to say "new".
//   - If both BM25 and embedding say "new" → TopicNew.
//   - If one says "new" and the other is unsure/abstain → ask LLM.
//   - Otherwise → TopicSame.
func (d *topicSwitchDetector) vote(bm25Vote, cosineVote signalVote, isActive bool, contextText, newMessage string) TopicDecision {
	// Conservative: any "same" vote blocks topic switch.
	if bm25Vote == voteSame || cosineVote == voteSame {
		return TopicSame
	}

	// Active conversation protection: require unanimous "new" from at least
	// 2 available signals. A single signal is not enough evidence to clear
	// an active conversation.
	if isActive {
		newCount := 0
		availCount := 0
		for _, v := range []signalVote{bm25Vote, cosineVote} {
			if v != voteAbstain {
				availCount++
				if v == voteNew {
					newCount++
				}
			}
		}
		// Need at least 2 available signals, all agreeing on "new".
		if availCount < 2 || newCount < availCount {
			return TopicSame
		}
		log.Printf("[TopicDetector] active conversation: all %d signals say new → TopicNew", newCount)
		return TopicNew
	}

	// Non-active: both signals agree on "new" → clear.
	if bm25Vote == voteNew && cosineVote == voteNew {
		log.Printf("[TopicDetector] both signals say new → TopicNew")
		return TopicNew
	}

	// One says "new", other is unsure or abstain → ask LLM as tiebreaker.
	if bm25Vote == voteNew || cosineVote == voteNew {
		if d.llmClient == nil {
			return TopicSame // conservative fallback
		}
		decision := d.confirmWithLLM(contextText, newMessage)
		log.Printf("[TopicDetector] llm tiebreaker → %v", decision)
		return decision
	}

	// Both unsure/abstain → conservative.
	return TopicSame
}

// confirmWithLLM makes a very short LLM call (~50-100 tokens) to determine
// if the new message is a topic switch. Returns TopicSame on any error.
func (d *topicSwitchDetector) confirmWithLLM(contextText, newMessage string) TopicDecision {
	httpClient, cfg := d.llmClient()
	if cfg.URL == "" || cfg.Model == "" {
		return TopicSame
	}

	contextText = truncateRunes(contextText, 200)
	newMessage = truncateRunes(newMessage, 200)

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

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   messages,
		"max_tokens": 10,
	}
	data, _ := json.Marshal(reqBody)

	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"

	ctx, cancel := context.WithTimeout(context.Background(), d.llmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return TopicSame
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: d.llmTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return TopicSame
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return TopicSame
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Choices) == 0 {
		return TopicSame
	}

	answer := strings.TrimSpace(strings.ToLower(result.Choices[0].Message.Content))
	if strings.Contains(answer, "new") {
		return TopicNew
	}
	return TopicSame
}

// countWords returns a language-aware word count for a message.
// Each CJK ideograph (Han, Katakana, Hiragana, Hangul) counts as one word.
// Non-CJK segments are split by whitespace. This gives consistent thresholds
// across languages: "重启下" = 3, "restart" = 1, "restart the server" = 3,
// "帮我看看" = 4, "ok" = 1.
func countWords(s string) int {
	count := 0
	inLatinWord := false
	for _, r := range s {
		if isCJK(r) {
			if inLatinWord {
				count++ // close pending latin word
				inLatinWord = false
			}
			count++
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if inLatinWord {
				count++
				inLatinWord = false
			}
		} else {
			// Latin/Cyrillic/etc letter or digit.
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

// isCJK returns true if the rune is a CJK ideograph, Hiragana, Katakana, or Hangul.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// truncateRunes truncates a string to at most n runes, preserving
// multi-byte UTF-8 characters.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// buildQuickSummary creates a one-line summary from conversation entries
// for archival before auto-clearing.
func buildQuickSummary(entries []conversationEntry) string {
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
