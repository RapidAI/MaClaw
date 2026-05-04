package skill

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNormalizeStepForRunner_ActionAndParamAliases(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{
			"cmd":             "echo {{message}}",
			"cwd":             "scripts",
			"timeout_seconds": "45",
		},
	}

	got := NormalizeStepForRunner(step, `C:\skills\demo`)
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if got.Params["command"] != "echo {{message}}" {
		t.Fatalf("command alias was not normalized: %#v", got.Params["command"])
	}
	if got.Params["working_dir"] != "scripts" {
		t.Fatalf("working_dir alias was not normalized: %#v", got.Params["working_dir"])
	}
	if got.Params["timeout"] != float64(45) {
		t.Fatalf("timeout was not normalized to float64: %#v", got.Params["timeout"])
	}
}

func TestNormalizeStepForRunner_LanguageActions(t *testing.T) {
	skillDir := filepath.Join("tmp", "skill")
	step := corelib.NLSkillStep{
		Action: "python3",
		Params: map[string]interface{}{
			"script": "./scripts/run.py",
			"args":   []interface{}{"hello world", "out.txt"},
		},
	}

	got := NormalizeStepForRunner(step, skillDir)
	cmd, _ := got.Params["command"].(string)
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if !strings.HasPrefix(cmd, "python ") {
		t.Fatalf("command = %q, want python runtime", cmd)
	}
	normalizedCmd := strings.ReplaceAll(cmd, `\\`, `\`)
	if !strings.Contains(normalizedCmd, filepath.Clean(filepath.Join(skillDir, "scripts", "run.py"))) {
		t.Fatalf("relative script was not resolved against skill dir: %q", cmd)
	}
	if !strings.Contains(cmd, `"hello world"`) {
		t.Fatalf("args were not shell-quoted/appended: %q", cmd)
	}
}

func TestResolveStep_NormalizesCommunityShapeBeforeBinding(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "shell",
		Params: map[string]interface{}{"cmd": "echo {{message}}"},
	}

	result, err := ResolveStep(step, map[string]string{"message": "ok"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep error: %v", err)
	}
	if result.Step.Action != "bash" {
		t.Fatalf("Action = %q, want bash", result.Step.Action)
	}
	if got := result.Step.Params["command"]; got != "echo ok" {
		t.Fatalf("command = %#v, want echo ok", got)
	}
}
func TestNormalizeStepForRunner_ExecutionParamCompatibility(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":         "echo $TOKEN",
			"env":             map[string]string{"TOKEN": "secret"},
			"required_env":    "OPENAI_API_KEY, GITHUB_TOKEN",
			"timeout_seconds": 30,
		},
	}

	got := NormalizeStepForRunner(step, "")
	if got.Params["timeout"] != float64(30) {
		t.Fatalf("timeout = %#v, want float64(30)", got.Params["timeout"])
	}
	extraEnv, ok := got.Params["extra_env"].(map[string]interface{})
	if !ok || extraEnv["TOKEN"] != "secret" {
		t.Fatalf("extra_env was not normalized: %#v", got.Params["extra_env"])
	}
	requiredEnv, ok := got.Params["required_env"].([]interface{})
	if !ok || len(requiredEnv) != 2 || requiredEnv[0] != "OPENAI_API_KEY" || requiredEnv[1] != "GITHUB_TOKEN" {
		t.Fatalf("required_env was not normalized: %#v", got.Params["required_env"])
	}
}

func TestNormalizeStepForRunner_DoesNotDoubleWrapInterpreterCommand(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "python3",
		Params: map[string]interface{}{"command": "python3 scripts/run.py --flag"},
	}

	got := NormalizeStepForRunner(step, "")
	cmd, _ := got.Params["command"].(string)
	if strings.Count(cmd, "python") != 1 || strings.HasPrefix(cmd, "python \"python3") {
		t.Fatalf("interpreter command was double-wrapped: %q", cmd)
	}
}

func TestNormalizeStepForRunner_PollParamCompatibility(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "poll",
		Params: map[string]interface{}{
			"cmd":      "check-ready",
			"interval": "2",
			"match":    "READY",
		},
	}

	got := NormalizeStepForRunner(step, "")
	if got.Params["command"] != "check-ready" {
		t.Fatalf("poll command alias was not normalized: %#v", got.Params["command"])
	}
	if got.Params["interval_seconds"] != float64(2) {
		t.Fatalf("poll interval was not normalized: %#v", got.Params["interval_seconds"])
	}
	if got.Params["success_pattern"] != "READY" {
		t.Fatalf("poll success pattern was not normalized: %#v", got.Params["success_pattern"])
	}
}

func TestNormalizeStepForRunner_ResolvesBareRelativeScriptPath(t *testing.T) {
	skillDir := filepath.Join("tmp", "skill")
	step := corelib.NLSkillStep{
		Action: "python",
		Params: map[string]interface{}{"script": "scripts/run.py"},
	}

	got := NormalizeStepForRunner(step, skillDir)
	cmd, _ := got.Params["command"].(string)
	normalizedCmd := strings.ReplaceAll(cmd, `\\`, `\`)
	if !strings.Contains(normalizedCmd, filepath.Clean(filepath.Join(skillDir, "scripts", "run.py"))) {
		t.Fatalf("bare relative script path was not resolved: %q", cmd)
	}
}

func TestNormalizeStepForRunner_ResolvesFirstCommandTokenScriptPath(t *testing.T) {
	skillDir := filepath.Join("tmp", "skill")
	step := corelib.NLSkillStep{
		Action: "shell",
		Params: map[string]interface{}{"cmd": "scripts/run.py --format json"},
	}

	got := NormalizeStepForRunner(step, skillDir)
	cmd, _ := got.Params["command"].(string)
	normalizedCmd := strings.ReplaceAll(cmd, `\\`, `\`)
	if !strings.Contains(normalizedCmd, filepath.Clean(filepath.Join(skillDir, "scripts", "run.py"))) {
		t.Fatalf("first command token was not resolved: %q", cmd)
	}
	if !strings.Contains(cmd, "--format json") {
		t.Fatalf("command arguments were not preserved: %q", cmd)
	}
}

func TestNormalizeStepForRunner_LanguageCodeWithoutAction(t *testing.T) {
	step := corelib.NLSkillStep{
		Params: map[string]interface{}{
			"language": "python",
			"code":     "print(\"ok\")",
		},
	}

	got := NormalizeStepForRunner(step, "")
	cmd, _ := got.Params["command"].(string)
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if !strings.HasPrefix(cmd, "python -c ") || !strings.Contains(cmd, "print") {
		t.Fatalf("language/code step was not converted to python -c command: %q", cmd)
	}
}

func TestNormalizeStepForRunner_CommandArrayCompatibility(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{
			"command": []interface{}{"python", "scripts/run.py", "hello world"},
		},
	}

	got := NormalizeStepForRunner(step, "")
	cmd, _ := got.Params["command"].(string)
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if !strings.HasPrefix(cmd, "python ") || !strings.Contains(cmd, "\"hello world\"") {
		t.Fatalf("command array was not normalized/quoted: %q", cmd)
	}
}

func TestNormalizeStepForRunner_CommandObjectCompatibility(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "execute",
		Params: map[string]interface{}{
			"command": map[string]interface{}{
				"program": "node",
				"args":    []interface{}{"scripts/run.mjs", "hello world"},
			},
		},
	}

	got := NormalizeStepForRunner(step, "")
	cmd, _ := got.Params["command"].(string)
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if !strings.HasPrefix(cmd, "node ") || !strings.Contains(cmd, "\"hello world\"") {
		t.Fatalf("command object was not normalized/quoted: %q", cmd)
	}
}

func TestNormalizeStepForRunner_ShellPreferenceDoesNotBecomeCommand(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "shell",
		Params: map[string]interface{}{
			"shell": "bash",
			"cmd":   "echo ok",
		},
	}

	got := NormalizeStepForRunner(step, "")
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if got.Params["command"] != "echo ok" {
		t.Fatalf("command = %#v, want echo ok", got.Params["command"])
	}
	if got.Params["preferred_shell"] != "bash" {
		t.Fatalf("preferred_shell = %#v, want bash", got.Params["preferred_shell"])
	}
}

func TestNormalizeStepForRunner_ShellParamCanStillBeCommand(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{"shell": "echo from-shell-param"},
	}

	got := NormalizeStepForRunner(step, "")
	if got.Action != "bash" {
		t.Fatalf("Action = %q, want bash", got.Action)
	}
	if got.Params["command"] != "echo from-shell-param" {
		t.Fatalf("shell command alias was not preserved: %#v", got.Params["command"])
	}
}

func TestParseSkillYAMLFile_TopLevelStepParamsCompatibility(t *testing.T) {
	data := []byte(`name: compat
requires_env: API_TOKEN
preferred_shell: powershell
steps:
  - action: run
    command: [python, scripts/run.py, hello world]
    cwd: scripts
    shell: bash
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.RequiredEnv) != 1 || sf.RequiredEnv[0] != "API_TOKEN" {
		t.Fatalf("required env alias was not normalized: %#v", sf.RequiredEnv)
	}
	if sf.PreferredShell != "powershell" {
		t.Fatalf("preferred_shell alias was not normalized: %q", sf.PreferredShell)
	}
	if len(sf.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(sf.Steps))
	}
	if sf.Steps[0].Params["command"] == nil || sf.Steps[0].Params["cwd"] != "scripts" || sf.Steps[0].Params["shell"] != "bash" {
		t.Fatalf("top-level step params were not preserved: %#v", sf.Steps[0].Params)
	}

	step := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[0].Action, Params: sf.Steps[0].Params}, "")
	cmd, _ := step.Params["command"].(string)
	if step.Action != "bash" || !strings.Contains(cmd, "\"hello world\"") || step.Params["preferred_shell"] != "bash" {
		t.Fatalf("parsed step did not normalize for execution: action=%q command=%q params=%#v", step.Action, cmd, step.Params)
	}
}

