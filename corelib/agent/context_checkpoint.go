package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

const (
	defaultCheckpointThresholdPct = 50
	defaultCheckpointKeepGroups   = 12
	defaultCheckpointPreviewLimit = 4096
)

// ContextCheckpointOptions configures lossless model-context checkpoints.
// Compression is fail-closed: without an available reader or a persisted raw
// payload, Conversation is returned unchanged.
type ContextCheckpointOptions struct {
	ContextLimit   int
	ToolsTokens    int
	SessionKey     string
	Tools          []map[string]interface{}
	ThresholdPct   int
	KeepGroups     int
	PreviewLimit   int
	Root           string
	BeforeCompress func() error
	// DryRun evaluates checkpoint eligibility and savings without flushing,
	// persisting a handle, or changing Conversation. Used by shadow rollout.
	DryRun bool
}

type ContextCheckpointResult struct {
	Conversation []interface{}
	Applied      bool
	BeforeTokens int
	AfterTokens  int
	DroppedCount int
	Handle       *toolresult.Handle
	Reason       string
	WouldApply   bool
}

// ContextCheckpointMode controls the internal rollout. There is intentionally
// no user-facing setting: the environment switch is for operators/tests and
// defaults to the conservative active path.
type ContextCheckpointMode string

const (
	ContextCheckpointOff    ContextCheckpointMode = "off"
	ContextCheckpointShadow ContextCheckpointMode = "shadow"
	ContextCheckpointOn     ContextCheckpointMode = "on"
)

type contextCheckpointCounters struct {
	attempted       atomic.Int64
	applied         atomic.Int64
	fallbacks       atomic.Int64
	saved           atomic.Int64
	shadowEvaluated atomic.Int64
	shadowEligible  atomic.Int64
	shadowSaved     atomic.Int64
}

var globalContextCheckpoint contextCheckpointCounters

type ContextCheckpointStats struct {
	Attempted         int64 `json:"attempted"`
	Applied           int64 `json:"applied"`
	Fallbacks         int64 `json:"fallbacks"`
	SavedTokens       int64 `json:"saved_tokens"`
	ShadowEvaluated   int64 `json:"shadow_evaluated,omitempty"`
	ShadowEligible    int64 `json:"shadow_eligible,omitempty"`
	ShadowSavedTokens int64 `json:"shadow_saved_tokens,omitempty"`
}

func CurrentContextCheckpointStats() ContextCheckpointStats {
	return ContextCheckpointStats{
		Attempted:         globalContextCheckpoint.attempted.Load(),
		Applied:           globalContextCheckpoint.applied.Load(),
		Fallbacks:         globalContextCheckpoint.fallbacks.Load(),
		SavedTokens:       globalContextCheckpoint.saved.Load(),
		ShadowEvaluated:   globalContextCheckpoint.shadowEvaluated.Load(),
		ShadowEligible:    globalContextCheckpoint.shadowEligible.Load(),
		ShadowSavedTokens: globalContextCheckpoint.shadowSaved.Load(),
	}
}

