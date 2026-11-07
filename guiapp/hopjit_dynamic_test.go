package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestHopDynamicSkillCallValidatesArguments(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	calls := 0
	handler.hopDynamicLookup = func(_ context.Context, name string) (hopDynamicBinding, bool, error) {
		if name != "note_skill" {
			t.Fatalf("lookup %q", name)
		}
		return hopDynamicBinding{
			Name:        "note_skill",
			Description: "整理笔记",
			Schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"topic": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"topic"},
				"additionalProperties": false,
			},
			Call: func(_ context.Context, args map[string]any) (string, error) {
				calls++
				if args["topic"] != "笔记" {
					t.Fatalf("args=%v", args)
				}
				return "已整理", nil
			},
		}, true, nil
	}
	provider, definitions, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{
		StepID: "1", Tools: []string{"note_skill"},
	}, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(*hopToolProvider); !ok || len(definitions) != 1 {
		t.Fatalf("provider=%T definitions=%d", provider, len(definitions))
	}
	if _, err := provider.Execute(context.Background(), "note_skill", map[string]any{}); err == nil || calls != 0 {
		t.Fatalf("missing topic ran: err=%v calls=%d", err, calls)
	}
	value, err := provider.Execute(context.Background(), "note_skill", map[string]any{"topic": "笔记"})
	if err != nil || value != "已整理" || calls != 1 {
		t.Fatalf("value=%v err=%v calls=%d", value, err, calls)
	}
}

func TestHopDynamicRejectsUngovernedWithoutLookup(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	for _, name := range []string{"manage_skill", "call_mcp_tool", "run_skill", "mcp_demo_search"} {
		if err := handler.registry.Register(RegisteredTool{Name: name, Status: RegToolAvailable}); err != nil {
			t.Fatal(err)
		}
	}
	looked := false
	handler.hopDynamicLookup = func(context.Context, string) (hopDynamicBinding, bool, error) {
		looked = true
		return hopDynamicBinding{}, false, nil
	}
	identity := hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"}
	for _, name := range []string{"manage_skill", "call_mcp_tool", "run_skill", "mcp_demo_search"} {
		_, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "1", Tools: []string{name}}, identity)
		if err == nil || !strings.Contains(err.Error(), "unavailable or ungoverned") || looked {
			t.Fatalf("name=%s err=%v looked=%v", name, err, looked)
		}
	}
}

func TestHopDynamicMCPPlanPausesForReceipt(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	handler.hopDynamicLookup = func(_ context.Context, name string) (hopDynamicBinding, bool, error) {
		switch name {
		case "clock__now":
			return hopDynamicBinding{
				Name: "clock__now", Description: "当前时间", Kind: "mcp", ProviderID: "clock", ImplementationID: "now",
				Provisions: []tool.CapabilityProvision{{Capability: "information.current_time"}},
				Effects:    []tool.EffectClass{tool.EffectReadOnly},
				Plan: func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult {
					return tool.SelectionExecutionResult{Result: "12:00", Succeeded: true}
				},
			}, true, nil
		case "clock__send":
			return hopDynamicBinding{
				Name: "clock__send", Description: "送出", Kind: "mcp", ProviderID: "clock", ImplementationID: "send",
				Provisions: []tool.CapabilityProvision{{Capability: "information.current_time"}},
				Effects:    []tool.EffectClass{tool.EffectReadOnly},
				Plan: func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult {
					return tool.SelectionExecutionResult{Result: "等待回执", AwaitingReceipt: true}
				},
			}, true, nil
		case "clock__stale":
			return hopDynamicBinding{
				Name: "clock__stale", Description: "过期", Kind: "mcp", ProviderID: "clock", ImplementationID: "stale",
				Provisions: []tool.CapabilityProvision{{Capability: "information.current_time"}},
				Effects:    []tool.EffectClass{tool.EffectReadOnly},
				Plan: func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult {
					return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
				},
			}, true, nil
		default:
			return hopDynamicBinding{}, false, nil
		}
	}
	identity := hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"}
	ready, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "1", Tools: []string{"clock__now"}}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ready.(*tool.HopJITPlanToolProvider); !ok {
		t.Fatalf("provider=%T", ready)
	}
	value, err := ready.Execute(context.Background(), "clock__now", nil)
	if err != nil || value != "12:00" {
		t.Fatalf("value=%v err=%v", value, err)
	}
	paused, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "2", Tools: []string{"clock__send"}}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := paused.Execute(context.Background(), "clock__send", nil); err == nil || !strings.Contains(err.Error(), "hopjit_tool_awaiting_receipt") {
		t.Fatalf("err=%v", err)
	}
	stale, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "3", Tools: []string{"clock__stale"}}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stale.Execute(context.Background(), "clock__stale", nil); err == nil || !strings.Contains(err.Error(), "dynamic_binding_stale") {
		t.Fatalf("err=%v", err)
	}
}

