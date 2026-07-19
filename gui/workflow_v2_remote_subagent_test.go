package main

import (
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestExtractExperimentParamsPrefersPaperMetricOverReproductionMetric(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{ID: "paper_analysis", Output: "Paper reports final Accuracy: 92.5% on the benchmark."},
			{ID: "baseline_reproduction", Output: "Our reproduced Accuracy: 88.1% after three seeds."},
		},
	}

	params := extractExperimentParams(state)
	if params.BaselineMetricName != "Accuracy" && params.BaselineMetricName != "accuracy" {
		t.Fatalf("metric name = %q", params.BaselineMetricName)
	}
	if params.BaselineMetricValue != 92.5 {
		t.Fatalf("paper metric should remain target comparator, got %.4f", params.BaselineMetricValue)
	}
	if !params.MetricHigherBetter {
		t.Fatalf("accuracy should be higher-is-better")
	}
}

func TestExtractExperimentParamsFallsBackToReproductionMetricWhenPaperMissing(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{ID: "paper_analysis", Output: "The paper discusses the model but omits the final score."},
			{ID: "baseline_reproduction", Output: "Reproduction val_loss: 0.42"},
		},
	}

	params := extractExperimentParams(state)
	if params.BaselineMetricName != "val_loss" {
		t.Fatalf("metric name = %q", params.BaselineMetricName)
	}
	if params.BaselineMetricValue != 0.42 {
		t.Fatalf("fallback reproduction metric = %.4f", params.BaselineMetricValue)
	}
	if params.MetricHigherBetter {
		t.Fatalf("loss should be lower-is-better")
	}
}

func TestParseBaselineMetricPrefersLatestCandidateByTextPosition(t *testing.T) {
	output := "Early reproduction Accuracy: 88.1%. Later evaluation says val_loss: 0.42. Final report: Accuracy: 92.5%."
	name, value, higherBetter := parseBaselineMetric(output)
	if name != "Accuracy" {
		t.Fatalf("metric name = %q", name)
	}
	if value != 92.5 {
		t.Fatalf("metric value = %.4f", value)
	}
	if !higherBetter {
		t.Fatalf("accuracy should be higher-is-better")
	}
}

func TestParseBaselineMetricTakesLastMetricWithinSecondHalf(t *testing.T) {
	output := strings.Repeat("setup notes. ", 30) + "Validation Accuracy: 84.0%. Final Accuracy: 87.3%."
	name, value, _ := parseBaselineMetric(output)
	if name != "Accuracy" {
		t.Fatalf("metric name = %q", name)
	}
	if value != 87.3 {
		t.Fatalf("metric value = %.4f", value)
	}
}

func TestParseSSHHostStringHandlesCommonAndIPv6Forms(t *testing.T) {
	tests := []struct {
		input    string
		wantUser string
		wantHost string
		wantPort int
	}{
		{input: "example.com", wantUser: "root", wantHost: "example.com", wantPort: 22},
		{input: "alice@example.com:2200", wantUser: "alice", wantHost: "example.com", wantPort: 2200},
		{input: "ssh://bob@example.com:2222/home/project", wantUser: "bob", wantHost: "example.com", wantPort: 2222},
		{input: "2001:db8::10", wantUser: "root", wantHost: "2001:db8::10", wantPort: 22},
		{input: "carol@[2001:db8::10]:2022", wantUser: "carol", wantHost: "2001:db8::10", wantPort: 2022},
		{input: "[2001:db8::11]:2023", wantUser: "root", wantHost: "2001:db8::11", wantPort: 2023},
		{input: "dave@example.com:notaport", wantUser: "dave", wantHost: "example.com:notaport", wantPort: 22},
	}

	for _, tt := range tests {
		user, host, port := parseSSHHostString(tt.input)
		if user != tt.wantUser || host != tt.wantHost || port != tt.wantPort {
			t.Fatalf("parseSSHHostString(%q) = (%q,%q,%d), want (%q,%q,%d)", tt.input, user, host, port, tt.wantUser, tt.wantHost, tt.wantPort)
		}
	}
}

