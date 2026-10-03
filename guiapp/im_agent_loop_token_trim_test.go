package guiapp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestFirstAgentLoopRequestTokenLimitCapsArchivedHistory(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("system policy ", 300)},
	}
	for i := 0; i < 24; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("older transcript ", 700),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_USER_REQUEST_MUST_SURVIVE"})

	limit := firstAgentLoopRequestTokenLimit(80_000, conversation, nil)
	if limit != firstAgentLoopRequestTargetTokens {
		t.Fatalf("limit = %d, want latency target %d", limit, firstAgentLoopRequestTargetTokens)
	}
	trimmed := trimConversation(conversation, limit, 0, nil)
	if len(trimmed) >= len(conversation) {
		t.Fatalf("archived history was not compacted: before=%d after=%d", len(conversation), len(trimmed))
	}
	last, ok := trimmed[len(trimmed)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_USER_REQUEST_MUST_SURVIVE" {
		t.Fatalf("current user message lost after first-request trim: %#v", trimmed[len(trimmed)-1])
	}
}

func TestFirstAgentLoopRequestTokenLimitProtectsLargeCurrentUserMessage(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": strings.Repeat("document content ", 8_000)},
	}
	limit := firstAgentLoopRequestTokenLimit(80_000, conversation, nil)
	minimum := estimateSingleMsgTokens(conversation[0]) + estimateSingleMsgTokens(conversation[1]) + firstAgentLoopRequestHistoryReserveTokens
	if limit < minimum {
		t.Fatalf("limit = %d, must preserve protected content plus reserve %d", limit, minimum)
	}
}

func TestFirstAgentLoopRequestTokenLimitNeverExceedsProviderLimit(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("policy ", 2_000)},
		map[string]string{"role": "user", "content": strings.Repeat("attachment text ", 8_000)},
	}
	if got := firstAgentLoopRequestTokenLimit(6_000, conversation, nil); got != 6_000 {
		t.Fatalf("limit = %d, want provider limit 6000", got)
	}
}

func TestSharedFirstRequestCompactsArchivedHistoryOnly(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "off")
	h := &IMMessageHandler{}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("system policy ", 200)},
	}
	for i := 0; i < 32; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("old completed work ", 700),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "first-request-budget-test",
		llmCfg:  corelib.MaclawLLMConfig{ContextLength: 100_000},
	}
	got := cb.TransformConversation(conversation)
	if got == nil || len(got) >= len(conversation) {
		t.Fatalf("first request must compact archived history: before=%d after=%d", len(conversation), len(got))
	}
	if !cb.firstRequestBudgetApplied {
		t.Fatal("first request budget was not marked applied")
	}
	last, ok := got[len(got)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_REQUEST" {
		t.Fatalf("current request lost after shared-loop trim: %#v", got[len(got)-1])
	}

	// Later rounds deliberately return to normal model context capacity.
	if next := cb.TransformConversation(got); next != nil {
		t.Fatalf("unchanged later round must not re-trim conversation: %#v", next)
	}
}

func TestLegacyFirstRequestBudgetDoesNotPersistIntoLaterRounds(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "off")
	h := &IMMessageHandler{}
	ctx := NewLoopContext("legacy-first-budget", 3, nil)
	ctx.Kind = LoopKindBackground
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": strings.Repeat("old user context ", 8_000)},
		map[string]string{"role": "assistant", "content": strings.Repeat("old assistant context ", 8_000)},
		map[string]string{"role": "user", "content": strings.Repeat("older history ", 8_000)},
		map[string]string{"role": "assistant", "content": strings.Repeat("previous answer ", 8_000)},
		map[string]string{"role": "user", "content": "CURRENT_REQUEST"},
	}
	cfg := corelib.MaclawLLMConfig{ContextLength: 100_000}
	first := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  "legacy-first-budget-test",
		UserText:                "CURRENT_REQUEST",
		Iteration:               0,
		EffectiveMax:            3,
		Config:                  cfg,
		Conversation:            conversation,
		FirstRequest:            true,
		LastOutputTokens:        1,
		DirectModeToolsFiltered: false,
	})
	if first.EffectiveTokenLimit != cfg.EffectiveContextTokens() {
		t.Fatalf("first effective limit = %d, want normal provider limit %d", first.EffectiveTokenLimit, cfg.EffectiveContextTokens())
	}
	if len(first.Conversation) >= len(conversation) {
		t.Fatalf("first request did not compact history: before=%d after=%d", len(conversation), len(first.Conversation))
	}

	second := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:                 ctx,
		UserID:                  "legacy-first-budget-test",
		UserText:                "CURRENT_REQUEST",
		Iteration:               1,
		EffectiveMax:            3,
		Config:                  cfg,
		Conversation:            first.Conversation,
		FirstRequest:            false,
		LastOutputTokens:        1,
		DirectModeToolsFiltered: false,
	})
	if second.EffectiveTokenLimit != cfg.EffectiveContextTokens() {
		t.Fatalf("later effective limit = %d, want restored provider limit %d", second.EffectiveTokenLimit, cfg.EffectiveContextTokens())
	}
}

