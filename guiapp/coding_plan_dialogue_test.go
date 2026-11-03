package guiapp

import (
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestCodingUserWantsPlanFirst(t *testing.T) {
	for _, text := range []string{
		"先规划再开发一个登录页",
		"先出开发文档，确认后再写代码",
		"请进入规划模式，我们先定方案",
		"先不要写代码，先把方案定下来",
		"plan first, then add a login form",
		"design first and do not code yet",
	} {
		if !codingUserWantsPlanFirst(text) {
			t.Fatalf("expected plan-first intent: %q", text)
		}
	}
	for _, text := range []string{
		"规划模式是什么",
		"fix the button label",
		"改为图形界面版",
		"实现登录并加测试",
		"how does plan mode work",
	} {
		if codingUserWantsPlanFirst(text) {
			t.Fatalf("did not expect plan-first intent: %q", text)
		}
	}
}

func TestCodingPlanDialogueConfirmAndExitPhrases(t *testing.T) {
	for _, text := range []string{"开始", "开始实施", "确认", "start"} {
		if !codingPlanDialogueConfirms(text) {
			t.Fatalf("expected confirmation: %q", text)
		}
	}
	for _, text := range []string{"开始写登录", "好，改成用 jwt", "好的", "可以", "ok"} {
		if codingPlanDialogueConfirms(text) {
			t.Fatalf("revision must not count as confirmation: %q", text)
		}
	}
	if !codingPlanDialogueDirect("不用规划了，直接做") {
		t.Fatal("expected direct-execute phrase")
	}
	if codingPlanDialogueDirect("先规划再开发一个完整的登录页和测试") {
		t.Fatal("a plan-first request must not look like skipping the plan")
	}
	if !codingPlanDialogueCancels("取消规划") {
		t.Fatal("expected cancel phrase")
	}
}

func TestPrepareCodingPlanDialogueDraftsThenWaits(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	text := "先规划再开发一个带表单和测试的登录页"
	next, stop := h.prepareCodingPlanDialogue(userID, text, "D:/repo", true, nil, nil)
	if stop == nil {
		t.Fatal("plan-first request must stay in planning and not start coding")
	}
	if next != text {
		t.Fatalf("planning turn must keep the user text, got %q", next)
	}
	if !strings.Contains(stop.Text, "不会改文件") {
		t.Fatalf("planning reply should say files stay unchanged: %s", stop.Text)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !mem.PlanDialogue || mem.PlanDialogueBrief != text {
		t.Fatalf("dialogue state = %+v", mem.PlanDialogueBrief)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) < 2 {
		t.Fatalf("a concrete plan-first request should propose a plan, pending=%+v ok=%v", pending, ok)
	}
	if pending.UserText != text {
		t.Fatalf("plan goal = %q", pending.UserText)
	}
	if _, approved := h.takeStickyApprovedCodingPlan(userID); approved {
		t.Fatal("proposing a plan must not approve it")
	}
}

func TestPlanDialogueSecondTurnUsesTheSubstantiveGoal(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	if _, stop := h.prepareCodingPlanDialogue(userID, "先规划", "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected a question before a goal exists")
	}
	goal := "做一个带图形界面的贪吃蛇"
	if _, stop := h.prepareCodingPlanDialogue(userID, goal, "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("the follow-up goal should produce a plan")
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != goal || !strings.Contains(pending.Markdown, "贪吃蛇") {
		t.Fatalf("plan kept the filler brief: %+v ok=%v", pending, ok)
	}
	next, stop := h.prepareCodingPlanDialogue(userID, "直接做", "D:/repo", true, nil, nil)
	if stop != nil || next != goal {
		t.Fatalf("direct execute ran %q, stop=%v", next, stop != nil)
	}
}

func TestPlanInsertKeepsTrailingNotesLast(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Title: "实现移动"},
		{Title: "运行说明"},
	}
	if at := codingPlanInsertAt(tasks); at != 1 {
		t.Fatalf("insert slot = %d, want the place before 运行说明", at)
	}
	added, _ := patchCodingPlan(tasks, "需要有图形界面")
	if len(added) != 3 || added[1].Title != "图形界面" || added[2].Title != "运行说明" {
		t.Fatalf("graphical step landed after the notes: %#v", added)
	}
	noted, _ := patchCodingPlan(tasks, "运行说明里写上按键")
	if len(noted) != 2 || noted[1].Title != "运行说明" || !strings.Contains(noted[1].Description, "按键") {
		t.Fatalf("a sentence about the notes should update that step: %#v", noted)
	}
}

