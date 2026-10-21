package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func paperTranslatorTestSkill() corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:        "paper-translator",
		Status:      "active",
		Description: "Translate a paper into a formatted document.",
		Triggers:    []string{"论文翻译"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo translated"}}},
	}
}

func weeklyDigestTestSkill() corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:        "weekly-digest",
		Status:      "active",
		Description: "pdf 报告 文档 综述 markdown daily papers",
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo digest"}}},
	}
}

func lightLookupLoop(primary intent.IntentLabel) *LoopContext {
	ctx := fullDocumentLoop(primary)
	ctx.Runtime.Execution.Layer = string(executionLayerLight)
	ctx.Runtime.Execution.PromptProfile = "light"
	ctx.Runtime.Execution.Reason = "lookup"
	return ctx
}

func fullDocumentLoop(primary intent.IntentLabel) *LoopContext {
	return &LoopContext{
		UserID: "user-1",
		Runtime: RuntimeContext{
			Execution: ExecutionProfile{
				Layer:         string(executionLayerFull),
				PromptProfile: "full",
				Reason:        "semantic capability-managed mutating intent",
			},
			SemanticIntent: &intent.ClassificationResult{Primary: primary, Confidence: 0.98},
		},
	}
}

func TestSkillAdmissionBindsACompleteLocalSkill(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill(), weatherPDFTestSkill())
	h := &IMMessageHandler{app: app}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), "请做论文翻译")
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "paper-translator" {
		t.Fatalf("decision=%#v", decision)
	}
	if decision.Bound[0].Alias != "skill_paper_translator" || decision.Bound[0].ContentDigest == "" {
		t.Fatalf("binding=%#v", decision.Bound[0])
	}
}

func TestSkillAdmissionIgnoresAWeakDocumentOverlap(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, weatherPDFTestSkill())
	h := &IMMessageHandler{app: app}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), "生成一份项目报告 pdf")
	if decision.Mode != skillAdmissionRemote {
		t.Fatalf("weak overlap mode=%v, want remote", decision.Mode)
	}
}

func TestSkillAdmissionSkipsLookups(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill(), weatherPDFTestSkill())
	h := &IMMessageHandler{app: app}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelLiveData), "南京天气，生成pdf报告")
	if decision.Mode != skillAdmissionNone {
		t.Fatalf("lookup mode=%v, want none", decision.Mode)
	}
}

func TestSkillAdmissionFollowsANamedAgentGuidedWorkflow(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill(), weatherPDFTestSkill())
	h := &IMMessageHandler{app: app}
	ctx := fullDocumentLoop(intent.LabelDocumentGenerate)
	decision := h.skillAdmissionForTurn(ctx, "使用book-pdf skill 编写教程")
	if decision.Mode != skillAdmissionGuided || len(decision.Guided) != 1 || decision.Guided[0].Name != bookPDFTestSkill().Name {
		t.Fatalf("decision=%#v", decision)
	}
	prompt := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "user-1", Text: "使用book-pdf skill 编写教程", Platform: "desktop",
	}, nil, ctx, false, "", "", "", "")
	if !strings.Contains(prompt, "## Skill 使用文档") || !strings.Contains(prompt, "五个阶段") {
		t.Fatal("named agent-guided workflow was not injected")
	}
	if strings.Contains(prompt, `manage_skill(action="run"`) {
		t.Fatal("agent-guided workflow was sent through manage_skill")
	}
}