func TestFirstRequestOverWindowUsesLosslessCheckpoint(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 16; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": strings.Repeat("archived context ", 1_000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	// A lookup surface does not list the reader. The checkpoint must still
	// spill, and must not tell the model the history was discarded.
	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{Name: "read_tool_result", Description: "reader", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	got := h.compactAgentLoopConversation(nil, "first-request-checkpoint", conversation, tools, firstAgentLoopRequestTargetTokens, 0)
	if len(got) >= len(conversation) {
		t.Fatalf("first request was not structurally compacted: before=%d after=%d", len(conversation), len(got))
	}
	foundHandle := false
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("over-window first request used the omission placeholder: %s", content)
		}
		if strings.Contains(content, "[context_checkpoint]") && strings.Contains(content, "[tool_result_handle]") {
			foundHandle = true
		}
	}
	if !foundHandle {
		t.Fatal("over-window first request did not leave a lossless checkpoint handle")
	}
	last, ok := got[len(got)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_REQUEST" {
		t.Fatalf("current request lost after checkpoint: %#v", got[len(got)-1])
	}
}

func TestFewGroupsOverWindowUsesLosslessCheckpoint(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	// Fewer groups than the default keep window, but still over the budget.
	// That used to miss the checkpoint and fall through to the omission line.
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 4; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": strings.Repeat("archived source ", 2000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})
	h := &IMMessageHandler{}
	got := h.compactAgentLoopConversation(nil, "few-groups-checkpoint", conversation, nil, 8000, 0)
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("few over-budget groups used the omission placeholder: %s", content)
		}
	}
	found := false
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "[context_checkpoint]") && strings.Contains(content, "[tool_result_handle]") {
			found = true
		}
	}
	if !found {
		t.Fatal("few over-budget groups did not leave a lossless checkpoint handle")
	}
	last, ok := got[len(got)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_REQUEST" {
		t.Fatalf("current request lost: %#v", got[len(got)-1])
	}
}

func TestQuoteHeavyFitDoesNotFallThroughToOmission(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	// Quotes make json.Marshal much larger than the raw string. The checkpoint
	// and the trim must share one count, otherwise a window the checkpoint
	// calls intact is rewritten with the omission placeholder.
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 4; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": strings.Repeat(`"q"`, 2000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	cheap := 0
	for _, message := range conversation {
		cheap += agent.EstimateMessageTokens(message)
	}
	jsonEst := agent.EstimateConversationTokens(conversation)
	if jsonEst <= cheap {
		t.Fatalf("fixture did not make the json estimate stricter: cheap=%d json=%d", cheap, jsonEst)
	}
	limit := cheap + 1
	h := &IMMessageHandler{}
	got := h.compactAgentLoopConversation(nil, "quote-heavy-fit", conversation, nil, limit, 0)
	if len(got) != len(conversation) {
		t.Fatalf("in-budget transcript was rewritten: before=%d after=%d", len(conversation), len(got))
	}
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("in-budget transcript used the omission placeholder: %s", content)
		}
	}
}

