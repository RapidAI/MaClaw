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
	if !strings.Contains(pending.Markdown, "表单提交失败") || !strings.Contains(pending.Markdown, "阅读现有实现并确认改动范围") {
		t.Fatalf("revision should keep the current steps and add the change:\n%s", pending.Markdown)
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
