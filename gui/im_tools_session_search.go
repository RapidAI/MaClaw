package main

// session_search tool handler: performs full-text search across historical
// session transcripts using the FTS5 index in corelib/session/store.go.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/session"
)

// getSessionStore returns the session search store, lazily initializing it
// if not already created. Returns nil if initialization fails.
func (h *IMMessageHandler) getSessionStore() *session.Store {
	if h.app == nil {
		return nil
	}
	// Check if the app already has a session store field; if not, create one.
	h.app.sessionStoreMu.Do(func() {
		dbPath := h.app.sessionSearchDBPath()
		store, err := session.NewStore(dbPath)
		if err != nil {
			// Log but don't crash — search is a best-effort feature.
			fmt.Printf("[session_search] failed to open store: %v\n", err)
			return
		}
		h.app.sessionSearchStore = store
	})
	return h.app.sessionSearchStore
}

// toolSessionSearch handles the session_search tool call.
// Parameters:
//   - query (string, required): the search query
//   - max_results (int, optional, default 10): maximum number of results
func (h *IMMessageHandler) toolSessionSearch(args map[string]interface{}) string {
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}

	maxResults := 10
	if raw, ok := args["max_results"]; ok {
		switch v := raw.(type) {
		case float64:
			if int(v) > 0 {
				maxResults = int(v)
			}
		case int:
			if v > 0 {
				maxResults = v
			}
		}
	}

	store := h.getSessionStore()
	if store == nil {
		return "session search store 未初始化"
	}

	results, err := store.Search(query, maxResults)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}

	// The store returns a single result with Snippet="no results found" for empty results.
	if len(results) == 1 && results[0].SessionID == "" && results[0].Snippet == "no results found" {
		return "no results found"
	}

	// Format results as a readable string.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条匹配结果:\n\n", len(results)))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("--- 结果 %d ---\n", i+1))
		b.WriteString(fmt.Sprintf("会话 ID: %s\n", r.SessionID))
		b.WriteString(fmt.Sprintf("时间: %s\n", r.Timestamp))
		b.WriteString(fmt.Sprintf("平台: %s\n", r.Platform))
		if r.Topic != "" {
			b.WriteString(fmt.Sprintf("主题: %s\n", r.Topic))
		}
		b.WriteString(fmt.Sprintf("片段: %s\n", r.Snippet))
		b.WriteByte('\n')
	}
	return b.String()
}
