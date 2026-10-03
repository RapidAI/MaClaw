package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
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
	// One pass per message. The keep decision and the savings check reuse
	// these costs; measuring the tail again would re-encode every large
	// tool result that is about to be spilled.
	msgTokens := checkpointMessageTokens(conversation)
	for _, n := range msgTokens {
		result.BeforeTokens += n
	}
	if result.BeforeTokens*100 < budget*threshold {
		result.Reason = "below_threshold"
		return result
	}

	groups, inlineOK := buildCheckpointGroups(conversation[1:])
	groupCosts := checkpointGroupCosts(msgTokens, groups)
	keepGroups := opts.KeepGroups
	if keepGroups <= 0 {
		keepGroups = defaultCheckpointKeepGroups
	}
	previewLimit := opts.PreviewLimit
	if previewLimit <= 0 {
		previewLimit = defaultCheckpointPreviewLimit
	}
	// KeepGroups is a maximum inline window, not a promise to leave an
	// over-budget tail in the prompt. A transcript with only a handful of
	// huge tool results used to stop here (protected_window) and the caller
	// then replaced the dropped JSON with "history was omitted". Shrink the
	// window until it fits; the newest group stays even when it alone is
	// larger than the budget.
	systemTokens := 0
	if len(msgTokens) > 0 {
		systemTokens = msgTokens[0]
	}
	keepGroups = checkpointKeepCount(systemTokens, groupCosts, keepGroups, budget, previewLimit, result.BeforeTokens)
	if keepGroups < 1 {
		result.Reason = "protected_window"
		return result
	}
	// A broken tool pair must not be sent inline: providers reject it.
	// Drop that pair even when it sits inside the recent window, and keep
	// the valid messages around it. Failing the whole checkpoint used to
	// drop those messages into the omission line.
	suffixStart := len(groups) - keepGroups
	tail := conversation[1:]
	drop := make([]bool, len(groups))
	dropped := make([]interface{}, 0)
	opaqueBlocked := false
	for i, group := range groups {
		msgs := tail[group.Start:group.End]
		if i >= suffixStart && i < len(inlineOK) && inlineOK[i] {
			continue
		}
		// An image has to stay inline: the handle is text, and a spilled
		// picture cannot be shown again. Text around that image can still
		// be stored. Refusing the whole spill used to drop both.
		if checkpointContainsOpaqueContent(msgs) {
			opaqueBlocked = true
			continue
		}
		drop[i] = true
		dropped = append(dropped, msgs...)
	}
	if len(dropped) == 0 {
		if opaqueBlocked {
			result.Reason = "opaque_content"
		} else {
			result.Reason = "nothing_to_drop"
		}
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
	limit := previewLimit
	// Project replaces a caller preview that exceeds its budget with a raw
	// JSON dump. That drops the checkpoint instruction, so the model sees
	// an unlabeled blob instead of "page this handle before guessing".
	// Keep the head (instruction and goals) and the tail (older handles).
	if reserve := 512; limit > reserve && len(preview) > limit-reserve {
		preview = compactCheckpointText(preview, limit-reserve)
	}
	if opts.DryRun {
		globalContextCheckpoint.shadowEvaluated.Add(1)
		previewMsg := map[string]string{"role": "user", "content": toolresult.DefaultPreview(preview, limit)}
		result.AfterTokens = checkpointKeptTokens(systemTokens, groupCosts, drop, previewMsg)
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

	previewMsg := map[string]string{"role": "user", "content": projection.Preview}
	next := assembleCheckpointConversation(conversation, groups, drop, previewMsg["content"])
	result.AfterTokens = checkpointKeptTokens(systemTokens, groupCosts, drop, previewMsg)
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

// checkpointMessageTokens is one estimate per message, in conversation order.
// String bodies are measured by length. Encoding them to JSON just to count
// the bytes copies every large tool result before the spill encodes it again.
func checkpointMessageTokens(conversation []interface{}) []int {
	costs := make([]int, len(conversation))
	for i, message := range conversation {
		costs[i] = estimateCheckpointMessageTokens(message)
	}
	return costs
}

func estimateStringTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s)*10 + 24) / 25
}

// EstimateMessageTokens is the allocation-free size of one conversation
// message. String bodies and tool-call arguments are measured by length.
// Hosts use it for the fast trim check so a large tool argument is not
// encoded to JSON just to decide that the transcript already fits.
func EstimateMessageTokens(message interface{}) int {
	return estimateCheckpointMessageTokens(message)
}

