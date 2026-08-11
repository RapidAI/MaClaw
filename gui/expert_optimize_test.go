package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestExpertSaveBoundaryDropsOptimizationReviewMetadata(t *testing.T) {
	// ExpertDefinition is the persistence/session contract. This guards the
	// backend boundary even if a future client sends its UI-only diff fields.
	raw := []byte(`{
        "name":"优化专家",
        "system_prompt":"仅此内容应成为专家上下文",
        "source_name":"原专家",
        "source_system_prompt":"仅供差异展示的原提示词",
        "source_tools":["ssh"],
        "source_skills":["pptx-gen"]
    }`)
	var def ExpertDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("decode ExpertDefinition: %v", err)
	}
	stored, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("encode ExpertDefinition: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stored, &payload); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if payload["system_prompt"] != "仅此内容应成为专家上下文" {
		t.Fatalf("system prompt changed: %#v", payload)
	}
	for _, field := range []string{"source_name", "source_system_prompt", "source_tools", "source_skills"} {
		if _, found := payload[field]; found {
			t.Fatalf("review-only field %q leaked into ExpertDefinition: %#v", field, payload)
		}
	}
}

func TestSaveExpertRejectsInvalidOptimizationLineage(t *testing.T) {
	oldStore := defaultExpertStore
	defer func() { defaultExpertStore = oldStore }()
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))
	app := &App{}
	source := testExpert("expert-src", "2026-02-01T10:00:00Z")
	if err := defaultExpertStore.Save(source); err != nil {
		t.Fatalf("save source: %v", err)
	}
	otherSource := testExpert("expert-src-other", "2026-02-01T10:00:00Z")
	if err := defaultExpertStore.Save(otherSource); err != nil {
		t.Fatalf("save other source: %v", err)
	}

	for name, payload := range map[string]string{
		"missing source": `{"name":"优化专家","optimized_from_id":"missing-source"}`,
		"self reference": `{"id":"expert-src","name":"原专家","optimized_from_id":"expert-src"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := app.SaveExpert(payload); err == nil {
				t.Fatalf("SaveExpert(%s) must reject invalid optimization lineage", payload)
			}
		})
	}

	first, err := app.SaveExpert(`{"name":"优化专家","optimized_from_id":"expert-src","system_prompt":"正式提示词"}`)
	if err != nil {
		t.Fatalf("save first optimized expert: %v", err)
	}
	var saved ExpertDefinition
	if err := json.Unmarshal([]byte(first), &saved); err != nil {
		t.Fatalf("decode saved expert: %v", err)
	}
	if _, err := app.SaveExpert(`{"name":"重复优化专家","optimized_from_id":"expert-src","system_prompt":"另一个提示词"}`); err == nil {
		t.Fatal("creating a second direct optimized expert must be rejected")
	}
	if _, err := app.SaveExpert(`{"id":"` + saved.ID + `","name":"优化专家","optimized_from_id":"expert-src","system_prompt":"更新后的提示词"}`); err != nil {
		t.Fatalf("updating the existing optimized expert must remain allowed: %v", err)
	}
	for name, payload := range map[string]string{
		"clear lineage":    `{"id":"` + saved.ID + `","name":"优化专家","system_prompt":"错误地清除了谱系"}`,
		"reparent lineage": `{"id":"` + saved.ID + `","name":"优化专家","optimized_from_id":"expert-src-other","system_prompt":"错误地修改了谱系"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := app.SaveExpert(payload); err == nil {
				t.Fatalf("SaveExpert must reject mutable optimization lineage: %s", payload)
			}
		})
	}
}

func TestSaveExpertAllowsBuiltinOptimizationSource(t *testing.T) {
	oldStore := defaultExpertStore
	defer func() { defaultExpertStore = oldStore }()
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	builtins := builtinExperts()
	if len(builtins) == 0 {
		t.Skip("no builtin experts available")
	}
	sourceID := builtins[0].ID
	app := &App{}
	raw, err := app.SaveExpert(`{"name":"内置专家优化版","optimized_from_id":"` + sourceID + `","system_prompt":"保留内置专家工作流，并使用正式输出。"}`)
	if err != nil {
		t.Fatalf("builtin source optimization must save: %v", err)
	}
	var saved ExpertDefinition
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatalf("decode saved expert: %v", err)
	}
	if saved.OptimizedFromID != sourceID {
		t.Fatalf("builtin lineage lost: %+v", saved)
	}
}