func TestHopNamedToolAttachesOnlyAnExactMatch(t *testing.T) {
	note := []hopGovernedTool{{Name: "note_skill", Aliases: []string{"skill_note_id"}}}
	spec := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 整理\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	if got := prepareCompiledHopMarkdownNamed(spec, "整理笔记", note); strings.Contains(got, "note_skill") || strings.Contains(got, "web_search") {
		t.Fatalf("unnamed tool attached: %s", got)
	}
	named := prepareCompiledHopMarkdownNamed(spec, "用 note_skill 整理笔记", note)
	if strings.Contains(named, "web_search") || !strings.Contains(named, "工具: note_skill") || !strings.Contains(named, "[act]") {
		t.Fatalf("named=%s", named)
	}
	if err := compiledHopSpecErrorNamed(named, "用 note_skill 整理笔记", note); err != nil {
		t.Fatal(err)
	}
	alias := prepareCompiledHopMarkdownNamed(spec, "用 skill_note_id 整理笔记", note)
	if strings.Contains(alias, "web_search") || !strings.Contains(alias, "工具: note_skill") {
		t.Fatalf("alias=%s", alias)
	}
	covered := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [act] 整理\n  + → report % text\n  ```hop_python\n  report = note_skill(topic: \"笔记\")\n  ```\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	kept := prepareCompiledHopMarkdownNamed(covered, "用 note_skill 整理笔记", note)
	if !strings.Contains(kept, "report = note_skill(topic: \"笔记\")") || !strings.Contains(kept, "工具: note_skill") || strings.Contains(kept, "web_search") {
		t.Fatalf("covered body dropped: %s", kept)
	}
	literal := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [act] 整理\n  + → report % text\n  ```hop_python\n  report = \"原文\"\n  ```\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	dropped := prepareCompiledHopMarkdownNamed(literal, "用 note_skill 整理笔记", note)
	if strings.Contains(dropped, "hop_python") || strings.Contains(dropped, "原文") || !strings.Contains(dropped, "工具: note_skill") {
		t.Fatalf("literal body kept: %s", dropped)
	}
	both := []hopGovernedTool{{Name: "note_skill"}, {Name: "other_skill"}}
	if got := prepareCompiledHopMarkdownNamed(spec, "用 note_skill 和 other_skill 整理笔记", both); strings.Contains(got, "工具:") {
		t.Fatalf("guessed: %s", got)
	}
	if got := prepareCompiledHopMarkdownNamed(spec, "用 note_skill_extra 整理笔记", note); strings.Contains(got, "工具: note_skill") {
		t.Fatalf("prefix matched: %s", got)
	}
	mcp := []hopGovernedTool{{Name: "tengyun__post_work_log_add", Aliases: []string{"post_work_log_add"}}}
	logged := prepareCompiledHopMarkdownNamed(spec, "调用 post_work_log_add 记录笔记", mcp)
	if !strings.Contains(logged, "工具: tengyun__post_work_log_add") || strings.Contains(logged, "web_search") {
		t.Fatalf("mcp alias=%s", logged)
	}
}

func TestHopNamedLookupReplacesWebSearch(t *testing.T) {
	tools := []hopGovernedTool{{Name: "weather_skill"}}
	spec := "# Spec: 天气\nGoal: 查询北京天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	plain := prepareCompiledHopMarkdownNamed(spec, "北京天气", tools)
	if prepareCompiledHopMarkdown(spec, "北京天气") != prepareCompiledHopMarkdownNamed(spec, "北京天气", nil) {
		t.Fatal("nil tools changed lookup repair")
	}
	if !strings.Contains(plain, "工具: web_search") || strings.Contains(plain, "weather_skill") {
		t.Fatalf("unnamed lookup: %s", plain)
	}
	named := prepareCompiledHopMarkdownNamed(spec, "用 weather_skill 查询北京天气", tools)
	if strings.Contains(named, "web_search") || !strings.Contains(named, "工具: weather_skill") || !strings.Contains(named, "[act]") {
		t.Fatalf("named lookup: %s", named)
	}
	if err := liveLookupToolsRequired(named, "用 weather_skill 查询北京天气"); err == nil {
		t.Fatal("skill tool satisfied the web_search coverage check")
	}
	if err := liveLookupToolsRequiredNamed(named, "用 weather_skill 查询北京天气", tools); err != nil {
		t.Fatal(err)
	}
	if again := prepareCompiledHopMarkdownNamed(named, "用 weather_skill 查询北京天气", tools); again != named {
		t.Fatalf("not stable:\n%s\n---\n%s", named, again)
	}
	hyphen := []hopGovernedTool{{Name: "weather-skill"}}
	extra := prepareCompiledHopMarkdownNamed(spec, "用 weather-skill-extra 查价格", hyphen)
	if strings.Contains(extra, "工具: weather-skill") || !strings.Contains(extra, "工具: web_search") {
		t.Fatalf("hyphen prefix: %s", extra)
	}
	exact := prepareCompiledHopMarkdownNamed(spec, "用 weather-skill 查价格", hyphen)
	if strings.Contains(exact, "web_search") || !strings.Contains(exact, "工具: weather-skill") {
		t.Fatalf("hyphen exact: %s", exact)
	}
}