func TestSkillAdmissionPublishesTheBoundFunctionAndRunsThatIdentity(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill())
	h := &IMMessageHandler{app: app}
	ctx := fullDocumentLoop(intent.LabelDocumentGenerate)
	cb := &sharedAgentLoopCallbacks{
		handler:         h,
		userID:          "user-1",
		userText:        "请做论文翻译",
		loopCtx:         ctx,
		semanticSurface: &semanticCallSurface{},
		tools:           []map[string]interface{}{toolDef("generate_pdf", "pdf", nil, nil)},
	}
	cb.publishAdmittedSkillTools(cb.userText)
	if !cb.IsToolAllowed("skill_paper_translator") {
		t.Fatal("bound skill was not allowed on the managed turn")
	}
	found := false
	for _, def := range cb.tools {
		if extractToolName(def) == "skill_paper_translator" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tools=%v", toolNames(cb.tools))
	}
	cb.admittedSkillBindings["skill_paper_translator"] = admittedSkillBinding{
		Alias: "skill_paper_translator", Name: "paper-translator", StableID: "legacy:paper-translator", ContentDigest: "stale",
	}
	text, handled, ok := cb.executeAdmittedSkillHostCall("skill_paper_translator", `{"topic":"rust"}`)
	if !handled || ok || !strings.Contains(text, "skill_binding_stale") {
		t.Fatalf("handled=%v ok=%v text=%q", handled, ok, text)
	}
}

func TestSkillAdmissionOffersRemoteSearchWhenNothingLocalMatches(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t)
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler:         h,
		loopCtx:         fullDocumentLoop(intent.LabelOffice),
		semanticSurface: &semanticCallSurface{},
	}
	cb.publishAdmittedSkillTools("把这份表格做成带目录的手册")
	if !cb.skillRemoteSearchOffered || !cb.IsToolAllowed("search_and_install_skill") {
		t.Fatal("complex task without a local skill did not offer search and install")
	}
	if toolNames(cb.tools)["manage_skill"] {
		t.Fatal("remote admission reopened manage_skill")
	}
}

func TestSkillAdmissionAuthorizerRunsTheBoundSkillBeforeSubstitutes(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill())
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler:  h,
		userID:   "user-1",
		userText: "请做论文翻译",
		loopCtx:  fullDocumentLoop(intent.LabelDocumentGenerate),
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{
				"generate_pdf": {},
				"bash":         {},
			},
		},
	}
	cb.publishAdmittedSkillTools(cb.userText)
	if ok, reason := cb.IsToolCallAllowed("skill_paper_translator", `{"topic":"rust"}`); !ok {
		t.Fatalf("bound skill was offered and then rejected: %s", reason)
	}
	if ok, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok || !strings.Contains(reason, "skill_paper_translator") {
		t.Fatalf("generate_pdf allowed=%v reason=%q", ok, reason)
	}
	if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo hi"}`); ok {
		t.Fatalf("bash ran before the bound skill: %s", reason)
	}
	if !strings.Contains(cb.SkillObligationPrompt(), "skill_paper_translator") {
		t.Fatalf("prompt=%q", cb.SkillObligationPrompt())
	}
	_, handled, _ := cb.executeAdmittedSkillHostCall("skill_paper_translator", `{"topic":"rust"}`)
	if !handled || !cb.admittedSkillSettled {
		t.Fatal("calling the bound skill did not settle the obligation")
	}
	if prompt := cb.SkillObligationPrompt(); prompt != "" {
		t.Fatalf("obligation remained after the skill ran: %q", prompt)
	}
	if ok, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); !ok {
		t.Fatalf("generate_pdf stayed blocked after the skill ran: %s", reason)
	}
	if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo hi"}`); !ok {
		t.Fatalf("bash stayed blocked after the skill ran: %s", reason)
	}
}

func TestSkillAdmissionAuthorizerRequiresMarketSearchBeforeImprovising(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t)
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: fullDocumentLoop(intent.LabelOffice),
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{"bash": {}},
		},
	}
	cb.publishAdmittedSkillTools("把这份表格做成带目录的手册")
	if ok, reason := cb.IsToolCallAllowed("search_and_install_skill", `{"query":"手册"}`); !ok {
		t.Fatalf("remote search rejected: %s", reason)
	}
	if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); ok || !strings.Contains(reason, "search_and_install_skill") {
		t.Fatalf("bash allowed=%v reason=%q", ok, reason)
	}
	if !strings.Contains(cb.SkillObligationPrompt(), "search_and_install_skill") {
		t.Fatalf("prompt=%q", cb.SkillObligationPrompt())
	}
	cb.admittedSkillSettled = true
	if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); !ok {
		t.Fatalf("bash stayed blocked after search: %s", reason)
	}
	if cb.SkillObligationPrompt() != "" {
		t.Fatal("reminder repeated after search")
	}
}