func TestExtractSSHSessionIDFromConnectResultPrefersExplicitFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "chinese connect output",
			input: "SSH 连接成功\n会话 ID: ssh_root@example.com:22_3\n主机: root@example.com:22",
			want:  "ssh_root@example.com:22_3",
		},
		{
			name:  "json field",
			input: `{"session_id":"ssh_alice@host.internal:2200_12","status":"running"}`,
			want:  "ssh_alice@host.internal:2200_12",
		},
		{
			name:  "bracketed ipv6",
			input: "session-id = ssh_carol@[2001:db8::10]:2022_4",
			want:  "ssh_carol@[2001:db8::10]:2022_4",
		},
		{
			name:  "fallback regex",
			input: "reused ssh_bob@10.0.0.5:22_9 from previous connection",
			want:  "ssh_bob@10.0.0.5:22_9",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		if got := extractSSHSessionIDFromConnectResult(tt.input); got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}

func TestInferRemoteProjectDirFromSSHSessionHandlesCommonLaunchCommands(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "cd /repo/project && bash", want: "/repo/project"},
		{input: "cd '/repo/project with space'; exec bash", want: "/repo/project with space"},
		{input: "cd \"/srv/app\" && clear", want: "/srv/app"},
		{input: "cd -- /opt/service && exec bash", want: "/opt/service"},
		{input: "cd\t/var/www && bash", want: "/var/www"},
		{input: "cd -P /srv/physical && bash", want: "/srv/physical"},
		{input: "cd -L '/srv/link target' && bash", want: "/srv/link target"},
		{input: "cdfoo /wrong && bash", want: ""},
		{input: "echo hello", want: ""},
	}

	for _, tt := range tests {
		if got := inferRemoteProjectDirFromSSHSession(tt.input); got != tt.want {
			t.Fatalf("inferRemoteProjectDirFromSSHSession(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestScrubActivePhaseSensitiveFormDataRemovesSecretsOnly(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{
				ID: "ssh_form_phase",
				InputSchema: &v2.PhaseInputSchema{
					Fields: []v2.PhaseInputField{
						{Name: "ssh_host", Label: "主机", Type: "text"},
						{Name: "ssh_password", Label: "密码", Type: "text", Sensitive: true},
					},
				},
				FormData: map[string]interface{}{
					"ssh_host":        "10.0.0.8",
					"ssh_password":    "secret",
					"api_token":       "token",
					"project_request": "fix login",
				},
			},
		},
		CurrentPhase: 0,
	}

	if !scrubActivePhaseSensitiveFormData(state) {
		t.Fatalf("expected sensitive form data to be scrubbed")
	}
	formData := state.Phases[0].FormData
	for _, key := range []string{"ssh_password", "api_token"} {
		if _, ok := formData[key]; ok {
			t.Fatalf("%s should be removed from form data", key)
		}
	}
	if got := formData["ssh_host"]; got != "10.0.0.8" {
		t.Fatalf("ssh_host = %v, want preserved", got)
	}
	if got := formData["project_request"]; got != "fix login" {
		t.Fatalf("project_request = %v, want preserved", got)
	}
	if scrubActivePhaseSensitiveFormData(state) {
		t.Fatalf("second scrub should report no changes")
	}
}

func TestCodingWorkflowChoicePanelHasNoSimplifiedOrRemoteTemplates(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.askWorkflowConfirmChoice(IMUserMessage{UserID: "u", Text: "写一个 hello world"}, &v2.RouteResult{
		Target:       v2.RouteToWorkflow,
		WorkflowType: "coding",
	})
	if result.Response == nil {
		t.Fatal("expected choice response")
	}
	if strings.Contains(result.Response.Text, "简化编程") || strings.Contains(result.Response.Text, "远程编程（推荐") {
		t.Fatalf("choice panel should not advertise removed templates, got %q", result.Response.Text)
	}
	choices := map[string]bool{}
	for _, action := range result.Response.Actions {
		choice, _, ok := parseWorkflowChoiceCommand(action.Command)
		if ok {
			choices[choice] = true
		}
	}
	if !choices[workflowChoiceComplex] || !choices[workflowChoiceSkip] {
		t.Fatalf("expected complex+skip actions, got %#v", result.Response.Actions)
	}
	if choices["coding_subagent"] || choices["remote_coding_subagent"] || choices["simple"] {
		t.Fatalf("removed template choices still present: %#v", result.Response.Actions)
	}
}

