package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectScriptLanguage(t *testing.T) {
	tests := []struct {
		task     string
		expected string
	}{
		{"用 python 分析 CSV 数据", "python"},
		{"pip install requests 然后调用 API", "python"},
		{"用 pandas 处理 Excel", "python"},
		{"node 写个 HTTP 服务器", "node"},
		{"npm install express", "node"},
		{"用 javascript 解析 JSON", "node"},
		// "js" as standalone word should match, but not as substring.
		{"用 js 写个脚本", "node"},
	}
	for _, tt := range tests {
		result := detectScriptLanguage(tt.task)
		if result != tt.expected {
			t.Errorf("detectScriptLanguage(%q) = %q, want %q", tt.task, result, tt.expected)
		}
	}

	// "json" should NOT trigger node detection (false positive guard).
	result := detectScriptLanguage("parse json file")
	if result == "node" {
		t.Errorf("detectScriptLanguage(\"parse json file\") = %q, should not match 'js' in 'json'", result)
	}
}

func TestScriptExtension(t *testing.T) {
	tests := []struct {
		lang string
		ext  string
	}{
		{"python", ".py"},
		{"node", ".js"},
		{"javascript", ".js"},
		{"powershell", ".ps1"},
		{"bash", ".sh"},
		{"", ".sh"},
	}
	for _, tt := range tests {
		result := scriptExtension(tt.lang)
		if result != tt.ext {
			t.Errorf("scriptExtension(%q) = %q, want %q", tt.lang, result, tt.ext)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello_world"},
		{"test/path\\file", "test_path_file"},
		{"abc123", "abc123"},
		{"mix_中文_eng", "mix__eng"},
	}
	for _, tt := range tests {
		result := sanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}

	// CJK-only input should produce a hash-based name, not "script".
	cjk := sanitizeFilename("用中文描述")
	if !strings.HasPrefix(cjk, "task_") {
		t.Errorf("sanitizeFilename(CJK) = %q, expected prefix 'task_'", cjk)
	}
	if cjk == "task_00000000" {
		t.Error("CJK hash should not be zero")
	}

	// Empty input should also produce a hash-based name.
	empty := sanitizeFilename("")
	if empty != "task_00000000" {
		t.Errorf("sanitizeFilename(\"\") = %q, expected 'task_00000000'", empty)
	}

	// Different CJK inputs should produce different hashes.
	cjk2 := sanitizeFilename("另一个任务")
	if cjk == cjk2 {
		t.Errorf("different CJK inputs produced same hash: %q", cjk)
	}
}

func TestGenerateSkillName(t *testing.T) {
	name := generateSkillName("fetch weather data from API")
	if !strings.HasPrefix(name, "craft_") {
		t.Errorf("expected prefix 'craft_', got %q", name)
	}
	if strings.Contains(name, " ") {
		t.Errorf("skill name should not contain spaces: %q", name)
	}
}

func TestExtractTriggerKeywords(t *testing.T) {
	triggers := extractTriggerKeywords("fetch weather data from API and save to file")
	if len(triggers) == 0 {
		t.Error("expected at least one trigger keyword")
	}
	if len(triggers) > 5 {
		t.Errorf("expected at most 5 triggers, got %d", len(triggers))
	}
}

func TestStripScriptCodeFences(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"print('hello')", "print('hello')"},
		{"```python\nprint('hello')\n```", "print('hello')"},
		{"```\necho hello\n```", "echo hello"},
		{"  ```bash\necho hello\n```  ", "echo hello"},
	}
	for _, tt := range tests {
		result := stripScriptCodeFences(tt.input)
		if result != tt.expected {
			t.Errorf("stripScriptCodeFences(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSaveScript(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	script := "echo hello world"
	path, err := saveScript(script, "bash", "test echo")
	if err != nil {
		t.Fatalf("saveScript failed: %v", err)
	}
	defer os.Remove(path)

	// Verify file exists and has correct content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved script: %v", err)
	}
	if string(data) != script {
		t.Errorf("script content mismatch: got %q", string(data))
	}

	// Verify it's in the crafted_tools directory.
	if !strings.Contains(path, "crafted_tools") {
		t.Errorf("expected path to contain 'crafted_tools': %s", path)
	}

	// Verify extension.
	if filepath.Ext(path) != ".sh" {
		t.Errorf("expected .sh extension, got %s", filepath.Ext(path))
	}
}

func TestBuildRunCommand(t *testing.T) {
	tests := []struct {
		language string
		contains string
	}{
		{"python", "python"},
		{"node", "node"},
		{"powershell", "powershell"},
	}
	for _, tt := range tests {
		cmd := buildRunCommand("/tmp/test.py", tt.language)
		if !strings.Contains(cmd, tt.contains) {
			t.Errorf("buildRunCommand(%q) = %q, expected to contain %q", tt.language, cmd, tt.contains)
		}
	}
}

func TestBuildCraftUserPrompt_AddsArtifactRepairHints(t *testing.T) {
	prompt := buildCraftUserPrompt(
		craftToolRequest{Task: "generate report"},
		craftAttemptResult{
			Attempts:            1,
			VerificationStatus:  craftVerificationArtifactMissing,
			ArtifactPath:        "/tmp/out.pdf",
			VerificationMessage: "脚本未报告产物路径，且预期产物不存在：/tmp/out.pdf",
			Script:              "print('hi')",
			Output:              "done",
		},
	)
	for _, want := range []string{
		"修复要求：",
		"这次必须把最终产物写到这个精确路径：/tmp/out.pdf",
		"成功后必须输出一行：artifact: /tmp/out.pdf",
		"不要只打印成功信息，必须确保该路径上的文件真实存在。",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildCraftSystemPrompt_AddsArtifactRepairGuards(t *testing.T) {
	prompt := buildCraftSystemPrompt(
		craftToolRequest{RuntimeLanguage: "python"},
		craftRuntimeAvailability{Python: "/usr/bin/python"},
		craftAttemptResult{
			Attempts:           1,
			VerificationStatus: craftVerificationArtifactMissing,
			ArtifactPath:       "/tmp/out.pdf",
		},
	)
	for _, want := range []string{
		"这是修复轮次，请基于上次失败原因修复，不要重复同样错误。",
		"这是一次产物定向修复：不要只打印 artifact 行或成功提示，必须真正创建该文件，并保证该路径能被文件系统检测到。",
		"若脚本成功，必须在 stdout 输出精确一行：artifact: /tmp/out.pdf",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestCraftedToolsDir(t *testing.T) {
	dir := craftedToolsDir()
	if !strings.Contains(dir, ".maclaw") || !strings.Contains(dir, "crafted_tools") {
		t.Errorf("unexpected crafted tools dir: %s", dir)
	}
}

func TestNormalizeCraftToolArgs(t *testing.T) {
	tests := []struct {
		name            string
		args            map[string]interface{}
		wantTask        string
		wantArtifacts   []string
		wantInstruction bool
		wantError       bool
	}{
		{name: "prefer task", args: map[string]interface{}{"task": "run report", "instructions": "ignored"}, wantTask: "run report"},
		{name: "fallback to instructions", args: map[string]interface{}{"instructions": "legacy task"}, wantTask: "legacy task", wantInstruction: true},
		{name: "trim task", args: map[string]interface{}{"task": "  trimmed task  "}, wantTask: "trimmed task"},
		{name: "merge output into expected artifacts", args: map[string]interface{}{"task": "generate pdf", "output": "/tmp/demo.pdf"}, wantTask: "generate pdf\n\n必须把最终生成文件写到这个精确路径：/tmp/demo.pdf\n不要写到其他默认目录。", wantArtifacts: []string{"/tmp/demo.pdf"}},
		{name: "keep expected artifacts", args: map[string]interface{}{"task": "generate report", "expected_artifacts": []interface{}{"/tmp/a.txt", "/tmp/b.txt"}}, wantTask: "generate report", wantArtifacts: []string{"/tmp/a.txt", "/tmp/b.txt"}},
		{name: "missing task", args: map[string]interface{}{}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := normalizeCraftToolArgs(tt.args)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeCraftToolArgs() error = %v", err)
			}
			if got := normalized["task"]; got != tt.wantTask {
				t.Fatalf("normalized task = %v, want %q", got, tt.wantTask)
			}
			artifacts := normalizeCraftArtifactList(normalized["expected_artifacts"])
			if strings.Join(artifacts, "|") != strings.Join(tt.wantArtifacts, "|") {
				t.Fatalf("expected_artifacts = %#v, want %#v", artifacts, tt.wantArtifacts)
			}
		})
	}
}

func TestVerifyCraftExecution(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "done.pdf")
	if err := os.WriteFile(artifact, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	request := craftToolRequest{ExpectedArtifacts: []string{artifact}, VerificationMode: "artifact_required"}
	attempt := craftAttemptResult{Output: "artifact: " + artifact}
	verified := verifyCraftExecution(request, attempt)
	if verified.VerificationStatus != craftVerificationPassed {
		t.Fatalf("VerificationStatus = %q, want %q", verified.VerificationStatus, craftVerificationPassed)
	}
	if !strings.Contains(verified.VerificationMessage, "3 bytes") {
		t.Fatalf("expected verification message to include file size, got %q", verified.VerificationMessage)
	}

	missing := verifyCraftExecution(craftToolRequest{ExpectedArtifacts: []string{filepath.Join(t.TempDir(), "missing.pdf")}, VerificationMode: "artifact_required"}, craftAttemptResult{})
	if missing.VerificationStatus != craftVerificationArtifactMissing {
		t.Fatalf("missing VerificationStatus = %q, want %q", missing.VerificationStatus, craftVerificationArtifactMissing)
	}
	if !strings.Contains(missing.VerificationMessage, "未报告产物路径") {
		t.Fatalf("expected missing artifact message to mention unreported artifact path, got %q", missing.VerificationMessage)
	}

	stdoutOnly := verifyCraftExecution(craftToolRequest{VerificationMode: "artifact_required"}, craftAttemptResult{Output: "Gold Price Report\nprice: 123"})
	if stdoutOnly.VerificationStatus != craftVerificationPassed {
		t.Fatalf("stdout-only VerificationStatus = %q, want %q", stdoutOnly.VerificationStatus, craftVerificationPassed)
	}
	reportedMissingPath := filepath.Join(t.TempDir(), "reported-missing.pdf")
	reportedMissing := verifyCraftExecution(craftToolRequest{VerificationMode: "artifact_required"}, craftAttemptResult{Output: "artifact: " + reportedMissingPath})
	if reportedMissing.VerificationStatus != craftVerificationArtifactMissing {
		t.Fatalf("reported missing VerificationStatus = %q, want %q", reportedMissing.VerificationStatus, craftVerificationArtifactMissing)
	}
	if !strings.Contains(reportedMissing.VerificationMessage, "报告了产物路径，但文件不存在") {
		t.Fatalf("expected reported missing message, got %q", reportedMissing.VerificationMessage)
	}

	emptyFile := filepath.Join(t.TempDir(), "empty.pdf")
	if err := os.WriteFile(emptyFile, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	emptyResult := verifyCraftExecution(craftToolRequest{ExpectedArtifacts: []string{emptyFile}, VerificationMode: "artifact_required"}, craftAttemptResult{})
	if emptyResult.VerificationStatus != craftVerificationArtifactMissing {
		t.Fatalf("empty file VerificationStatus = %q, want %q", emptyResult.VerificationStatus, craftVerificationArtifactMissing)
	}
	if !strings.Contains(emptyResult.VerificationMessage, "空文件") {
		t.Fatalf("expected empty file message, got %q", emptyResult.VerificationMessage)
	}

	dirPath := filepath.Join(t.TempDir(), "outdir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	dirResult := verifyCraftExecution(craftToolRequest{ExpectedArtifacts: []string{dirPath}, VerificationMode: "artifact_required"}, craftAttemptResult{})
	if dirResult.VerificationStatus != craftVerificationArtifactMissing {
		t.Fatalf("directory VerificationStatus = %q, want %q", dirResult.VerificationStatus, craftVerificationArtifactMissing)
	}
	if !strings.Contains(dirResult.VerificationMessage, "目录不是文件") {
		t.Fatalf("expected directory message, got %q", dirResult.VerificationMessage)
	}

	reportedDirPath := filepath.Join(t.TempDir(), "reported-dir")
	if err := os.MkdirAll(reportedDirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(reported-dir) error = %v", err)
	}
	reportedDirResult := verifyCraftExecution(craftToolRequest{VerificationMode: "artifact_required"}, craftAttemptResult{Output: "artifact: " + reportedDirPath})
	if reportedDirResult.VerificationStatus != craftVerificationArtifactMissing {
		t.Fatalf("reported directory VerificationStatus = %q, want %q", reportedDirResult.VerificationStatus, craftVerificationArtifactMissing)
	}
	if !strings.Contains(reportedDirResult.VerificationMessage, "该路径是目录不是文件") {
		t.Fatalf("expected reported directory message, got %q", reportedDirResult.VerificationMessage)
	}
}

func TestShouldAutoRegisterCraftRequest(t *testing.T) {
	if !shouldAutoRegisterCraftRequest(craftToolRequest{SaveAsSkill: true, RegisterPolicy: craftRegisterPolicyAuto}) {
		t.Fatal("expected auto register for reusable request")
	}
	if shouldAutoRegisterCraftRequest(craftToolRequest{SaveAsSkill: true, RegisterPolicy: craftRegisterPolicyAuto, ExpectedArtifacts: []string{"/tmp/out.pdf"}}) {
		t.Fatal("expected output-bound request to skip auto register")
	}
	if shouldAutoRegisterCraftRequest(craftToolRequest{SaveAsSkill: true, RegisterPolicy: craftRegisterPolicyManual}) {
		t.Fatal("expected manual policy to skip auto register")
	}
}

func TestShouldRetryCraftAttempt(t *testing.T) {
	tests := []struct {
		name      string
		request   craftToolRequest
		attempt   craftAttemptResult
		current   int
		max       int
		wantRetry bool
	}{
		{name: "stop at max attempts", attempt: craftAttemptResult{VerificationMessage: "syntax error"}, current: 2, max: 2, wantRetry: false},
		{name: "stop on runtime missing", attempt: craftAttemptResult{VerificationStatus: craftVerificationRuntimeMissing}, current: 1, max: 2, wantRetry: false},
		{name: "retry artifact missing with reported path", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, ArtifactPath: "/tmp/out.pdf", VerificationMessage: "脚本报告了产物路径，但文件不存在：/tmp/out.pdf"}, current: 1, max: 2, wantRetry: true},
		{name: "stop artifact missing without reported path", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, ArtifactPath: "/tmp/out.pdf", VerificationMessage: "脚本未报告产物路径，且预期产物不存在：/tmp/out.pdf"}, current: 1, max: 2, wantRetry: false},
		{name: "stop artifact missing without artifact path", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, VerificationMessage: "脚本已运行，但既未报告产物路径，也未检测到预期产物。"}, current: 1, max: 2, wantRetry: false},
		{name: "stop on auth failure", attempt: craftAttemptResult{VerificationMessage: "authentication required for api access"}, current: 1, max: 2, wantRetry: false},
		{name: "stop on network environment failure", attempt: craftAttemptResult{VerificationMessage: "dial tcp: lookup api.example.com: no such host"}, current: 1, max: 2, wantRetry: false},
		{name: "stop on interactive flow", attempt: craftAttemptResult{VerificationMessage: "manual login required before export"}, current: 1, max: 2, wantRetry: false},
		{name: "stop on codebase scale mismatch", attempt: craftAttemptResult{VerificationMessage: "this requires create_session for a repository-wide refactor"}, current: 1, max: 2, wantRetry: false},
		{name: "stop on invalid working dir", request: craftToolRequest{WorkingDir: "/tmp/missing"}, attempt: craftAttemptResult{VerificationMessage: "working directory error: no such file or directory"}, current: 1, max: 2, wantRetry: false},
		{name: "retry syntax error", attempt: craftAttemptResult{VerificationStatus: craftVerificationOutputSuspicious, VerificationMessage: "syntaxerror near line 2"}, current: 1, max: 2, wantRetry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryCraftAttempt(tt.request, tt.attempt, tt.current, tt.max); got != tt.wantRetry {
				t.Fatalf("shouldRetryCraftAttempt() = %v, want %v", got, tt.wantRetry)
			}
		})
	}
}

func TestClassifyCraftFailure(t *testing.T) {
	tests := []struct {
		name         string
		request      craftToolRequest
		attempt      craftAttemptResult
		wantCategory string
		wantAdvice   string
	}{
		{name: "artifact missing with reported path", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, ArtifactPath: "/tmp/out.pdf", VerificationMessage: "脚本报告了产物路径，但文件不存在：/tmp/out.pdf"}, wantCategory: craftFailureCategoryArtifact, wantAdvice: "磁盘上找不到该文件"},
		{name: "artifact missing without reported path", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, ArtifactPath: "/tmp/out.pdf", VerificationMessage: "脚本未报告产物路径，且预期产物不存在：/tmp/out.pdf"}, wantCategory: craftFailureCategoryArtifact, wantAdvice: "没有明确回报产物路径"},
		{name: "artifact missing generic", attempt: craftAttemptResult{VerificationStatus: craftVerificationArtifactMissing, ArtifactPath: "/tmp/out.pdf"}, wantCategory: craftFailureCategoryArtifact, wantAdvice: "写到指定路径"},
		{name: "runtime missing", attempt: craftAttemptResult{VerificationStatus: craftVerificationRuntimeMissing}, wantCategory: craftFailureCategoryEnvironment, wantAdvice: "缺少可用脚本运行时"},
		{name: "permission issue", attempt: craftAttemptResult{VerificationMessage: "permission denied while writing file"}, wantCategory: craftFailureCategoryPermission, wantAdvice: "权限或认证问题"},
		{name: "environment issue", attempt: craftAttemptResult{VerificationMessage: "dial tcp: lookup api.example.com: no such host"}, wantCategory: craftFailureCategoryEnvironment, wantAdvice: "运行环境或外部依赖问题"},
		{name: "capability issue", attempt: craftAttemptResult{VerificationMessage: "this requires create_session for a repository-wide refactor"}, wantCategory: craftFailureCategoryCapability, wantAdvice: "超出单脚本自动化边界"},
		{name: "script issue", attempt: craftAttemptResult{VerificationMessage: "Traceback: syntaxerror"}, wantCategory: craftFailureCategoryScript, wantAdvice: "脚本本身的可修复错误"},
		{name: "artifact required fallback", request: craftToolRequest{VerificationMode: "artifact_required"}, attempt: craftAttemptResult{VerificationStatus: craftVerificationExecutionFailed, VerificationMessage: "unknown failure"}, wantCategory: craftFailureCategoryArtifact, wantAdvice: "要求生成可验证产物"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, advice := classifyCraftFailure(tt.request, tt.attempt)
			if category != tt.wantCategory {
				t.Fatalf("category = %q, want %q", category, tt.wantCategory)
			}
			if !strings.Contains(advice, tt.wantAdvice) {
				t.Fatalf("advice = %q, want to contain %q", advice, tt.wantAdvice)
			}
		})
	}
}

