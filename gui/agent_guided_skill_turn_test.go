package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func bookPDFTestSkill() corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:        "Book-PDF: 书籍级PDF手册全流程",
		Status:      "active",
		Source:      "clawhub",
		Description: "深度调研一个主题，生成100页+书籍级PDF手册。",
		Triggers:    []string{"huashu-book-pdf"},
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{
				"instructions": "# Book-PDF：书籍级PDF手册全流程\n\n五个阶段：调研 → 规划 → 写作 → 构建 → 版本更新。\n启动多个background agent并行调研，多Agent并行写作。\n与用户确认大纲后进入写作。使用 templates/ 和 scripts/init-project.sh。version.json 记录语义化版本。",
			},
		}},
	}
}

func weatherPDFTestSkill() corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:        "craft_dongguan_weather_pdf_report",
		Status:      "active",
		Source:      "learned",
		Description: "东莞天气，输出 格式化pdf报告",
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo pdf"}}},
	}
}

func newSkillExecutorWithEntries(t *testing.T, entries ...corelib.NLSkillEntry) (*App, *SkillExecutor) {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: entries}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	exec := NewSkillExecutor(app, nil, nil)
	app.skillExecutor = exec
	return app, exec
}

func TestPreferredLocalSkillNamesAgentGuidedOverWeatherPDF(t *testing.T) {
	_, exec := newSkillExecutorWithEntries(t, weatherPDFTestSkill(), bookPDFTestSkill())
	for _, msg := range []string{
		"book-pdf skill",
		"使用book-pdf skill 编写教程",
		"使用 book_pdf 生成书籍",
		"使用book pdf skill生成书籍",
	} {
		name, _ := matchPreferredLocalSkill(exec, msg, corelib.SkillDomainGeneral)
		if name != bookPDFTestSkill().Name {
			t.Fatalf("msg %q preferred %q, want Book-PDF", msg, name)
		}
	}
}

func TestPreferredLocalSkillDoesNotVolunteerAgentGuidedForGenericPDF(t *testing.T) {
	_, exec := newSkillExecutorWithEntries(t, weatherPDFTestSkill(), bookPDFTestSkill())
	name, _ := matchPreferredLocalSkill(exec, "南京天气，生成pdf报告", corelib.SkillDomainGeneral)
	if name == bookPDFTestSkill().Name {
		t.Fatal("generic PDF report must not prefer the agent-guided Book-PDF workflow")
	}
	if name != weatherPDFTestSkill().Name {
		t.Fatalf("generic PDF report preferred %q, want the learned weather skill", name)
	}
}

func TestInitialAgentLoopPhaseAgentGuidedNamedSkill(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, weatherPDFTestSkill(), bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	phase := h.initialAgentLoopPhase("book-pdf skill", nil)
	if !phase.ForceSkillPreference {
		t.Fatal("named Book-PDF turn must force skill preference")
	}
	if phase.SkillMode != skillPreferenceAgentGuided {
		t.Fatalf("SkillMode=%q, want %q", phase.SkillMode, skillPreferenceAgentGuided)
	}
	if phase.PreferredSkillName != bookPDFTestSkill().Name {
		t.Fatalf("PreferredSkillName=%q", phase.PreferredSkillName)
	}
	prompt := buildSkillPreferenceConvergePrompt(phase)
	if !strings.Contains(prompt, "agent-guided workflow") || strings.Contains(prompt, `manage_skill(action="run"`) {
		t.Fatalf("converge prompt still treats the workflow as manage_skill:\n%s", prompt)
	}
	if shouldRestrictToSkillSearch(phase) {
		t.Fatal("agent-guided mode must not restrict the surface to skill search")
	}
}