func TestExpertStoreFindOptimizedFor(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	source := testExpert("expert-src", "2026-02-01T10:00:00Z")
	other := testExpert("expert-other", "2026-02-02T10:00:00Z")
	optimized := testExpert("expert-opt", "2026-02-03T10:00:00Z")
	optimized.OptimizedFromID = source.ID
	for _, def := range []ExpertDefinition{source, other, optimized} {
		if err := store.Save(def); err != nil {
			t.Fatalf("save %s: %v", def.ID, err)
		}
	}

	found, ok, err := store.FindOptimizedFor(source.ID)
	if err != nil || !ok {
		t.Fatalf("FindOptimizedFor(%q) ok=%v err=%v", source.ID, ok, err)
	}
	if found.ID != optimized.ID {
		t.Fatalf("FindOptimizedFor returned %q, want %q", found.ID, optimized.ID)
	}

	if _, ok, err := store.FindOptimizedFor(other.ID); err != nil || ok {
		t.Fatalf("source without optimized expert: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.FindOptimizedFor("  "); err != nil || ok {
		t.Fatalf("blank origin: ok=%v err=%v", ok, err)
	}
}

func TestExpertStoreFindOptimizedForPrefersLatest(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	older := testExpert("expert-opt-old", "2026-02-01T10:00:00Z")
	older.OptimizedFromID = "expert-src"
	newer := testExpert("expert-opt-new", "2026-02-04T10:00:00Z")
	newer.OptimizedFromID = "expert-src"
	// Save newest first so lookup order does not decide the winner.
	if err := store.Save(newer); err != nil {
		t.Fatalf("save newer: %v", err)
	}
	if err := store.Save(older); err != nil {
		t.Fatalf("save older: %v", err)
	}

	found, ok, err := store.FindOptimizedFor("expert-src")
	if err != nil || !ok {
		t.Fatalf("FindOptimizedFor ok=%v err=%v", ok, err)
	}
	if found.ID != newer.ID {
		t.Fatalf("duplicate lineage should resolve to latest updated, got %q", found.ID)
	}
}

func TestExpertStoreSaveNewOptimizedIsAtomic(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))
	first := testExpert("expert-opt-first", "2026-02-01T10:00:00Z")
	first.OptimizedFromID = "expert-source"
	second := testExpert("expert-opt-second", "2026-02-01T10:00:00Z")
	second.OptimizedFromID = "expert-source"

	if existingID, err := store.SaveNewOptimized(first); err != nil || existingID != "" {
		t.Fatalf("first optimized save existing=%q err=%v", existingID, err)
	}
	if existingID, err := store.SaveNewOptimized(second); err != nil || existingID != first.ID {
		t.Fatalf("second optimized save existing=%q err=%v, want %q", existingID, err, first.ID)
	}
	found, ok, err := store.FindOptimizedFor("expert-source")
	if err != nil || !ok || found.ID != first.ID {
		t.Fatalf("atomic optimized save persisted the wrong expert: %+v ok=%v err=%v", found, ok, err)
	}
}

func TestExpertStoreUpdateOptimizedLocksLineage(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))
	optimized := testExpert("expert-opt", "2026-02-01T10:00:00Z")
	optimized.OptimizedFromID = "expert-source"
	if err := store.Save(optimized); err != nil {
		t.Fatalf("seed optimized expert: %v", err)
	}

	updated := optimized
	updated.SystemPrompt = "updated prompt"
	if err := store.UpdateOptimized(updated); err != nil {
		t.Fatalf("update optimized expert: %v", err)
	}
	reparented := updated
	reparented.OptimizedFromID = "other-source"
	if err := store.UpdateOptimized(reparented); err == nil {
		t.Fatal("optimized lineage update must be rejected atomically")
	}
	stored, ok, err := store.Get(optimized.ID)
	if err != nil || !ok || stored.OptimizedFromID != optimized.OptimizedFromID || stored.SystemPrompt != updated.SystemPrompt {
		t.Fatalf("failed update must preserve the prior optimized record: %+v ok=%v err=%v", stored, ok, err)
	}
}