func TestParseSkillYAMLFile_TopLevelCommandBecomesExecutableStep(t *testing.T) {
	data := []byte(`name: top-command
description: command-only skill
command: echo hello
env:
  API_TOKEN: value
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 1 {
		t.Fatalf("steps = %#v, want one synthesized step", sf.Steps)
	}
	if sf.Steps[0].Action != "run" || sf.Steps[0].Params["command"] != "echo hello" {
		t.Fatalf("step = %#v, want run command", sf.Steps[0])
	}
	if len(sf.RequiredEnv) != 1 || sf.RequiredEnv[0] != "API_TOKEN" {
		t.Fatalf("required env from env map = %#v, want [API_TOKEN]", sf.RequiredEnv)
	}
}

func TestParseSkillYAMLFile_ToleratesMapParamSchemaAndStringLists(t *testing.T) {
	data := []byte(`name: compat-schema
required_env: API_TOKEN, OTHER_TOKEN
params:
  input:
    desc: Input file
    aliases: [src]
    flag: --input
    required: true
  output: Output file
steps:
  - action: run
    command: echo {{input}} {{output}}
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.RequiredEnv) != 2 || sf.RequiredEnv[0] != "API_TOKEN" || sf.RequiredEnv[1] != "OTHER_TOKEN" {
		t.Fatalf("required_env string was not normalized: %#v", sf.RequiredEnv)
	}
	if len(sf.Params) != 2 {
		t.Fatalf("params = %#v, want 2 entries", sf.Params)
	}
	byName := map[string]SkillYAMLParam{}
	for _, param := range sf.Params {
		byName[param.Name] = param
	}
	if byName["input"].Description != "Input file" || byName["input"].CLIFlag != "--input" || !byName["input"].Required || len(byName["input"].Aliases) != 1 || byName["input"].Aliases[0] != "src" {
		t.Fatalf("input param was not normalized: %#v", byName["input"])
	}
	if byName["output"].Description != "Output file" {
		t.Fatalf("output param was not normalized: %#v", byName["output"])
	}
}