func TestPrepareCodingPlanDialogueAsksWhenGoalIsThin(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	_, stop := h.prepareCodingPlanDialogue(userID, "先规划", "D:/repo", true, nil, nil)
	if stop == nil || !strings.Contains(stop.Text, "补充") {
		t.Fatalf("a bare plan request should ask for the goal, got %#v", stop)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("a question turn must not publish an execution plan")
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if !mem.PlanDialogue {
		t.Fatal("asking still keeps the planning conversation open")
	}
}

func TestPrepareCodingPlanDialogueConfirmStartsApprovedPlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划再开发一个带表单和测试的登录页"
	if _, stop := h.prepareCodingPlanDialogue(userID, goal, "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected a draft")
	}
	next, stop := h.prepareCodingPlanDialogue(userID, "开始", "D:/repo", true, nil, nil)
	if stop != nil {
		t.Fatalf("confirmation should hand the turn to execution, got %s", stop.Text)
	}
	if next != "开始" {
		t.Fatalf("confirmation text = %q", next)
	}
	approved, ok := h.takeStickyApprovedCodingPlan(userID)
	if !ok || approved.UserText != goal || len(approved.Tasks) < 2 {
		t.Fatalf("confirmed plan = %+v ok=%v", approved, ok)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanDialogue || strings.TrimSpace(mem.PendingPlanJSON) != "" {
		t.Fatal("confirmation should leave planning and clear the pending copy")
	}
}

func TestPrepareCodingPlanDialogueRevisionKeepsOriginalGoal(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划再开发一个带表单和测试的登录页"
	if _, stop := h.prepareCodingPlanDialogue(userID, goal, "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected a draft")
	}
	_, stop := h.prepareCodingPlanDialogue(userID, "验证要覆盖表单提交失败", "D:/repo", true, nil, nil)
	if stop == nil {
		t.Fatal("a revision stays in planning")
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != goal {
		t.Fatalf("revision must keep the original goal, pending=%+v ok=%v", pending, ok)
	}
	if !strings.Contains(pending.Markdown, "表单提交失败") || !strings.Contains(pending.Markdown, "确认改动范围") {
		t.Fatalf("revision should keep the current steps and add the change:\n%s", pending.Markdown)
	}
	if len(pending.Tasks) != 3 || !strings.Contains(pending.Tasks[2].Description, "表单提交失败") || strings.Contains(pending.Tasks[2].Description, "确认前不写文件") {
		t.Fatalf("the extra check belongs on the verify step: %#v", pending.Tasks)
	}
	if strings.Contains(pending.Markdown, "按已向用户复述的需求改写") {
		t.Fatalf("revision fell back to a generic rewrite plan:\n%s", pending.Markdown)
	}
	if strings.Contains(stop.Text, "改成图形界面版本") {
		t.Fatalf("revision was rewritten as a new implementation:\n%s", stop.Text)
	}
}

func TestPendingPlanFollowUpEditsThePlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划一下c++版贪吃蛇，确定后再开工"
	h.storeStickyPendingCodingPlan(userID, goal, "plan", []*v2.TaskItem{
		{Index: 1, Title: "实现蛇的移动和吃食物", Description: "控制台逻辑"},
		{Index: 2, Title: "撞墙与自撞结束", Description: "判定失败"},
		{Index: 3, Title: "构建、逻辑测试与运行说明", Description: "cmake 构建并写 README"},
	})
	_, stop := h.prepareCodingPlanDialogue(userID, "需要有图形界面", "D:/repo", true, nil, nil)
	if stop == nil {
		t.Fatal("a follow-up must revise the waiting plan")
	}
	if strings.Contains(stop.Text, "改成图形界面版本") {
		t.Fatalf("follow-up was treated as a rewrite:\n%s", stop.Text)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != goal {
		t.Fatalf("original goal changed: %+v ok=%v", pending, ok)
	}
	for _, want := range []string{"实现蛇的移动和吃食物", "撞墙与自撞结束", "构建、逻辑测试与运行说明", "图形界面"} {
		if !strings.Contains(pending.Markdown, want) {
			t.Fatalf("plan missing %q:\n%s", want, pending.Markdown)
		}
	}
	if gui, build := strings.Index(pending.Markdown, "图形界面"), strings.Index(pending.Markdown, "构建、逻辑测试与运行说明"); gui < 0 || build < 0 || gui > build {
		t.Fatalf("graphical step should be added before the closing build step:\n%s", pending.Markdown)
	}
	if !h.getStickyCodingWorkbenchMemory(userID).PlanDialogue {
		t.Fatal("editing a waiting plan should stay in planning")
	}
}