func TestNoSavingsDoesNotFallThroughToOmission(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	// The newest group is over the budget and the prefix is smaller than the
	// checkpoint preview, so the spill saves nothing. That must not be
	// rewritten into the omission line.
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": "KEEP_PREFIX " + strings.Repeat("a", 40)},
		map[string]string{"role": "user", "content": "ALSO_PREFIX"},
		map[string]string{"role": "assistant", "content": strings.Repeat("d", 15000)},
	}
	h := &IMMessageHandler{}
	got := h.compactAgentLoopConversation(nil, "no-savings-keep", conversation, nil, 4000, 0)
	if len(got) != len(conversation) {
		t.Fatalf("no-savings transcript was rewritten: before=%d after=%d", len(conversation), len(got))
	}
	prefix, ok := got[1].(map[string]string)
	if !ok || !strings.HasPrefix(prefix["content"], "KEEP_PREFIX") {
		t.Fatalf("prefix was replaced: %#v", got[1])
	}
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("no-savings transcript used the omission placeholder: %s", content)
		}
	}
}

func TestOpaqueImageDoesNotFallThroughToOmission(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	// Every group the checkpoint would drop is a picture. The picture has to
	// stay in the request, and the fallback must not replace it with the
	// omission line.
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 14; i++ {
		conversation = append(conversation, map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAA"}},
			},
		})
	}
	conversation = append(conversation, map[string]string{
		"role":    "user",
		"content": strings.Repeat("d", 15000),
	})
	h := &IMMessageHandler{}
	got := h.compactAgentLoopConversation(nil, "opaque-image-keep", conversation, nil, 4000, 0)
	if len(got) != len(conversation) {
		t.Fatalf("image transcript was rewritten: before=%d after=%d", len(conversation), len(got))
	}
	foundImage := false
	for _, message := range got {
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("image transcript used the omission placeholder: %s", content)
		}
		if strings.Contains(content, "image_url") {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatal("image left the inline conversation")
	}
}

func TestKeptTailMismatchDoesNotFallThroughToOmission(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 8; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": strings.Repeat("payload ", 400),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_QUESTION"})
	conversation = append(conversation,
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
			map[string]interface{}{"id": "declared", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "different", "content": "result"},
	)
	h := &IMMessageHandler{}
	got := h.compactAgentLoopConversation(nil, "kept-mismatch", conversation, nil, 4000, 0)
	foundQuestion := false
	for _, message := range got {
		if agent.MsgRole(message) == "tool" {
			t.Fatalf("broken tool pair stayed inline: %#v", message)
		}
		_, content := agent.ExtractRoleContent(message)
		if strings.Contains(content, "已被省略") {
			t.Fatalf("mismatched tail used the omission placeholder: %s", content)
		}
		if strings.Contains(content, "CURRENT_QUESTION") {
			foundQuestion = true
		}
	}
	if !foundQuestion {
		t.Fatal("the valid message beside the broken pair left the inline conversation")
	}
}

func TestTrimConversationHandoffAlternatesBeforeNextUser(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "请改 login.go 的过期时间"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "read",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "r1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"login.go"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "r1", "content": "package main"},
		map[string]string{"role": "assistant", "content": strings.Repeat("archived work ", 4000)},
		map[string]string{"role": "user", "content": "KEEP_TAIL"},
	}
	once := trimConversation(conversation, 4000, 0, nil)
	prevRole := ""
	for i, message := range once {
		role := msgRole(message)
		if role == "user" && prevRole == "user" {
			t.Fatalf("consecutive user messages at %d", i)
		}
		prevRole = role
	}
	again := append([]interface{}{}, once...)
	again = append(again,
		map[string]string{"role": "assistant", "content": strings.Repeat("more archived work ", 4000)},
		map[string]string{"role": "user", "content": "NEXT_TAIL"},
	)
	twice := trimConversation(again, 4000, 0, nil)
	var blob strings.Builder
	for _, message := range twice {
		data, _ := json.Marshal(message)
		blob.Write(data)
	}
	text := blob.String()
	if !strings.Contains(text, "请改 login.go") || !strings.Contains(text, "login.go") || !strings.Contains(text, "NEXT_TAIL") {
		t.Fatalf("second trim lost the carried request or path:\n%s", text)
	}
}

