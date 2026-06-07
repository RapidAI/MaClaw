package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// PageIndex — cross-page context retrieval for compacted conversation pages.
// ---------------------------------------------------------------------------

const (
	// maxPagesPerUser is the hard cap on indexed pages per user.
	maxPagesPerUser = 20
	// keepPagesAfterEviction is how many recent pages survive FIFO eviction.
	keepPagesAfterEviction = 15
	// maxItemsPerPage caps indexed items extracted from a single page.
	maxItemsPerPage = 50
	// toolOutputSummaryLen caps tool output summaries at first N characters.
	toolOutputSummaryLen = 200

	// recencyBoostMax is the boost applied to the most recent page.
	recencyBoostMax = 3.0
	// recencyBoostDecay is how much the boost decays per page distance.
	recencyBoostDecay = 1.0
	// recencyBoostMin is the minimum boost applied to any page.
	recencyBoostMin = 0.5
)

// PageIndex maintains per-user indexes of compacted page content for
// cross-page retrieval. Lives in corelib/memory/, usable by all hosts
// without host-specific logic.
type PageIndex struct {
	mu    sync.RWMutex
	users map[string]*userPageIndex
}

// userPageIndex holds the indexed pages for a single user.
type userPageIndex struct {
	pages []indexedPage // ordered oldest-first; max maxPagesPerUser
}

// indexedPage holds extracted items from one compacted page.
type indexedPage struct {
	PageID    string
	Timestamp time.Time
	Items     []pageIndexItem
}

// pageIndexItem is a single indexed fact from a compacted page.
type pageIndexItem struct {
	Content     string // file path, tool output summary, decision text, or entity name
	Kind        string // "file_path" | "tool_output" | "decision" | "entity"
	Fingerprint string // SHA-256 of content for dedup
}

// pageIndexCandidate is a scored query result from the PageIndex.
type pageIndexCandidate struct {
	Content   string
	Kind      string
	PageID    string
	Score     float64
	PageIndex int // 0 = most recent page
}

// NewPageIndex creates a new empty PageIndex.
func NewPageIndex() *PageIndex {
	return &PageIndex{
		users: make(map[string]*userPageIndex),
	}
}

// IndexCompactedPage extracts and indexes items from entries being compacted.
// Called by the host when trimHistoryWithSummary creates a compaction boundary.
func (pi *PageIndex) IndexCompactedPage(userID string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	// Extract items from entries.
	items := extractPageItems(entries)

	// Cap at maxItemsPerPage.
	if len(items) > maxItemsPerPage {
		items = items[:maxItemsPerPage]
	}

	// Generate page ID based on timestamp.
	pageID := generatePageID(userID, time.Now())

	page := indexedPage{
		PageID:    pageID,
		Timestamp: time.Now(),
		Items:     items,
	}

	pi.mu.Lock()
	defer pi.mu.Unlock()

	upi, ok := pi.users[userID]
	if !ok {
		upi = &userPageIndex{}
		pi.users[userID] = upi
	}

	// Dedup: remove items whose fingerprints already exist in other pages.
	page.Items = pi.dedupItemsLocked(upi, page.Items)

	upi.pages = append(upi.pages, page)

	// FIFO eviction: if over maxPagesPerUser, keep most recent keepPagesAfterEviction.
	if len(upi.pages) > maxPagesPerUser {
		upi.pages = upi.pages[len(upi.pages)-keepPagesAfterEviction:]
	}

	return nil
}

// Query returns page-indexed items matching the query with BM25-like scoring
// and page-proximity recency boost.
func (pi *PageIndex) Query(userID string, query string, queryTokens []string) []pageIndexCandidate {
	if query == "" && len(queryTokens) == 0 {
		return nil
	}

	pi.mu.RLock()
	defer pi.mu.RUnlock()

	upi, ok := pi.users[userID]
	if !ok || len(upi.pages) == 0 {
		return nil
	}

	// Tokenize the query for BM25-like scoring.
	qTokens := queryTokens
	if len(qTokens) == 0 {
		qTokens = tokenizeForBM25(query)
	}
	if len(qTokens) == 0 {
		return nil
	}

	totalPages := len(upi.pages)
	var candidates []pageIndexCandidate

	for i, page := range upi.pages {
		// Page distance from most recent: 0 = most recent, 1 = second most recent, etc.
		pageDistance := totalPages - 1 - i
		recencyBoost := math.Max(recencyBoostMin, recencyBoostMax-float64(pageDistance)*recencyBoostDecay)

		for _, item := range page.Items {
			score := scoreBM25Like(item.Content, qTokens)
			if score <= 0 {
				continue
			}
			// Apply recency boost.
			finalScore := score + recencyBoost

			candidates = append(candidates, pageIndexCandidate{
				Content:   item.Content,
				Kind:      item.Kind,
				PageID:    page.PageID,
				Score:     finalScore,
				PageIndex: pageDistance,
			})
		}
	}

	// Sort by score descending.
	sortPageCandidates(candidates)

	return candidates
}