func TestDirectExecuteKeepsAcceptedRevisions(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划一下c++版贪吃蛇，确定后再开工"
	h.storeStickyPendingCodingPlan(userID, goal, "plan", []*v2.TaskItem{
		{Index: 1, Title: "实现蛇的移动和吃食物", Description: "控制台逻辑"},
		{Index: 2, Title: "撞墙与自撞结束", Description: "判定失败"},
		{Index: 3, Title: "构建、逻辑测试与运行说明", Description: "cmake 构建并写 README"},
	})
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		m.PlanDialogueBrief = goal
	})
	if _, stop := h.prepareCodingPlanDialogue(userID, "这个方案怎么样", "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected the question to stay in planning")
	}
	if _, stop := h.prepareCodingPlanDialogue(userID, "需要有图形界面", "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected the addition to revise the plan")
	}
	next, stop := h.prepareCodingPlanDialogue(userID, "直接做", "D:/repo", true, nil, nil)
	if stop != nil {
		t.Fatalf("direct execute should leave planning, got %s", stop.Text)
	}
	if !strings.Contains(next, goal) || !strings.Contains(next, "需要有图形界面") || strings.Contains(next, "这个方案怎么样") {
		t.Fatalf("direct execute dropped or polluted the revision: %q", next)
	}
}

func TestPlanReviewInFlightDoesNotStartCoding(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划再开发一个带表单和测试的登录页"
	h.planReviewLive.Store(userID, int64(1))
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		m.PlanDialogueBrief = goal
		m.PlanReviewRunning = true
	})
	next, stop := h.prepareCodingPlanDialogue(userID, "需要有图形界面", "D:/repo", true, nil, nil)
	if stop == nil || !strings.Contains(stop.Text, "后台评审") {
		t.Fatalf("an in-flight review must not fall through to coding: %#v", stop)
	}
	if next != "需要有图形界面" {
		t.Fatalf("next = %q", next)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("the waiting sentence must not publish a plan")
	}
	if !h.getStickyCodingWorkbenchMemory(userID).PlanReviewRunning {
		t.Fatal("waiting must leave the in-flight review running")
	}
}

func TestSupersededReviewDoesNotStoreThePlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 2
	})
	tasks := []*v2.TaskItem{
		{Title: "确认改动范围", Description: "看现状"},
		{Title: "按目标实现", Description: "做登录"},
	}
	resp := h.commitCodingPlanDraft(userID, "先规划再开发登录页", tasks, "note", 1, nil)
	if resp == nil || !strings.Contains(resp.Text, "取代") {
		t.Fatalf("superseded review reply = %#v", resp)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("a replaced review must not publish a plan")
	}
}

func TestSystemContinuationDoesNotStartWhileAPlanIsWaiting(t *testing.T) {
	open := &IMMessageHandler{}
	if _, stop := open.prepareCodingPlanDialogue(stickyTestUserID(t), "[系统续接][execute] 按步骤执行目标：登录页", "D:/repo", true, nil, nil); stop != nil {
		t.Fatalf("a continuation without a plan must proceed: %#v", stop)
	}
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	tasks := []*v2.TaskItem{
		{Title: "确认改动范围", Description: "看登录页现状"},
		{Title: "按目标实现", Description: "做登录页"},
	}
	if resp := h.commitCodingPlanDraft(userID, "登录页", tasks, "note", 0, nil); resp == nil {
		t.Fatal("plan was not stored")
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
	})
	for _, text := range []string{
		"[系统续接][execute] 按步骤执行目标：登录页",
		"[系统续轮] 继续完成同一任务。",
	} {
		_, stop := h.prepareCodingPlanDialogue(userID, text, "D:/repo", true, nil, nil)
		if stop == nil || !strings.Contains(stop.Text, "不改文件") {
			t.Fatalf("continuation %q was allowed to code: %#v", text, stop)
		}
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != "登录页" {
		t.Fatalf("continuation cleared the plan: %#v", pending)
	}
	_, stop := h.prepareCodingPlanDialogue(userID, "[系统续接][execute] 按步骤执行目标：登录页", "D:/repo", true, nil, nil)
	if stop == nil {
		t.Fatal("a waiting plan still let the continuation through")
	}

	app, store, engine := newGoalBarApp(t)
	goalUser := stickyTestUserID(t)
	t.Cleanup(func() { engine.CancelPending(goalUser) })
	if _, err := store.Set(goalUser, "做登录页"); err != nil {
		t.Fatal(err)
	}
	handler := app.imHandler
	if resp := handler.commitCodingPlanDraft(goalUser, "登录页", tasks, "note", 0, nil); resp == nil {
		t.Fatal("goal plan was not stored")
	}
	handler.maybeScheduleGoalContinuation(goalUser, &IMAgentResponse{Text: "方案已写好"}, "desktop")
	if goalBarPending(engine, goalUser) {
		t.Fatal("an unconfirmed plan scheduled another coding turn")
	}
	handler.exitCodingPlanDialogue(goalUser, true)
	handler.maybeScheduleGoalContinuation(goalUser, &IMAgentResponse{Text: "开始做"}, "desktop")
	if !goalBarPending(engine, goalUser) {
		t.Fatal("a confirmed plan should still schedule the goal")
	}
}