func TestCompileHopSpecAttachesNamedSkill(t *testing.T) {
	caller := &guiHopJITCaller{
		governedTools: []hopGovernedTool{{Name: "note_skill"}},
		invoke: func(context.Context, []interface{}, bool) (string, error) {
			return "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 整理\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n", nil
		},
	}
	spec, err := compileHopSpec(context.Background(), "用 note_skill 整理笔记", caller)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spec, "web_search") || !strings.Contains(spec, "工具: note_skill") || !strings.Contains(spec, "[act]") {
		t.Fatalf("spec=%s", spec)
	}
}

func TestHopNamedLookupKeepsCoveringSkillBody(t *testing.T) {
	tools := []hopGovernedTool{{Name: "weather_skill"}}
	spec := "# Spec: 天气\nGoal: 查询北京天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n  ```hop_python\n  report = weather_skill(city: \"北京\")\n  ```\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	got := prepareCompiledHopMarkdownNamed(spec, "用 weather_skill 查询北京天气", tools)
	if !strings.Contains(got, "report = weather_skill(city: \"北京\")") || !strings.Contains(got, "工具: weather_skill") || strings.Contains(got, "web_search") {
		t.Fatalf("covering lookup body dropped: %s", got)
	}
	if err := liveLookupToolsRequired(got, "用 weather_skill 查询北京天气"); err == nil {
		t.Fatal("skill assignment satisfied the web_search coverage check")
	}
	if err := liveLookupToolsRequiredNamed(got, "用 weather_skill 查询北京天气", tools); err != nil {
		t.Fatal(err)
	}
	if again := prepareCompiledHopMarkdownNamed(got, "用 weather_skill 查询北京天气", tools); again != got {
		t.Fatalf("not stable:\n%s\n---\n%s", got, again)
	}
	dup := []hopGovernedTool{{Name: "weather_skill"}, {Name: "weather_skill"}}
	ambiguous := prepareCompiledHopMarkdownNamed(spec, "用 weather_skill 查询北京天气", dup)
	if strings.Contains(ambiguous, "工具: weather_skill") || !strings.Contains(ambiguous, "工具: web_search") {
		t.Fatalf("duplicate name attached: %s", ambiguous)
	}
}

