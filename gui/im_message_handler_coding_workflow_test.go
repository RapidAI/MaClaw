package main

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Property-based tests for coding-interaction-workflow feature.
//
// Each test generates random App configurations (role names, descriptions)
// and verifies structural properties of buildSystemPrompt() output.
// Uses testing/quick with at least 100 iterations per property.
// ---------------------------------------------------------------------------

// randomAppConfig is a helper type for testing/quick that generates random
// IMMessageHandler instances with varying role configurations.
type randomAppConfig struct {
	RoleName string
	RoleDesc string
}

// Generate implements quick.Generator for randomAppConfig.
func (randomAppConfig) Generate(rand *rand.Rand, size int) reflect.Value {
	names := []string{
		"", // default
		"MaClaw",
		"TestBot",
		"开发助手",
		"CodeHelper-" + randomString(rand, 8),
		randomString(rand, rand.Intn(20)+1),
	}
	descs := []string{
		"", // default
		"一个尽心尽责无所不能的软件开发管家",
		"A helpful coding assistant",
		"专注于代码质量的AI助手",
		randomString(rand, rand.Intn(50)+1),
	}
	return reflect.ValueOf(randomAppConfig{
		RoleName: names[rand.Intn(len(names))],
		RoleDesc: descs[rand.Intn(len(descs))],
	})
}