func TestParseExpertOptimizeResponse(t *testing.T) {
	valid := "```json\n{\"name\":\"论文润色·优化\",\"description\":\"d\",\"icon\":\"📝\",\"system_prompt\":\"prompt\"}\n```"
	out, err := parseExpertOptimizeResponse(valid)
	if err != nil {
		t.Fatalf("parse fenced response: %v", err)
	}
	if out.Name != "论文润色·优化" || out.SystemPrompt != "prompt" {
		t.Fatalf("unexpected parse result: %+v", out)
	}

	if _, err := parseExpertOptimizeResponse(""); err == nil {
		t.Fatal("empty response must fail")
	}
	if _, err := parseExpertOptimizeResponse("no json here"); err == nil {
		t.Fatal("response without JSON object must fail")
	}
	if _, err := parseExpertOptimizeResponse(`{"name":"x"}`); err == nil {
		t.Fatal("missing system_prompt must fail")
	}
}

func TestBuildExpertOptimizeDraft(t *testing.T) {
	source := &ExpertDefinition{
		ID:           "expert-src",
		Name:         "论文润色",
		Description:  "源简介",
		Icon:         "📝",
		SystemPrompt: "源 prompt",
		Tools:        []string{"read_file"},
		Skills:       []string{"pptx-gen"},
	}
	output := expertOptimizeLLMOutput{
		Name:         "论文精修",
		Description:  "优化简介",
		Icon:         "✨",
		SystemPrompt: "优化后的系统提示词：保留原有论文润色流程，新增正式书面语要求，并按优先级输出修改建议。",
	}

	// New optimized expert: fresh id, lineage recorded, tools/skills inherited.
	draft := buildExpertOptimizeDraft(source, output, nil)
	if draft.UpdateExisting || draft.ID != "" {
		t.Fatalf("new draft must not reference an existing expert: %+v", draft)
	}
	if draft.OptimizedFromID != source.ID || draft.SourceName != source.Name {
		t.Fatalf("lineage not recorded: %+v", draft)
	}
	if draft.SourceSystemPrompt != source.SystemPrompt || len(draft.SourceTools) != 1 || draft.SourceTools[0] != "read_file" || len(draft.SourceSkills) != 1 || draft.SourceSkills[0] != "pptx-gen" {
		t.Fatalf("source configuration needed for the review diff was not preserved: %+v", draft)
	}
	if draft.Name != "论文精修" || draft.SystemPrompt != output.SystemPrompt {
		t.Fatalf("distillation output not applied: %+v", draft)
	}
	if len(draft.Tools) != 1 || draft.Tools[0] != "read_file" || len(draft.Skills) != 1 || draft.Skills[0] != "pptx-gen" {
		t.Fatalf("tools/skills must be inherited from source: %+v", draft)
	}

	// LLM echoed the source name (or left it empty) → forced distinct default.
	echo := buildExpertOptimizeDraft(source, expertOptimizeLLMOutput{Name: source.Name, SystemPrompt: "p"}, nil)
	if echo.Name != source.Name+"·优化" {
		t.Fatalf("same-name draft must be renamed, got %q", echo.Name)
	}
	blank := buildExpertOptimizeDraft(source, expertOptimizeLLMOutput{SystemPrompt: "p"}, nil)
	if blank.Name != source.Name+"·优化" || blank.Icon != source.Icon || blank.Description != source.Description {
		t.Fatalf("blank fields must fall back to source: %+v", blank)
	}

	// Existing optimized expert: id, (possibly user-renamed) name, tools/skills
	// and about are kept; distilled fields refresh.
	existing := &ExpertDefinition{
		ID:           "expert-opt",
		Name:         "用户改的名",
		SystemPrompt: "已接受的当前优化提示词",
		Tools:        []string{"custom_tool"},
		Skills:       []string{"custom_skill"},
		About:        "作者：李四",
	}
	update := buildExpertOptimizeDraft(source, output, existing)
	if !update.UpdateExisting || update.ID != "expert-opt" {
		t.Fatalf("update draft must target the existing optimized expert: %+v", update)
	}
	if update.Name != "用户改的名" {
		t.Fatalf("existing optimized expert name must be kept, got %q", update.Name)
	}
	if update.SourceSystemPrompt != existing.SystemPrompt || len(update.SourceTools) != 1 || update.SourceTools[0] != "custom_tool" || len(update.SourceSkills) != 1 || update.SourceSkills[0] != "custom_skill" || update.SourceName != source.Name {
		t.Fatalf("re-optimization review must compare with the accepted current base: %+v", update)
	}
	if update.SystemPrompt != output.SystemPrompt || update.OptimizedFromID != source.ID {
		t.Fatalf("update draft must refresh distilled fields: %+v", update)
	}
	if len(update.Tools) != 1 || update.Tools[0] != "custom_tool" || len(update.Skills) != 1 || update.Skills[0] != "custom_skill" {
		t.Fatalf("update draft must keep existing tools/skills: %+v", update)
	}
	if update.About != "作者：李四" {
		t.Fatalf("update draft must keep existing about: %+v", update)
	}
	weakUpdate := buildExpertOptimizeDraft(source, expertOptimizeLLMOutput{SystemPrompt: "太短"}, &ExpertDefinition{SystemPrompt: "已有的优化提示词不能丢失"})
	if weakUpdate.SystemPrompt != "已有的优化提示词不能丢失" {
		t.Fatalf("weak re-optimization must preserve the existing optimized prompt: %+v", weakUpdate)
	}
	if update.Name != existing.Name || update.SourceName != source.Name {
		t.Fatalf("re-optimization must retain the editable name while preserving original lineage: %+v", update)
	}
}