// Clear removes all indexed pages for a user.
func (pi *PageIndex) Clear(userID string) {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	delete(pi.users, userID)
}

// PageCount returns the number of indexed pages for a user (for testing).
func (pi *PageIndex) PageCount(userID string) int {
	pi.mu.RLock()
	defer pi.mu.RUnlock()
	upi, ok := pi.users[userID]
	if !ok {
		return 0
	}
	return len(upi.pages)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// extractPageItems extracts file paths, tool output summaries, decisions,
// and entity names from a set of entries.
func extractPageItems(entries []Entry) []pageIndexItem {
	var items []pageIndexItem
	seen := make(map[string]bool)

	for _, e := range entries {
		// Extract file paths from content.
		paths := extractFilePaths(e.Content)
		for _, p := range paths {
			fp := fingerprint(p)
			if seen[fp] {
				continue
			}
			seen[fp] = true
			items = append(items, pageIndexItem{
				Content:     p,
				Kind:        "file_path",
				Fingerprint: fp,
			})
		}

		// Extract tool output summaries (first 200 chars of content for tool-like entries).
		if isToolOutput(e) {
			summary := truncateToChars(e.Content, toolOutputSummaryLen)
			if summary != "" {
				fp := fingerprint(summary)
				if !seen[fp] {
					seen[fp] = true
					items = append(items, pageIndexItem{
						Content:     summary,
						Kind:        "tool_output",
						Fingerprint: fp,
					})
				}
			}
		}

		// Extract decisions from content.
		decisions := extractDecisions(e.Content)
		for _, d := range decisions {
			fp := fingerprint(d)
			if seen[fp] {
				continue
			}
			seen[fp] = true
			items = append(items, pageIndexItem{
				Content:     d,
				Kind:        "decision",
				Fingerprint: fp,
			})
		}

		// Extract entity names via ExpandQuery.
		expanded := ExpandQuery(e.Content)
		for _, entity := range expanded.Entities {
			fp := fingerprint(entity)
			if seen[fp] {
				continue
			}
			seen[fp] = true
			items = append(items, pageIndexItem{
				Content:     entity,
				Kind:        "entity",
				Fingerprint: fp,
			})
		}

		// Also index tags as entities.
		for _, tag := range e.Tags {
			if tag == "" {
				continue
			}
			fp := fingerprint(tag)
			if seen[fp] {
				continue
			}
			seen[fp] = true
			items = append(items, pageIndexItem{
				Content:     tag,
				Kind:        "entity",
				Fingerprint: fp,
			})
		}

		if len(items) >= maxItemsPerPage {
			break
		}
	}

	if len(items) > maxItemsPerPage {
		items = items[:maxItemsPerPage]
	}
	return items
}

// dedupItemsLocked removes items whose fingerprint already exists in other pages.
// Caller must hold pi.mu (write lock).
func (pi *PageIndex) dedupItemsLocked(upi *userPageIndex, items []pageIndexItem) []pageIndexItem {
	existing := make(map[string]bool)
	for _, page := range upi.pages {
		for _, item := range page.Items {
			existing[item.Fingerprint] = true
		}
	}

	var deduped []pageIndexItem
	for _, item := range items {
		if !existing[item.Fingerprint] {
			deduped = append(deduped, item)
		}
	}
	return deduped
}

// generatePageID creates a unique page identifier.
func generatePageID(userID string, ts time.Time) string {
	data := userID + "|" + ts.Format(time.RFC3339Nano)
	hash := sha256.Sum256([]byte(data))
	return "page_" + hex.EncodeToString(hash[:8])
}

// fingerprint computes a SHA-256 fingerprint of content for dedup.
func fingerprint(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:16])
}

// extractFilePaths extracts Windows and Unix file paths from text.
func extractFilePaths(text string) []string {
	var paths []string
	seen := make(map[string]bool)

	// Split into words/tokens and check each.
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, "\"'`(),;:[]{}|")
		if isFilePath(word) && !seen[word] {
			seen[word] = true
			paths = append(paths, word)
		}
	}
	return paths
}