func TestRetiredWorkflowChoiceCommandsGuideToSupportedPaths(t *testing.T) {
	h := &IMMessageHandler{}
	pending := &pendingWorkflowChoice{
		Msg:         IMUserMessage{UserID: "u", Text: "写 hello"},
		RouteResult: &v2.RouteResult{Target: v2.RouteToWorkflow, WorkflowType: "coding"},
		ChoiceID:    "wc_retired",
	}
	for _, choice := range []string{"coding_subagent", "remote_coding_subagent", "simple"} {
		h.pendingWorkflowChoice.Store("u", pending)
		got := h.handleCodingComplexityCommand(IMUserMessage{UserID: "u", Text: buildWorkflowChoiceCommand(choice, "wc_retired")}, buildWorkflowChoiceCommand(choice, "wc_retired"))
		if got == nil || got.Response == nil {
			t.Fatalf("choice %q: expected response", choice)
		}
		if !strings.Contains(got.Response.Text, "该入口已下线") {
			t.Fatalf("choice %q: expected retired guidance, got %q", choice, got.Response.Text)
		}
		if got.ReplayText != "" {
			t.Fatalf("choice %q: should not replay original text", choice)
		}
	}
}

func TestHasPendingTemplateSubAgentExecutionRequiresTemplateExecutionContext(t *testing.T) {
	h := &IMMessageHandler{}
	if h.hasPendingTemplateSubAgentExecution("u") {
		t.Fatalf("empty handler state should not be pending template execution")
	}

	h.pendingV2SubAgentExecution.Store("u", true)
	if h.hasPendingTemplateSubAgentExecution("u") {
		t.Fatalf("generic pending v2 execution without coding template context should not be treated as pending")
	}

	h.pendingTemplateCodingProjectPath.Store("u", "D:/repo")
	if !h.hasPendingTemplateSubAgentExecution("u") {
		t.Fatalf("pure coding pending context should be pending template execution")
	}
	h.pendingTemplateCodingProjectPath.Delete("u")

	h.pendingTemplateRemoteCoding.Store("u", remoteCodingTemplateContext{SessionID: "ssh-1", ProjectDir: "/repo", WorkDir: "/repo"})
	if !h.hasPendingTemplateSubAgentExecution("u") {
		t.Fatalf("pure remote coding pending context should be pending template execution")
	}
}


func TestClearPerUserSessionStateClearsTemplateSubAgentPendingState(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "u"
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.pendingTemplateCodingProjectPath.Store(userID, "D:/repo")
	h.pendingTemplateRemoteCoding.Store(userID, remoteCodingTemplateContext{SessionID: "ssh-1", ProjectDir: "/repo", WorkDir: "/repo"})

	h.clearPerUserSessionState(userID)

	if _, ok := h.pendingV2SubAgentExecution.Load(userID); ok {
		t.Fatalf("pendingV2SubAgentExecution should be cleared")
	}
	if _, ok := h.pendingTemplateCodingProjectPath.Load(userID); ok {
		t.Fatalf("pendingTemplateCodingProjectPath should be cleared")
	}
	if _, ok := h.pendingTemplateRemoteCoding.Load(userID); ok {
		t.Fatalf("pendingTemplateRemoteCoding should be cleared")
	}
}