func TestChooseOptimizedSystemPromptRejectsWeakReplacement(t *testing.T) {
	source := "你是论文润色专家。保留事实，使用正式书面语，并按摘要、问题、建议的顺序输出。"
	if got := chooseOptimizedSystemPrompt(source, "更正式"); got != source {
		t.Fatalf("short replacement must retain source prompt, got %q", got)
	}
	if got := chooseOptimizedSystemPrompt(source, "   "); got != source {
		t.Fatalf("empty replacement must retain source prompt, got %q", got)
	}
	candidate := "你是论文润色专家。保留原有流程。新增要求：始终使用正式书面语，并将修改建议按优先级排序。"
	if got := chooseOptimizedSystemPrompt(source, candidate); got != candidate {
		t.Fatalf("meaningful replacement should remain editable, got %q", got)
	}
	if got := chooseOptimizedSystemPrompt(source, strings.Repeat("失控扩写", 500)); got != source {
		t.Fatalf("oversized replacement must retain source prompt")
	}
}

func TestExpertOptimizeBaseKeepsAcceptedOptimizations(t *testing.T) {
	source := &ExpertDefinition{ID: "source", Name: "原专家", SystemPrompt: "原始提示词"}
	existing := &ExpertDefinition{ID: "optimized", Name: "优化专家", SystemPrompt: "已接受的优化提示词"}
	if got := expertOptimizeBase(source, existing); got != existing {
		t.Fatalf("re-optimization must use the existing optimized expert as its base")
	}
	if got := expertOptimizeBase(source, &ExpertDefinition{}); got != source {
		t.Fatalf("empty existing prompt must fall back to the source expert")
	}
	message := buildExpertOptimizeUserMessage(existing, "用户：新增正式语气")
	if !strings.Contains(message, "已接受的优化提示词") || strings.Contains(message, "原始提示词") {
		t.Fatalf("optimization input must contain the selected base configuration: %q", message)
	}
}

func TestBuildExpertOptimizeTranscript(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-src")

	// Empty session → empty transcript (the binding turns this into the
	// "还没有会话经验可提炼" error).
	if got := buildExpertOptimizeTranscript(h, userID); got != "" {
		t.Fatalf("empty session transcript = %q, want empty", got)
	}

	entries := []agent.ConversationEntry{
		{Role: "system", Content: "persona"},
		{Role: "user", Content: "把语气改正式一些"},
		{Role: "assistant", Content: "好的，后续都用正式书面语。"},
	}
	h.memory.Save(userID, entries)

	got := buildExpertOptimizeTranscript(h, userID)
	if !strings.Contains(got, "用户：把语气改正式一些") || !strings.Contains(got, "专家：好的，后续都用正式书面语。") {
		t.Fatalf("transcript missing conversation lines: %q", got)
	}
	if strings.Contains(got, "persona") {
		t.Fatalf("system entries must be excluded: %q", got)
	}

	// User evidence has its own budget, so long assistant replies cannot crowd
	// out recent user corrections.
	many := make([]agent.ConversationEntry, 0, expertOptimizeMaxUserMessages+15)
	for i := 0; i < expertOptimizeMaxUserMessages+15; i++ {
		many = append(many, agent.ConversationEntry{Role: "user", Content: strings.Repeat("问", i+1)})
	}
	h.memory.Save(userID, many)
	tail := buildExpertOptimizeTranscript(h, userID)
	if strings.Contains(tail, "用户：问\n") { // the oldest one-rune message must be dropped
		t.Fatalf("oldest messages must be dropped beyond the user evidence cap: %q", tail[:80])
	}
	if got := len([]rune(tail)); got > expertOptimizeMaxTranscriptRunes {
		t.Fatalf("transcript exceeds rune cap: %d > %d", got, expertOptimizeMaxTranscriptRunes)
	}
}

