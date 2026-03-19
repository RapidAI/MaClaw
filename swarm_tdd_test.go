package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// extractCodeBlock
// ---------------------------------------------------------------------------

func TestExtractCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "go code block",
			input: "这是测试代码：\n```go\nfunc TestAdd(t *testing.T) {\n\tt.Log(\"ok\")\n}\n```\n完毕",
			want:  "func TestAdd(t *testing.T) {\n\tt.Log(\"ok\")\n}",
		},
		{
			name:  "no language tag",
			input: "```\nsome code\n```",
			want:  "some code",
		},
		{
			name:  "no code block",
			input: "just plain text",
			want:  "",
		},
		{
			name:  "unclosed code block",
			input: "```go\nfunc main() {}\n",
			want:  "func main() {}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCodeBlock(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}


// ---------------------------------------------------------------------------
// extractTestFilePath
// ---------------------------------------------------------------------------

func TestExtractTestFilePath(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "chinese file label",
			output: "测试文件: auth_test.go\n```go\npackage main\n```",
			want:   "auth_test.go",
		},
		{
			name:   "english file label",
			output: "File: test_login.py\ncode here",
			want:   "test_login.py",
		},
		{
			name:   "js spec file",
			output: "文件: auth.spec.ts\ncode",
			want:   "auth.spec.ts",
		},
		{
			name:   "no file path",
			output: "just some code without file info",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTestFilePath(tt.output)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// inferTestFileName
// ---------------------------------------------------------------------------

func TestInferTestFileName(t *testing.T) {
	tests := []struct {
		techStack string
		want      string
	}{
		{"Go", "task_test.go"},
		{"golang + gin", "task_test.go"},
		{"Python + FastAPI", "test_task.py"},
		{"TypeScript + React", "task.test.ts"},
		{"JavaScript", "task.test.ts"},
		{"Rust", "tests/task_test.rs"},
		{"Java + Spring", "src/test/java/TaskTest.java"},
		{"unknown", "test_task"},
	}
	for _, tt := range tests {
		t.Run(tt.techStack, func(t *testing.T) {
			got := inferTestFileName(tt.techStack)
			if got != tt.want {
				t.Errorf("inferTestFileName(%q) = %q, want %q", tt.techStack, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildTargetedTestCmd
// ---------------------------------------------------------------------------

func TestBuildTargetedTestCmd(t *testing.T) {
	tests := []struct {
		name     string
		baseCmd  string
		testFile string
		want     string
	}{
		{
			name:     "go test root",
			baseCmd:  "go test ./...",
			testFile: "auth_test.go",
			want:     "go test -v -count=1 ./...",
		},
		{
			name:     "go test subdir",
			baseCmd:  "go test ./...",
			testFile: "internal/auth/auth_test.go",
			want:     "go test -v -count=1 ./internal/auth/...",
		},
		{
			name:     "pytest",
			baseCmd:  "pytest",
			testFile: "tests/test_login.py",
			want:     "pytest -v tests/test_login.py",
		},
		{
			name:     "npm test",
			baseCmd:  "npm test",
			testFile: "src/auth.test.ts",
			want:     "npx jest --verbose src/auth.test.ts",
		},
		{
			name:     "cargo test",
			baseCmd:  "cargo test",
			testFile: "tests/auth.rs",
			want:     "cargo test",
		},
		{
			name:     "unknown base cmd",
			baseCmd:  "make test",
			testFile: "test_file",
			want:     "make test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTargetedTestCmd(tt.baseCmd, tt.testFile)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// countTestFailures / countTestTotal
// ---------------------------------------------------------------------------

func TestCountTestFailures(t *testing.T) {
	output := `=== RUN   TestLogin
--- FAIL: TestLogin (0.01s)
=== RUN   TestLogout
--- PASS: TestLogout (0.00s)
=== RUN   TestRegister
--- FAIL: TestRegister (0.02s)
FAIL`
	got := countTestFailures(output)
	if got != 2 {
		t.Errorf("countTestFailures = %d, want 2", got)
	}
}

func TestCountTestTotal(t *testing.T) {
	output := `--- PASS: TestA (0.00s)
--- FAIL: TestB (0.01s)
--- PASS: TestC (0.00s)`
	got := countTestTotal(output)
	if got != 3 {
		t.Errorf("countTestTotal = %d, want 3", got)
	}
}

func TestCountTestFailures_NoExplicitFail(t *testing.T) {
	// 只有 FAIL 关键字但没有 "--- FAIL:" 行
	output := "FAIL github.com/example/pkg"
	got := countTestFailures(output)
	if got < 1 {
		t.Errorf("should count at least 1 failure, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// extractFailingSummary
// ---------------------------------------------------------------------------

func TestExtractFailingSummary(t *testing.T) {
	output := `--- FAIL: TestLogin (0.01s)
    login_test.go:15: expected 200, got 401
--- PASS: TestLogout (0.00s)
FAIL`
	got := extractFailingSummary(output)
	if !strings.Contains(got, "FAIL: TestLogin") {
		t.Errorf("summary should contain failing test name, got %q", got)
	}
	if !strings.Contains(got, "FAIL") {
		t.Errorf("summary should contain FAIL line, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// extractTestArtifacts
// ---------------------------------------------------------------------------

func TestExtractTestArtifacts(t *testing.T) {
	output := "测试文件: user_test.go\n```go\npackage main\n\nfunc TestUser(t *testing.T) {}\n```"
	code, file := extractTestArtifacts(output, "Go")
	if file != "user_test.go" {
		t.Errorf("testFile = %q, want user_test.go", file)
	}
	if !strings.Contains(code, "TestUser") {
		t.Errorf("testCode should contain TestUser, got %q", code)
	}
}

func TestExtractTestArtifacts_NoCodeBlock(t *testing.T) {
	output := "just plain test code without markdown"
	code, file := extractTestArtifacts(output, "Python")
	if code != output {
		t.Errorf("should use full output as code, got %q", code)
	}
	if file != "test_task.py" {
		t.Errorf("should infer Python test file, got %q", file)
	}
}

func TestExtractTestArtifacts_Empty(t *testing.T) {
	code, file := extractTestArtifacts("", "Go")
	if code != "" || file != "" {
		t.Errorf("empty output should return empty, got code=%q file=%q", code, file)
	}
}

// ---------------------------------------------------------------------------
// VerifyByTest
// ---------------------------------------------------------------------------

func TestVerifyByTest_NoTestCmd(t *testing.T) {
	v := NewTaskVerifier(MaclawLLMConfig{})
	verdict := v.VerifyByTest("/tmp", "", "test_file.go")
	if !verdict.Pass {
		t.Error("no test command should default to pass")
	}
	if verdict.Score != 50 {
		t.Errorf("score = %d, want 50", verdict.Score)
	}
}

func TestVerifyByTest_EchoPass(t *testing.T) {
	v := NewTaskVerifier(MaclawLLMConfig{})
	// echo 命令退出码为 0，应该通过
	verdict := v.VerifyByTest(".", "echo all tests passed", "")
	if !verdict.Pass {
		t.Errorf("echo command should pass, got: %s", verdict.Reason)
	}
	if verdict.Score != 100 {
		t.Errorf("score = %d, want 100", verdict.Score)
	}
}

// ---------------------------------------------------------------------------
// verifyAgentOutput — TDD 模式分支
// ---------------------------------------------------------------------------

func TestVerifyAgentOutput_TDDMode(t *testing.T) {
	o := &SwarmOrchestrator{
		taskVerifier: NewTaskVerifier(MaclawLLMConfig{}),
	}
	// 使用 "echo" 技术栈，inferTestCommand 会返回 "echo no test command configured"
	// 这样 VerifyByTest 会快速执行 echo 命令（退出码 0）
	run := &SwarmRun{TechStack: "unknown-stack"}
	agent := &SwarmAgent{Output: "implemented code", WorktreePath: "."}
	task := SubTask{
		Index:       0,
		Description: "implement feature",
		TestFile:    "feature_test.go",
		TestCode:    "func TestFeature(t *testing.T) {}",
	}

	// 有 TestFile + TestCode → 走 TDD 验证路径（VerifyByTest）
	verdict := o.verifyAgentOutput(run, agent, task)
	if verdict == nil {
		t.Fatal("TDD mode should return a verdict")
	}
	// echo 命令退出码 0 → pass
	if !verdict.Pass {
		t.Errorf("echo-based test should pass, got: %s", verdict.Reason)
	}
}

func TestVerifyAgentOutput_FallbackToLLM(t *testing.T) {
	o := &SwarmOrchestrator{
		taskVerifier: NewTaskVerifier(MaclawLLMConfig{}),
	}
	run := &SwarmRun{TechStack: "Go"}
	agent := &SwarmAgent{Output: "implemented code"}
	task := SubTask{
		Index:       0,
		Description: "implement feature",
		// 没有 TestFile/TestCode → 回退到 LLM 验证
	}

	// LLM 调用会失败（无配置），但 Verify 内部会默认通过
	verdict := o.verifyAgentOutput(run, agent, task)
	if verdict == nil {
		t.Fatal("LLM fallback should return a verdict")
	}
	// LLM 调用失败时默认 pass=true, score=50
	if !verdict.Pass {
		t.Errorf("LLM fallback with no config should default to pass")
	}
}

// ---------------------------------------------------------------------------
// Developer prompt — TDD 模式
// ---------------------------------------------------------------------------

func TestDeveloperPrompt_TDDMode(t *testing.T) {
	ctx := PromptContext{
		ProjectName: "test-project",
		TechStack:   "Go",
		TaskDesc:    "实现用户认证",
		TestFile:    "auth_test.go",
		TestCode:    "func TestAuth(t *testing.T) { t.Fatal(\"not implemented\") }",
		TestCommand: "go test ./...",
	}
	got, err := RenderPrompt(RoleDeveloper, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "TDD 模式") {
		t.Error("TDD mode prompt should contain 'TDD 模式'")
	}
	if !strings.Contains(got, "auth_test.go") {
		t.Error("TDD mode prompt should contain test file path")
	}
	if !strings.Contains(got, "TestAuth") {
		t.Error("TDD mode prompt should contain test code")
	}
	if !strings.Contains(got, "go test") {
		t.Error("TDD mode prompt should contain test command")
	}
	// TDD 模式下不应该出现普通验收标准部分
	if strings.Contains(got, "验收标准（你的代码必须满足以下所有条件）") {
		t.Error("TDD mode should not show generic acceptance criteria section")
	}
}

// ---------------------------------------------------------------------------
// TestWriter prompt
// ---------------------------------------------------------------------------

func TestTestWriterPrompt(t *testing.T) {
	ctx := PromptContext{
		ProjectName:        "test-project",
		TechStack:          "Go",
		TaskDesc:           "实现登录功能",
		AcceptanceCriteria: "1. POST /login 返回 JWT\n2. 密码错误返回 401\n",
	}
	got, err := RenderPrompt(RoleTestWriter, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "测试工程师") {
		t.Error("test writer prompt should contain role description")
	}
	if !strings.Contains(got, "TDD") {
		t.Error("test writer prompt should mention TDD")
	}
	if !strings.Contains(got, "POST /login") {
		t.Error("test writer prompt should contain acceptance criteria")
	}
	if !strings.Contains(got, "红灯阶段") {
		t.Error("test writer prompt should mention red phase")
	}
}

// ---------------------------------------------------------------------------
// SubTask TDD fields
// ---------------------------------------------------------------------------

func TestSubTask_TDDFields(t *testing.T) {
	task := SubTask{
		Index:              0,
		Description:        "实现登录",
		AcceptanceCriteria: []string{"返回 JWT"},
		TestFile:           "login_test.go",
		TestCode:           "func TestLogin(t *testing.T) {}",
	}
	if task.TestFile != "login_test.go" {
		t.Errorf("TestFile = %q", task.TestFile)
	}
	if task.TestCode == "" {
		t.Error("TestCode should not be empty")
	}
}

func TestRoleTestWriter_Constant(t *testing.T) {
	if RoleTestWriter != "test_writer" {
		t.Errorf("RoleTestWriter = %q, want 'test_writer'", RoleTestWriter)
	}
}