// isFilePath returns true if the string looks like a file path.
func isFilePath(s string) bool {
	if len(s) < 3 {
		return false
	}
	// Windows absolute path: C:\... or D:\...
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	// Unix absolute path: /home/... /usr/... /opt/... /tmp/... /var/...
	if s[0] == '/' && len(s) > 1 && s[1] != '/' {
		// Avoid matching URLs like //...
		return true
	}
	// Home directory path: ~/...
	if strings.HasPrefix(s, "~/") {
		return true
	}
	return false
}

// isToolOutput returns true if the entry looks like a tool output.
func isToolOutput(e Entry) bool {
	// Entries with certain categories or tags that indicate tool output.
	content := strings.ToLower(e.Content)
	if strings.Contains(content, "tool_result") ||
		strings.Contains(content, "tool output") ||
		strings.Contains(content, "执行结果") ||
		strings.Contains(content, "命令输出") {
		return true
	}
	// Check for content patterns that look like command output.
	if strings.HasPrefix(e.Content, "$") || strings.HasPrefix(e.Content, ">") {
		return true
	}
	// Entries from assistant/tool role with structured content.
	if e.Category == "session_checkpoint" || e.Category == "conversation_summary" {
		return true
	}
	return false
}

// extractDecisions extracts decision-like text from content.
func extractDecisions(text string) []string {
	var decisions []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		// Look for decision markers.
		if strings.Contains(lower, "决定") ||
			strings.Contains(lower, "确认") ||
			strings.Contains(lower, "chosen") ||
			strings.Contains(lower, "decided") ||
			strings.Contains(lower, "confirmed") ||
			strings.Contains(lower, "selected") ||
			strings.Contains(lower, "选择") ||
			strings.Contains(lower, "采用") {
			if len(line) > 10 && len(line) < 500 {
				decisions = append(decisions, line)
			}
		}
	}
	return decisions
}

// truncateToChars truncates text to at most n characters.
func truncateToChars(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// tokenizeForBM25 splits a query into tokens for BM25-like scoring.
func tokenizeForBM25(query string) []string {
	query = strings.ToLower(query)
	var tokens []string
	seen := make(map[string]bool)

	for _, word := range strings.Fields(query) {
		word = strings.Trim(word, ".,;:!?\"'`()[]{}|")
		if len(word) < 2 {
			continue
		}
		if seen[word] {
			continue
		}
		seen[word] = true
		tokens = append(tokens, word)
	}
	return tokens
}

// scoreBM25Like computes a lightweight BM25-inspired TF-IDF score for a
// single item against the query tokens. Since we don't have corpus-level
// IDF statistics, we use term frequency with length normalization.
func scoreBM25Like(content string, queryTokens []string) float64 {
	contentLower := strings.ToLower(content)
	contentTokens := strings.Fields(contentLower)
	docLen := len(contentTokens)
	if docLen == 0 {
		// For short items (file paths, entities), use substring matching.
		if contentLower == "" {
			return 0
		}
		return scoreSubstring(contentLower, queryTokens)
	}

	// BM25 parameters.
	const k1 = 1.2
	const b = 0.75
	const avgDL = 10.0 // assumed average document length for short items

	var score float64
	for _, qt := range queryTokens {
		// Count term frequency.
		tf := 0
		for _, ct := range contentTokens {
			if ct == qt {
				tf++
			} else if len(qt) >= 3 && strings.Contains(ct, qt) {
				// Only substring match if query token is at least 3 chars
				// to avoid spurious matches from very short tokens.
				tf++
			} else if len(ct) >= 3 && strings.Contains(qt, ct) {
				// Content token contains query token (partial match).
				tf++
			}
		}
		if tf == 0 {
			// Also check substring match for Chinese/CJK text.
			if strings.Contains(contentLower, qt) {
				tf = 1
			}
		}
		if tf == 0 {
			continue
		}

		// BM25 TF saturation.
		tfFloat := float64(tf)
		denom := tfFloat + k1*(1-b+b*float64(docLen)/avgDL)
		if denom <= 0 {
			continue
		}
		tfScore := (tfFloat * (k1 + 1)) / denom

		// Simple IDF approximation: treat each query token as moderately rare.
		idf := 1.0
		score += idf * tfScore
	}

	return score
}

// scoreSubstring scores very short items (like file paths) using substring matching.
func scoreSubstring(contentLower string, queryTokens []string) float64 {
	var score float64
	for _, qt := range queryTokens {
		if strings.Contains(contentLower, qt) {
			score += 1.0
		}
	}
	return score
}

// sortPageCandidates sorts candidates by score descending.
func sortPageCandidates(candidates []pageIndexCandidate) {
	// Simple insertion sort — candidate lists are typically small.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].Score > candidates[j-1].Score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
}