func TestHopNamedLookupLeavesCheckCallInPlace(t *testing.T) {
	tools := []hopGovernedTool{{Name: "weather_skill"}}
	spec := "# Spec: 天气\nGoal: 查询北京天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 记录\n  + → report % text\n  + → accepted % bool\n  ```hop_python\n  report = weather_skill(city: \"北京\")\n  ```\n3. [exit]\n"
	got := prepareCompiledHopMarkdownNamed(spec, "用 weather_skill 查询北京天气", tools)
	parsed, _ := hopspec.Parse(got)
	var checkReport, actTool bool
	for _, step := range parsed.AllSteps() {
		if step.Type == hopspec.StepCheck && stepWritesOutput(step, "report") && strings.Contains(step.Body, "weather_skill(city:") {
			checkReport = true
		}
		if step.Type == hopspec.StepAct && hopStepListsTool(step, "weather_skill") {
			actTool = true
		}
	}
	if !checkReport || actTool || !strings.Contains(got, "report = weather_skill(city: \"北京\")") {
		t.Fatalf("check call moved check=%v act=%v\n%s", checkReport, actTool, got)
	}
	err := compiledHopSpecErrorNamed(got, "用 weather_skill 查询北京天气", tools)
	if err == nil || !strings.Contains(err.Error(), "LIVE_LOOKUP_TOOLS_REQUIRED") || !compiledHopSpecShouldRetry(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestHopNamedDuplicateDoesNotAttach(t *testing.T) {
	spec := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 整理\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n"
	dup := []hopGovernedTool{{Name: "note_skill"}, {Name: "note_skill"}}
	got := prepareCompiledHopMarkdownNamed(spec, "用 note_skill 整理笔记", dup)
	if strings.Contains(got, "工具: note_skill") || strings.Contains(got, "web_search") {
		t.Fatalf("duplicate attached: %s", got)
	}
	if err := compiledHopSpecErrorNamed(got, "用 note_skill 整理笔记", dup); err != nil {
		t.Fatal(err)
	}
}

func TestHopNamedCheckOnlyWriterNeedsRewrite(t *testing.T) {
	note := []hopGovernedTool{{Name: "note_skill"}}
	only := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [check] 写出\n  + → report % text\n  + → accepted % bool\n2. [exit]\n"
	got := prepareCompiledHopMarkdownNamed(only, "用 note_skill 整理笔记", note)
	if strings.Contains(got, "工具: note_skill") || strings.Contains(got, "[act]") {
		t.Fatalf("invented an act: %s", got)
	}
	err := compiledHopSpecErrorNamed(got, "用 note_skill 整理笔记", note)
	if err == nil || !strings.Contains(err.Error(), "NAMED_HOP_TOOL_UNATTACHED") || !compiledHopSpecShouldRetry(err) {
		t.Fatalf("err=%v", err)
	}
	moved := "# Spec: 笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 起草\n2. [check] 写出\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	got = prepareCompiledHopMarkdownNamed(moved, "用 note_skill 整理笔记", note)
	parsed, _ := hopspec.Parse(got)
	var actReport, checkReport, listed bool
	for _, step := range parsed.AllSteps() {
		switch step.Type {
		case hopspec.StepAct:
			if hopStepListsTool(step, "note_skill") {
				listed = true
			}
			for _, out := range step.Outputs {
				if out.Name == "report" {
					actReport = true
				}
			}
		case hopspec.StepCheck:
			for _, out := range step.Outputs {
				if out.Name == "report" {
					checkReport = true
				}
			}
		}
	}
	if !listed || !actReport || checkReport || strings.Contains(got, "web_search") {
		t.Fatalf("move failed listed=%v act=%v check=%v\n%s", listed, actReport, checkReport, got)
	}
	if err := compiledHopSpecErrorNamed(got, "用 note_skill 整理笔记", note); err != nil {
		t.Fatal(err)
	}
}

func TestHopDynamicMCPPlanPinsAutoCapability(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	id := agentservice.AutoMCPCapabilityIDForBinding("clock", "now")
	if id == "" {
		t.Fatal("missing auto capability")
	}
	handler.hopDynamicLookup = func(_ context.Context, name string) (hopDynamicBinding, bool, error) {
		if name != "clock__now" {
			return hopDynamicBinding{}, false, nil
		}
		return hopDynamicBinding{
			Name: "clock__now", Description: "当前时间", Kind: "mcp", ProviderID: "clock", ImplementationID: "now",
			Provisions: []tool.CapabilityProvision{
				{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}},
				{Capability: id},
			},
			Effects: []tool.EffectClass{tool.EffectExternalEffect},
			Plan: func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult {
				return tool.SelectionExecutionResult{Result: "12:00", Succeeded: true}
			},
		}, true, nil
	}
	identity := hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"}
	ready, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "1", Tools: []string{"clock__now"}}, identity)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ready.Execute(context.Background(), "clock__now", nil)
	if err != nil || value != "12:00" {
		t.Fatalf("value=%v err=%v", value, err)
	}
	handler.hopDynamicLookup = func(_ context.Context, name string) (hopDynamicBinding, bool, error) {
		return hopDynamicBinding{
			Name: "clock__now", Description: "当前时间", Kind: "mcp", ProviderID: "clock", ImplementationID: "now",
			Provisions: []tool.CapabilityProvision{
				{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}},
				{Capability: id},
			},
			Effects: []tool.EffectClass{tool.EffectSensitive},
			Plan: func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult {
				return tool.SelectionExecutionResult{Result: "12:00", Succeeded: true}
			},
		}, true, nil
	}
	if _, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "2", Tools: []string{"clock__now"}}, identity); err == nil {
		t.Fatal("sensitive effect planned past the namespace ceiling")
	}
}