func TestRejectAndApproveInvalidateTheInFlightReview(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	tasks := []*v2.TaskItem{
		{Title: "确认改动范围", Description: "看登录页现状"},
		{Title: "按目标实现", Description: "做登录页"},
	}
	if resp := h.commitCodingPlanDraft(userID, "登录页", tasks, "note", 0, nil); resp == nil {
		t.Fatal("plan was not stored")
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 5
		m.PlanReviewRunning = true
		m.PlanDialogue = true
	})
	h.planReviewLive.Store(userID, int64(5))

	rejected := h.handleCodingPlanSlash(userID, "D:/repo", "/plan reject")
	if rejected == nil || !strings.Contains(rejected.Text, "拒绝") {
		t.Fatalf("reject reply = %#v", rejected)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("reject left the plan in place")
	}
	late := h.commitCodingPlanDraft(userID, "登录页", tasks, "late", 5, nil)
	if late == nil || !strings.Contains(late.Text, "取代") {
		t.Fatalf("rejected review was still current: %#v", late)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("a rejected review published the plan again")
	}
	if h.codingPlanReviewIsLive(userID) {
		t.Fatal("reject left the review live")
	}

	if resp := h.commitCodingPlanDraft(userID, "登录页", tasks, "note", 0, nil); resp == nil {
		t.Fatal("second plan was not stored")
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 8
		m.PlanReviewRunning = true
	})
	h.planReviewLive.Store(userID, int64(8))
	if _, ok := h.promotePendingToApprovedCodingPlan(userID); !ok {
		t.Fatal("approve did not take the plan")
	}
	late = h.commitCodingPlanDraft(userID, "登录页", tasks, "late", 8, nil)
	if late == nil || !strings.Contains(late.Text, "取代") {
		t.Fatalf("approved review was still current: %#v", late)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("an approved review opened a new pending plan")
	}
	if strings.TrimSpace(h.getStickyCodingWorkbenchMemory(userID).ApprovedPlanJSON) == "" {
		t.Fatal("approve dropped the accepted plan")
	}
}

func TestBackgroundPlanReviewReachesTheChat(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	h := &IMMessageHandler{app: app, manager: manager}
	userID := stickyTestUserID(t)
	tasks := []*v2.TaskItem{
		{Title: "确认改动范围", Description: "看登录页现状"},
		{Title: "按目标实现", Description: "做登录页"},
	}
	if resp := h.commitCodingPlanDraft(userID, "登录页", tasks, "方案正文，确认前不会改文件。", 0, nil); resp == nil {
		t.Fatal("plan was not stored")
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 3
		m.PlanReviewRunning = true
	})
	h.planReviewLive.Store(userID, int64(3))
	session := manager.CreateAIBackgroundSession("规划评审", "D:/repo", nil)
	if session == nil {
		t.Fatal("background session was not created")
	}
	var gotUser, gotText string
	prev := emitCodingPlanReviewChat
	emitCodingPlanReviewChat = func(_ *IMMessageHandler, id, text string) {
		gotUser, gotText = id, text
	}
	defer func() { emitCodingPlanReviewChat = prev }()

	h.publishCodingPlanReview(userID, session.ID, 3, &IMAgentResponse{Text: "方案正文，确认前不会改文件。"})
	if gotUser != userID || !strings.Contains(gotText, "不会改文件") {
		t.Fatalf("chat missed the plan: user=%q text=%q", gotUser, gotText)
	}
	if h.codingPlanReviewIsLive(userID) || h.getStickyCodingWorkbenchMemory(userID).PlanReviewRunning {
		t.Fatal("a published review stayed in flight")
	}
	gotUser, gotText = "", ""
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 4
	})
	h.planReviewLive.Store(userID, int64(4))
	h.publishCodingPlanReview(userID, session.ID, 3, &IMAgentResponse{Text: "旧方案"})
	if gotText != "" {
		t.Fatalf("a cancelled review reached the chat: %q", gotText)
	}
	if !h.codingPlanReviewIsLive(userID) {
		t.Fatal("the cancelled publish removed the newer review")
	}
}