func TestSkillAdmissionAuthorizerKeepsGuidedFileToolsAndClosesPDF(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "user-1",
		loopCtx: fullDocumentLoop(intent.LabelDocumentGenerate),
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{"generate_pdf": {}},
		},
	}
	cb.publishAdmittedSkillTools("使用book-pdf skill 编写教程")
	if !cb.agentGuidedHostTools {
		t.Fatal("named workflow was not guided")
	}
	if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); !ok {
		t.Fatalf("guided workflow lost bash: %s", reason)
	}
	if ok, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok {
		t.Fatalf("guided workflow allowed generate_pdf: %s", reason)
	}
	cb.noteGuidedSkillUse("bash")
	if cb.SkillObligationPrompt() != "" {
		t.Fatal("guided workflow still demanded a call after bash ran")
	}
	if ok, _ := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok {
		t.Fatal("guided workflow reopened generate_pdf after the first file tool")
	}
}

func TestHostAdmittedSkillSearchKeepsAComplexTaskQuery(t *testing.T) {
	query := "修改这份表格的目录结构"
	if skillSearchQueryAllowed(query, false) {
		t.Fatal("a modify query is not a market search by itself")
	}
	if !skillSearchQueryAllowed(query, true) {
		t.Fatal("host-admitted complex task was dropped by the lookup filter")
	}
}

func TestSkillAdmissionExplicitInvocationLeavesTheLightProfile(t *testing.T) {
	live := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98}
	named := classifyIMExecutionProfileWithSemantic(IMUserMessage{
		Text: "使用 book-pdf skill 编写教程", Platform: "desktop",
	}, false, false, live)
	if named.IsLight() || named.PromptIsLight() || named.IterationBudget != 0 || named.Reason != "explicit skill invocation" {
		t.Fatalf("named skill stayed on the lookup budget: %+v", named)
	}
	weather := classifyIMExecutionProfileWithSemantic(IMUserMessage{
		Text: "南京天气", Platform: "desktop",
	}, false, false, live)
	if !weather.IsLight() || !weather.PromptIsLight() || weather.IterationBudget != 3 {
		t.Fatalf("lookup changed budget: %+v", weather)
	}
	report := classifyIMExecutionProfileWithSemantic(IMUserMessage{
		Text: "南京天气，生成pdf报告", Platform: "desktop",
	}, false, false, live)
	if !report.IsLight() || !report.PromptIsLight() {
		t.Fatalf("weather report left the lookup: %+v", report)
	}
	attached := "南京天气\n" + agent.AutoExtractNotice + "\n" +
		"--- auto_extract: begin path=\"paper.pdf\" format=\"pdf\" ---\n" +
		"使用 book-pdf skill 编写教程\n" +
		"--- auto_extract: end path=\"paper.pdf\" ---"
	fromAttachment := classifyIMExecutionProfileWithSemantic(IMUserMessage{
		Text: attached, Platform: "desktop",
	}, false, false, live)
	if !fromAttachment.IsLight() || !fromAttachment.PromptIsLight() {
		t.Fatalf("a skill named only in an attachment left the lookup: %+v", fromAttachment)
	}
	if got := (&IMMessageHandler{}).continuationKeepsParentExecution(weather, "user-1", "使用 book-pdf skill 编写教程", live); got.IsLight() || got.Reason != "explicit skill invocation" {
		t.Fatalf("continuation kept the lookup budget: %+v", got)
	}
}

