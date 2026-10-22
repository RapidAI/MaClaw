package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestLearnedSkillTaskFromHostPromptKeepsTheTask(t *testing.T) {
	plain := learnedSkillTaskFromHostPrompt("deploy the application")
	if plain != "deploy the application" {
		t.Fatalf("plain task = %q", plain)
	}

	plan := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"[Plan step T2/6] Fix the login check",
		"",
		"Reject expired tokens.",
		"",
		"You are executing plan step T2/6: Fix the login check",
		"## Overall user request",
		"do everything",
		"[Session continuity turn 2]",
		"Session plan / overall goal:",
		"Ship auth",
	}, "\n"))
	if plan != "Fix the login check\nReject expired tokens." {
		t.Fatalf("plan task = %q", plan)
	}

	coding := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"请执行以下编码任务：",
		"",
		"## T1: Fix the login check",
		"",
		"Reject expired tokens.",
		"",
		"**Before editing**:",
		"1. Use code_navigation",
	}, "\n"))
	if coding != "Fix the login check\nReject expired tokens." {
		t.Fatalf("coding task = %q", coding)
	}

	carried := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"rebuild the index",
		"",
		"[Session continuity turn 2]",
		"Session plan / overall goal:",
		"Ship auth",
	}, "\n"))
	if carried != "rebuild the index" {
		t.Fatalf("carried task = %q", carried)
	}

	// U+0130 lowercases to a different byte length. The cut must still use
	// an index into the original prompt.
	withDot := learnedSkillTaskFromHostPrompt("列出 İ 目录\n\n**Before editing**:\n1. Use code_navigation")
	if withDot != "列出 İ 目录" {
		t.Fatalf("case-fold task = %q", withDot)
	}
	planDot := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"[Plan step T1/2] 列出目录",
		"",
		"目录 İ 的内容",
		"",
		"You are executing plan step T1/2: 列出目录",
	}, "\n"))
	if planDot != "列出目录\n目录 İ 的内容" {
		t.Fatalf("plan case-fold task = %q", planDot)
	}

	outline := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"写一份 plan outline 给发布说明",
		"",
		"Plan outline (titles only; later steps are separate remote tasks — do not execute them now):",
		"- T2: 别的",
	}, "\n"))
	if outline != "写一份 plan outline 给发布说明" {
		t.Fatalf("outline task = %q", outline)
	}

	planOutline := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"[Plan step T1/2] 写发布说明",
		"",
		"写一份 plan outline 给发布说明",
		"",
		"You are executing plan step T1/2: 写发布说明",
		"Plan outline (titles only; later steps are separate remote tasks — do not execute them now):",
		"- T2: 别的",
	}, "\n"))
	if planOutline != "写发布说明\n写一份 plan outline 给发布说明" {
		t.Fatalf("plan outline task = %q", planOutline)
	}

	truncated := learnedSkillTaskFromHostPrompt("请执行以下编码任务： ## T1: 生成helloworld c++版本 生成helloworld c++版本 **当前…")
	if truncated != "生成helloworld c++版本 生成helloworld c++版本" {
		t.Fatalf("truncated trailer task = %q", truncated)
	}
	prose := learnedSkillTaskFromHostPrompt("写一份 plan outline…")
	if prose != "写一份 plan outline…" {
		t.Fatalf("prose ellipsis task = %q", prose)
	}
}

func TestLearnedSkillIdentityTriggersDropTheHostWrapper(t *testing.T) {
	const stored = "请执行以下编码任务： ## T1: 生成helloworld c++版本 生成helloworld c++版本 **当前…"
	storedTriggers := []string{"请执行以下编码任务：", "##", "t1:", "生成helloworld", "c++版本"}
	kept := learnedSkillIdentityTriggers(stored, storedTriggers)
	if strings.Join(kept, "|") != "生成helloworld|c++版本" {
		t.Fatalf("identity triggers = %#v", kept)
	}
	learned := NLSkillDefinition{
		Name: "craft_generate_hello_world_cpp", Source: "learned", Description: stored, Triggers: storedTriggers,
	}
	if _, identity, _ := preferredLocalSkillScore(learned, strings.ToLower("请把这段说明改成 ## 标题")); identity {
		t.Fatal("wrapper trigger counted as identity")
	}
	if _, identity, score := preferredLocalSkillScore(learned, strings.ToLower("生成helloworld 的程序")); !identity || score < 10 {
		t.Fatalf("task trigger identity=%v score=%d", identity, score)
	}
	market := learned
	market.Source = "market"
	if _, identity, _ := preferredLocalSkillScore(market, strings.ToLower("请把这段说明改成 ## 标题")); !identity {
		t.Fatal("hand-written trigger was dropped")
	}
}

func TestLearnedSkillTaskFromHostPromptDropsScaffoldWithNoTask(t *testing.T) {
	scaffold := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"请执行以下编码任务：",
		"",
		"**Before editing**:",
		"1. Use code_navigation",
	}, "\n"))
	if scaffold != "" {
		t.Fatalf("scaffold task = %q, want empty", scaffold)
	}
	continuity := learnedSkillTaskFromHostPrompt(strings.Join([]string{
		"[Session continuity turn 2]",
		"Session plan / overall goal:",
		"Ship auth",
	}, "\n"))
	if continuity != "" {
		t.Fatalf("continuity task = %q, want empty", continuity)
	}
}