func TestBuildCraftFailureResultIncludesCategoryAndAdvice(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		Language:            "python",
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: "permission denied while writing file",
		ScriptPath:          "/tmp/tool.py",
		Attempts:            1,
	}, "", "")
	if !strings.Contains(result, "failure_category: permission") {
		t.Fatalf("expected permission category in result, got %s", result)
	}
	if !strings.Contains(result, "权限或认证问题") {
		t.Fatalf("expected advice in result, got %s", result)
	}

	artifactResult := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		Language:            "python",
		VerificationStatus:  craftVerificationArtifactMissing,
		VerificationMessage: "脚本未报告产物路径，且预期产物不存在：/tmp/out.pdf",
		ArtifactPath:        "/tmp/out.pdf",
	}, "", "")
	if !strings.Contains(artifactResult, "failure_category: artifact") {
		t.Fatalf("expected artifact category in result, got %s", artifactResult)
	}
	if !strings.Contains(artifactResult, "expected_artifact: /tmp/out.pdf") {
		t.Fatalf("expected structured expected_artifact in result, got %s", artifactResult)
	}
}

func TestRegisterCraftedSkillEntry(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	scriptPath := filepath.Join(tempHome, "demo.sh")
	if err := os.WriteFile(scriptPath, []byte("echo hi"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result := registerCraftedSkillEntry(app, "collect logs", "", scriptPath, "bash")
	if !strings.Contains(result, "已注册为 Skill") {
		t.Fatalf("unexpected register result: %s", result)
	}

	skills := app.skillExecutor.loadSkills()
	if len(skills) == 0 {
		t.Fatal("expected crafted skill to be registered")
	}
	last := skills[len(skills)-1]
	if last.Source != "crafted" {
		t.Fatalf("expected crafted source, got %q", last.Source)
	}
	if len(last.Steps) != 1 || last.Steps[0].Action != "bash" {
		t.Fatalf("unexpected crafted steps: %#v", last.Steps)
	}
}

func TestRegisterCraftedSkillEntryAppendsExtractedArgparsePlaceholders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	scriptPath := filepath.Join(tempHome, "demo.py")
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("--input", required=True)
parser.add_argument("--format", default="pdf")
parser.add_argument("--verbose", action="store_true")
`
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result := registerCraftedSkillEntry(app, "convert file", "", scriptPath, "python")
	if !strings.Contains(result, "Skill") {
		t.Fatalf("unexpected register result: %s", result)
	}

	skills := app.skillExecutor.loadSkills()
	if len(skills) == 0 {
		t.Fatal("expected crafted skill to be registered")
	}
	last := skills[len(skills)-1]
	command, _ := last.Steps[0].Params["command"].(string)
	if !strings.Contains(command, "--input {{input}}") || !strings.Contains(command, "--format {{format}}") {
		t.Fatalf("crafted command = %q, want extracted argparse params appended as placeholders", command)
	}
	if strings.Contains(command, "verbose") {
		t.Fatalf("crafted command = %q, store_true flag should not require a value placeholder", command)
	}
	byName := map[string]bool{}
	required := map[string]bool{}
	for _, param := range last.Params {
		byName[param.Name] = true
		required[param.Name] = param.Required
	}
	if !byName["input"] || !required["input"] || !byName["format"] || required["format"] || byName["verbose"] {
		t.Fatalf("crafted params = %#v, want required input, optional format, no verbose value param", last.Params)
	}
}

func TestRegisterCraftedSkillEntryExtractsRuntimeMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	scriptPath := filepath.Join(tempHome, "metadata.py")
	script := `
import os
import requests

print(os.environ["OPENAI_API_KEY"])
print(requests.__version__)
`
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result := registerCraftedSkillEntry(app, "call api", "", scriptPath, "python")
	if !strings.Contains(result, "Skill") {
		t.Fatalf("unexpected register result: %s", result)
	}

	skills := app.skillExecutor.loadSkills()
	if len(skills) == 0 {
		t.Fatal("expected crafted skill to be registered")
	}
	last := skills[len(skills)-1]
	if len(last.RequiredEnv) != 1 || last.RequiredEnv[0] != "OPENAI_API_KEY" {
		t.Fatalf("crafted required env = %#v, want OPENAI_API_KEY", last.RequiredEnv)
	}
	if len(last.RequiresPython) != 1 || last.RequiresPython[0] != "requests" {
		t.Fatalf("crafted python dependencies = %#v, want requests", last.RequiresPython)
	}
	if len(last.RequiresNode) != 0 {
		t.Fatalf("crafted node dependencies = %#v, want none", last.RequiresNode)
	}
}

func TestBuildCraftToolRequestExtractsExtraEnv(t *testing.T) {
	request := buildCraftToolRequest(map[string]interface{}{
		"task":      "print token",
		"language":  "python",
		"extra_env": map[string]interface{}{"API_TOKEN": "secret"},
	}, craftRuntimeAvailability{Python: "python"})

	if request.ExtraEnv["API_TOKEN"] != "secret" {
		t.Fatalf("ExtraEnv = %#v, want API_TOKEN", request.ExtraEnv)
	}
}

func TestExecuteScriptWithContextInjectsExtraEnvWithoutProcessMutation(t *testing.T) {
	const key = "MACLAW_CRAFT_EXTRA_ENV_TEST"
	t.Setenv(key, "")

	runtimes := detectAvailableScriptRuntimes()
	language := ""
	script := ""
	switch {
	case runtimes.Python != "":
		language = "python"
		script = "import os\nprint(os.environ.get('" + key + "', ''))\n"
	case runtimes.Node != "":
		language = "node"
		script = "console.log(process.env." + key + " || '')\n"
	case runtimes.PowerShell != "":
		language = "powershell"
		script = "Write-Output $env:" + key + "\n"
	case runtimes.Bash != "":
		language = "bash"
		script = "printf '%s\\n' \"$" + key + "\"\n"
	default:
		t.Skip("no script runtime available")
	}

	scriptPath := filepath.Join(t.TempDir(), "env"+scriptExtension(language))
	perm := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		perm = 0o755
	}
	if err := os.WriteFile(scriptPath, []byte(script), perm); err != nil {
		t.Fatalf("write script: %v", err)
	}

	out, err := executeScriptWithContext(context.Background(), scriptPath, language, t.TempDir(), 30, runtimes, map[string]string{key: "from-extra"})
	if err != nil {
		t.Fatalf("executeScriptWithContext() error = %v; output=%s", err, out)
	}
	if !strings.Contains(out, "from-extra") {
		t.Fatalf("output = %q, want injected env value", out)
	}
	if got := os.Getenv(key); got != "" {
		t.Fatalf("process env mutated to %q, want unchanged empty value", got)
	}
}

func TestRegisterCraftedSkillEntryAppendsSysArgvPositionals(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	scriptPath := filepath.Join(tempHome, "argv.py")
	if err := os.WriteFile(scriptPath, []byte("import sys\nprint(sys.argv[1], sys.argv[2])\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result := registerCraftedSkillEntry(app, "join args", "", scriptPath, "python")
	if !strings.Contains(result, "Skill") {
		t.Fatalf("unexpected register result: %s", result)
	}

	skills := app.skillExecutor.loadSkills()
	if len(skills) == 0 {
		t.Fatal("expected crafted skill to be registered")
	}
	last := skills[len(skills)-1]
	command, _ := last.Steps[0].Params["command"].(string)
	if !strings.Contains(command, "{{input}} {{output}}") {
		t.Fatalf("crafted command = %q, want positional placeholders appended", command)
	}
}

func TestBuildCraftFailureResult_WithProviderInfo(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: "script execution failed",
	}, "智谱编程", "https://open.bigmodel.cn/api/anthropic")

	want := "provider: 智谱编程 (https://open.bigmodel.cn/api/anthropic)"
	if !strings.Contains(result, want) {
		t.Fatalf("expected result to contain %q, got:\n%s", want, result)
	}
}

func TestBuildCraftFailureResult_WithoutProviderInfo(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: "script execution failed",
	}, "", "")

	if strings.Contains(result, "provider:") {
		t.Fatalf("expected result to NOT contain 'provider:', got:\n%s", result)
	}
}

func TestBuildCraftFailureResult_HumanizesAPIError(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: `HTTP 400: {"type":"error","error":{"message":"网络错误，错误id：20250715，请稍后重试","code":"1234"}}`,
	}, "", "")

	// The ⚠️ line should contain the humanized message.
	if !strings.Contains(result, "API 服务端临时故障（code:1234）") {
		t.Fatalf("expected humanized message in result, got:\n%s", result)
	}
	// The ⚠️ line should NOT contain the raw JSON.
	if strings.Contains(result, `"type":"error"`) {
		t.Fatalf("expected raw JSON to be replaced, but found '\"type\":\"error\"' in result:\n%s", result)
	}
}

func TestBuildCraftFailureResult_ProviderNameOnly(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: "script execution failed",
	}, "智谱编程", "")

	if !strings.Contains(result, "provider: 智谱编程") {
		t.Fatalf("expected provider name in result, got:\n%s", result)
	}
	// Should NOT have parenthesized URL
	if strings.Contains(result, "(") {
		t.Fatalf("expected no URL in parentheses, got:\n%s", result)
	}
}

func TestBuildCraftFailureResult_ProviderURLOnly(t *testing.T) {
	result := buildCraftFailureResult(craftToolRequest{}, craftAttemptResult{
		VerificationStatus:  craftVerificationExecutionFailed,
		VerificationMessage: "script execution failed",
	}, "", "https://open.bigmodel.cn/api/anthropic")

	if !strings.Contains(result, "provider: https://open.bigmodel.cn/api/anthropic") {
		t.Fatalf("expected provider URL in result, got:\n%s", result)
	}
}

func TestHumanizeCraftAPIError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		changed bool // true if the message should be replaced
	}{
		{
			name:    "code:1234 compact",
			input:   `HTTP 400: {"type":"error","error":{"message":"网络错误","code":"1234"}}`,
			want:    "API 服务端临时故障（code:1234），请稍后重试",
			changed: true,
		},
		{
			name:    "code:1234 spaced",
			input:   `HTTP 400: {"error":{"code": "1234", "message": "网络错误"}}`,
			want:    "API 服务端临时故障（code:1234），请稍后重试",
			changed: true,
		},
		{
			name:    "type:error without code:1234",
			input:   `HTTP 400: {"type":"error","error":{"message":"invalid request"}}`,
			want:    "API 返回错误响应，请检查配置或稍后重试",
			changed: true,
		},
		{
			name:    "HTTP 429",
			input:   "HTTP 429: Too Many Requests",
			want:    "API 调用频率超限，请稍后重试",
			changed: true,
		},
		{
			name:    "rate limit lowercase",
			input:   "rate limit exceeded",
			want:    "API 调用频率超限，请稍后重试",
			changed: true,
		},
		{
			name:    "too many requests",
			input:   "Too Many Requests",
			want:    "API 调用频率超限，请稍后重试",
			changed: true,
		},
		{
			name:    "script error unchanged",
			input:   "permission denied while writing file",
			want:    "permission denied while writing file",
			changed: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    "",
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanizeCraftAPIError(tt.input)
			if got != tt.want {
				t.Errorf("humanizeCraftAPIError(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
