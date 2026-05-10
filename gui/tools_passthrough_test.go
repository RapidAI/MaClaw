package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestToolPassthroughTaskSaveAndShow(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	result := h.toolPassthroughTask(map[string]interface{}{
		"action":           "save",
		"name":             "repair-env",
		"script_path":      filepath.Join(dir, "repair.ps1"),
		"template_args":    []interface{}{"--target", "${target}"},
		"runtime":          "powershell",
		"timeout_seconds":  float64(30),
		"confirm_required": true,
		"enabled":          true,
		"params_text":      "target:path:required::D:\\workprj\\aicoder\ndeep:boolean:optional:false:true",
	})
	if !strings.Contains(result, "已保存直通任务 repair-env") || !strings.Contains(result, "/run repair-env") || !strings.Contains(result, "/runctl save repair-env") || !strings.Contains(result, "--params-json") || !strings.Contains(result, "/runctl show repair-env") {
		t.Fatalf("unexpected save result:\n%s", result)
	}

	show := h.toolPassthroughTask(map[string]interface{}{"action": "show", "name": "repair-env"})
	for _, want := range []string{`"name": "repair-env"`, `"runtime": "powershell"`, `"--target"`, `"${target}"`, `"name": "target"`} {
		if !strings.Contains(show, want) {
			t.Fatalf("show missing %q:\n%s", want, show)
		}
	}
}

func TestToolPassthroughTaskSaveParamsJSON(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	result := h.toolPassthroughTask(map[string]interface{}{
		"action":      "save",
		"name":        "git-status",
		"command":     `git -C ${target} status --short`,
		"runtime":     "direct",
		"params_json": `[{"name":"target","type":"path","required":true,"example":"D:\\workprj\\aicoder"}]`,
	})
	if !strings.Contains(result, "已保存直通任务 git-status") || !strings.Contains(result, "--params-json") {
		t.Fatalf("unexpected save result:\n%s", result)
	}
	cmd, ok, err := app.passthroughRegistry.Get("git-status")
	if err != nil || !ok {
		t.Fatalf("Get failed ok=%v err=%v", ok, err)
	}
	if len(cmd.Params) != 1 || cmd.Params[0].Example != `D:\workprj\aicoder` {
		t.Fatalf("unexpected params: %+v", cmd.Params)
	}
}

func TestToolPassthroughTaskSaveCommandTemplate(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	result := h.toolPassthroughTask(map[string]interface{}{
		"action":  "save",
		"name":    "git-status",
		"command": `git -C ${target} status --short`,
		"runtime": "direct",
		"params": []interface{}{
			map[string]interface{}{"name": "target", "type": "path", "required": true, "example": `D:\workprj\aicoder`},
		},
	})
	if !strings.Contains(result, "git-status") {
		t.Fatalf("save result=%s", result)
	}
	cmd, ok, err := app.passthroughRegistry.Get("git-status")
	if err != nil || !ok {
		t.Fatalf("Get failed ok=%v err=%v", ok, err)
	}
	if cmd.ScriptPath != "git" || strings.Join(cmd.TemplateArgs, " ") != "-C ${target} status --short" {
		t.Fatalf("unexpected command template: %#v", cmd)
	}
}

func TestToolPassthroughTaskSaveRejectsInvalidRuntimeAndParamExamples(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	badRuntime := h.toolPassthroughTask(map[string]interface{}{
		"action":      "save",
		"name":        "bad-runtime",
		"script_path": "git",
		"runtime":     "zsh",
	})
	if !strings.Contains(badRuntime, "save passthrough task failed") || !strings.Contains(badRuntime, "unsupported runtime") {
		t.Fatalf("unexpected bad runtime result:\n%s", badRuntime)
	}

	badExample := h.toolPassthroughTask(map[string]interface{}{
		"action":      "save",
		"name":        "bad-example",
		"script_path": "git",
		"runtime":     "direct",
		"params": []interface{}{
			map[string]interface{}{"name": "count", "type": "number", "example": "many"},
		},
	})
	if !strings.Contains(badExample, "save passthrough task failed") || !strings.Contains(badExample, "invalid example") {
		t.Fatalf("unexpected bad example result:\n%s", badExample)
	}

	commands, err := app.passthroughRegistry.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("invalid tool saves should not persist commands: %+v", commands)
	}
}