// CheckpointConversation replaces completed old message groups with a compact
// structured checkpoint. The exact removed JSON is stored behind a
// read_tool_result handle, so summarization is never the sole information source.
func CheckpointConversation(conversation []interface{}, opts ContextCheckpointOptions) ContextCheckpointResult {
	conversation = FoldComputerUseObserves(conversation)
	result := ContextCheckpointResult{Conversation: conversation}
	if len(conversation) <= 3 || opts.ContextLimit <= 0 {
		result.Reason = "insufficient_context"
		return result
	}
	if !hasToolDefinition(opts.Tools, "read_tool_result") {
		result.Reason = "reader_unavailable"
		return result
	}
	budget := opts.ContextLimit - opts.ToolsTokens
	if budget < 4000 {
		budget = 4000
	}
	threshold := opts.ThresholdPct
	if threshold <= 0 || threshold > 100 {
		threshold = defaultCheckpointThresholdPct
	}
	result.BeforeTokens = EstimateConversationTokens(conversation)
	if result.BeforeTokens*100 < budget*threshold {
		result.Reason = "below_threshold"
		return result
	}

	groups, validGroups := buildCheckpointGroups(conversation[1:])
	if !validGroups {
		result.Reason = "invalid_tool_group"
		return result
	}
	keepGroups := opts.KeepGroups
	if keepGroups <= 0 {
		keepGroups = defaultCheckpointKeepGroups
	}
	if len(groups) <= keepGroups {
		result.Reason = "protected_window"
		return result
	}
	dropGroups := groups[:len(groups)-keepGroups]
	dropped := flattenCheckpointGroups(conversation[1:], dropGroups)
	if len(dropped) == 0 {
		result.Reason = "nothing_to_drop"
		return result
	}
	if checkpointContainsOpaqueContent(dropped) {
		result.Reason = "opaque_content"
		return result
	}
	globalContextCheckpoint.attempted.Add(1)
	if !opts.DryRun && opts.BeforeCompress != nil {
		if err := opts.BeforeCompress(); err != nil {
			globalContextCheckpoint.fallbacks.Add(1)
			result.Reason = "preflight_failed"
			return result
		}
	}
	preview := buildContextCheckpointPreview(dropped)
	limit := opts.PreviewLimit
	if limit <= 0 {
		limit = defaultCheckpointPreviewLimit
	}
	if opts.DryRun {
		globalContextCheckpoint.shadowEvaluated.Add(1)
		kept := groups[len(groups)-keepGroups:]
		next := make([]interface{}, 0, 2+len(conversation)-len(dropped))
		next = append(next, conversation[0])
		next = append(next, map[string]string{"role": "user", "content": toolresult.DefaultPreview(preview, limit)})
		for _, group := range kept {
			next = append(next, conversation[1+group.Start:1+group.End]...)
		}
		result.AfterTokens = EstimateConversationTokens(next)
		if result.AfterTokens >= result.BeforeTokens {
			result.Reason = "no_savings"
			return result
		}
		result.WouldApply = true
		result.DroppedCount = len(dropped)
		result.Reason = "dry_run"
		globalContextCheckpoint.shadowEligible.Add(1)
		globalContextCheckpoint.shadowSaved.Add(int64(result.BeforeTokens - result.AfterTokens))
		return result
	}
	raw, err := json.Marshal(dropped)
	if err != nil {
		globalContextCheckpoint.fallbacks.Add(1)
		result.Reason = "marshal_failed"
		return result
	}
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "context_checkpoint",
		SessionKey: opts.SessionKey,
		Content:    string(raw),
		Preview:    preview,
		Limit:      limit,
		Root:       opts.Root,
		ForceSpill: true,
	})
	if err != nil || projection.Handle == nil || !projection.Spilled {
		globalContextCheckpoint.fallbacks.Add(1)
		result.Reason = "spill_failed"
		return result
	}

	kept := groups[len(groups)-keepGroups:]
	next := make([]interface{}, 0, 2+len(conversation)-len(dropped))
	next = append(next, conversation[0])
	next = append(next, map[string]string{
		"role":    "user",
		"content": projection.Preview,
	})
	for _, group := range kept {
		next = append(next, conversation[1+group.Start:1+group.End]...)
	}
	result.AfterTokens = EstimateConversationTokens(next)
	if result.AfterTokens >= result.BeforeTokens {
		// The just-created handle was never exposed in Conversation, so it is an
		// orphan on this fail-closed path. Remove only this call's fresh file.
		_ = os.Remove(projection.Handle.Path)
		globalContextCheckpoint.fallbacks.Add(1)
		result.Reason = "no_savings"
		return result
	}
	result.Conversation = next
	result.Applied = true
	result.WouldApply = true
	result.DroppedCount = len(dropped)
	result.Handle = projection.Handle
	result.Reason = "applied"
	globalContextCheckpoint.applied.Add(1)
	globalContextCheckpoint.saved.Add(int64(result.BeforeTokens - result.AfterTokens))
	return result
}

func checkpointContainsOpaqueContent(messages []interface{}) bool {
	for _, message := range messages {
		mm, ok := message.(map[string]interface{})
		if !ok {
			continue
		}
		blocks, ok := mm["content"].([]interface{})
		if !ok {
			continue
		}
		for _, block := range blocks {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			kind, _ := bm["type"].(string)
			if kind == "image" || kind == "image_url" || kind == "input_image" {
				return true
			}
		}
	}
	return false
}

func hasToolDefinition(tools []map[string]interface{}, name string) bool {
	for _, def := range tools {
		if tool.ExtractToolName(def) == name {
			return true
		}
	}
	return false
}

func buildCheckpointGroups(tail []interface{}) ([]EntryGroup, bool) {
	groups := make([]EntryGroup, 0, len(tail))
	for i := 0; i < len(tail); {
		start := i
		if MsgRole(tail[i]) == "tool" {
			return nil, false
		}
		if MsgRole(tail[i]) == "assistant" && MsgHasToolCalls(tail[i]) {
			declared, ok := checkpointDeclaredToolCallIDs(tail[i])
			if !ok {
				return nil, false
			}
			i++
			actual := make(map[string]int, len(declared))
			for i < len(tail) && MsgRole(tail[i]) == "tool" {
				id := checkpointToolMessageID(tail[i])
				if id == "" {
					return nil, false
				}
				actual[id]++
				i++
			}
			if !sameCheckpointToolIDs(declared, actual) {
				return nil, false
			}
		} else {
			i++
		}
		groups = append(groups, EntryGroup{Start: start, End: i})
	}
	return groups, true
}