func TestSupersededReviewDoesNotReplaceTheDialogue(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	tasks := []*v2.TaskItem{
		{Title: "确认改动范围", Description: "看设置页现状"},
		{Title: "按目标实现", Description: "做设置页"},
	}
	if resp := h.commitCodingPlanDraft(userID, "设置页", tasks, "note", 0, nil); resp == nil {
		t.Fatal("newer plan was not stored")
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 2
		m.PlanDialogue = true
		m.PlanDialogueBrief = "设置页"
	})
	resp := h.runCodingPlanDialogueTurn(userID, "先规划做一个贪吃蛇", "", 1, nil, nil)
	if resp == nil || !strings.Contains(resp.Text, "取代") {
		t.Fatalf("old review reply = %#v", resp)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != "设置页" {
		t.Fatalf("old review replaced the newer plan: %#v", pending)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanDialogueBrief != "设置页" || !mem.PlanDialogue {
		t.Fatalf("old review rewrote the dialogue: %#v", mem.PlanDialogueBrief)
	}
	resp = h.reviseStickyPendingCodingPlan(userID, "需要有图形界面", "", 1, nil, nil)
	if resp == nil || !strings.Contains(resp.Text, "取代") {
		t.Fatalf("old revision reply = %#v", resp)
	}
	pending, ok = h.loadStickyPendingCodingPlan(userID)
	if !ok || strings.Contains(pending.Markdown, "图形界面") {
		t.Fatalf("old revision edited the newer plan: %#v", pending.Markdown)
	}
	if notes := h.getStickyCodingWorkbenchMemory(userID).PlanDialogueNotes; notes != "" {
		t.Fatalf("old revision recorded a note: %q", notes)
	}
}

func TestStalePlanReviewFlagDoesNotBlock(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewRunning = true
	})
	text := "先规划再开发一个带表单和测试的登录页"
	_, stop := h.prepareCodingPlanDialogue(userID, text, "D:/repo", true, nil, nil)
	if stop == nil || !strings.Contains(stop.Text, "不会改文件") {
		t.Fatalf("a review flag left by a restart should not block a new plan: %#v", stop)
	}
	if h.getStickyCodingWorkbenchMemory(userID).PlanReviewRunning {
		t.Fatal("a review with no live task should be cleared")
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); !ok {
		t.Fatal("the new request should still draft a plan")
	}
}

func TestSelfReviewInsertsBeforeNamedStep(t *testing.T) {
	tasks := []*v2.TaskItem{{Title: "实现移动"}, {Title: "构建并验证"}}
	okReview, issues, parsed := parseCodingPlanSelfReview(`{"ok":false,"issues":[{"op":"insert","target":"构建并验证","change":"图形界面"}]}`)
	if !parsed || okReview || len(issues) != 1 {
		t.Fatalf("review parse ok=%v issues=%d parsed=%v", okReview, len(issues), parsed)
	}
	next, _, applied := applyCodingPlanReviewIssue(tasks, issues[0])
	if !applied || len(next) != 3 || next[1].Title != "图形界面" || next[2].Title != "构建并验证" {
		t.Fatalf("self-review did not insert before the named step: %#v", next)
	}
	if _, _, parsed := parseCodingPlanSelfReview(`{"ok":true}`); !parsed {
		t.Fatal("a clean review should parse")
	}
}

func TestSkipFlagDoesNotReopenPlanning(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	text := "先规划再开发一个带表单和测试的登录页"
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.SkipNextPlan = true
		m.SkipPlanText = text
	})
	next, stop := h.prepareCodingPlanDialogue(userID, text, "D:/repo", true, nil, nil)
	if stop != nil || next != text {
		t.Fatalf("a skip already in force reopened planning: stop=%v next=%q", stop != nil, next)
	}
	if h.getStickyCodingWorkbenchMemory(userID).PlanDialogue {
		t.Fatal("skip should not open a new planning conversation")
	}
}

func TestPrepareCodingPlanDialogueDirectAndCancel(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划再开发一个带表单和测试的登录页"
	if _, stop := h.prepareCodingPlanDialogue(userID, goal, "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected a draft")
	}
	next, stop := h.prepareCodingPlanDialogue(userID, "直接做", "D:/repo", true, nil, nil)
	if stop != nil {
		t.Fatalf("direct execute should continue the coding turn, got %s", stop.Text)
	}
	if next != goal {
		t.Fatalf("direct execute goal = %q", next)
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanDialogue || !mem.SkipNextPlan {
		t.Fatalf("direct execute state dialogue=%v skip=%v", mem.PlanDialogue, mem.SkipNextPlan)
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("direct execute should drop the pending plan")
	}

	if _, stop := h.prepareCodingPlanDialogue(userID, "先出方案再实现设置页", "D:/repo", true, nil, nil); stop == nil {
		t.Fatal("expected to re-enter planning")
	}
	_, stop = h.prepareCodingPlanDialogue(userID, "取消规划", "D:/repo", true, nil, nil)
	if stop == nil || !strings.Contains(stop.Text, "没有改文件") {
		t.Fatalf("cancel reply = %#v", stop)
	}
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.PlanDialogue {
		t.Fatal("cancel should leave planning")
	}
	if _, ok := h.loadStickyPendingCodingPlan(userID); ok {
		t.Fatal("cancel should clear the pending plan")
	}
}