func TestParseSkillYAMLFile_ToleratesJSONSchemaInputSchema(t *testing.T) {
	data := []byte(`name: compat-json-schema
input_schema:
  type: object
  required: [input]
  properties:
    input:
      description: Input path
    format:
      description: Output format
      default: pdf
steps:
  - action: run
    command: echo ok
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Params) != 2 {
		t.Fatalf("params = %#v, want 2 entries", sf.Params)
	}
	byName := map[string]SkillYAMLParam{}
	for _, param := range sf.Params {
		byName[param.Name] = param
	}
	if !byName["input"].Required || byName["input"].Description != "Input path" {
		t.Fatalf("input schema param was not normalized: %#v", byName["input"])
	}
	if byName["format"].Default != "pdf" || byName["format"].Description != "Output format" {
		t.Fatalf("format schema param was not normalized: %#v", byName["format"])
	}
}

func TestParseSkillYAMLFile_ToleratesStringStepsAndWithParams(t *testing.T) {
	data := []byte(`name: compat-steps
steps:
  - echo hello
  - name: run-with
    command: node scripts/run.mjs
    with:
      input: file.txt
      format: json
  - prompt: Summarize {{input}}
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(sf.Steps))
	}
	first := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[0].Action, Params: sf.Steps[0].Params}, "")
	if first.Action != "bash" || first.Params["command"] != "echo hello" {
		t.Fatalf("string step was not normalized: action=%q params=%#v", first.Action, first.Params)
	}
	second := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[1].Action, Params: sf.Steps[1].Params}, "")
	if second.Action != "bash" || second.Params["input"] != "file.txt" || second.Params["format"] != "json" {
		t.Fatalf("with params were not merged: action=%q params=%#v", second.Action, second.Params)
	}
	third := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[2].Action, Params: sf.Steps[2].Params}, "")
	if third.Action != "craft_tool" || third.Params["instructions"] != "Summarize {{input}}" {
		t.Fatalf("prompt-only step was not normalized to craft_tool: action=%q params=%#v", third.Action, third.Params)
	}
}

