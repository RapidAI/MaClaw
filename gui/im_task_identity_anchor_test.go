package main

import (
	"path/filepath"
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
	ppt := taskIdentityAnchor{OriginalRequest: "码卡龙平台仓库 github.com/rapidia/maclaw 编写介绍PPT"}
	if !taskAnchorBlocksMemoryRead(ppt, memoryToolActionRecall, "继续改进ppt呀") {
		t.Fatal("a PPT charter must not silently recall another task's long-term memory")
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

func TestTaskIdentityAnchorFromToolArgsAcceptsOriginalRequestOnly(t *testing.T) {
	if _, ok := taskIdentityAnchorFromToolArgs(map[string]interface{}{
		"_task_identity_anchor": taskIdentityAnchor{OriginalRequest: "码卡龙企业级 Agent 平台介绍 PPT"},
	}); !ok {
		t.Fatal("original-request-only charter must be accepted as trusted host metadata")
	}
}

func TestTaskIdentityAnchorKeepsPPTCharterAcrossMathStyleFollowUp(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw  , 编写一个介绍AI Native组织的驱动系统，maclaw开源企业级agent平台的介绍PPT, 需要突出AI Native组织的运行原理，码卡龙平台的企业级特性对此的支撑，以及码卡龙的先进技术。先写提纲，再写ppt"

	h.updateTaskIdentityAnchorFromUserText(owner, original)
	h.updateTaskIdentityAnchorFromUserText(owner, "每个概念的文字解释太少，需要做到图、文、公式并茂，概念解释还需要联系物理意义与AI中的应用，方便理解")
	h.updateTaskIdentityAnchorFromUserText(owner, "忘掉前面的错误提示。ppt需要专业风格，现在太朴素了。")
	h.updateTaskIdentityAnchorFromUserText(owner, "继续改进ppt呀")

	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected a task charter after the original PPT request")
	}
	if !strings.Contains(anchor.OriginalRequest, "码卡龙") || !strings.Contains(anchor.OriginalRequest, "maclaw") {
		t.Fatalf("original request drifted: %#v", anchor)
	}
	if strings.Contains(anchor.OriginalRequest, "每个概念") || strings.Contains(anchor.OriginalRequest, "数学") {
		t.Fatalf("math-style follow-up replaced the charter: %#v", anchor)
	}
	if anchor.WorkKind != "ppt" {
		t.Fatalf("work kind = %q, want ppt", anchor.WorkKind)
	}
	prompt := taskIdentityAnchorPrompt(anchor)
	for _, want := range []string{"码卡龙", "继续改进 ppt", "其他主题"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("charter prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "每个概念的文字解释太少") {
		t.Fatalf("follow-up leaked into charter prompt:\n%s", prompt)
	}

	mathArgs := map[string]interface{}{"action": "write_pptx", "file_path": "ai-math-foundations.pptx"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", mathArgs); reason == "" {
		t.Fatal("math lecture PPT must not be writable on a MaClaw PPT charter")
	}
	maclawArgs := map[string]interface{}{"action": "write_pptx", "file_path": "maclaw-ai-native-org.pptx"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", maclawArgs); reason != "" {
		t.Fatalf("MaClaw PPT write blocked: %s", reason)
	}

	h.rememberTaskAnchorDeliverable(owner, &anchor, "office", maclawArgs)
	if len(anchor.PrimaryFiles) != 1 || !strings.Contains(anchor.PrimaryFiles[0], "maclaw-ai-native-org.pptx") {
		t.Fatalf("primary files = %#v", anchor.PrimaryFiles)
	}
	chartArgs := map[string]interface{}{"action": "write_pptx", "file_path": "maclaw-ai-native-org-charts.pptx"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", chartArgs); reason != "" {
		t.Fatalf("primary variant blocked: %s", reason)
	}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", mathArgs); reason == "" {
		t.Fatal("math PPT must stay blocked after the primary deliverable is recorded")
	}
}

func TestTaskIdentityAnchorRecoversOriginalRequestFromSessionHistory(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt-recover"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw 编写一个介绍AI Native组织的驱动系统的介绍PPT"
	h.memory.Save(owner, []agent.ConversationEntry{
		{Role: "user", Content: original, Timestamp: 100},
		{Role: "assistant", Content: "提纲已写", Timestamp: 200},
	})

	anchor, ok := h.taskIdentityAnchorForTurn(owner, "继续改进ppt呀")
	if !ok {
		t.Fatal("expected recovered charter")
	}
	if !strings.Contains(anchor.OriginalRequest, "码卡龙") {
		t.Fatalf("did not recover original request: %#v", anchor)
	}
	if strings.Contains(anchor.OriginalRequest, "继续改进") {
		t.Fatalf("used the follow-up as the charter: %#v", anchor)
	}
}

func TestTaskIdentityAnchorPPTCharterIsInjectedWithoutPersonSubject(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt-prompt"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw 编写一个介绍AI Native组织的驱动系统的介绍PPT"
	started := h.buildAgentLoopConversationStart("chat", owner, original, "system", "desktop", nil, corelib.MaclawLLMConfig{}, nil, 0, nil, nil, nil, true)
	anchorSeen := false
	for _, raw := range started.Conversation {
		message, ok := raw.(map[string]string)
		if !ok || message["role"] != "system" {
			continue
		}
		if strings.Contains(message["content"], "任务身份与来源锚点") && strings.Contains(message["content"], "码卡龙") {
			anchorSeen = true
		}
	}
	if !anchorSeen {
		t.Fatal("PPT charter must be injected even without a person-name subject")
	}
}

func TestTaskIdentityAnchorIgnoresCasualChat(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:casual"
	h.updateTaskIdentityAnchorFromUserText(owner, "你好")
	h.updateTaskIdentityAnchorFromUserText(owner, "南京天气，输出格式化pdf报告")
	if _, ok := h.taskIdentityAnchorForUser(owner); ok {
		t.Fatal("casual chat must not install an enforced task charter")
	}
	if reason := taskAnchorDeliverableWriteBlockReason(&taskIdentityAnchor{OriginalRequest: "南京天气，输出格式化pdf报告"}, "office", map[string]interface{}{
		"action":    "write_pptx",
		"file_path": "ai-math-foundations.pptx",
	}); reason != "" {
		t.Fatalf("unenforced weather charter must not gate PPT writes: %s", reason)
	}
}

func TestTaskIdentityAnchorRecoversFromMultimodalHistory(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt-multimodal"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw 编写一个介绍AI Native组织的驱动系统的介绍PPT"
	h.memory.Save(owner, []agent.ConversationEntry{{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": original},
		},
		Timestamp: 50,
	}})
	anchor, ok := h.taskIdentityAnchorForTurn(owner, "继续改进ppt呀")
	if !ok || !strings.Contains(anchor.OriginalRequest, "码卡龙") {
		t.Fatalf("multimodal original request was not recovered: %#v", anchor)
	}
}