func TestLastGroupTruncateKeepsEarlierRequest(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "请改 login.go 的过期时间"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "read",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "r1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"login.go"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "r1", "content": "package main"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "run",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "b1", "function": map[string]interface{}{"name": "bash", "arguments": `{"command":"test"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "b1", "content": strings.Repeat("log ", 8000)},
	}
	trimmed := trimConversation(conversation, 4000, 0, nil)
	var blob strings.Builder
	for _, message := range trimmed {
		data, _ := json.Marshal(message)
		blob.Write(data)
	}
	text := blob.String()
	if !strings.Contains(text, "请改 login.go") || !strings.Contains(text, "login.go") {
		t.Fatalf("truncated tail dropped the earlier request:\n%s", text)
	}
	if strings.Contains(text, "已被省略") {
		t.Fatal("generic omission placeholder replaced the handoff")
	}
	prev := ""
	for i, message := range trimmed {
		role := msgRole(message)
		if role == "tool" && prev != "assistant" && prev != "tool" {
			t.Fatalf("orphaned tool at %d after %s", i, prev)
		}
		prev = role
	}
}

func TestTrimConversationNilSummarizerKeepsRequestAndPath(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "请改 login.go 的过期时间"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "read",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "r1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"login.go"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "r1", "content": "package main"},
	}
	for i := 0; i < 6; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("archived work ", 800),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "KEEP_TAIL"})

	trimmed := trimConversation(conversation, 4000, 0, nil)
	var blob strings.Builder
	for _, message := range trimmed {
		data, _ := json.Marshal(message)
		blob.Write(data)
	}
	text := blob.String()
	if !strings.Contains(text, "请改 login.go") || !strings.Contains(text, "login.go") {
		t.Fatalf("nil summarizer dropped the request or path:\n%s", text)
	}
	if !strings.Contains(text, "KEEP_TAIL") {
		t.Fatal("recent tail was dropped")
	}
	if strings.Contains(text, "已被省略") {
		t.Fatal("generic omission placeholder replaced the handoff")
	}
}

func TestOmissionPlaceholderDoesNotSitAgainstCurrentUser(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "assistant", "content": "earlier note"},
		map[string]string{"role": "assistant", "content": strings.Repeat("archived answer ", 6_000)},
		map[string]string{"role": "user", "content": "CURRENT_OVERSIZED_REQUEST " + strings.Repeat("attachment text ", 12_000)},
	}
	trimmed := trimConversation(conversation, 4_000, 0, nil)
	if msgRole(trimmed[len(trimmed)-1]) != "user" {
		t.Fatalf("tail role = %s", msgRole(trimmed[len(trimmed)-1]))
	}
	if msgRole(trimmed[len(trimmed)-2]) == "user" {
		t.Fatal("placeholder sat directly against the current user message")
	}
}

func TestFirstRequestTrimNeverDropsOversizedCurrentUserMessage(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "assistant", "content": strings.Repeat("archived answer ", 6_000)},
		map[string]string{"role": "user", "content": "CURRENT_OVERSIZED_REQUEST " + strings.Repeat("attachment text ", 12_000)},
	}

	trimmed := trimConversation(conversation, 4_000, 0, nil)
	last, ok := trimmed[len(trimmed)-1].(map[string]string)
	if !ok || !strings.HasPrefix(last["content"], "CURRENT_OVERSIZED_REQUEST ") {
		t.Fatalf("current oversized request was lost in fallback: %#v", trimmed)
	}
	archived, ok := conversation[1].(map[string]string)
	if !ok || !strings.HasPrefix(archived["content"], "archived answer ") {
		t.Fatalf("trim mutated the caller's archived history: %#v", conversation)
	}
}