func TestNormalizeStepForRunner_EnvCompatibilityAliases(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{
			"command":      "echo ok",
			"requires_env": "API_TOKEN, OTHER_TOKEN",
			"extra_env":    []interface{}{"TOKEN=secret", "MODE=test"},
		},
	}

	got := NormalizeStepForRunner(step, "")
	required, ok := got.Params["required_env"].([]interface{})
	if !ok || len(required) != 2 || required[0] != "API_TOKEN" || required[1] != "OTHER_TOKEN" {
		t.Fatalf("required env alias was not normalized: %#v", got.Params["required_env"])
	}
	extra, ok := got.Params["extra_env"].(map[string]interface{})
	if !ok || extra["TOKEN"] != "secret" || extra["MODE"] != "test" {
		t.Fatalf("extra_env assignments were not normalized: %#v", got.Params["extra_env"])
	}
}

func TestNormalizeStepForRunner_CommandObjectArgAliases(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "run",
		Params: map[string]interface{}{
			"command": map[string]interface{}{
				"program": "python",
				"argv":    []interface{}{"scripts/run.py", "hello world"},
			},
		},
	}

	got := NormalizeStepForRunner(step, "")
	cmd, _ := got.Params["command"].(string)
	if !strings.HasPrefix(cmd, "python ") || !strings.Contains(cmd, "\"hello world\"") {
		t.Fatalf("argv alias was not normalized into command args: %q", cmd)
	}
}

