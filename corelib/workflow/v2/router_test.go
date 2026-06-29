package v2

import (
	"testing"
)

func setupTestRouter() *WorkflowRouter {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	return NewWorkflowRouter(machine, templates, nil)
}

func TestRoute_CodingTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "在d:\\game2 下开发贪吃蛇 C++", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.WorkflowType != "coding" {
		t.Fatalf("type = %q, want coding", result.WorkflowType)
	}
	if result.ProjectPath != "d:\\game2" {
		t.Fatalf("projectPath = %q, want d:\\game2", result.ProjectPath)
	}
}

func TestRoute_NonCodingTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "帮我查一下杭州天气", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop", result.Target)
	}
}

func TestRoute_NilDependenciesFallBackToAgentLoop(t *testing.T) {
	var nilRouter *WorkflowRouter
	if result := nilRouter.Route("user1", "build a backend service", nil); result.Target != RouteToAgentLoop {
		t.Fatalf("nil router target = %q, want agent_loop", result.Target)
	}

	r := &WorkflowRouter{}
	if result := r.Route("user1", "build a backend service", nil); result.Target != RouteToAgentLoop {
		t.Fatalf("missing template registry target = %q, want agent_loop", result.Target)
	}

	r = &WorkflowRouter{templates: NewTemplateRegistry()}
	if result := r.Route("user1", "build a backend service", nil); result.Target != RouteToAgentLoop {
		t.Fatalf("missing state machine target = %q, want agent_loop", result.Target)
	}
}

func TestRoute_SkipSignal(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "直接做一个贪吃蛇游戏", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (skip signal)", result.Target)
	}
}

func TestRoute_BugFix(t *testing.T) {
	r := setupTestRouter()
	// A plain bug fix should not start a coding workflow; it is handled by the
	// normal agent loop with full tools.
	result := r.Route("user1", "修复加载卡住的bug", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (plain bug fix)", result.Target)
	}
}

func TestRoute_BugFixWithCreation(t *testing.T) {
	r := setupTestRouter()
	// "开发一个bug追踪系统" has both bug-fix and creation keywords
	result := r.Route("user1", "开发一个bug追踪系统", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (creation overrides bug-fix)", result.Target)
	}
}

func TestRoute_LLMConfirmationCanRejectStructuredTemplateMatch(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	router := NewWorkflowRouter(machine, templates, func(text, workflowType string) bool {
		return false
	})

	result := router.Route("user1", "build backend service with APIs and database migrations", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop when LLM rejects candidate", result.Target)
	}
}

func TestRoute_CodingComplexityNoneFallsBackToAgentLoop(t *testing.T) {
	// After the user-choice refactor, ComplexityFunc is no longer consumed
	// by the router. Coding tasks always route to RouteToWorkflow; the GUI
	// layer asks the user to choose complexity. Setting ComplexityFunc has
	// no effect on routing.
	r := setupTestRouter()
	r.SetComplexityFunc(func(text string) TaskComplexity {
		return ComplexityNone
	})

	result := r.Route("user1", "build backend service with APIs and database migrations", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (complexity is now user-chosen in GUI)", result.Target)
	}
}

func TestRoute_CodingComplexitySimpleGoesDirectCoding(t *testing.T) {
	// After the user-choice refactor, ComplexityFunc is no longer consumed
	// by the router. All coding tasks go to RouteToWorkflow; the GUI layer
	// presents a choice panel for the user to select simple/complex/skip.
	r := setupTestRouter()
	r.SetComplexityFunc(func(text string) TaskComplexity {
		return ComplexitySimple
	})

	result := r.Route("user1", "d:\\service build backend service with APIs and database migrations", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (complexity is now user-chosen in GUI)", result.Target)
	}
	if result.WorkflowType != "coding" {
		t.Fatalf("workflowType = %q, want coding", result.WorkflowType)
	}
}

func TestRoute_PPTTask(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "帮我设计一个产品介绍PPT", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.WorkflowType != "presentation_design" {
		t.Fatalf("type = %q, want presentation_design", result.WorkflowType)
	}
}