func TestSkillAdmissionLightPromptRunsTheAdmittedTools(t *testing.T) {
	lightSurface := func() *semanticCallSurface {
		return &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{
				"bash":         {},
				"generate_pdf": {},
				"web_search":   {},
			},
		}
	}
	assertVisible := func(t *testing.T, cb *sharedAgentLoopCallbacks, name string, want bool) {
		t.Helper()
		if got := cb.IsToolAllowed(name); got != want {
			t.Fatalf("IsToolAllowed(%s)=%v, want %v", name, got, want)
		}
		if got := cb.IsToolAllowedForPromptProfile(name, agent.PromptProfileLight); got != want {
			t.Fatalf("IsToolAllowedForPromptProfile(%s)=%v, want %v", name, got, want)
		}
	}

	t.Run("guided", func(t *testing.T) {
		app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
		cb := &sharedAgentLoopCallbacks{
			handler:         &IMMessageHandler{app: app},
			userID:          "user-1",
			userText:        "使用 book-pdf skill 编写教程",
			loopCtx:         lightLookupLoop(intent.LabelLiveData),
			semanticSurface: lightSurface(),
		}
		cb.publishAdmittedSkillTools(cb.userText)
		if !cb.agentGuidedHostTools {
			t.Fatal("named workflow was not admitted on the light prompt")
		}
		assertVisible(t, cb, "bash", true)
		assertVisible(t, cb, "read_file", true)
		if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); !ok {
			t.Fatalf("guided bash rejected: %s", reason)
		}
		assertVisible(t, cb, "generate_pdf", false)
		if ok, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok {
			t.Fatalf("light guided turn allowed generate_pdf: %s", reason)
		}
	})

	t.Run("lookup", func(t *testing.T) {
		app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
		cb := &sharedAgentLoopCallbacks{
			handler:         &IMMessageHandler{app: app},
			userID:          "user-1",
			userText:        "南京天气",
			loopCtx:         lightLookupLoop(intent.LabelLiveData),
			semanticSurface: lightSurface(),
		}
		cb.publishAdmittedSkillTools(cb.userText)
		if cb.agentGuidedHostTools || cb.skillRemoteSearchOffered || len(cb.admittedSkillBindings) > 0 {
			t.Fatal("light lookup admitted a skill")
		}
		assertVisible(t, cb, "bash", false)
		assertVisible(t, cb, "read_file", false)
	})

	t.Run("bound", func(t *testing.T) {
		app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill())
		cb := &sharedAgentLoopCallbacks{
			handler:         &IMMessageHandler{app: app},
			userID:          "user-1",
			userText:        "使用 paper-translator skill 翻译这篇论文",
			loopCtx:         lightLookupLoop(intent.LabelLiveData),
			semanticSurface: lightSurface(),
		}
		cb.publishAdmittedSkillTools(cb.userText)
		assertVisible(t, cb, "skill_paper_translator", true)
		assertVisible(t, cb, "bash", false)
		if ok, reason := cb.IsToolCallAllowed("skill_paper_translator", `{"topic":"rust"}`); !ok {
			t.Fatalf("bound skill rejected on the light prompt: %s", reason)
		}
		if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); ok {
			t.Fatalf("bound admission opened bash: %s", reason)
		}
		if ok, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok {
			t.Fatalf("bound admission opened generate_pdf: %s", reason)
		}
	})

	t.Run("remote", func(t *testing.T) {
		app, _ := newSkillExecutorWithEntries(t)
		cb := &sharedAgentLoopCallbacks{
			handler:         &IMMessageHandler{app: app},
			userID:          "user-1",
			userText:        "使用 weekly-digest skill 做周报",
			loopCtx:         lightLookupLoop(intent.LabelLiveData),
			semanticSurface: lightSurface(),
		}
		cb.publishAdmittedSkillTools(cb.userText)
		if !cb.skillRemoteSearchOffered {
			t.Fatal("named miss did not offer market search")
		}
		assertVisible(t, cb, "search_and_install_skill", true)
		assertVisible(t, cb, "bash", false)
		if ok, reason := cb.IsToolCallAllowed("bash", `{"command":"echo"}`); ok {
			t.Fatalf("remote admission opened bash before search: %s", reason)
		}
	})
}