func TestTaskIdentityAnchorPrefersEarliestTimestampedRequest(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt-ts"
	h.memory.Save(owner, []agent.ConversationEntry{
		{Role: "user", Content: "编写人工智能数学基础讲义PPT，图文公式并茂", Timestamp: 200},
		{Role: "user", Content: "码卡龙平台仓库： github.com/rapidia/maclaw 编写介绍AI Native组织的PPT", Timestamp: 100},
	})
	anchor, ok := h.taskIdentityAnchorForTurn(owner, "继续改进ppt呀")
	if !ok || !strings.Contains(anchor.OriginalRequest, "码卡龙") {
		t.Fatalf("later unchartered follow-up won the charter: %#v", anchor)
	}
}

func TestRememberTaskAnchorDeliverableSkipsContradiction(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-ppt-record"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw 编写一个介绍AI Native组织的驱动系统的介绍PPT"
	h.updateTaskIdentityAnchorFromUserText(owner, original)
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected charter")
	}
	h.rememberTaskAnchorDeliverable(owner, &anchor, "office", map[string]interface{}{
		"action": "write_pptx", "file_path": `C:\ws\ai-math-foundations.pptx`,
	})
	if len(anchor.PrimaryFiles) != 0 {
		t.Fatalf("contradicting write mutated snapshot: %#v", anchor.PrimaryFiles)
	}
	got, _ := h.taskIdentityAnchorForUser(owner)
	if len(got.PrimaryFiles) != 0 {
		t.Fatalf("contradicting write persisted: %#v", got.PrimaryFiles)
	}
	h.rememberTaskAnchorDeliverable(owner, &anchor, "office", map[string]interface{}{
		"action": "write_pptx", "file_path": `C:\ws\maclaw-ai-native-org.pptx`,
	})
	if len(anchor.PrimaryFiles) != 1 || filepath.Base(anchor.PrimaryFiles[0]) != "maclaw-ai-native-org.pptx" {
		t.Fatalf("primary snapshot = %#v", anchor.PrimaryFiles)
	}
	prompt := taskIdentityAnchorPrompt(anchor)
	if strings.Contains(prompt, `C:\ws\`) {
		t.Fatalf("prompt leaked full path:\n%s", prompt)
	}
	if !strings.Contains(prompt, "maclaw-ai-native-org.pptx") {
		t.Fatalf("prompt missing basename:\n%s", prompt)
	}
}

func TestTaskIdentityAnchorChineseTopicBlocksEnglishMathFilename(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-zh"
	h.updateTaskIdentityAnchorFromUserText(owner, "编写一个介绍码卡龙企业级Agent平台的演讲PPT")
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected Chinese PPT charter")
	}
	mathArgs := map[string]interface{}{"action": "write_pptx", "file_path": `C:\ws\ai-math-foundations.pptx`}
	reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", mathArgs)
	if reason == "" {
		t.Fatal("Chinese MaClaw charter must still block an English math lecture filename")
	}
	if strings.Contains(reason, `C:\ws\`) {
		t.Fatalf("block reason leaked a full path: %s", reason)
	}
	if !strings.Contains(reason, "ai-math-foundations.pptx") {
		t.Fatalf("block reason should name the rejected file: %s", reason)
	}
	maclawArgs := map[string]interface{}{"action": "write_pptx", "file_path": "maclaw-ai-native-org.pptx"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", maclawArgs); reason != "" {
		t.Fatalf("aliased MaClaw filename blocked: %s", reason)
	}
	genericArgs := map[string]interface{}{"action": "write_pptx", "file_path": "presentation.pptx"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", genericArgs); reason != "" {
		t.Fatalf("generic first PPT write blocked: %s", reason)
	}
}

func TestTaskIdentityAnchorTopicSignalIsNotContinuation(t *testing.T) {
	if isTaskAnchorContinuationText("ppt需要做成码卡龙介绍，专业风格") {
		t.Fatal("a named-topic PPT request must not be treated as a continuation")
	}
	if !isTaskAnchorContinuationText("忘掉前面的错误提示。ppt需要专业风格，现在太朴素了。") {
		t.Fatal("style-only follow-up should stay a continuation")
	}
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-named"
	h.updateTaskIdentityAnchorFromUserText(owner, "ppt需要做成码卡龙介绍，专业风格")
	anchor, ok := h.taskIdentityAnchorForUser(owner)
	if !ok || !strings.Contains(anchor.OriginalRequest, "码卡龙") {
		t.Fatalf("named-topic request should become the charter: %#v", anchor)
	}
}

func TestTaskIdentityAnchorIgnoresHomeDirectoryMaclawSegment(t *testing.T) {
	anchor := taskIdentityAnchor{
		Subject:         "马勇",
		OriginalRequest: "撰写马勇的科研简介",
		SourcePaths:     []string{`C:\Users\ma139\.maclaw\data\cloud-workspaces\cws_x\resume.pdf`},
	}
	reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", map[string]interface{}{
		"action": "write_pptx", "file_path": "maclaw-ai-native-org.pptx",
	})
	if reason == "" {
		t.Fatal("a resume task must not inherit MaClaw identity from the .maclaw data directory")
	}
}

func TestTaskIdentityAnchorSpeechWithoutPPTIsNotWorkKind(t *testing.T) {
	if got := extractTaskWorkKind("长江学者申请后的学术演讲安排"); got != "" {
		t.Fatalf("bare 演讲 must not classify as %q", got)
	}
	if got := extractTaskWorkKind("编写一个介绍码卡龙企业级Agent平台的演讲PPT"); got != "ppt" {
		t.Fatalf("演讲PPT = %q, want ppt", got)
	}
}

func TestTaskIdentityAnchorGenericVersionedFilenameAllowed(t *testing.T) {
	anchor := taskIdentityAnchor{OriginalRequest: "编写一个介绍码卡龙企业级Agent平台的演讲PPT", WorkKind: "ppt"}
	if reason := taskAnchorDeliverableWriteBlockReason(&anchor, "office", map[string]interface{}{
		"action": "write_pptx", "file_path": "presentation-v2.pptx",
	}); reason != "" {
		t.Fatalf("versioned generic PPT blocked: %s", reason)
	}
}

func TestTaskIdentityAnchorBookMentionIsNotDocumentCharter(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:books"
	h.updateTaskIdentityAnchorFromUserText(owner, "帮我找几本机器学习书籍")
	if _, ok := h.taskIdentityAnchorForUser(owner); ok {
		t.Fatal("a book-shopping request must not install a document charter")
	}
	h.updateTaskIdentityAnchorFromUserText(owner, "把讲义拍个照发给我")
	if _, ok := h.taskIdentityAnchorForUser(owner); ok {
		t.Fatal("photographing lecture notes must not install a document charter")
	}
	if got := extractTaskWorkKind("使用book-pdf skill 编写人工智能数学入门"); got != "document" {
		t.Fatalf("book-pdf handbook request = %q, want document", got)
	}
}

func TestTaskIdentityAnchorPrefersActiveBranchOverEarlierInactiveTopic(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-active-branch"
	math := "编写人工智能数学基础讲义PPT，图文公式并茂"
	maclaw := "码卡龙平台仓库： github.com/rapidia/maclaw 编写介绍AI Native组织的PPT"
	h.memory.Save(owner, []agent.ConversationEntry{
		{Role: "user", Content: math, ID: "math", Timestamp: 50},
		{Role: "assistant", Content: "ok", ID: "math-a", ParentID: "math", Timestamp: 51},
		{Role: "user", Content: maclaw, ID: "maclaw", Timestamp: 100},
		{Role: "assistant", Content: "ok", ID: "maclaw-a", ParentID: "maclaw", Timestamp: 101},
	})
	anchor, ok := h.taskIdentityAnchorForTurn(owner, "继续改进ppt呀")
	if !ok || !strings.Contains(anchor.OriginalRequest, "码卡龙") {
		t.Fatalf("active-branch MaClaw request lost to an earlier inactive topic: %#v", anchor)
	}
}

func TestTaskIdentityAnchorFollowUpDoesNotRewriteUnchangedCharter(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	const owner = "desktop-user:maclaw-stable"
	original := "码卡龙平台仓库： github.com/rapidia/maclaw 编写一个介绍AI Native组织的驱动系统的介绍PPT"
	h.updateTaskIdentityAnchorFromUserText(owner, original)
	first, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected charter")
	}
	h.updateTaskIdentityAnchorFromUserText(owner, "继续改进ppt呀")
	second, ok := h.taskIdentityAnchorForUser(owner)
	if !ok {
		t.Fatal("expected charter after follow-up")
	}
	if !first.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("unchanged follow-up rewrote the charter timestamp: %v vs %v", first.UpdatedAt, second.UpdatedAt)
	}
	if second.OriginalRequest != first.OriginalRequest {
		t.Fatalf("follow-up changed original request: %#v", second)
	}
}