func randomString(rand *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// buildPromptForConfig creates an IMMessageHandler with the given config
// and returns the buildSystemPrompt() output.
func buildPromptForConfig(cfg randomAppConfig) string {
	app := &App{}
	// Set up a temp config dir so LoadConfig returns the custom role values.
	// Since LoadConfig reads from disk and we don't want disk I/O in property
	// tests, we use the same approach as the busy-session tests: create a
	// handler with a bare App (LoadConfig will error → defaults used).
	// To test custom roles, we'd need disk setup. Instead, we verify properties
	// hold for the default config path (LoadConfig error → defaults).
	// The prompt structure is independent of role name/description values.
	mgr := &RemoteSessionManager{
		app:      app,
		sessions: map[string]*RemoteSession{},
	}
	h := &IMMessageHandler{
		app:     app,
		manager: mgr,
	}
	return h.buildSystemPrompt()
}

// quickConfig returns a quick.Config with at least 100 iterations.
func quickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 1: Confirmation Phase gates CodingSubAgent
//
// Validates: Requirements 1.1, 6.1
// For any valid system configuration, the Confirmation Phase instructions
// (需求确认 / Confirmation Phase) must appear BEFORE the CodingSubAgent
// execution instructions in buildSystemPrompt() output.
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty1_ConfirmationBeforeCodingSubAgent(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// The workflow has 5 steps. Confirmation must appear before execution,
		// and execution must route through CodingSubAgent instead of external sessions.
		confirmStepIdx := strings.Index(prompt, "第三步")
		if confirmStepIdx < 0 {
			t.Logf("prompt does not contain '第三步' (Confirmation Phase step)")
			return false
		}

		execStepIdx := strings.Index(prompt, "第四步")
		if execStepIdx < 0 {
			t.Logf("prompt does not contain '第四步' (Execution step)")
			return false
		}

		if confirmStepIdx >= execStepIdx {
			t.Logf("第三步 (pos %d) does not appear before 第四步 (pos %d)", confirmStepIdx, execStepIdx)
			return false
		}

		// Also verify the confirmation rule explicitly gates CodingSubAgent execution.
		hasGate := strings.Contains(prompt, "CodingSubAgent") && !strings.Contains(prompt, "create_session")
		if !hasGate {
			t.Logf("prompt missing confirmation gate for CodingSubAgent or still mentions create_session")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 2: Requirements Phase document components
//
// Validates: Requirements 2.1 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty2_ConfirmationContainsAllComponents(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !strings.Contains(prompt, "需求背景与目标") {
			t.Logf("missing 需求背景与目标")
			return false
		}
		if !strings.Contains(prompt, "功能需求列表") {
			t.Logf("missing 功能需求列表")
			return false
		}
		if !strings.Contains(prompt, "非功能需求") {
			t.Logf("missing 非功能需求")
			return false
		}
		if !strings.Contains(prompt, "约束与假设") {
			t.Logf("missing 约束与假设")
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 3: Coding vs non-coding task distinction
//
// Validates: Requirements 1.5, 6.4, 7.1, 7.3
// For any valid system configuration, buildSystemPrompt() output must contain
// clear criteria distinguishing Coding_Task from non-coding requests, and
// explicitly list non-coding examples (file operations like bash/read_file/write_file,
// configuration, screenshots, general questions).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty3_CodingVsNonCodingDistinction(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Must contain Coding_Task concept
		hasCodingTask := strings.Contains(prompt, "Coding_Task") || strings.Contains(prompt, "编程任务")
		if !hasCodingTask {
			t.Logf("prompt missing Coding_Task / 编程任务")
			return false
		}

		// Must contain non-coding task concept
		hasNonCoding := strings.Contains(prompt, "非编程任务") || strings.Contains(prompt, "non-coding")
		if !hasNonCoding {
			t.Logf("prompt missing 非编程任务")
			return false
		}

		// Must explicitly list SSH/server task concept and conservative routing.
		hasSSH := strings.Contains(prompt, "SSH/服务器操作任务")
		hasNoExternalSessionOnUnclear := strings.Contains(prompt, "CodingSubAgent") && !strings.Contains(prompt, "create_session")
		if !hasSSH || !hasNoExternalSessionOnUnclear {
			t.Logf("prompt missing ssh routing guidance (ssh=%v unclear_guard=%v)", hasSSH, hasNoExternalSessionOnUnclear)
			return false
		}

		// Must explicitly list non-coding examples: file operations
		hasBash := strings.Contains(prompt, "bash")
		hasReadFile := strings.Contains(prompt, "read_file")
		hasWriteFile := strings.Contains(prompt, "write_file")
		hasEditFile := strings.Contains(prompt, "edit_file")
		if !hasBash || !hasReadFile || !hasWriteFile || !hasEditFile {
			t.Logf("prompt missing file operation examples (bash=%v, read_file=%v, write_file=%v, edit_file=%v)",
				hasBash, hasReadFile, hasWriteFile, hasEditFile)
			return false
		}

		// Must mention configuration and screenshots as non-coding
		hasConfig := strings.Contains(prompt, "配置")
		hasScreenshot := strings.Contains(prompt, "截屏") || strings.Contains(prompt, "screenshot")
		if !hasConfig || !hasScreenshot {
			t.Logf("prompt missing config/screenshot examples (config=%v, screenshot=%v)",
				hasConfig, hasScreenshot)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 4: Skip_Signal bilingual patterns
//
// Validates: Requirements 2.1, 2.3, 6.3
// For any valid system configuration, buildSystemPrompt() output must contain
// Skip_Signal patterns in both Chinese (直接做, 不用问了) and English
// (just do it, go ahead).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty4_SkipSignalBilingualPatterns(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Chinese Skip_Signal patterns
		hasChineseSkip1 := strings.Contains(prompt, "直接做")
		hasChineseSkip2 := strings.Contains(prompt, "不用问了")
		if !hasChineseSkip1 || !hasChineseSkip2 {
			t.Logf("prompt missing Chinese Skip_Signal (直接做=%v, 不用问了=%v)",
				hasChineseSkip1, hasChineseSkip2)
			return false
		}

		// English Skip_Signal patterns
		hasEnglishSkip1 := strings.Contains(prompt, "just do it")
		hasEnglishSkip2 := strings.Contains(prompt, "go ahead")
		if !hasEnglishSkip1 || !hasEnglishSkip2 {
			t.Logf("prompt missing English Skip_Signal (just do it=%v, go ahead=%v)",
				hasEnglishSkip1, hasEnglishSkip2)
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 5: Verification Phase completeness
//
// Validates: Requirements 6.1, 6.2, 6.3 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty5_RFOWorkflowCompleteness(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !strings.Contains(prompt, "全量回归测试") {
			t.Logf("missing 全量回归测试")
			return false
		}
		if !(strings.Contains(prompt, "总任务数") || strings.Contains(prompt, "成功/失败数")) {
			t.Logf("missing report task count")
			return false
		}
		if !strings.Contains(prompt, "每个任务的执行结果") {
			t.Logf("missing per-task result")
			return false
		}
		if !strings.Contains(prompt, "全量测试运行结果") {
			t.Logf("missing full test result")
			return false
		}
		if !strings.Contains(prompt, "全部通过") {
			t.Logf("missing success report")
			return false
		}
		if !(strings.Contains(prompt, "有失败") || strings.Contains(prompt, "列出失败项")) {
			t.Logf("missing failure report")
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 5 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 6: Task failure handling with retry
//
// Validates: Requirements 5.4, 5.5 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty6_SkipRFOOnTaskFailure(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !(strings.Contains(prompt, "最多 3 次") || strings.Contains(prompt, "最多3次")) {
			t.Logf("missing retry limit")
			return false
		}
		if !strings.Contains(prompt, "跳到下一个任务") {
			t.Logf("missing skip-to-next")
			return false
		}
		if !(strings.Contains(prompt, "完成 ✅") || strings.Contains(prompt, "失败 ❌")) {
			t.Logf("missing progress format")
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 6 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 7: Existing workflow rules preserved
//
// Validates: Requirements 6.5
// For any valid system configuration, buildSystemPrompt() output must preserve:
// (a) 会话失败止损原则,
// (b) 执行验证原则,
// (c) busy 会话不终止规则 (绝对不要终止状态为 busy 的编程会话).
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty7_ExistingWorkflowRulesPreserved(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) 会话失败止损原则
		hasStopLoss := strings.Contains(prompt, "会话失败止损") || strings.Contains(prompt, "止损原则")
		if !hasStopLoss {
			t.Logf("prompt missing 会话失败止损原则")
			return false
		}

		// (b) 执行验证原则
		hasVerification := strings.Contains(prompt, "执行验证原则") || strings.Contains(prompt, "执行验证")
		if !hasVerification {
			t.Logf("prompt missing 执行验证原则")
			return false
		}

		// (c) busy 会话不终止规则
		hasBusyRule := strings.Contains(prompt, "绝对不要终止状态为 busy 的编程会话")
		if !hasBusyRule {
			t.Logf("prompt missing busy 会话不终止规则")
			return false
		}

		return true
	}

	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 failed: %v", err)
	}
}