func TestFilterToolsForAgentGuidedWorkflowDropsDiscoveryKeepsHostTools(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("discover_tool", "search tools", nil, nil),
		toolDef("generate_pdf", "make pdf", nil, nil),
		toolDef("search_and_install_skill", "install skill", nil, nil),
		toolDef("bash", "run bash", nil, nil),
		toolDef("read_file", "read", nil, nil),
	}
	all := append(tools, toolDef("write_file", "write", nil, nil), toolDef("edit_file", "edit", nil, nil))
	got := ensureAgentGuidedHostTools(filterToolsForAgentGuidedWorkflow(tools), all)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	for _, blocked := range []string{"discover_tool", "generate_pdf", "search_and_install_skill"} {
		if names[blocked] {
			t.Fatalf("agent-guided surface still has %s: %#v", blocked, names)
		}
	}
	for _, host := range []string{"bash", "read_file", "write_file", "edit_file"} {
		if !names[host] {
			t.Fatalf("agent-guided surface missing host tool %s: %#v", host, names)
		}
	}
}

func TestWriteRegisteredSkillsSectionSplitsAgentGuided(t *testing.T) {
	var b strings.Builder
	writeRegisteredSkillsSection(&b, []NLSkillDefinition{
		{Name: "empty-skill", Status: "active"},
		{
			Name:        weatherPDFTestSkill().Name,
			Status:      "active",
			Description: weatherPDFTestSkill().Description,
			Steps:       weatherPDFTestSkill().Steps,
		},
		{
			Name:        bookPDFTestSkill().Name,
			Status:      "active",
			Source:      "clawhub",
			Description: bookPDFTestSkill().Description,
			Triggers:    bookPDFTestSkill().Triggers,
			Steps:       bookPDFTestSkill().Steps,
		},
	})
	got := b.String()
	if strings.Contains(got, "empty-skill") {
		t.Fatalf("empty-skill leaked into the advertised list:\n%s", got)
	}
	if !strings.Contains(got, "### Agent 工作流 Skill（已安装）") {
		t.Fatalf("missing agent-guided section:\n%s", got)
	}
	if !strings.Contains(got, "「"+bookPDFTestSkill().Name+"」") {
		t.Fatalf("colonated name must be quoted so it is not parsed as Name: Description:\n%s", got)
	}
	if !strings.Contains(got, weatherPDFTestSkill().Name+":") {
		t.Fatalf("runnable skill missing:\n%s", got)
	}
	if !strings.Contains(got, "禁止 discover_tool") {
		t.Fatalf("missing discover_tool prohibition:\n%s", got)
	}
}

func TestShouldPreferSkillForTaskNamedInvocationWithoutPDFHint(t *testing.T) {
	if !shouldPreferSkillForTask("使用 hello skill 跑一下") {
		t.Fatal("explicit skill invocation must prefer skills even without pdf/report hints")
	}
}

func TestInitialAgentLoopPhaseIgnoresCasualRunnableSkillMention(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, corelib.NLSkillEntry{
		Name:        "contract-review",
		Status:      "active",
		Description: "审查合同条款",
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}}},
	})
	h := &IMMessageHandler{app: app}
	phase := h.initialAgentLoopPhase("contract-review 这个名字怎么来的", nil)
	if phase.ForceSkillPreference {
		t.Fatalf("casual mention of a runnable skill must not strip bash: mode=%q name=%q", phase.SkillMode, phase.PreferredSkillName)
	}
}

func TestAgentGuidedIdentityBeatsQueryIntentGate(t *testing.T) {
	app, exec := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	name, _, mode := matchPreferredLocalSkillMode(exec, "看看 book-pdf skill 是什么", corelib.SkillDomainGeneral)
	if name != bookPDFTestSkill().Name || mode != skillPreferenceAgentGuided {
		t.Fatalf("query-intent identity match: name=%q mode=%q", name, mode)
	}
	h := &IMMessageHandler{app: app}
	phase := h.initialAgentLoopPhase("看看 book-pdf skill 是什么", nil)
	if phase.SkillMode != skillPreferenceAgentGuided {
		t.Fatalf("SkillMode=%q, want agent_guided so query wording cannot bounce to remote search", phase.SkillMode)
	}
}