func TestSelectExpertOptimizeEvidencePrioritizesUserCorrections(t *testing.T) {
	lines := make([]expertOptimizeTranscriptLine, 0, expertOptimizeMaxAssistantMessages+6)
	for i := 0; i < expertOptimizeMaxAssistantMessages+5; i++ {
		lines = append(lines, expertOptimizeTranscriptLine{role: "assistant", content: "长回答"})
	}
	lines = append(lines, expertOptimizeTranscriptLine{role: "user", content: "最终用户纠正"})
	selected := selectExpertOptimizeEvidence(lines)
	if len(selected) != expertOptimizeMaxAssistantMessages+1 {
		t.Fatalf("unexpected selection length: %d", len(selected))
	}
	if selected[len(selected)-1].role != "user" || selected[len(selected)-1].content != "最终用户纠正" {
		t.Fatalf("recent user correction must be retained: %+v", selected)
	}
}

func TestExpertOptimizeTranscriptBudgetPrioritizesUserCorrections(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-user-priority")
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "用户最终要求：始终用正式、简洁的语气回答"},
	}
	for i := 0; i < expertOptimizeMaxAssistantMessages; i++ {
		entries = append(entries, agent.ConversationEntry{
			Role:    "assistant",
			Content: strings.Repeat("冗长回答", expertOptimizeMaxMessageRunes),
		})
	}
	h.memory.Save(userID, entries)

	got := buildExpertOptimizeTranscript(h, userID)
	if !strings.Contains(got, "用户最终要求：始终用正式、简洁的语气回答") {
		t.Fatalf("user correction must survive assistant-heavy transcript truncation: %q", got)
	}
	if len([]rune(got)) > expertOptimizeMaxTranscriptRunes {
		t.Fatalf("transcript exceeds rune cap: %d > %d", len([]rune(got)), expertOptimizeMaxTranscriptRunes)
	}
}

func TestExpertOptimizeTranscriptPreservesWholeRoleLinesWhenTruncated(t *testing.T) {
	longest := "用户：" + strings.Repeat("长", expertOptimizeMaxTranscriptRunes/2)
	transcript := longest + "\n专家：保留第二条完整证据\n用户：最终偏好"
	got := trimExpertOptimizeTranscript(transcript, len([]rune("专家：保留第二条完整证据\n用户：最终偏好")))
	if got != "专家：保留第二条完整证据\n用户：最终偏好" {
		t.Fatalf("truncation must discard complete oldest lines, got %q", got)
	}
	if got := compactExpertOptimizeMessage("  请  使用\n正式   语气  "); got != "请 使用 正式 语气" {
		t.Fatalf("message whitespace should be compacted, got %q", got)
	}
}

func TestExpertOptimizeTranscriptCompactsLongAndDuplicateEvidence(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-dedupe")
	long := "前置要求" + strings.Repeat("内容", expertOptimizeMaxMessageRunes) + "最后必须正式输出"
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "请用正式语气"},
		{Role: "user", Content: "请用正式语气"},
		{Role: "assistant", Content: long},
	})
	got := buildExpertOptimizeTranscript(h, userID)
	if strings.Count(got, "用户：请用正式语气") != 1 {
		t.Fatalf("duplicate evidence must not be repeated: %q", got)
	}
	if !strings.Contains(got, "前置要求") || !strings.Contains(got, "最后必须正式输出") || !strings.Contains(got, "内容过长已省略") {
		t.Fatalf("long evidence should keep both useful edges: %q", got)
	}
}