func TestPrepareCodingPlanDialogueIgnoresOrdinaryRequests(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	for _, text := range []string{"fix the button label", "改为图形界面版", "规划模式是什么"} {
		next, stop := h.prepareCodingPlanDialogue(userID, text, "D:/repo", true, nil, nil)
		if stop != nil || next != text {
			t.Fatalf("ordinary request entered planning: %q stop=%v next=%q", text, stop != nil, next)
		}
	}
	if h.getStickyCodingWorkbenchMemory(userID).PlanDialogue {
		t.Fatal("ordinary requests must not open planning")
	}
}

func TestPatchCodingPlanAddsAndDeletesSteps(t *testing.T) {
	base := []*v2.TaskItem{
		{Index: 1, Title: "实现蛇的移动和吃食物", Description: "控制台逻辑"},
		{Index: 2, Title: "撞墙与自撞结束", Description: "判定失败"},
		{Index: 3, Title: "构建、逻辑测试与运行说明", Description: "cmake 构建并写 README"},
	}
	added, summary := patchCodingPlan(base, "需要有图形界面")
	if len(added) != 4 || !strings.Contains(summary, "图形界面") {
		t.Fatalf("add = %d %q", len(added), summary)
	}
	if added[2].Title != "图形界面" || strings.Contains(added[2].Description, "确认前不写文件") {
		t.Fatalf("graphical step should sit before verify, got %#v", added[2])
	}
	if added[0].Title != base[0].Title || added[1].Title != base[1].Title || added[3].Title != base[2].Title {
		t.Fatalf("add rewrote existing steps: %#v", added)
	}
	refined, summary := patchCodingPlan(added, "图形界面用 SDL")
	if len(refined) != 4 || !strings.Contains(refined[2].Description, "SDL") {
		t.Fatalf("refine should update the graphical step, steps=%d summary=%q desc=%q", len(refined), summary, refined[2].Description)
	}
	removed, summary := patchCodingPlan(refined, "去掉撞墙")
	if len(removed) != 3 || strings.Contains(summary, "没能") {
		t.Fatalf("delete = %d %q", len(removed), summary)
	}
	for _, task := range removed {
		if strings.Contains(task.Title, "撞墙") {
			t.Fatalf("collision step still present: %#v", removed)
		}
	}
}

func TestEnglishBuildRequestDoesNotRewriteTheVerifyStep(t *testing.T) {
	tasks := hostCodingPlanScaffold("plan first, then build a login form")
	added, _ := patchCodingPlan(tasks, "build the installer")
	if len(added) != 4 || added[3].Title != "Build and verify" {
		t.Fatalf("installer request rewrote the plan: %#v", added)
	}
	if strings.Contains(added[3].Description, "installer") {
		t.Fatalf("installer was written into verify: %q", added[3].Description)
	}
	if added[2].Title != "build installer" {
		t.Fatalf("new step = %#v", added[2])
	}
	refined, _ := patchCodingPlan(added, "verify should cover form errors")
	if len(refined) != 4 || !strings.Contains(refined[3].Description, "form errors") {
		t.Fatalf("verify follow-up did not update the closing step: %#v", refined)
	}
	if strings.Contains(refined[2].Description, "form errors") {
		t.Fatalf("verify follow-up rewrote the installer step: %q", refined[2].Description)
	}
}

func TestEnglishGoalDraftsEnglishSteps(t *testing.T) {
	goal := "plan first, then build a login form"
	tasks := hostCodingPlanScaffold(goal)
	if len(tasks) != 3 || tasks[0].Title != "Confirm the scope" || tasks[1].Title != "Implement the goal" || tasks[2].Title != "Build and verify" {
		t.Fatalf("english scaffold = %#v", tasks)
	}
	if codingPlanInsertAt(tasks) != 2 {
		t.Fatalf("insert slot = %d, want the place before Build and verify", codingPlanInsertAt(tasks))
	}
	h := &IMMessageHandler{}
	draft, reply, asked := h.draftCodingPlanDialogue(stickyTestUserID(t), goal, goal, "")
	if asked || len(draft) != 3 || draft[2].Title != "Build and verify" || !strings.Contains(reply, "draft") {
		t.Fatalf("english draft = asked %v reply %q tasks %#v", asked, reply, draft)
	}
	resp := h.runCodingPlanDialogueTurn(stickyTestUserID(t), "plan first", "", 0, nil, nil)
	if resp == nil || !strings.Contains(resp.Text, "what to build") {
		t.Fatalf("thin english request = %#v", resp)
	}
}