func TestRouteWithHint_PrefersConcreteWorkflowHintOverConflictingBM25Match(t *testing.T) {
	r := setupTestRouter()

	result := r.RouteWithHint("user1", "高考志愿填报参考", nil, "presentation_design")
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.WorkflowType != "presentation_design" {
		t.Fatalf("type = %q, want presentation_design when concrete hint is present", result.WorkflowType)
	}
}

func TestRoute_ActiveWorkflow_Confirm(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	// Set classifier so "确认" is recognized as confirm
	machine.SetConfirmClassifier(func(phaseContext, userText string) string {
		return ClassifyConfirmIntentKeyword(userText)
	})
	router := NewWorkflowRouter(machine, templates, nil)

	// Start a workflow and record output
	machine.Create("user1", "coding", "d:\\project", "build app")
	machine.RecordOutput("user1", "# Requirements")

	// User confirms
	result := router.Route("user1", "确认", nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow", result.Target)
	}
	if result.HandleResult == nil || result.HandleResult.Action != ActionRunPhase {
		t.Fatalf("action = %v", result.HandleResult)
	}
}

func TestRoute_ActiveWorkflow_UnrelatedMessage(t *testing.T) {
	store := NewMemoryStore()
	templates := NewTemplateRegistry()
	RegisterBuiltinTemplates(templates)
	machine := NewStateMachine(store, templates)
	router := NewWorkflowRouter(machine, templates, nil)

	machine.Create("user1", "coding", "d:\\project", "build app")
	machine.RecordOutput("user1", "# Requirements")

	// Unrelated short message
	result := router.Route("user1", "嗯", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (unrelated)", result.Target)
	}
}

func TestRoute_AttachmentWithShortText(t *testing.T) {
	r := setupTestRouter()
	result := r.Route("user1", "看看这个", []Attachment{{Type: "image", Name: "screenshot.png"}})
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (attachment)", result.Target)
	}
}

func TestExtractProjectPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"在d:\\game2 下开发贪吃蛇", "d:\\game2"},
		{"在d:\\workprj\\snake 目录开发", "d:\\workprj\\snake"},
		{"在/home/user/project 下写代码", "/home/user/project"},
		{"开发一个游戏", ""},
		{"d:\\snake55 开发贪吃蛇", "d:\\snake55"},
	}
	for _, tc := range tests {
		got := ExtractProjectPathFromText(tc.input)
		if got != tc.want {
			t.Errorf("ExtractProjectPathFromText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRoute_PatentApplication_NotConfusedWithPatentAnalysis(t *testing.T) {
	r := setupTestRouter()

	// "写专利申请书" should route to patent_application
	cases := []struct {
		input    string
		wantType string
	}{
		{"帮我根据交底书写一份发明专利申请", "patent_application"},
		{"撰写专利申请书，交底书在D盘", "patent_application"},
		{"分析这个专利的侵权风险", "patent_analysis"},
		{"帮我做个专利布局分析", "patent_analysis"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != tc.wantType {
			t.Errorf("Route(%q): type = %q, want %q", tc.input, result.WorkflowType, tc.wantType)
		}
	}
}

func TestRoute_USPatentApplication(t *testing.T) {
	r := setupTestRouter()

	// BM25 text routing for US patent: only strong signals ("USPTO", "美国专利")
	// route reliably. Ambiguous inputs (like "帮我申请美国专利" where "专利" also
	// matches the CN patent template) may need IUM LLM classification for
	// disambiguation — that's by design (BM25 is the fallback, not primary).
	cases := []struct {
		input    string
		wantType string
	}{
		{"draft USPTO patent claims and specification", "us_patent_application"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != tc.wantType {
			t.Errorf("Route(%q): type = %q, want %q", tc.input, result.WorkflowType, tc.wantType)
		}
	}
}

func TestRoute_GaokaoApplication(t *testing.T) {
	r := setupTestRouter()

	cases := []struct {
		input    string
		wantType string
	}{
		{"帮我做高考志愿填报参考，山东物化生位次32850", "gaokao_application"},
		{"这个位次能报哪些中外合办学校和专业", "gaokao_application"},
		{"厦门大学马来西亚分校这类境外校区志愿怎么填", "gaokao_application"},
		{"厦门大学马来西亚分校这个位次能报吗", "gaokao_application"},
		{"河北工业大学芬兰校区能不能作为保底志愿", "gaokao_application"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != tc.wantType {
			t.Errorf("Route(%q): type = %q, want %q", tc.input, result.WorkflowType, tc.wantType)
		}
	}
}

func TestRoute_GaokaoApplicationCampusIntroDoesNotStartWorkflow(t *testing.T) {
	r := setupTestRouter()

	for _, input := range []string{
		"介绍一下厦门大学马来西亚分校",
		"河北工业大学芬兰校区在哪里",
		"这个基金项目能不能保底",
	} {
		result := r.Route("user1", input, nil)
		if result.Target == RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want agent_loop for non-application campus query", input, result.Target)
		}
	}
}

func TestRoute_StrongActionWithArbitraryTarget(t *testing.T) {
	r := setupTestRouter()

	// Strong action verbs ("开发"/"实现"/"搭建"/"构建"/"develop"/"implement")
	// should trigger workflow even when the target is not in the object signals list.
	cases := []struct {
		input    string
		wantType string
	}{
		{"开发一个hello world", "coding"},
		{"开发一个计算器", "coding"},
		{"实现一个flappy bird", "coding"},
		{"构建一个微服务框架", "coding"},
		{"develop a tic-tac-toe game", "coding"},
		{"implement a chat server in go", "coding"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != tc.wantType {
			t.Errorf("Route(%q): type = %q, want %q", tc.input, result.WorkflowType, tc.wantType)
		}
	}
}

func TestRoute_StrongActionTooShort_NoWorkflow(t *testing.T) {
	r := setupTestRouter()
	// Very short messages with strong action ("开发" = 2 runes, total < 6 runes)
	// should NOT trigger workflow — likely incomplete input.
	result := r.Route("user1", "开发", nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("Route(\"开发\"): target = %q, want agent_loop (too short)", result.Target)
	}
}

func TestRoute_AmbiguousEnglishAction_NoWorkflow(t *testing.T) {
	r := setupTestRouter()
	// "build" and "create" are excluded from strong signals because they are
	// too ambiguous for short commands. These should NOT trigger workflow
	// unless they also contain an object signal word.
	cases := []string{
		"build the project",   // operational command, not "create new project"
		"create a folder",     // filesystem operation
		"create a new branch", // git operation
	}
	for _, input := range cases {
		result := r.Route("user1", input, nil)
		if result.Target == RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want agent_loop (ambiguous action)", input, result.Target)
		}
	}
}

func TestRoute_TraditionalChinese(t *testing.T) {
	r := setupTestRouter()

	// Traditional Chinese users should trigger workflow via strong action verbs.
	// Note: without UIC (test environment), non-coding templates may not match
	// precisely because BM25 template descriptions are in simplified Chinese.
	// The key assertion is that strong coding actions route to workflow.
	cases := []struct {
		input string
	}{
		{"開發一個計算機程式"},
		{"實現一個聊天機器人"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != "coding" {
			t.Errorf("Route(%q): type = %q, want coding", tc.input, result.WorkflowType)
		}
	}
}


func TestRoute_LongTechnicalText_NoWorkflow(t *testing.T) {
	r := setupTestRouter()

	// This is the exact message that triggered the paper_reproduction false positive.
	// Contains "生成" (in workflowActionSignals) and "代码" (in workflowObjectSignals)
	// but at 519 runes it exceeds maxWeakSignalTextLength (200), so weak signals
	// are suppressed.
	nginxMsg := `Hub.mypapers.top的nginx设置有问题？04 Gateway Timeout，60.4 秒。

找到根因了：

Hub（hub.mypapers.top）前方的 nginx 的 proxy_read_timeout 仍然是 60 秒
模型生成 250 行 HTML 的 tool call 需要 >60 秒（含 reasoning 思考时间）
60 秒后 nginx 断开连接返回 504
客户端收到部分 SSE 数据（finish_reason 为空）→ JSON 截断
根因确认：不是模型 output 上限问题，是 nginx proxy_read_timeout=60s 太短。

Hub 代码里的 600s timeout 已部署没用——nginx 在 Hub 前面，60 秒就断了。

修复：在 Hub 前方的 nginx 配置中加：

location /api/llm/ {
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    proxy_connect_timeout 30s;
    proxy_buffering off;
}`

	result := r.Route("user1", nginxMsg, nil)
	if result.Target != RouteToAgentLoop {
		t.Fatalf("target = %q, want agent_loop (long technical text with scattered keywords)", result.Target)
	}
}

func TestRoute_WeakSignalShortText_StillWorks(t *testing.T) {
	r := setupTestRouter()

	// Short messages (< 200 runes) with weak action+object should still trigger.
	cases := []struct {
		input    string
		wantType string
	}{
		{"帮我生成一份竞品分析报告", "competitive_analysis"},
		{"做一个完整的商业计划书", "business_plan"},
		{"写一份详细的研究报告", "research_report"},
		{"帮我设计一个管理系统", "coding"},
	}
	for _, tc := range cases {
		result := r.Route("user1", tc.input, nil)
		if result.Target != RouteToWorkflow {
			t.Errorf("Route(%q): target = %q, want workflow", tc.input, result.Target)
			continue
		}
		if result.WorkflowType != tc.wantType {
			t.Errorf("Route(%q): type = %q, want %q", tc.input, result.WorkflowType, tc.wantType)
		}
	}
}

func TestRoute_LongTextSuppression_ExplicitSignalStillWorks(t *testing.T) {
	r := setupTestRouter()

	// Long text (> 200 runes) with explicit workflow signals should still trigger.
	// explicitWorkflowObjectSignals and strongCodingAction bypass the weak signal guard.
	longWithExplicit := "我需要申请一个国自然基金项目，主要研究方向是大语言模型在代码生成领域的应用，" +
		"目前已经有了初步的实验结果和论文草稿，需要系统地整理成申请书格式，" +
		"包括研究背景、研究内容、研究方案、可行性分析、预期成果等章节，" +
		"预算大约在50万左右，计划执行周期3年"
	result := r.Route("user1", longWithExplicit, nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (explicit signal '国自然' in long text)", result.Target)
	}
}

func TestRoute_LongTextSuppression_StrongActionStillWorks(t *testing.T) {
	r := setupTestRouter()

	// Long text with strong coding action should still trigger via the
	// hasStrongCodingActionInText path (checked before weak signal guard).
	longWithStrongAction := "在d:\\workprj\\myproject 下开发一个完整的在线商城系统，" +
		"要求包含用户注册登录、商品浏览、购物车、订单管理、支付集成、物流跟踪、" +
		"评价系统、管理后台等完整功能模块，使用 React + Node.js 技术栈，" +
		"数据库用 PostgreSQL，缓存用 Redis，消息队列用 RabbitMQ"
	result := r.Route("user1", longWithStrongAction, nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (strong action '开发' bypasses length guard)", result.Target)
	}
}

func TestRoute_LongTextSuppression_MediumTextWithWeakSignals(t *testing.T) {
	r := setupTestRouter()

	// Medium text (100-200 runes) with weak action+object signals should
	// still trigger — the threshold only kicks in at 200+ runes.
	mediumText := "帮我做一个详细的竞品分析，覆盖头部三家的产品功能对比、定价策略、用户评价，输出一份可以给管理层看的分析报告"
	result := r.Route("user1", mediumText, nil)
	if result.Target != RouteToWorkflow {
		t.Fatalf("target = %q, want workflow (medium text still within threshold)", result.Target)
	}
}