func TestLightPromptStillCarriesANamedGuidedSkill(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill(), weatherPDFTestSkill())
	h := &IMMessageHandler{app: app}
	ctx := &LoopContext{
		UserID: "user-1",
		Runtime: RuntimeContext{
			Execution: ExecutionProfile{
				Layer:         string(executionLayerLight),
				PromptProfile: "light",
				Reason:        "lookup",
			},
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98},
		},
	}
	prompt := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "user-1", Text: "使用 book-pdf skill 编写教程", Platform: "desktop",
	}, nil, ctx, false, "", "", "", "")
	if !strings.Contains(prompt, "## Skill 使用文档") || !strings.Contains(prompt, "五个阶段") {
		t.Fatal("light prompt dropped the named workflow")
	}
	if strings.Contains(prompt, `manage_skill(action="run"`) {
		t.Fatal("light prompt sent the workflow through manage_skill")
	}
	weather := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "user-1", Text: "南京天气", Platform: "desktop",
	}, nil, ctx, false, "", "", "", "")
	if strings.Contains(weather, "## Skill 使用文档") || strings.Contains(weather, "五个阶段") {
		t.Fatal("light lookup injected a skill document")
	}
}

func TestSkillAdmissionIgnoresASkillNamedOnlyInsideAnAttachment(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill(), bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	text := "使用 paper-translator 处理这份附件\n" +
		agent.AutoExtractNotice + "\n" +
		"--- auto_extract: begin path=\"paper.pdf\" format=\"pdf\" ---\n" +
		"使用 book-pdf skill 编写教程\n" +
		"--- auto_extract: end path=\"paper.pdf\" ---"
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), text)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "paper-translator" {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestHostAdmittedSkillSearchUsesTheAdmittedTask(t *testing.T) {
	const task = "修改这份表格的目录结构"
	var got string
	h := &IMMessageHandler{
		skillSearchInstallHandler: func(args map[string]interface{}, _ tool.ProgressCallback) searchAndInstallSkillResult {
			got, _ = args["query"].(string)
			return searchAndInstallSkillResult{Text: "searched", Success: false}
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler:                  h,
		userText:                 task,
		skillRemoteSearchOffered: true,
		semanticSurface:          &semanticCallSurface{},
	}
	_, handled, _ := cb.executeAdmittedSkillHostCall("search_and_install_skill", `{"query":"还记得 api2 服务器吗？"}`)
	if !handled || got != task {
		t.Fatalf("handled=%v query=%q", handled, got)
	}
	if !cb.admittedSkillSettled {
		t.Fatal("searching the admitted task did not settle")
	}
}

func TestModelCannotAdmitItsOwnSkillSearch(t *testing.T) {
	result := (&IMMessageHandler{}).toolSearchAndInstallSkillResult(map[string]interface{}{
		"query":         "修改这份表格的目录结构",
		"host_admitted": true,
	}, nil)
	if result.Success || !strings.Contains(result.Text, "Skill search skipped") {
		t.Fatalf("forged admission was honored: %+v", result)
	}
}

func TestSkillAdmissionBadArgumentsDoNotCountAsUsingTheSkill(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, paperTranslatorTestSkill())
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler:  h,
		userID:   "user-1",
		userText: "请做论文翻译",
		loopCtx:  fullDocumentLoop(intent.LabelDocumentGenerate),
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{"generate_pdf": {}},
		},
	}
	cb.publishAdmittedSkillTools(cb.userText)
	for _, argsJSON := range []string{`not-json`, `null`, `true`} {
		_, handled, ok := cb.executeAdmittedSkillHostCall("skill_paper_translator", argsJSON)
		if !handled || ok || cb.admittedSkillSettled {
			t.Fatalf("args %s handled=%v ok=%v settled=%v", argsJSON, handled, ok, cb.admittedSkillSettled)
		}
	}
	if allowed, reason := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); allowed {
		t.Fatalf("bad skill arguments opened generate_pdf: %s", reason)
	}
}