func TestFailedReviewTellsTheChat(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	h := &IMMessageHandler{app: app, manager: manager}
	userID := stickyTestUserID(t)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 3
		m.PlanReviewRunning = true
		m.LastSummary = "已读完登录页"
	})
	h.planReviewLive.Store(userID, int64(3))
	session := manager.CreateAIBackgroundSession("规划评审", "D:/repo", nil)
	if session == nil {
		t.Fatal("background session was not created")
	}
	var got string
	prev := emitCodingPlanReviewChat
	emitCodingPlanReviewChat = func(_ *IMMessageHandler, id, text string) {
		if id == userID {
			got = text
		}
	}
	defer func() { emitCodingPlanReviewChat = prev }()

	h.failCodingPlanReview(userID, session.ID, 3, "规划评审中断了，方案没有定下来。")
	if !strings.Contains(got, "中断") {
		t.Fatalf("chat missed the failure: %q", got)
	}
	if h.codingPlanReviewIsLive(userID) || h.getStickyCodingWorkbenchMemory(userID).PlanReviewRunning {
		t.Fatal("a failed review stayed in flight")
	}
	if h.getStickyCodingWorkbenchMemory(userID).LastSummary != "已读完登录页" {
		t.Fatal("failure wiped the session summary")
	}
	got = ""
	h.planReviewLive.Store(userID, int64(4))
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanReviewSeq = 4
		m.PlanReviewRunning = true
	})
	h.failCodingPlanReview(userID, session.ID, 3, "旧评审")
	if got != "" {
		t.Fatalf("an old failure reached the chat: %q", got)
	}
	if !h.codingPlanReviewIsLive(userID) {
		t.Fatal("the old failure removed the newer review")
	}
}

func TestPlanInsertUsesTheStepTitleNotTheGoalText(t *testing.T) {
	tasks := hostCodingPlanScaffold("先规划再开发一个带表单和测试的登录页")
	if at := codingPlanInsertAt(tasks); at != 2 {
		t.Fatalf("insert slot = %d, want the place before 构建并验证", at)
	}
	added, _ := patchCodingPlan(tasks, "需要有图形界面")
	if len(added) != 4 || added[1].Title != "按目标实现" || added[2].Title != "图形界面" || added[3].Title != "构建并验证" {
		t.Fatalf("graphical step rewrote the draft: %#v", added)
	}
}

func TestPendingPlanQuestionDoesNotAddAStep(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.storeStickyPendingCodingPlan(userID, "先规划一下c++版贪吃蛇，确定后再开工", "plan", []*v2.TaskItem{
		{Index: 1, Title: "实现蛇的移动和吃食物", Description: "控制台逻辑"},
		{Index: 2, Title: "撞墙与自撞结束", Description: "判定失败"},
		{Index: 3, Title: "构建、逻辑测试与运行说明", Description: "cmake 构建并写 README"},
	})
	_, stop := h.prepareCodingPlanDialogue(userID, "这个方案怎么样", "D:/repo", true, nil, nil)
	if stop == nil || !strings.Contains(stop.Text, "没有改方案") {
		t.Fatalf("a question should not be written in as a step: %#v", stop)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || len(pending.Tasks) != 3 || strings.Contains(pending.Markdown, "这个方案怎么样") {
		t.Fatalf("question changed the plan:\n%s", pending.Markdown)
	}
}

func TestPatchCodingPlanKeepsUnmatchedRemovalAndFullPlan(t *testing.T) {
	base := []*v2.TaskItem{
		{Index: 1, Title: "实现蛇的移动和吃食物", Description: "控制台逻辑"},
		{Index: 2, Title: "撞墙与自撞结束", Description: "判定失败"},
		{Index: 3, Title: "构建、逻辑测试与运行说明", Description: "cmake 构建并写 README"},
	}
	kept, summary := patchCodingPlan(base, "去掉计分")
	if len(kept) != 3 || !strings.Contains(summary, "没有删") {
		t.Fatalf("unmatched delete = %d %q", len(kept), summary)
	}
	if codingPlanFeedbackFocus("申请登录接口") != "申请登录接口" {
		t.Fatalf("focus stripped a character inside 申请: %q", codingPlanFeedbackFocus("申请登录接口"))
	}
	if got := codingPlanFeedbackFocus("把图形界面改成SDL"); !strings.Contains(got, "图形界面") || strings.Contains(got, "把") {
		t.Fatalf("focus = %q", got)
	}
	refined, _ := patchCodingPlan(append(cloneCodingWorkbenchTasks(base), &v2.TaskItem{Title: "图形界面", Description: "窗口"}), "试一下图形界面")
	if len(refined) != 4 || refined[3].Title != "图形界面" || !strings.Contains(refined[3].Description, "试一下图形界面") {
		t.Fatalf("mention of an existing step should update it: %#v", refined)
	}
	full := []*v2.TaskItem{
		{Title: "一步"}, {Title: "二步"}, {Title: "三步"}, {Title: "四步"}, {Title: "五步"},
		{Title: "构建并验证"},
	}
	folded, summary := patchCodingPlan(full, "需要有图形界面")
	if len(folded) != 6 || !strings.Contains(folded[4].Description, "需要有图形界面") || !strings.Contains(summary, "五步") {
		t.Fatalf("full plan should keep the change on the last implementation step: %q %#v", summary, folded)
	}
}

func TestModelDeleteDoesNotApplyToAnAdd(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Title: "实现蛇的移动和吃食物"},
		{Title: "撞墙与自撞结束"},
		{Title: "构建、逻辑测试与运行说明"},
	}
	if codingPlanModelOpAgrees(tasks, "delete", 1, nil, "需要有图形界面") {
		t.Fatal("adding a GUI must not accept a model delete")
	}
	if !codingPlanModelOpAgrees(tasks, "insert", 0, &v2.TaskItem{Title: "图形界面", Description: "SDL 窗口"}, "需要有图形界面") {
		t.Fatal("an insert about the requested GUI should be accepted")
	}
	if !codingPlanHostPatchConfident(tasks, "需要有图形界面") {
		t.Fatal("a short add should be patched locally")
	}
	withGUI := append(cloneCodingWorkbenchTasks(tasks), &v2.TaskItem{Title: "图形界面"})
	if codingPlanModelOpAgrees(withGUI, "insert", 0, &v2.TaskItem{Title: "图形界面"}, "图形界面用 SDL") {
		t.Fatal("a sentence about an existing step must not insert a second copy")
	}
	renamed, _, ok := applyCodingPlanOp(tasks, "update", 1, &v2.TaskItem{Title: "按已理解需求实现并验证", Description: "改成图形界面版本"})
	if !ok || renamed[0].Title != "实现蛇的移动和吃食物" {
		t.Fatalf("an update must not replace the step title: %#v", renamed)
	}
}