func TestExpertOptimizeTranscriptRedactsSensitiveEvidence(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-redaction")
	privateKey := "-----BEGIN PRIVATE KEY-----\nvery-secret-key-material\n-----END PRIVATE KEY-----"
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "Authorization: Bearer top-secret-token"},
		{Role: "assistant", Content: "请继续使用正式语气。Authorization: Basic basic-secret-token; \"x-api-key\": \"another-secret-token\"; email: person@example.com; 电话13800138000，身份证11010519491231002X。"},
		{Role: "user", Content: privateKey},
	})

	got := buildExpertOptimizeTranscript(h, userID)
	for _, secret := range []string{"top-secret-token", "basic-secret-token", "another-secret-token", "very-secret-key-material", "person@example.com", "13800138000", "11010519491231002X"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sensitive evidence %q leaked into optimization transcript: %q", secret, got)
		}
	}
	if !strings.Contains(got, "[已移除敏感凭据]") || !strings.Contains(got, "[已移除个人联系方式]") || !strings.Contains(got, "[已移除个人敏感信息]") {
		t.Fatalf("redaction markers missing from transcript: %q", got)
	}
}

func TestRedactExpertOptimizeSensitiveTextPreservesUnicodeGuards(t *testing.T) {
	input := "请联系张三电话13800138000，身份证11010519491231002X。"
	got := redactExpertOptimizeSensitiveText(input)
	if strings.Contains(got, "13800138000") || strings.Contains(got, "11010519491231002X") {
		t.Fatalf("numeric PII leaked: %q", got)
	}
	if !strings.Contains(got, "电话[已移除个人联系方式]，身份证[已移除个人敏感信息]。") {
		t.Fatalf("surrounding Chinese text or punctuation was lost: %q", got)
	}
}

func TestExpertOptimizeTranscriptDropsRedactionOnlyTurns(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-redaction-only")
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "Authorization: Bearer top-secret-token"},
		{Role: "assistant", Content: "请始终使用正式语气"},
	})

	got := buildExpertOptimizeTranscript(h, userID)
	if strings.Contains(got, "已移除敏感凭据") || strings.Contains(got, "top-secret-token") {
		t.Fatalf("redaction-only turn must not become optimization evidence: %q", got)
	}
	if !strings.Contains(got, "请始终使用正式语气") {
		t.Fatalf("non-sensitive evidence should remain: %q", got)
	}
	if got := buildExpertOptimizeTranscript(&IMMessageHandler{memory: func() *agent.ConversationMemory {
		memory := agent.NewConversationMemory()
		memory.Save(userID, []agent.ConversationEntry{{Role: "user", Content: "api_key=secret-only"}, {Role: "assistant", Content: "邮箱: person@example.com"}})
		return memory
	}()}, userID); got != "" {
		t.Fatalf("redaction-only history must be treated as empty evidence: %q", got)
	}
}

func TestExpertOptimizeContentHasMeaningfulEvidence(t *testing.T) {
	for _, content := range []string{
		"Authorization: [已移除敏感凭据]",
		"邮箱: [已移除个人联系方式]",
		"身份证[已移除个人敏感信息]。",
		"[已移除敏感凭据] ; ,",
	} {
		if expertOptimizeContentHasMeaningfulEvidence(content) {
			t.Fatalf("redacted metadata must not count as evidence: %q", content)
		}
	}
	if !expertOptimizeContentHasMeaningfulEvidence("请将回复保持正式。凭据：[已移除敏感凭据]") {
		t.Fatal("non-sensitive user preference must remain evidence")
	}
}

func TestParseExpertOptimizeResponseFindsBalancedJSON(t *testing.T) {
	raw := "说明 {不应作为 JSON 解析} 后面的结果：\n{\"name\":\"优化专家\",\"description\":\"简介\",\"icon\":\"✨\",\"system_prompt\":\"保留 {花括号} 内容\"}\n附注"
	parsed, err := parseExpertOptimizeResponse(raw)
	if err != nil || parsed.SystemPrompt != "保留 {花括号} 内容" {
		t.Fatalf("parser must skip prose braces and find the JSON object: %+v, %v", parsed, err)
	}
	object := "{\"name\":\"优化专家\",\"description\":\"简介\",\"icon\":\"✨\",\"system_prompt\":\"保留 {花括号} 内容\"} trailing } prose"
	parsed, err = parseExpertOptimizeResponse(object)
	if err != nil || parsed.SystemPrompt != "保留 {花括号} 内容" {
		t.Fatalf("balanced JSON with braces in a string must parse: %+v, %v", parsed, err)
	}
}