func TestSSHConnectFailureMessage(t *testing.T) {
	// Empty reason keeps the original generic guidance.
	generic := sshConnectFailureMessage("root", "example.com", 22, "")
	if generic != "无法连接到远程服务器 root@example.com:22，请检查网络和凭据" {
		t.Fatalf("generic message = %q", generic)
	}
	// Reason is appended single-line so users see the actual cause
	// (auth failure vs dial timeout) instead of a bare generic hint.
	reason := sshConnectFailureMessage("root", "example.com", 22, "SSH 连接失败: dial tcp 1.2.3.4:22: i/o timeout\n\nextra detail")
	if !strings.HasPrefix(reason, "无法连接到远程服务器 root@example.com:22：") {
		t.Fatalf("reason message should keep base context, got %q", reason)
	}
	if !strings.Contains(reason, "i/o timeout extra detail") {
		t.Fatalf("reason should be whitespace-collapsed, got %q", reason)
	}
	if strings.Contains(reason, "\n") {
		t.Fatalf("reason should be single-line, got %q", reason)
	}
	// Long reasons are truncated so dialog error boxes stay readable.
	long := sshConnectFailureMessage("root", "example.com", 22, strings.Repeat("x", 500))
	if n := len([]rune(long)); n > 200 {
		t.Fatalf("long reason should be truncated, got %d runes", n)
	}
}


func TestBuildRemoteCodingPlanStepTextStopsAtCurrentStep(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "检查远程环境与工作目录", Description: "ls -la /home; python3 --version"},
		{Index: 2, Title: "初始化项目结构", Description: "mkdir /home/sysinfo-viewer; 创建 main.py 空框架"},
		{Index: 3, Title: "实现系统信息查看功能", Description: "在 main.py 中编写 get_os_info 等函数"},
		{Index: 4, Title: "验证运行并生成输出", Description: "python3 main.py 并保存 verification_output.txt"},
		{Index: 5, Title: "输出项目结构与验收结果", Description: "写 final_report.txt"},
	}
	fullPlan := `### T1: 检查远程环境与工作目录
描述: ls
### T2: 初始化项目结构
描述: mkdir 并创建 main.py 空框架
### T3: 实现系统信息查看功能
描述: 编写 get_os_info get_cpu_info get_memory_info get_disk_info
### T4: 验证运行并生成输出
描述: python3 main.py
### T5: 输出项目结构与验收结果
描述: final_report.txt`

	_ = fullPlan // recipes must not appear in planned step prompt
	text := buildRemoteCodingPlanStepText(
		tasks[0], 1, 5, true,
		"开发一个系统信息查看软件",
		tasks,
		0,
		"开发一个系统信息查看软件",
		"",
		nil,
	)

	// Local-parity stop constraints.
	for _, want := range []string{
		"[Plan step T1/5] 检查远程环境与工作目录",
		"You are executing plan step T1/5: 检查远程环境与工作目录",
		"Focus on this step only; do not skip ahead.",
		"Complete the CURRENT step fully, then stop; later steps run as separate tasks.",
		"Do not implement, create files for, verify, or report on work that belongs to later plan steps.",
		"If you use todo_write, only subdivide THIS step — do not re-list the whole plan as todos.",
		"## Overall user request (context only — not a license to finish later steps)",
		"开发一个系统信息查看软件",
		"Plan outline (titles only; later steps are separate remote tasks — do not execute them now):",
		"- T1: 检查远程环境与工作目录  ← current (do only this)",
		"- T2: 初始化项目结构",
		"- T5: 输出项目结构与验收结果",
		"ls -la /home; python3 --version",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("planned T1 prompt missing %q\n---\n%s", want, text)
		}
	}

	// Must NOT dump later-step implementation recipes that caused T1 rush-ahead.
	for _, banned := range []string{
		"Active multi-step execution plan:",
		"Session plan / overall goal:",
		"创建 main.py 空框架",
		"get_os_info get_cpu_info",
		"verification_output.txt",
		"final_report.txt",
		"在 main.py 中编写 get_os_info",
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("planned T1 prompt must not include later-step details %q\n---\n%s", banned, text)
		}
	}
}