func TestAgentGuidedNoToolPromptDoesNotCallManageSkill(t *testing.T) {
	got := buildNoToolActionPromptForSkillMode(true, true, bookPDFTestSkill().Name, "")
	if strings.Contains(got, `manage_skill(action="run"`) {
		t.Fatalf("agent-guided no-tool prompt still pushes manage_skill run:\n%s", got)
	}
	if !strings.Contains(got, "Skill 使用文档") {
		t.Fatalf("missing workflow instruction:\n%s", got)
	}
	stall := buildNoToolStallRecoverPromptForSkillMode(2, true, true, bookPDFTestSkill().Name, "")
	if strings.Contains(stall, `manage_skill(action="run"`) {
		t.Fatalf("agent-guided stall prompt still pushes manage_skill run:\n%s", stall)
	}
}

func TestAgentGuidedDocsInjectWithoutTriggers(t *testing.T) {
	entry := bookPDFTestSkill()
	skills := []NLSkillDefinition{{
		Name:     entry.Name,
		Type:     "executable",
		Status:   "active",
		Source:   "clawhub",
		Steps:    entry.Steps,
		Triggers: nil,
	}}
	got := runInjection(t, skills, "使用book-pdf skill 编写教程", 0)
	if !strings.Contains(got, "### Skill: "+entry.Name) {
		t.Fatalf("agent-guided skill with empty triggers must still inject:\n%s", got)
	}
}

func lightExplicitTool(name, cap string) map[string]interface{} {
	def := toolDef(name, name+" tool", nil, nil)
	def["x_execution_contract"] = map[string]interface{}{
		"capabilities":            []string{cap},
		"requires_agent_planning": false,
		"supports_direct":         true,
	}
	return def
}

func TestAgentGuidedSurfaceReplacesDiscoveryOnlyList(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("discover_tool", "search tools", nil, nil),
		toolDef("generate_pdf", "make pdf", nil, nil),
	}
	catalog := append(append([]map[string]interface{}{}, tools...),
		toolDef("bash", "run bash", nil, nil),
		toolDef("write_file", "write", nil, nil),
	)
	got := applyAgentGuidedWorkflowSurface(tools, catalog)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	if names["discover_tool"] || names["generate_pdf"] {
		t.Fatalf("discovery-only list leaked through: %#v", names)
	}
	if !names["bash"] || !names["write_file"] {
		t.Fatalf("host tools missing after replacing discovery-only list: %#v", names)
	}
}

func TestAgentGuidedStickyContinuationKeepsWorkflow(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	first := h.initialAgentLoopPhase("使用book-pdf skill 编写教程", nil)
	if first.SkillMode != skillPreferenceAgentGuided {
		t.Fatalf("first turn SkillMode=%q", first.SkillMode)
	}
	follow := h.initialAgentLoopPhase("将大纲保存为markdown格式", nil)
	if follow.SkillMode != skillPreferenceAgentGuided || follow.PreferredSkillName != bookPDFTestSkill().Name {
		t.Fatalf("follow-up dropped the workflow: mode=%q name=%q", follow.SkillMode, follow.PreferredSkillName)
	}
}

func TestAgentGuidedStickyDoesNotCaptureSavePassword(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	_ = h.initialAgentLoopPhase("book-pdf skill", nil)
	phase := h.initialAgentLoopPhase("帮我保存wifi密码到文件", nil)
	if phase.SkillMode == skillPreferenceAgentGuided {
		t.Fatalf("unrelated 保存 request reused Book-PDF sticky: %+v", phase)
	}
}

func TestAgentGuidedStickySurvivesAskUserResponse(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	owner := "desktop-user:cloud-book"
	ctx := &LoopContext{UserID: owner}
	first := h.initialAgentLoopPhase("book-pdf skill", ctx)
	if first.SkillMode != skillPreferenceAgentGuided {
		t.Fatalf("first turn SkillMode=%q", first.SkillMode)
	}
	askCtx := &LoopContext{UserID: owner, IsAskUserResponse: true}
	phase := h.initialAgentLoopPhase("选第一个方案", askCtx)
	if phase.SkillMode != skillPreferenceAgentGuided || phase.PreferredSkillName != bookPDFTestSkill().Name {
		t.Fatalf("ask-user reply dropped the workflow: mode=%q name=%q", phase.SkillMode, phase.PreferredSkillName)
	}
}