func TestToolPassthroughTaskExport(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	_ = h.toolPassthroughTask(map[string]interface{}{
		"action":  "save",
		"name":    "git-status",
		"command": `git -C ${target} status --short`,
		"params": []interface{}{
			map[string]interface{}{"name": "target", "type": "path", "required": true, "example": `D:\workprj\aicoder`},
		},
	})
	result := h.toolPassthroughTask(map[string]interface{}{"action": "export", "name": "git-status"})
	if !strings.HasPrefix(result, "/runctl save git-status") || !strings.Contains(result, "--params-json") || strings.Contains(result, "运行示例") {
		t.Fatalf("unexpected export result:\n%s", result)
	}
}

func TestToolPassthroughTaskPreviewDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	_ = h.toolPassthroughTask(map[string]interface{}{
		"action":  "save",
		"name":    "git-status",
		"command": `git -C ${target} status --short`,
		"params": []interface{}{
			map[string]interface{}{"name": "target", "type": "path", "required": true, "example": `D:\workprj\aicoder`},
		},
	})

	result := h.toolPassthroughTask(map[string]interface{}{
		"action": "preview",
		"name":   "git-status",
		"values": map[string]interface{}{"target": `D:\workprj\aicoder`},
	})
	if !strings.Contains(result, `argv: git -C D:\workprj\aicoder status --short`) {
		t.Fatalf("unexpected preview result:\n%s", result)
	}
	audit, err := app.passthroughRegistry.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "registry" {
		t.Fatalf("preview should not add execution audit: %+v", audit)
	}
}

func TestToolPassthroughTaskPreviewDraftCommand(t *testing.T) {
	dir := t.TempDir()
	app := &App{passthroughRegistry: newPassthroughRegistry(filepath.Join(dir, "commands.json"))}
	h := &IMMessageHandler{app: app}

	result := h.toolPassthroughTask(map[string]interface{}{
		"action":      "preview",
		"name":        "git-status",
		"command":     `git -C ${target} status --short`,
		"params_json": `[{"name":"target","type":"path","required":true,"example":"D:\\workprj\\aicoder"}]`,
	})
	if !strings.Contains(result, `argv: git -C D:\workprj\aicoder status --short`) {
		t.Fatalf("unexpected preview result:\n%s", result)
	}
	if commands, err := app.passthroughRegistry.List(); err != nil || len(commands) != 0 {
		t.Fatalf("preview draft should not persist command: commands=%+v err=%v", commands, err)
	}
}

func TestToolPassthroughTaskSetEnabledAndStatus(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "commands.json")
	app := &App{passthroughRegistry: newPassthroughRegistry(registryPath)}
	h := &IMMessageHandler{app: app}
	_ = h.toolPassthroughTask(map[string]interface{}{
		"action":      "save",
		"name":        "repair-env",
		"script_path": filepath.Join(dir, "repair.cmd"),
		"runtime":     "cmd",
	})

	disabled := h.toolPassthroughTask(map[string]interface{}{"action": "set_enabled", "name": "repair-env", "enabled": false})
	if !strings.Contains(disabled, "已禁用直通任务 repair-env") {
		t.Fatalf("unexpected disable result: %s", disabled)
	}
	status := h.toolPassthroughTask(map[string]interface{}{"action": "status"})
	if !strings.Contains(status, "任务：1 个") || !strings.Contains(status, "启用 0 个") {
		t.Fatalf("unexpected status:\n%s", status)
	}
	if !strings.Contains(status, registryPath) {
		t.Fatalf("status should use actual registry path %q:\n%s", registryPath, status)
	}
	audit := h.toolPassthroughTask(map[string]interface{}{"action": "audit", "limit": 10})
	for _, want := range []string{"save repair-env", "disable repair-env", "source=assistant:tool"} {
		if !strings.Contains(audit, want) {
			t.Fatalf("audit missing %q:\n%s", want, audit)
		}
	}
}

func TestPassthroughParamsTextKeepsWindowsPathExample(t *testing.T) {
	params, err := parsePassthroughParamsText("target:path:required::D:\\workprj\\aicoder")
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params[0].Example != `D:\workprj\aicoder` {
		t.Fatalf("params=%+v", params)
	}
}

func TestPassthroughParamsTextKeepsWindowsPathDefault(t *testing.T) {
	params, err := parsePassthroughParamsText("target:path:required:D:\\workprj\\aicoder")
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params[0].Default != `D:\workprj\aicoder` || params[0].Example != "" {
		t.Fatalf("params=%+v", params)
	}
}