func TestBuildRemoteCodingPlanStepTextUnplannedKeepsFullPlanContext(t *testing.T) {
	text := buildRemoteCodingPlanStepText(
		nil, 1, 1, false,
		"fix remote project",
		nil,
		1,
		"keep improving remote",
		"previous summary here",
		[]string{"/home/app/main.py"},
	)
	for _, want := range []string{
		"fix remote project",
		"[Session continuity turn 2]",
		"Session plan / overall goal:",
		"keep improving remote",
		"Previous turn summary:",
		"previous summary here",
		"Files modified earlier: /home/app/main.py",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("unplanned prompt missing %q\n---\n%s", want, text)
		}
	}
	// Stale sticky multi-step recipes must not re-enter free-form follow-ups.
	for _, banned := range []string{
		"Active multi-step execution plan:",
		"Focus on this step only",
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("unplanned prompt must not include %q\n---\n%s", banned, text)
		}
	}
}

func TestBuildRemoteCodingPlanStepTextLaterStepUsesOwnDescriptionOnly(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "检查环境", Description: "仅检查"},
		{Index: 2, Title: "实现功能", Description: "编写 get_cpu_info 并格式化输出"},
	}
	carry := formatRemotePlanStepCarrySummary(1, "检查环境", "success", "✅ 全部完成 — 项目总结\nmain.py written")
	text := buildRemoteCodingPlanStepText(
		tasks[1], 2, 2, true,
		"做系统信息工具",
		tasks,
		1,
		"",
		carry,
		[]string{"/home/a.txt"},
	)
	if !strings.Contains(text, "[Plan step T2/2] 实现功能") {
		t.Fatalf("expected current step header, got:\n%s", text)
	}
	if !strings.Contains(text, "编写 get_cpu_info 并格式化输出") {
		t.Fatalf("current step description should be present, got:\n%s", text)
	}
	if !strings.Contains(text, "Prior plan step T1") || !strings.Contains(text, "still execute the CURRENT step fully") {
		t.Fatalf("sanitized prior summary should carry forward, got:\n%s", text)
	}
	if !strings.Contains(text, "- T2: 实现功能  ← current (do only this)") {
		t.Fatalf("current step should be marked in outline, got:\n%s", text)
	}
}

func TestFormatRemotePlanStepCarrySummary(t *testing.T) {
	got := formatRemotePlanStepCarrySummary(1, "检查", "success", "全部完成 — 项目总结\n"+strings.Repeat("x", 500))
	if !strings.Contains(got, "Prior plan step T1 (检查) status=success") {
		t.Fatalf("header missing: %q", got)
	}
	if !strings.Contains(got, "still execute the CURRENT step fully") {
		t.Fatalf("guard missing: %q", got)
	}
	// Body is truncated; full 500 x's should not all fit after prefix.
	if strings.Count(got, "x") >= 500 {
		t.Fatalf("expected body truncation, got %d x runes in %d-len string", strings.Count(got, "x"), len(got))
	}
}

func TestSetRemotePlanLastSummary(t *testing.T) {
	var mem stickyCodingWorkbenchMemory
	setRemotePlanLastSummary(&mem, true, 1, "检查", "success", "全部完成 — 项目总结")
	if !strings.Contains(mem.LastSummary, "Prior plan step T1") || !strings.Contains(mem.LastSummary, "CURRENT step fully") {
		t.Fatalf("planned carry: %q", mem.LastSummary)
	}
	setRemotePlanLastSummary(&mem, false, 1, "检查", "success", "plain summary")
	if mem.LastSummary != "plain summary" {
		t.Fatalf("unplanned should store raw summary: %q", mem.LastSummary)
	}
	setRemotePlanLastSummary(&mem, true, 2, "x", "ok", "   ")
	if mem.LastSummary != "plain summary" {
		t.Fatalf("empty sum should not clear: %q", mem.LastSummary)
	}
}
