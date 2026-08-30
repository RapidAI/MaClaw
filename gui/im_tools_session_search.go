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
		dbPath := h.getSessionSearchDBPath()
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
	if args == nil {
		args = map[string]interface{}{}
	}
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
	// A model-controlled result count should not turn an accidental broad query
	// into a large transcript injection. The normal UI asks for at most ten.
	const maxSessionSearchResults = 20
	if maxResults > maxSessionSearchResults {
		maxResults = maxSessionSearchResults
	}

	store := h.getSessionStore()
	if store == nil {
		return "session search store 未初始化"
	}
	ownerID := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrent(args)
	if ownerID == "" {
		return "session search owner is missing; refusing an unscoped cross-session search"
	}
	anchor, anchored := taskIdentityAnchorFromToolArgs(args)
	if !anchored {
		anchor, anchored = h.taskIdentityAnchorForUser(ownerID)
	}
	if anchored && len(anchor.SourcePaths) > 0 && !taskAnchorAllowsCrossTaskRecall(sessionSearchUserText(args)) {
		return "[system rejected] 当前任务已绑定对象和源文件。请先重读当前来源完成续写/浓缩；只有用户明确要求搜索历史会话时，才能检索其它任务。"
	}

	// Never search another user's transcripts. This used to call Search(),
	// which made any old desktop task eligible as a substitute source.
	results, err := store.SearchOwned(query, ownerID, maxResults)
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

// sessionSearchUserText only accepts dispatcher-injected current-turn text.
// Direct legacy callers can still perform an owned search when no source-bound
// task is active, but cannot smuggle an authorization flag into a bound task.
func sessionSearchUserText(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	text, _ := args["_user_text"].(string)
	return text
}

func taskIdentityAnchorFromToolArgs(args map[string]interface{}) (taskIdentityAnchor, bool) {
	if args == nil {
		return taskIdentityAnchor{}, false
	}
	anchor, ok := args["_task_identity_anchor"].(taskIdentityAnchor)
	if !ok || !taskIdentityAnchorEnforced(anchor) {
		return taskIdentityAnchor{}, false
	}
	return anchor, true
}