func TestAgentGuidedOwnerIDUsesLoopUserNotPolicyOwner(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	ctx := &LoopContext{UserID: "desktop-user:cloud-book", Runtime: RuntimeContext{PolicyOwnerID: "desktop-user", RequestID: "req-1"}}
	_ = h.initialAgentLoopPhase("book-pdf skill", ctx)
	if h.recallAgentGuidedSkill("desktop-user", app.skillExecutor) != "" {
		t.Fatal("sticky must not be keyed by PolicyOwnerID")
	}
	if h.recallAgentGuidedSkill("desktop-user:cloud-book", app.skillExecutor) == "" {
		t.Fatal("sticky must be keyed by LoopContext.UserID")
	}
	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "将大纲保存为markdown格式", agentGuidedSkillOwnerID(ctx, "desktop-user"))
	if !strings.Contains(b.String(), "五个阶段") {
		t.Fatalf("docs injection must use UserID so follow-up finds sticky:\n%s", b.String())
	}
}

func TestAgentGuidedStickyDoesNotCaptureNewPDFTask(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	_ = h.initialAgentLoopPhase("book-pdf skill", nil)
	phase := h.initialAgentLoopPhase("南京天气，生成pdf报告", nil)
	if phase.SkillMode == skillPreferenceAgentGuided {
		t.Fatalf("new weather PDF task reused Book-PDF sticky: %+v", phase)
	}
}

func TestStickyFollowUpInjectsAgentGuidedDocs(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	_ = h.initialAgentLoopPhase("book-pdf skill", nil)
	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "将大纲保存为markdown格式", "")
	got := b.String()
	if !strings.Contains(got, "### Skill: "+bookPDFTestSkill().Name) || !strings.Contains(got, "五个阶段") {
		t.Fatalf("sticky follow-up must still inject Book-PDF docs:\n%s", got)
	}
}

func TestMergeStickyAgentGuidedDocsSkipsDuplicate(t *testing.T) {
	entry := bookPDFTestSkill()
	skills := []NLSkillDefinition{{
		Name:     entry.Name,
		Status:   "active",
		Source:   "clawhub",
		Steps:    entry.Steps,
		Triggers: entry.Triggers,
	}}
	matched := collectMatchedSkillDocs(skills, "使用book-pdf skill 编写教程")
	if len(matched) == 0 {
		t.Fatal("expected identity match")
	}
	got := mergeStickyAgentGuidedDocs(matched, skills, entry.Name)
	if len(got) != len(matched) {
		t.Fatalf("duplicate sticky insert: %d -> %d", len(matched), len(got))
	}
}

func TestClearSessionForgetsAgentGuidedSticky(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	_ = h.initialAgentLoopPhase("book-pdf skill", nil)
	h.clearPerUserSessionState("")
	phase := h.initialAgentLoopPhase("将大纲保存为markdown格式", nil)
	if phase.SkillMode == skillPreferenceAgentGuided {
		t.Fatal("/new must forget the sticky agent-guided workflow")
	}
}

func TestAgentGuidedSurfaceSurvivesLightExecutionProfile(t *testing.T) {
	routed := []map[string]interface{}{
		lightExplicitTool("discover_tool", "status"),
		lightExplicitTool("generate_pdf", "web"),
		lightExplicitTool("web_search", "web"),
		toolDef("bash", "run bash", nil, nil),
	}
	catalog := append(append([]map[string]interface{}{}, routed...),
		toolDef("write_file", "write", nil, nil),
		toolDef("edit_file", "edit", nil, nil),
		toolDef("read_file", "read", nil, nil),
	)
	profile := ExecutionProfile{
		Layer:                string(executionLayerLight),
		PromptProfile:        "light",
		ToolBudget:           8,
		RequiredCapabilities: []string{"web", "status"},
	}
	afterLight := filterToolsForExecutionProfile(routed, profile)
	got := applyAgentGuidedWorkflowSurface(afterLight, catalog)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	for _, blocked := range []string{"discover_tool", "generate_pdf"} {
		if names[blocked] {
			t.Fatalf("light+seal still has %s: %#v", blocked, names)
		}
	}
	for _, host := range []string{"bash", "write_file", "edit_file", "read_file"} {
		if !names[host] {
			t.Fatalf("light+seal missing host tool %s: %#v", host, names)
		}
	}
}