func estimateCheckpointMessageTokens(message interface{}) int {
	switch m := message.(type) {
	case map[string]string:
		return estimateStringTokens(m["role"]) + estimateStringTokens(m["content"]) + 4
	case map[string]interface{}:
		if _, multimodal := m["content"].([]interface{}); multimodal {
			return EstimateConversationTokens([]interface{}{message})
		}
		n := 4
		if role, ok := m["role"].(string); ok {
			n += estimateStringTokens(role)
		}
		switch content := m["content"].(type) {
		case string:
			n += estimateStringTokens(content)
		case nil:
		default:
			return EstimateConversationTokens([]interface{}{message})
		}
		if s, ok := m["reasoning_content"].(string); ok {
			n += estimateStringTokens(s)
		}
		if s, ok := m["tool_call_id"].(string); ok {
			n += estimateStringTokens(s)
		}
		if s, ok := m["name"].(string); ok {
			n += estimateStringTokens(s)
		}
		if calls, ok := m["tool_calls"]; ok && calls != nil {
			extra, known := estimateToolCallArgTokens(calls)
			if !known {
				return EstimateConversationTokens([]interface{}{message})
			}
			n += extra
		}
		return n
	default:
		return EstimateConversationTokens([]interface{}{message})
	}
}

func estimateToolCallArgTokens(calls interface{}) (int, bool) {
	switch arr := calls.(type) {
	case []interface{}:
		n := 0
		for _, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return 0, false
			}
			add, ok := estimateCallMapTokens(m)
			if !ok {
				return 0, false
			}
			n += add
		}
		return n, true
	case []map[string]interface{}:
		n := 0
		for _, m := range arr {
			add, ok := estimateCallMapTokens(m)
			if !ok {
				return 0, false
			}
			n += add
		}
		return n, true
	case []llm.ToolCall:
		n := 0
		for _, call := range arr {
			n += 8
			n += estimateStringTokens(call.ID)
			n += estimateStringTokens(call.Type)
			n += estimateStringTokens(call.Function.Name)
			n += estimateStringTokens(call.Function.Arguments)
		}
		return n, true
	default:
		return 0, false
	}
}

func estimateCallMapTokens(m map[string]interface{}) (int, bool) {
	n := 8
	if name, ok := m["name"].(string); ok {
		n += estimateStringTokens(name)
	}
	if id, ok := m["id"].(string); ok {
		n += estimateStringTokens(id)
	}
	add, ok := estimateArgumentTokens(m["arguments"])
	if !ok {
		return 0, false
	}
	n += add
	switch fn := m["function"].(type) {
	case nil:
	case map[string]interface{}:
		if name, ok := fn["name"].(string); ok {
			n += estimateStringTokens(name)
		}
		add, ok = estimateArgumentTokens(fn["arguments"])
		if !ok {
			return 0, false
		}
		n += add
	case map[string]string:
		n += estimateStringTokens(fn["name"]) + estimateStringTokens(fn["arguments"])
	default:
		return 0, false
	}
	return n, true
}

// estimateArgumentTokens counts a tool-call argument payload. A missing
// payload is zero. A non-string payload is refused so the caller falls back
// to the JSON estimator instead of treating a large object as empty.
func estimateArgumentTokens(args interface{}) (int, bool) {
	if args == nil {
		return 0, true
	}
	s, ok := args.(string)
	if !ok {
		return 0, false
	}
	return estimateStringTokens(s), true
}

// checkpointGroupCosts sums the already-measured tail messages covered by
// each group. Index 0 of msgTokens is the system message and is not a group.
func checkpointGroupCosts(msgTokens []int, groups []EntryGroup) []int {
	costs := make([]int, len(groups))
	for i, group := range groups {
		for j := group.Start; j < group.End; j++ {
			idx := j + 1
			if idx < 0 || idx >= len(msgTokens) {
				continue
			}
			costs[i] += msgTokens[idx]
		}
	}
	return costs
}

// checkpointKeptTokens is the system prompt, the checkpoint preview, and
// every group that stayed inline, including an image pinned in front of
// the spilled text.
func checkpointKeptTokens(systemTokens int, groupCosts []int, drop []bool, previewMessage interface{}) int {
	total := systemTokens + estimateCheckpointMessageTokens(previewMessage)
	for i, cost := range groupCosts {
		if i < len(drop) && drop[i] {
			continue
		}
		total += cost
	}
	return total
}

func assembleCheckpointConversation(conversation []interface{}, groups []EntryGroup, drop []bool, preview string) []interface{} {
	tail := conversation[1:]
	next := make([]interface{}, 0, len(conversation))
	if len(conversation) > 0 {
		next = append(next, conversation[0])
	}
	inserted := false
	for i, group := range groups {
		if i < len(drop) && drop[i] {
			if !inserted {
				next = append(next, map[string]string{"role": "user", "content": preview})
				inserted = true
			}
			continue
		}
		if group.Start < 0 || group.End > len(tail) || group.Start > group.End {
			continue
		}
		next = append(next, tail[group.Start:group.End]...)
	}
	return next
}