func TestBindLearnedSkillOperandsSharesALiteral(t *testing.T) {
	steps := []skill.SkillYAMLStep{
		{Action: "bash", Params: map[string]interface{}{
			"command":     `Get-ChildItem C:\Users\ma139\.maclaw`,
			"working_dir": `C:\Users\ma139\.maclaw`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": "printf see https://example.com/a and ma139@example.com",
		}},
	}
	bound, params, required, err := bindLearnedSkillOperands(steps)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	command, _ := bound[0].Params["command"].(string)
	work, _ := bound[0].Params["working_dir"].(string)
	if command != "Get-ChildItem {{path_1}}" || work != "{{path_1}}" {
		t.Fatalf("path binding command=%q work=%q", command, work)
	}
	second, _ := bound[1].Params["command"].(string)
	if second != "printf see {{url_1}} and {{email_1}}" {
		t.Fatalf("second command = %q", second)
	}
	if strings.Join(required, ",") != "path_1,url_1,email_1" {
		t.Fatalf("required = %#v", required)
	}
	if len(params) != 3 || !params[0].Required || params[0].Name != "path_1" {
		t.Fatalf("params = %#v", params)
	}
	for _, step := range bound {
		if learnedSkillValueHasSessionOperand(step.Params) {
			t.Fatalf("bound step still has a session operand: %#v", step.Params)
		}
	}
}

func TestBindLearnedSkillOperandsJoinsASpacedPath(t *testing.T) {
	steps := []skill.SkillYAMLStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `Get-ChildItem "C:\Program Files\app"`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `Get-ChildItem 'C:\Program Files'`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `Get-ChildItem C:\Program Files\app -Force`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `Get-ChildItem C:\Program Files -Force`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `python -c "print(r'C:\Users\ma139\.maclaw')"`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `Copy-Item C:\Program Files\app ..\backup`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `curl https://example.com/a out/dir`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `Get-ChildItem C:\Program Files\.hidden\app`,
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": `python -c "print(\"C:\Program Files\app\")"`,
		}},
	}
	bound, _, required, err := bindLearnedSkillOperands(steps)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	got := make([]string, len(bound))
	for i, step := range bound {
		got[i], _ = step.Params["command"].(string)
		if learnedSkillValueHasSessionOperand(step.Params) {
			t.Fatalf("step %d still has a session operand: %q", i, got[i])
		}
	}
	want := []string{
		`Get-ChildItem "{{path_1}}"`,
		`Get-ChildItem '{{path_2}}'`,
		`Get-ChildItem {{path_1}} -Force`,
		`Get-ChildItem {{path_3}} Files -Force`,
		`python -c "print(r'{{path_4}}')"`,
		`Copy-Item {{path_1}} ..\backup`,
		`curl {{url_1}} out/dir`,
		`Get-ChildItem {{path_5}}`,
		`python -c "print(\"{{path_1}}\")"`,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command %d = %q, want %q", i, got[i], want[i])
		}
	}
	if strings.Join(required, ",") != "path_1,path_2,path_3,path_4,url_1,path_5" {
		t.Fatalf("required = %#v", required)
	}
}

func TestBindLearnedSkillOperandsDoesNotCrossANewline(t *testing.T) {
	steps := []skill.SkillYAMLStep{{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "Get-ChildItem C:\\Program\nFiles\\app -Force",
		},
	}}
	bound, _, required, err := bindLearnedSkillOperands(steps)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	command, _ := bound[0].Params["command"].(string)
	if command != "Get-ChildItem {{path_1}}\nFiles\\app -Force" {
		t.Fatalf("command = %q", command)
	}
	if strings.Join(required, ",") != "path_1" {
		t.Fatalf("required = %#v", required)
	}
}

func TestLearnedSkillMissingParameterContract(t *testing.T) {
	path := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"working_dir": `C:\Users\ma139\.maclaw\workspace\book`},
	}}
	if learnedSkillMissingParameterContract("manual", path) {
		t.Fatal("an installed skill may name a path its author wrote")
	}
	if !learnedSkillMissingParameterContract("learned", path) {
		t.Fatal("a learned skill still carrying that path has no parameter contract")
	}
	bound := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"working_dir": "{{path_1}}", "command": "echo copy"},
	}}
	if learnedSkillMissingParameterContract("learned", bound) {
		t.Fatal("a placeholder is the parameter contract")
	}
	plain := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "echo copy"},
	}}
	if learnedSkillMissingParameterContract("learned", plain) {
		t.Fatal("a fixed command is a complete learned skill")
	}
	described := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "echo copy"},
	}}
	if learnedSkillMissingParameterContract("learned", described) {
		t.Fatal("a redacted <路径> in the description is not a step operand")
	}
}