func TestCodingPlanRevisionDropsUnrelatedRewrite(t *testing.T) {
	current := []*v2.TaskItem{
		{Title: "实现蛇的移动和吃食物"},
		{Title: "撞墙与自撞结束"},
		{Title: "构建、逻辑测试与运行说明"},
	}
	rewrite := []*v2.TaskItem{
		{Title: "阅读现有实现并确认改动范围", Description: "需要有图形界面"},
		{Title: "按已理解需求实现并验证", Description: "改成图形界面版本"},
	}
	if !codingPlanRevisionDropsSteps(current, rewrite, "需要有图形界面") {
		t.Fatal("a two-step rewrite that mentions the GUI must not replace the current plan")
	}
	kept := append(cloneCodingWorkbenchTasks(current), &v2.TaskItem{Title: "图形界面", Description: "SDL 窗口"})
	if codingPlanRevisionDropsSteps(current, kept, "需要有图形界面") {
		t.Fatal("a plan that keeps every current title is a valid revision")
	}
}

func TestParseCodingPlanPatch(t *testing.T) {
	op, index, patch, reply, ok := parseCodingPlanPatch(`{"action":"patch","op":"insert","index":0,"title":"图形界面","description":"SDL 窗口","reply":"加上窗口"}`)
	if !ok || op != "insert" || index != 0 || patch == nil || patch.Title != "图形界面" || reply != "加上窗口" {
		t.Fatalf("patch parse op=%s index=%d patch=%#v reply=%q ok=%v", op, index, patch, reply, ok)
	}
	if _, _, _, _, ok := parseCodingPlanPatch(`{"action":"propose","reply":"整份替换","steps":[]}`); ok {
		t.Fatal("a full proposal is not a patch")
	}
}

func TestParseCodingPlanDialogue(t *testing.T) {
	tasks, reply, asked, ok := parseCodingPlanDialogue(`{"action":"ask","reply":"登录要接哪家账号？","steps":[]}`)
	if !ok || !asked || reply == "" || len(tasks) != 0 {
		t.Fatalf("ask parse = tasks %d reply %q asked %v ok %v", len(tasks), reply, asked, ok)
	}
	tasks, reply, asked, ok = parseCodingPlanDialogue("```json\n" + `{"action":"propose","reply":"按表单登录来规划","steps":[{"title":"读现状","description":"看登录入口"},{"title":"实现","description":"加表单"}]}` + "\n```")
	if !ok || asked || len(tasks) != 2 || reply == "" {
		t.Fatalf("propose parse = tasks %d reply %q asked %v ok %v", len(tasks), reply, asked, ok)
	}
}