func TestSkillAdmissionDoesNotOfferAWeakerOverlapBesideANamedSkill(t *testing.T) {
	app, exec := newSkillExecutorWithEntries(t, paperTranslatorTestSkill(), weeklyDigestTestSkill())
	h := &IMMessageHandler{app: app}
	msg := "使用 paper-translator 生成 pdf 报告 文档 综述 markdown daily papers"
	var overlapScore int
	var overlapIdentity bool
	for _, def := range exec.List() {
		if def.Name != weeklyDigestTestSkill().Name {
			continue
		}
		overlapScore, overlapIdentity, _ = preferredLocalSkillScore(def, strings.ToLower(msg))
	}
	if overlapIdentity || overlapScore < skillAdmissionMinScore {
		t.Fatalf("overlap fixture score=%d identity=%v, want a weak score at or above the floor", overlapScore, overlapIdentity)
	}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), msg)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "paper-translator" {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestSkillAdmissionStillBindsAStrongOverlapWhenNothingIsNamed(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, weeklyDigestTestSkill())
	h := &IMMessageHandler{app: app}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), "生成 pdf 报告 文档 综述 markdown daily papers")
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != weeklyDigestTestSkill().Name {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestGuidedSkillUseCountsWhenTheOutputQuotesARejection(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "user-1",
		loopCtx: fullDocumentLoop(intent.LabelDocumentGenerate),
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{"generate_pdf": {}},
		},
	}
	cb.publishAdmittedSkillTools("使用book-pdf skill 编写教程")
	cb.noteGuidedSkillUseResult("read_file", "log line\n[system rejected] quoted from the file\n")
	if !cb.admittedSkillSettled || cb.SkillObligationPrompt() != "" {
		t.Fatal("tool output that quotes a rejection was treated as the host refusing the call")
	}
	if ok, _ := cb.IsToolCallAllowed("generate_pdf", `{"content":"# t"}`); ok {
		t.Fatal("quoting a rejection reopened generate_pdf")
	}
}

func TestSkillAdmissionPrefersGuidedWorkflowOnAScoreTie(t *testing.T) {
	app, exec := newSkillExecutorWithEntries(t, paperTranslatorTestSkill(), bookPDFTestSkill())
	h := &IMMessageHandler{app: app}
	msg := strings.ToLower("使用 book-pdf 和 paper-translator")
	var paperScore, bookScore int
	for _, def := range exec.List() {
		score, _, identityScore := preferredLocalSkillScore(def, msg)
		switch def.Name {
		case paperTranslatorTestSkill().Name:
			paperScore = score
			if identityScore != 20 {
				t.Fatalf("paper identity=%d, want the name hit only", identityScore)
			}
		case bookPDFTestSkill().Name:
			bookScore = score
			if identityScore != 20 {
				t.Fatalf("book identity=%d, want the name hit only", identityScore)
			}
		}
	}
	// The recipe's description adds a weak "paper" token on top of the same
	// name hit. That bonus must not hide the workflow the user also named.
	if paperScore <= bookScore {
		t.Fatalf("paper score=%d book score=%d, want the weak token to make the recipe total higher", paperScore, bookScore)
	}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), msg)
	if decision.Mode != skillAdmissionGuided || len(decision.Guided) != 1 || decision.Guided[0].Name != bookPDFTestSkill().Name {
		t.Fatalf("decision=%#v", decision)
	}
}