func TestParseSkillYAMLFile_ToleratesLooseParamListTypes(t *testing.T) {
	data := []byte(`name: loose-params
params:
  - name: input
    desc: Input path
    alias: src, file
    flag: --input
    required: "yes"
    default: 123
steps:
  - echo ok
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Params) != 1 {
		t.Fatalf("params = %#v, want 1", sf.Params)
	}
	param := sf.Params[0]
	if param.Name != "input" || param.Description != "Input path" || param.CLIFlag != "--input" || !param.Required || param.Default != "123" {
		t.Fatalf("loose param fields were not normalized: %#v", param)
	}
	if len(param.Aliases) != 2 || param.Aliases[0] != "src" || param.Aliases[1] != "file" {
		t.Fatalf("aliases were not normalized: %#v", param.Aliases)
	}
}

func TestNormalizeStepForRunner_MCPUsesAndWithArguments(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "call_tool",
		Params: map[string]interface{}{
			"uses": "browser/screenshot",
			"url":  "https://example.test",
			"full": true,
		},
	}

	got := NormalizeStepForRunner(step, "")
	if got.Action != "call_mcp_tool" {
		t.Fatalf("Action = %q, want call_mcp_tool", got.Action)
	}
	if got.Params["server_id"] != "browser" || got.Params["tool_name"] != "screenshot" {
		t.Fatalf("uses was not split into server/tool: %#v", got.Params)
	}
	args, ok := got.Params["arguments"].(map[string]interface{})
	if !ok || args["url"] != "https://example.test" || args["full"] != true {
		t.Fatalf("loose MCP args were not collected: %#v", got.Params["arguments"])
	}
}

func TestParseSkillYAMLFile_PreservesTopLevelTimeoutSeconds(t *testing.T) {
	data := []byte(`name: timeout-compat
steps:
  - action: run
    command: echo ok
    timeout_seconds: "45"
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 1 || sf.Steps[0].Params["timeout_seconds"] != "45" {
		t.Fatalf("timeout_seconds was not preserved in params: %#v", sf.Steps)
	}
	step := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[0].Action, Params: sf.Steps[0].Params}, "")
	if step.Params["timeout"] != float64(45) || step.Params["timeout_seconds"] != float64(45) {
		t.Fatalf("timeout_seconds was not normalized for runner: %#v", step.Params)
	}
}

func TestParseSkillYAMLFile_ToleratesStepControlFlowAliases(t *testing.T) {
	data := []byte(`name: control-flow
steps:
  - command: echo maybe
    if: "{{mode}} == run"
    continue: true
  - command: echo repair
    on_failure: true
    error_policy: ignore
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(sf.Steps))
	}
	if sf.Steps[0].When != "{{mode}} == run" || sf.Steps[0].OnError != "continue" {
		t.Fatalf("if/continue aliases were not normalized: %+v", sf.Steps[0])
	}
	if sf.Steps[1].Condition != "on_failure" || sf.Steps[1].OnError != "ignore" {
		t.Fatalf("on_failure/error_policy aliases were not preserved for runner normalization: %+v", sf.Steps[1])
	}
	step := NormalizeStepForRunner(corelib.NLSkillStep{Action: sf.Steps[1].Action, Params: sf.Steps[1].Params, OnError: sf.Steps[1].OnError}, "")
	if step.OnError != "continue" {
		t.Fatalf("on_error alias was not normalized for runner: %q", step.OnError)
	}
}

func TestNormalizeStepForRunner_OnErrorPolicyAliases(t *testing.T) {
	cases := map[string]string{
		"":                  "stop",
		"fail":              "stop",
		"ignore":            "continue",
		"warn":              "continue",
		"continue-on-error": "continue",
		"skip":              "skip",
	}
	for input, want := range cases {
		got := NormalizeStepForRunner(corelib.NLSkillStep{Action: "run", OnError: input, Params: map[string]interface{}{"command": "echo ok"}}, "")
		if got.OnError != want {
			t.Fatalf("OnError %q normalized to %q, want %q", input, got.OnError, want)
		}
	}
}

func TestParseSkillYAMLFile_ToleratesCapturePollLoopAliases(t *testing.T) {
	data := []byte(`name: structured-compat
steps:
  - command: echo token=abc
    capture:
      - name: token
        regex: token=(\w+)
    poll:
      pattern: READY
      retries: "3"
      every: "2"
    loop:
      iterations: "4"
      verify_step: check
      success_pattern: OK
      repair_step: fix
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(sf.Steps))
	}
	step := sf.Steps[0]
	if step.Capture["token"] != `token=(\w+)` {
		t.Fatalf("capture list was not normalized: %#v", step.Capture)
	}
	if step.Poll == nil || step.Poll.UntilMatch != "READY" || step.Poll.MaxAttempts != 3 || step.Poll.Interval != 2 {
		t.Fatalf("poll aliases were not normalized: %#v", step.Poll)
	}
	if step.Loop == nil || step.Loop.MaxIterations != 4 || step.Loop.UntilStep != "check" || step.Loop.UntilMatch != "OK" || step.Loop.OnFailStep != "fix" {
		t.Fatalf("loop aliases were not normalized: %#v", step.Loop)
	}
}

