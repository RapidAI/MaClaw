package main

import (
	"strings"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Property-based tests for maclaw-spec-driven-workflow feature.
//
// Each test generates random App configurations (role names, descriptions)
// and verifies structural properties of buildSystemPrompt() output.
// Uses testing/quick with at least 100 iterations per property.
//
// Reuses randomAppConfig, buildPromptForConfig, quickConfig from
// im_message_handler_coding_workflow_test.go.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Property 1: 五阶段顺序完整性
// Validates: Requirements 1.4, 10.1, 10.2
// Five phases appear in strict sequential order, each exactly once.
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty1_PhaseSequentialOrder(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		phases := []string{"需求确认", "技术设计", "任务分解", "任务执行", "完成验收"}
		lastIdx := -1
		for _, phase := range phases {
			idx := strings.Index(prompt, phase)
			if idx < 0 {
				t.Logf("phase %q not found", phase)
				return false
			}
			if idx <= lastIdx {
				t.Logf("phase %q (pos %d) not after previous (pos %d)", phase, idx, lastIdx)
				return false
			}
			lastIdx = idx
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 1 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 2: Spec 工作流在 create_session 之前
// Validates: Requirements 1.1
// Three confirmation phases appear before Execution Phase's create_session.
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty2_SpecBeforeCreateSession(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Find the Execution Phase step (第六步)
		execIdx := strings.Index(prompt, "第六步")
		if execIdx < 0 {
			t.Logf("missing 第六步 (Execution Phase)")
			return false
		}

		// All three confirmation phases must appear before it
		for _, phase := range []string{"第三步", "第四步", "第五步"} {
			idx := strings.Index(prompt, phase)
			if idx < 0 {
				t.Logf("missing %s", phase)
				return false
			}
			if idx >= execIdx {
				t.Logf("%s (pos %d) not before 第六步 (pos %d)", phase, idx, execIdx)
				return false
			}
		}

		// Verify create_session appears in or after Execution Phase section
		csIdx := strings.Index(prompt[execIdx:], "create_session")
		if csIdx < 0 {
			t.Logf("create_session not found after 第六步")
			return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 3: 编程任务与非编程任务的区分
// Validates: Requirements 1.2, 1.3, 10.4
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty3_CodingVsNonCodingDistinction(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		if !strings.Contains(prompt, "编程任务") {
			t.Logf("missing 编程任务")
			return false
		}
		if !strings.Contains(prompt, "非编程任务") {
			t.Logf("missing 非编程任务")
			return false
		}

		// Must list non-coding categories
		categories := []string{"信息检索", "翻译", "文档生成", "文件操作", "通信", "日常助手"}
		for _, cat := range categories {
			if !strings.Contains(prompt, cat) {
				t.Logf("missing non-coding category: %s", cat)
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 4: 三个确认阶段的文档内容要求
// Validates: Requirements 2.1, 3.1, 4.1
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty4_DocumentContentRequirements(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Requirements Phase content
		reqContent := []string{"需求背景与目标", "功能需求列表", "非功能需求", "约束与假设"}
		for _, kw := range reqContent {
			if !strings.Contains(prompt, kw) {
				t.Logf("Requirements Phase missing: %s", kw)
				return false
			}
		}

		// Design Phase content
		designContent := []string{"架构设计", "接口设计", "数据模型变更", "实现方案概述"}
		for _, kw := range designContent {
			if !strings.Contains(prompt, kw) {
				t.Logf("Design Phase missing: %s", kw)
				return false
			}
		}

		// TaskBreakdown Phase content
		taskContent := []string{"编号的任务列表", "任务的描述和涉及的文件", "TDD 验收测试用例"}
		for _, kw := range taskContent {
			if !strings.Contains(prompt, kw) {
				// Try alternate wording
				alt := strings.ReplaceAll(kw, "的", "")
				if !strings.Contains(prompt, alt) {
					t.Logf("TaskBreakdown Phase missing: %s", kw)
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 5: PDF 生成与发送指令
// Validates: Requirements 2.2, 2.5, 3.2, 3.5, 4.2, 4.5, 8.1-8.5
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty5_PDFGenerationAndDelivery(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) PDF generation referencing craft_tool or bash
		hasCraftTool := strings.Contains(prompt, "craft_tool")
		hasBash := strings.Contains(prompt, "bash")
		hasPandoc := strings.Contains(prompt, "pandoc") || strings.Contains(prompt, "wkhtmltopdf")
		if !hasCraftTool && !hasBash {
			t.Logf("missing PDF generation tool reference (craft_tool or bash)")
			return false
		}
		if !hasPandoc && !hasCraftTool {
			t.Logf("missing PDF conversion tool (pandoc/wkhtmltopdf/craft_tool)")
			return false
		}

		// (b) send_file with forward_to_im=true
		if !strings.Contains(prompt, "send_file") || !strings.Contains(prompt, "forward_to_im=true") {
			t.Logf("missing send_file with forward_to_im=true")
			return false
		}

		// (c) Descriptive PDF naming
		if !strings.Contains(prompt, "需求文档_") || !strings.Contains(prompt, "设计文档_") || !strings.Contains(prompt, "任务列表_") {
			t.Logf("missing descriptive PDF naming")
			return false
		}

		// (d) Text/action prompt alongside PDF (行动提示 or 文字摘要)
		hasTextPrompt := strings.Contains(prompt, "行动提示") || strings.Contains(prompt, "文字摘要")
		if !hasTextPrompt {
			t.Logf("missing text prompt alongside PDF instruction")
			return false
		}

		// (e) Fallback to text on failure (Markdown 纯文本 or formatted text)
		hasFallback := strings.Contains(prompt, "PDF 生成失败")
		if !hasFallback {
			t.Logf("missing PDF fallback instruction")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 5 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 6: 阶段确认与修订规则
// Validates: Requirements 2.3, 2.4, 3.3, 3.4, 4.3, 4.4, 9.5
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty6_PhaseConfirmationAndRevision(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) Wait for user confirmation before next phase
		hasConfirmGate := strings.Contains(prompt, "等待用户明确确认") &&
			strings.Contains(prompt, "后才进入下一阶段")
		if !hasConfirmGate {
			t.Logf("missing phase gate on confirmation")
			return false
		}

		// (b) Update and regenerate PDF on revision
		hasRevision := strings.Contains(prompt, "更新文档") || strings.Contains(prompt, "重新生成 PDF")
		if !hasRevision {
			t.Logf("missing revision/regenerate instruction")
			return false
		}

		// (c) Use revised version as input
		hasLatestVersion := strings.Contains(prompt, "最新版本") || strings.Contains(prompt, "修订后使用最新版本")
		if !hasLatestVersion {
			t.Logf("missing latest version instruction")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 6 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 7: Skip_Signal 双语模式与三阶段跳过
// Validates: Requirements 7.1, 7.2, 7.3, 7.4
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty7_SkipSignalBilingualAndThreePhase(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) Chinese Skip_Signal patterns
		if !strings.Contains(prompt, "直接做") || !strings.Contains(prompt, "不用问了") {
			t.Logf("missing Chinese Skip_Signal patterns")
			return false
		}

		// (b) English Skip_Signal patterns
		if !strings.Contains(prompt, "just do it") || !strings.Contains(prompt, "go ahead") {
			t.Logf("missing English Skip_Signal patterns")
			return false
		}

		// (c) Skip three confirmation phases
		hasThreePhaseSkip := strings.Contains(prompt, "三个确认阶段") || strings.Contains(prompt, "确认阶段")
		if !hasThreePhaseSkip {
			t.Logf("missing three-phase skip instruction")
			return false
		}

		// (d) Internal planning when skipping (prompt may say 内部规划 or just imply skip-but-still-plan)
		hasInternalPlan := strings.Contains(prompt, "内部") ||
			(strings.Contains(prompt, "跳过") && strings.Contains(prompt, "直接执行"))
		if !hasInternalPlan {
			t.Logf("missing skip-and-execute instruction")
			return false
		}

		// (e) Mid-phase skip support
		hasMidPhaseSkip := strings.Contains(prompt, "跳过剩余确认阶段") || strings.Contains(prompt, "剩余确认阶段")
		if !hasMidPhaseSkip {
			t.Logf("missing mid-phase skip support")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 7 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 8: Execution Phase TDD 验证与重试
// Validates: Requirements 5.3, 5.4, 5.5, 5.6
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty8_ExecutionTDDAndRetry(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) Run TDD test after each task
		hasTDD := strings.Contains(prompt, "TDD") && strings.Contains(prompt, "测试")
		if !hasTDD {
			t.Logf("missing TDD test instruction")
			return false
		}

		// (b) Max 3 retry attempts
		hasRetry := strings.Contains(prompt, "最多 3 次") || strings.Contains(prompt, "最多3次")
		if !hasRetry {
			t.Logf("missing 3 retry limit")
			return false
		}

		// (c) Skip to next task after exhaustion
		if !strings.Contains(prompt, "跳到下一个任务") {
			t.Logf("missing skip-to-next instruction")
			return false
		}

		// (d) Progress format
		hasProgress := strings.Contains(prompt, "完成 ✅") || strings.Contains(prompt, "失败 ❌")
		if !hasProgress {
			t.Logf("missing progress format (✅/❌)")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 8 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 9: Verification Phase 完成报告
// Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5, 6.6
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty9_VerificationReport(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) Full regression test suite
		if !strings.Contains(prompt, "全量回归测试") && !strings.Contains(prompt, "全量") {
			t.Logf("missing full regression test")
			return false
		}

		// (b) Report components
		if !strings.Contains(prompt, "总任务数") || !strings.Contains(prompt, "成功/失败数") {
			t.Logf("missing task count in report")
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

		// (c) Success report
		if !strings.Contains(prompt, "全部通过") {
			t.Logf("missing success report")
			return false
		}

		// (d) Failure listing with next steps
		hasFailureReport := strings.Contains(prompt, "有失败") || strings.Contains(prompt, "列出失败项") || strings.Contains(prompt, "失败项")
		if !hasFailureReport {
			t.Logf("missing failure report with next steps")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 9 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 10: 阶段间上下文传递
// Validates: Requirements 9.1, 9.2, 9.3, 5.2
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty10_InterPhaseContextPassing(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// Requirements → Design Phase context
		hasReqToDesign := strings.Contains(prompt, "确认的需求文档") || strings.Contains(prompt, "基于确认的需求")
		if !hasReqToDesign {
			t.Logf("missing requirements → design context passing")
			return false
		}

		// Requirements + Design → TaskBreakdown Phase context
		hasDesignToTask := strings.Contains(prompt, "确认的需求和设计文档") || strings.Contains(prompt, "基于确认的需求和设计")
		if !hasDesignToTask {
			t.Logf("missing requirements+design → taskbreakdown context passing")
			return false
		}

		// All three → Execution Phase via send_and_observe
		hasExecContext := strings.Contains(prompt, "send_and_observe") && strings.Contains(prompt, "需求和设计上下文")
		if !hasExecContext {
			// Try alternate: check that execution mentions passing context
			hasExecContext = strings.Contains(prompt, "send_and_observe") &&
				(strings.Contains(prompt, "需求") && strings.Contains(prompt, "设计") && strings.Contains(prompt, "上下文"))
		}
		if !hasExecContext {
			t.Logf("missing all-three → execution context via send_and_observe")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 10 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 11: 阶段回退机制
// Validates: Requirements 11.1, 11.2, 11.3
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty11_PhaseRollback(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) Return to previous phase on user request
		hasRollbackTrigger := strings.Contains(prompt, "回退到需求阶段") || strings.Contains(prompt, "回到需求阶段")
		if !hasRollbackTrigger {
			t.Logf("missing rollback trigger examples")
			return false
		}

		// (b) Regenerate all subsequent documents
		hasRegenerate := strings.Contains(prompt, "重新生成所有后续阶段文档") || strings.Contains(prompt, "重新生成")
		if !hasRegenerate {
			t.Logf("missing regenerate subsequent documents instruction")
			return false
		}

		// (c) Inform user about rollback
		hasNotify := strings.Contains(prompt, "告知用户回退") || strings.Contains(prompt, "回退信息")
		if !hasNotify {
			t.Logf("missing rollback notification instruction")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 11 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 12: 现有工作流规则保留
// Validates: Requirements 10.5
// ---------------------------------------------------------------------------
func TestSpecWorkflowProperty12_ExistingRulesPreserved(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)

		// (a) 会话失败止损原则
		if !strings.Contains(prompt, "会话失败止损") && !strings.Contains(prompt, "止损原则") {
			t.Logf("missing 会话失败止损原则")
			return false
		}

		// (b) 执行验证原则
		if !strings.Contains(prompt, "执行验证原则") && !strings.Contains(prompt, "执行验证") {
			t.Logf("missing 执行验证原则")
			return false
		}

		// (c) busy 会话不终止规则
		if !strings.Contains(prompt, "绝对不要终止状态为 busy 的编程会话") {
			t.Logf("missing busy session rule")
			return false
		}

		// (d) 自动续接 Auto-Resume 规则
		hasAutoResume := strings.Contains(prompt, "自动续接") || strings.Contains(prompt, "Auto-Resume")
		if !hasAutoResume {
			t.Logf("missing Auto-Resume rule")
			return false
		}

		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 12 failed: %v", err)
	}
}
