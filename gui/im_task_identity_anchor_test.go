package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestTaskIdentityAnchorRetainsSubjectAndSourceAcrossGenericFollowUp(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	source := `C:\Users\test\Desktop\resume-academic.pdf`
	initial := "基于简历，撰写马勇的学术简介\n\n[用户选择的本地文件路径]\n" + source +
		"\n\n--- auto_extract: begin path=\"" + source + "\" format=\"pdf\" total_chars=20 injected_chars=20 truncated=false ---\n马 勇 软件架构师\n--- auto_extract: end path=\"" + source + "\" ---"

	h.updateTaskIdentityAnchorFromUserText(owner, initial)
	h.updateTaskIdentityAnchorFromUserText(owner, "浓缩成300字左右的个人学术简介")

	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected an active task identity anchor")
	}
	if anchor.Subject != "马勇" {
		t.Fatalf("subject = %q, want 马勇", anchor.Subject)
	}
	if len(anchor.SourcePaths) != 1 || anchor.SourcePaths[0] != source {
		t.Fatalf("source paths = %#v, want %q", anchor.SourcePaths, source)
	}
	prompt := taskIdentityAnchorPrompt(anchor)
	for _, want := range []string{"马勇", source, "不得替换为其他人", "不得用长期记忆"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("anchor prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestTaskIdentityAnchorIsClearedAtConversationBoundary(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写马勇的科研简介\nC:\\resume.pdf")
	if _, ok := h.taskIdentityAnchorForUser(owner); !ok {
		t.Fatal("expected anchor before reset")
	}
	h.clearPerUserSessionState(owner)
	if _, ok := h.taskIdentityAnchorForUser(owner); ok {
		t.Fatal("anchor should not survive a new conversation")
	}
}

func TestTaskIdentityAnchorBlocksForeignProfileMemorySave(t *testing.T) {
	anchor := taskIdentityAnchor{Subject: "马勇"}
	if !anchoredMemorySaveRejected(anchor, "李新帅，博士，研究方向为非平稳时间序列") {
		t.Fatal("foreign academic profile should be rejected")
	}
	if anchoredMemorySaveRejected(anchor, "马勇，人工智能博士，研究方向为大语言模型") {
		t.Fatal("anchored profile should be allowed")
	}
	if anchoredMemorySaveRejected(anchor, "已完成 Markdown 排版") {
		t.Fatal("ordinary operational note should be allowed")
	}
	if anchoredMemorySaveRejected(anchor, "科研计划已更新，待确认下一步") {
		t.Fatal("operational note with a profile keyword but no person should be allowed")
	}
	if !anchoredMemorySaveRejected(anchor, "李新帅，博士，研究方向为大语言模型；马勇仅在合作名单中出现") {
		t.Fatal("foreign profile headed by another person should be rejected even when it mentions the anchored subject")
	}
}

func TestTaskIdentityAnchorCrossTaskConsentIsOneTurnOnly(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写马勇的科研简介\nC:\\resume.pdf")
	if !taskAnchorAllowsCrossTaskRecall("请搜索历史会话，看看此前版本") {
		t.Fatal("explicit history request should allow cross-task recall for its own turn")
	}
	if taskAnchorAllowsCrossTaskRecall("浓缩成300字") {
		t.Fatal("generic follow-up must not inherit cross-task recall consent")
	}
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok || anchor.Subject != "马勇" {
		t.Fatalf("anchor should remain intact while consent stays per-turn: %#v", anchor)
	}
}

func TestTaskIdentityAnchorIsAddedToConversationAfterHistoricalAttachmentsAreStripped(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	source := `C:\Users\test\Desktop\resume-academic.pdf`
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写马勇的学术简介\n"+source)

	history := []agent.ConversationEntry{{
		Role: "user",
		Content: "原始材料\n[用户选择的本地文件路径]\n" + source +
			"\n--- auto_extract: begin path=\"" + source + "\" format=\"pdf\" ---\n马勇的完整正文\n--- auto_extract: end path=\"" + source + "\" ---",
	}}
	started := h.buildAgentLoopConversationStart("chat", owner, "浓缩为300字", "system", "desktop", nil, corelib.MaclawLLMConfig{}, history, 0, nil, nil, nil, true)

	if len(started.Conversation) < 2 {
		t.Fatalf("conversation unexpectedly short: %d", len(started.Conversation))
	}
	anchorSeen := false
	for _, raw := range started.Conversation {
		message, ok := raw.(map[string]string)
		if !ok || message["role"] != "system" {
			continue
		}
		if strings.Contains(message["content"], "任务身份与来源锚点") && strings.Contains(message["content"], "马勇") {
			anchorSeen = true
		}
	}
	if !anchorSeen {
		t.Fatal("expected host-owned task anchor in the LLM conversation")
	}
}

func TestTaskIdentityAnchorSubjectExtractionCoversDirectResearchBioRequest(t *testing.T) {
	if got := extractTaskAnchorSubject("写马勇科研简介"); got != "马勇" {
		t.Fatalf("subject = %q, want 马勇", got)
	}
}