func checkpointDeclaredToolCallIDs(message interface{}) (map[string]int, bool) {
	mm, ok := message.(map[string]interface{})
	if !ok {
		return nil, false
	}
	calls := mm["tool_calls"]
	raw, err := json.Marshal(calls)
	if err != nil {
		return nil, false
	}
	var decoded []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded) == 0 {
		return nil, false
	}
	ids := make(map[string]int, len(decoded))
	for _, call := range decoded {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			return nil, false
		}
		ids[id]++
	}
	return ids, true
}

func checkpointToolMessageID(message interface{}) string {
	mm, ok := message.(map[string]interface{})
	if !ok {
		return ""
	}
	id, _ := mm["tool_call_id"].(string)
	return strings.TrimSpace(id)
}

func sameCheckpointToolIDs(declared, actual map[string]int) bool {
	if len(declared) != len(actual) {
		return false
	}
	for id, count := range declared {
		if count != 1 || actual[id] != 1 {
			return false
		}
	}
	return true
}

func flattenCheckpointGroups(tail []interface{}, groups []EntryGroup) []interface{} {
	var out []interface{}
	for _, group := range groups {
		out = append(out, tail[group.Start:group.End]...)
	}
	return out
}

func buildContextCheckpointPreview(dropped []interface{}) string {
	var users []string
	var progress []string
	var handles []string
	toolCount := 0
	for _, msg := range dropped {
		role := MsgRole(msg)
		if role == "tool" {
			toolCount++
		}
		_, content := ExtractRoleContent(msg)
		content = strings.TrimSpace(content)
		switch role {
		case "user":
			if strings.HasPrefix(content, "[context_checkpoint]") {
				if handle := checkpointHandleSummary(content); handle != "" {
					handles = append(handles, handle)
				}
			} else if content != "" {
				users = append(users, compactCheckpointText(content, 1200))
			}
		case "assistant":
			if content != "" {
				progress = append(progress, compactCheckpointText(content, 700))
			}
		case "tool":
			if handle := checkpointHandleSummary(content); handle != "" {
				handles = append(handles, handle)
			}
			if checkpointOperationalSignal(content) {
				progress = append(progress, compactCheckpointText(content, 500))
			}
		}
	}
	if len(progress) > 8 {
		progress = progress[len(progress)-8:]
	}
	if len(handles) > 12 {
		handles = handles[len(handles)-12:]
	}
	var b strings.Builder
	b.WriteString("[context_checkpoint]\n")
	fmt.Fprintf(&b, "dropped_messages: %d\n", len(dropped))
	fmt.Fprintf(&b, "dropped_tool_results: %d\n", toolCount)
	b.WriteString("status: full original message JSON is stored in the handle below\n")
	b.WriteString("instruction: preserve the user goals and constraints below; continue from recent messages; use read_tool_result on this checkpoint before guessing any omitted decision, path, error, or tool detail\n")
	if len(users) > 0 {
		b.WriteString("preserved_user_goals_and_constraints:\n")
		for _, user := range users {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(user, "\n", "\n  "))
		}
	}
	if len(progress) > 0 {
		b.WriteString("recent_progress_decisions_paths_and_errors:\n")
		for _, item := range progress {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(item, "\n", "\n  "))
		}
	}
	if len(handles) > 0 {
		b.WriteString("older_lossless_tool_handles:\n")
		for _, handle := range handles {
			fmt.Fprintf(&b, "- %s\n", handle)
		}
	}
	return b.String()
}

func compactCheckpointText(content string, limit int) string {
	if len(content) <= limit {
		return content
	}
	marker := "\n...\n"
	budget := limit - len(marker)
	if budget <= 0 {
		return checkpointUTF8Prefix(content, limit)
	}
	head := budget * 2 / 3
	tail := budget - head
	return checkpointUTF8Prefix(content, head) + marker + checkpointUTF8Suffix(content, tail)
}

func checkpointUTF8Prefix(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	end := limit
	for end > 0 && end < len(s) && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

func checkpointUTF8Suffix(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	start := len(s) - limit
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func checkpointHandleSummary(content string) string {
	marker := toolresult.HandleFooterMarker
	idx := strings.LastIndex(content, marker)
	if idx < 0 {
		return ""
	}
	var id, toolName string
	for _, line := range strings.Split(content[idx:], "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "id:"):
			id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "tool:"):
			toolName = strings.TrimSpace(strings.TrimPrefix(line, "tool:"))
		}
	}
	if id == "" {
		return ""
	}
	return fmt.Sprintf("tool=%s id=%s", toolName, id)
}

func checkpointOperationalSignal(content string) bool {
	lower := strings.ToLower(content)
	for _, cue := range []string{"error", "failed", "failure", "denied", "warning", "path:", "file:", "错误", "失败", "拒绝", "路径"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}