// checkpointKeepCount chooses how many trailing groups stay inline.
// Zero means the transcript already fits, so the caller leaves it untouched.
// KeepGroups limits how far back a shrink reaches; it does not hide a
// fitting history behind a handle. The newest group is never dropped.
// groupCosts must already be measured; this does not walk message bodies.
func checkpointKeepCount(systemTokens int, groupCosts []int, maxKeep, budget, previewLimit, beforeTokens int) int {
	if len(groupCosts) <= 1 || budget <= 0 {
		return 0
	}
	if beforeTokens <= budget {
		return 0
	}
	if maxKeep < 1 {
		maxKeep = 1
	}
	if len(groupCosts) <= maxKeep {
		maxKeep = len(groupCosts) - 1
	}
	if maxKeep < 1 {
		return 0
	}
	// System prompt plus the checkpoint preview that will replace the prefix.
	running := systemTokens
	if previewLimit > 0 {
		// Same rounding as EstimateBytesToTokens.
		running += (previewLimit*10 + 24) / 25
	}
	keep := 0
	oldest := len(groupCosts) - maxKeep
	for i := len(groupCosts) - 1; i >= oldest; i-- {
		cost := groupCosts[i]
		// A contiguous trailing window. Stop at the first older group that
		// does not fit; do not skip it to retain something further back.
		if keep >= 1 && running+cost > budget {
			break
		}
		running += cost
		keep++
	}
	if keep < 1 {
		return 1
	}
	return keep
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

func buildCheckpointGroups(tail []interface{}) ([]EntryGroup, []bool) {
	groups := make([]EntryGroup, 0, len(tail))
	inlineOK := make([]bool, 0, len(tail))
	for i := 0; i < len(tail); {
		start := i
		if MsgRole(tail[i]) == "tool" {
			i++
			groups = append(groups, EntryGroup{Start: start, End: i})
			inlineOK = append(inlineOK, false)
			continue
		}
		if MsgRole(tail[i]) == "assistant" && MsgHasToolCalls(tail[i]) {
			declared, ok := checkpointDeclaredToolCallIDs(tail[i])
			i++
			actual := map[string]int{}
			pairOK := ok
			for i < len(tail) && MsgRole(tail[i]) == "tool" {
				id := checkpointToolMessageID(tail[i])
				if id == "" {
					pairOK = false
				} else {
					actual[id]++
				}
				i++
			}
			if pairOK && !sameCheckpointToolIDs(declared, actual) {
				pairOK = false
			}
			groups = append(groups, EntryGroup{Start: start, End: i})
			inlineOK = append(inlineOK, pairOK)
			continue
		}
		i++
		groups = append(groups, EntryGroup{Start: start, End: i})
		inlineOK = append(inlineOK, true)
	}
	return groups, inlineOK
}

func checkpointDeclaredToolCallIDs(message interface{}) (map[string]int, bool) {
	mm, ok := message.(map[string]interface{})
	if !ok {
		return nil, false
	}
	ids := map[string]int{}
	ok = visitToolCalls(mm["tool_calls"], func(id, _ string) bool {
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		ids[id]++
		return true
	})
	if !ok {
		return nil, false
	}
	return ids, true
}

// visitToolCalls walks a tool_calls value for ids and names. Arguments are
// not encoded: a bash or file-write payload can be the whole file, and both
// the checkpoint grouper and the desktop-observe fold only need the name.
// An unrecognized shape returns false so the caller can fail closed.
func visitToolCalls(calls interface{}, visit func(id, name string) bool) bool {
	switch arr := calls.(type) {
	case nil:
		return true
	case []llm.ToolCall:
		for _, call := range arr {
			if !visit(call.ID, call.Function.Name) {
				return false
			}
		}
		return true
	case []interface{}:
		for _, item := range arr {
			id, name, ok := toolCallIDAndName(item)
			if !ok || !visit(id, name) {
				return false
			}
		}
		return true
	case []map[string]interface{}:
		for _, item := range arr {
			id, name, ok := toolCallIDAndName(item)
			if !ok || !visit(id, name) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func toolCallIDAndName(item interface{}) (string, string, bool) {
	switch v := item.(type) {
	case llm.ToolCall:
		return v.ID, v.Function.Name, true
	case map[string]interface{}:
		id, _ := v["id"].(string)
		name, _ := v["name"].(string)
		switch fn := v["function"].(type) {
		case nil:
		case map[string]interface{}:
			if n, ok := fn["name"].(string); ok && strings.TrimSpace(n) != "" {
				name = n
			}
		case map[string]string:
			if strings.TrimSpace(fn["name"]) != "" {
				name = fn["name"]
			}
		default:
			return "", "", false
		}
		return id, name, true
	default:
		return "", "", false
	}
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