func TestSessionSearchCurrentTurnConsent(t *testing.T) {
	if !taskAnchorAllowsCrossTaskRecall(sessionSearchUserText(map[string]interface{}{"_user_text": "搜索历史会话"})) {
		t.Fatal("current-turn history request should be recognized")
	}
	if taskAnchorAllowsCrossTaskRecall(sessionSearchUserText(map[string]interface{}{"_user_text": "浓缩成300字"})) {
		t.Fatal("ordinary continuation must not authorize cross-task search")
	}
}

func TestTaskIdentityAnchorBlocksMemoryRecallUntilCurrentTurnExplicitlyRequestsIt(t *testing.T) {
	anchor := taskIdentityAnchor{Subject: "马勇", SourcePaths: []string{`C:\mayong.pdf`}}
	for _, action := range []memoryToolAction{memoryToolActionRecall, memoryToolActionList, memoryToolActionSummary, memoryToolActionDerived} {
		if !taskAnchorBlocksMemoryRead(anchor, action, "浓缩成300字") {
			t.Fatalf("source-bound task should block %q without a current-turn memory request", action)
		}
	}
	if taskAnchorBlocksMemoryRead(anchor, memoryToolActionSave, "浓缩成300字") {
		t.Fatal("memory write is governed by the separate identity-aware write gate")
	}
	if taskAnchorBlocksMemoryRead(anchor, memoryToolActionRecall, "请从长期记忆中补充我的写作偏好") {
		t.Fatal("an explicit current-turn long-term memory request should be allowed")
	}
	if !taskAnchorBlocksMemoryRead(anchor, memoryToolActionRecall, "搜索历史会话，看看此前版本") {
		t.Fatal("history-search consent must not implicitly authorize long-term memory")
	}
}

func TestSessionSearchIsRegisteredAsOwnerScoped(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	tool, ok := h.registry.Get("session_search")
	if !ok || tool == nil {
		t.Fatal("session_search should be registered")
	}
	if !tool.RuntimePolicyOwnerArg {
		t.Fatal("session_search must receive a host-owned runtime owner")
	}
}

func TestTaskIdentityAnchorSourceOnlyDoesNotReplaceExistingSubject(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写马勇的科研简介\nC:\\first.pdf")
	h.updateTaskIdentityAnchorFromUserText(owner, "补充参考文件\nC:\\second.pdf")
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok || anchor.Subject != "马勇" {
		t.Fatalf("source-only follow-up replaced subject: %#v", anchor)
	}
	if len(anchor.SourcePaths) != 2 {
		t.Fatalf("source paths = %#v, want both documents", anchor.SourcePaths)
	}
}

func TestTaskIdentityAnchorReplacesSourceWhenUserSwitchesSubject(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写马勇的科研简介\nC:\\mayong.pdf")
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写李新帅的科研简介\nC:\\lixinshuai.pdf")
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok || anchor.Subject != "李新帅" {
		t.Fatalf("subject = %#v, want 李新帅", anchor)
	}
	if len(anchor.SourcePaths) != 1 || anchor.SourcePaths[0] != `C:\lixinshuai.pdf` {
		t.Fatalf("sources = %#v, want only new subject source", anchor.SourcePaths)
	}
}

func TestTaskIdentityAnchorSnapshotSurvivesLaterAnchorUpdate(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user"
	first, ok := h.taskIdentityAnchorForTurn(owner, "撰写马勇的科研简介\nC:\\mayong.pdf")
	if !ok {
		t.Fatal("expected first anchor")
	}
	h.updateTaskIdentityAnchorFromUserText(owner, "撰写李新帅的科研简介\nC:\\lixinshuai.pdf")
	if first.Subject != "马勇" || len(first.SourcePaths) != 1 || first.SourcePaths[0] != `C:\mayong.pdf` {
		t.Fatalf("first snapshot mutated: %#v", first)
	}
}

func TestTaskIdentityAnchorSurvivesSystemPromptRebuild(t *testing.T) {
	first := &taskIdentityAnchor{Subject: "马勇", SourcePaths: []string{`C:\mayong.pdf`}}
	cb := &sharedAgentLoopCallbacks{taskAnchor: first}
	refreshed := cb.systemPromptWithTaskAnchor("rebuilt system prompt")
	if !strings.Contains(refreshed, "马勇") || !strings.Contains(refreshed, `C:\mayong.pdf`) {
		t.Fatalf("rebuilt prompt lost the turn anchor: %q", refreshed)
	}
	if got := cb.systemPromptWithTaskAnchor(refreshed); strings.Count(got, "[任务身份与来源锚点 — 强制约束]") != 1 {
		t.Fatalf("anchor should be idempotent across repeated refreshes: %q", got)
	}
	if first.Subject != "马勇" {
		t.Fatalf("turn snapshot mutated: %#v", first)
	}
}

func TestTaskIdentityAnchorFromToolArgsRejectsWrongType(t *testing.T) {
	if _, ok := taskIdentityAnchorFromToolArgs(map[string]interface{}{"_task_identity_anchor": map[string]interface{}{"Subject": "李新帅"}}); ok {
		t.Fatal("serialized/model-shaped anchor must not be accepted as trusted host metadata")
	}
}