func TestParseSkillYAMLFile_ToleratesStringPoll(t *testing.T) {
	data := []byte(`name: string-poll
steps:
  - command: echo READY
    poll: READY
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if len(sf.Steps) != 1 || sf.Steps[0].Poll == nil || sf.Steps[0].Poll.UntilMatch != "READY" {
		t.Fatalf("string poll was not normalized: %#v", sf.Steps)
	}
}

func TestParseSkillYAMLFile_ToleratesOperationMapAndRequireAliases(t *testing.T) {
	data := []byte(`name: api-compat
requires_gui: "true"
global_timeout: "240"
pip: requests, beautifulsoup4
npm:
  - playwright
operations:
  generate:
    desc: Generate report
    steps: [create, verify]
    params: input, output
  query: status
steps:
  - label: create
    command: echo create
  - label: verify
    command: echo verify
  - label: status
    command: echo status
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if sf.Mode != "api_workflow" || !sf.RequiresGUI || sf.GlobalTimeout != 240 {
		t.Fatalf("mode/scalars were not normalized: mode=%q gui=%v timeout=%d", sf.Mode, sf.RequiresGUI, sf.GlobalTimeout)
	}
	if sf.Requires == nil || len(sf.Requires.Python) != 2 || sf.Requires.Python[0] != "requests" || len(sf.Requires.Node) != 1 || sf.Requires.Node[0] != "playwright" {
		t.Fatalf("requires aliases were not normalized: %#v", sf.Requires)
	}
	if len(sf.Operations) != 2 {
		t.Fatalf("operations = %#v, want 2", sf.Operations)
	}
	byName := map[string]SkillYAMLOperation{}
	for _, op := range sf.Operations {
		byName[op.Name] = op
	}
	if byName["generate"].Description != "Generate report" || len(byName["generate"].Labels) != 2 || byName["generate"].Labels[0] != "create" || len(byName["generate"].Params) != 2 {
		t.Fatalf("operation map was not normalized: %#v", byName["generate"])
	}
	if len(byName["query"].Labels) != 1 || byName["query"].Labels[0] != "status" {
		t.Fatalf("operation string label was not normalized: %#v", byName["query"])
	}
}

func TestParseSkillYAMLFile_ToleratesPipelineAliases(t *testing.T) {
	data := []byte(`name: pipeline-compat
pipeline:
  - extract
  - use: transform
    params: input=raw.json, output=clean.json
    checkpoint: "yes"
    message: Review transform
    continue: "true"
  - load:
      target: warehouse
`)

	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile error: %v", err)
	}
	if sf.Mode != "pipeline" || len(sf.Pipeline) != 3 {
		t.Fatalf("pipeline mode/steps were not normalized: mode=%q pipeline=%#v", sf.Mode, sf.Pipeline)
	}
	if sf.Pipeline[0].Skill != "extract" {
		t.Fatalf("string pipeline step was not normalized: %#v", sf.Pipeline[0])
	}
	if sf.Pipeline[1].Skill != "transform" || sf.Pipeline[1].Params["input"] != "raw.json" || !sf.Pipeline[1].Checkpoint || sf.Pipeline[1].CheckpointMessage != "Review transform" || !sf.Pipeline[1].ContinueOnFail {
		t.Fatalf("pipeline aliases were not normalized: %#v", sf.Pipeline[1])
	}
	if sf.Pipeline[2].Skill != "load" || sf.Pipeline[2].Params["target"] != "warehouse" {
		t.Fatalf("pipeline map step was not normalized: %#v", sf.Pipeline[2])
	}
}