func TestSharedFirstRequestSkipsTrimWhenConversationFitsWindow(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "off")
	h := &IMMessageHandler{}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": "earlier requirements"},
		map[string]string{"role": "assistant", "content": "acknowledged"},
		map[string]string{"role": "user", "content": "CURRENT_REQUEST"},
	}

	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "first-request-fits-window-test",
		llmCfg:  corelib.MaclawLLMConfig{ContextLength: 100_000},
	}
	if got := cb.TransformConversation(conversation); got != nil {
		t.Fatalf("conversation that fits the provider window must not be trimmed on first request: %#v", got)
	}
	if !cb.firstRequestBudgetApplied {
		t.Fatal("first request budget was not marked applied")
	}
}

func TestFirstRequestTrimSummarizesDroppedHistory(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	for i := 0; i < 8; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("archived work ", 1_000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})

	var summarizerInput string
	summarizer := func(raw string) string {
		summarizerInput = raw
		return "SUMMARY_OF_DROPPED_HISTORY"
	}
	trimmed := trimConversation(conversation, firstAgentLoopRequestTargetTokens, 0, summarizer)
	if len(trimmed) >= len(conversation) {
		t.Fatalf("archived history was not trimmed: before=%d after=%d", len(conversation), len(trimmed))
	}
	if !strings.Contains(summarizerInput, "archived work ") {
		t.Fatalf("summarizer did not receive the dropped history: len=%d", len(summarizerInput))
	}
	foundSummary := false
	for _, message := range trimmed {
		m, ok := message.(map[string]string)
		if !ok {
			continue
		}
		if strings.Contains(m["content"], "[对话历史摘要]") && strings.Contains(m["content"], "SUMMARY_OF_DROPPED_HISTORY") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("dropped history was not replaced by a summary placeholder: %#v", trimmed)
	}
	last, ok := trimmed[len(trimmed)-1].(map[string]string)
	if !ok || last["content"] != "CURRENT_REQUEST" {
		t.Fatalf("current request lost after summarized trim: %#v", trimmed[len(trimmed)-1])
	}
}

func TestFirstRequestGateUsesSharedEstimate(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
	}
	body := strings.Repeat(`"q"`, 3000)
	for i := 0; i < 6; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": body,
		})
	}
	shared := estimateConversationTokens(conversation)
	perMessage := 0
	for _, message := range conversation {
		perMessage += estimateSingleMsgTokens(message)
	}
	if shared != perMessage {
		t.Fatalf("conversation estimate %d != per-message sum %d", shared, perMessage)
	}
	jsonEst := agent.EstimateConversationTokens(conversation)
	if jsonEst <= shared {
		t.Fatalf("fixture did not make json stricter: shared=%d json=%d", shared, jsonEst)
	}
	effective := shared + (jsonEst-shared)/2
	limit, budgeted := firstRequestCompactionLimit(effective, shared, conversation, nil)
	if budgeted || limit != effective {
		t.Fatalf("shared estimate inside the window was budgeted: limit=%d budgeted=%v effective=%d shared=%d json=%d", limit, budgeted, effective, shared, jsonEst)
	}
}

func TestFirstRequestCompactionLimitFitsWindowKeepsNormalLimit(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": "small request"},
	}
	beforeTokens := estimateConversationTokens(conversation) // well below 80_000
	limit, budgeted := firstRequestCompactionLimit(80_000, beforeTokens, conversation, nil)
	if budgeted {
		t.Fatalf("fitting conversation was budgeted: before=%d", beforeTokens)
	}
	if limit != 80_000 {
		t.Fatalf("limit = %d, want normal limit 80000", limit)
	}
}

func TestFirstRequestCompactionLimitOverWindowAppliesBudget(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": strings.Repeat("policy ", 2_000)},
		map[string]string{"role": "user", "content": strings.Repeat("context ", 32_000)},
	}
	beforeTokens := estimateConversationTokens(conversation)
	if beforeTokens <= 80_000 {
		t.Fatalf("test conversation must exceed the window: before=%d", beforeTokens)
	}
	limit, budgeted := firstRequestCompactionLimit(80_000, beforeTokens, conversation, nil)
	if !budgeted {
		t.Fatalf("over-window conversation was not budgeted: before=%d", beforeTokens)
	}
	if limit > 80_000 {
		t.Fatalf("budgeted limit %d exceeds provider window 80000", limit)
	}
}
