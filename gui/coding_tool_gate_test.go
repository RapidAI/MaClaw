package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Unit tests (example-based) for the Coding Tool Gate.
// ---------------------------------------------------------------------------

func makeToolCall(name string) llm.ToolCall {
	return llm.ToolCall{
		ID:   "call_" + name,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: "{}"},
	}
}

// 4.2 Test blocklist contains all specified coding and browser tools.
func TestCodingGate_BlocklistContainsAllCodingTools(t *testing.T) {
	// Coding session tools.
	codingTools := []string{
		"create_session", "bash", "write_file", "edit_file", "edit_lines",
		"craft_tool", "send_and_observe", "control_session",
	}
	// Browser automation tools — unified "browser" tool replaces individual
	// browser_* tools. Remaining individual tools are task/record/ocr/gui.
	browserTools := []string{
		"browser",
		"browser_task_run", "browser_task_replay", "browser_task_verify", "browser_task_status",
		"browser_record_start", "browser_record_stop", "browser_list_flows",
		"browser_ocr",
		"gui_record_start", "gui_record_stop",
		"gui_observe", "gui_verify",
	}
	expected := append(codingTools, browserTools...)
	for _, name := range expected {
		if !codingToolBlocklist[name] {
			t.Errorf("codingToolBlocklist missing %q", name)
		}
	}
	if len(codingToolBlocklist) != len(expected) {
		t.Errorf("codingToolBlocklist has %d entries, expected %d", len(codingToolBlocklist), len(expected))
	}
}

// 4.3 Test allowlist contains all specified delivery tools.
func TestCodingGate_AllowlistContainsAllDeliveryTools(t *testing.T) {
	expected := []string{
		"generate_pdf", "send_file", "memory", "open",
		"set_nickname", "manage_config", "ask_user", "task",
		"office",
	}
	for _, name := range expected {
		if !deliveryToolAllowlist[name] {
			t.Errorf("deliveryToolAllowlist missing %q", name)
		}
	}
	if len(deliveryToolAllowlist) != len(expected) {
		t.Errorf("deliveryToolAllowlist has %d entries, expected %d", len(deliveryToolAllowlist), len(expected))
	}
}

// ---------------------------------------------------------------------------
// Fail-closed design: without classifiers, gate defaults to active=true.
// This is the correct safety behavior — coding tools are blocked until
// the classifier can make an informed decision.
// ---------------------------------------------------------------------------

// 4.4 Test newCodingToolGateConfig without classifiers returns fail-closed (active=true).
func TestCodingGate_WithoutClassifiers_FailClosed(t *testing.T) {
	cfg := newCodingToolGateConfig("帮我写代码", LoopKindChat)
	if !cfg.active {
		t.Errorf("without classifiers, expected active=true (fail-closed), got false; reason=%s", cfg.reason)
	}
}

// 4.5 Test fail-closed applies to ALL messages when classifiers are unavailable.
// This is by design: the gate cannot distinguish coding from non-coding
// without a classifier, so it conservatively blocks coding tools for all.
func TestCodingGate_WithoutClassifiers_FailClosedForAll(t *testing.T) {
	cases := []string{
		"帮我翻译这段话",
		"ssh 到服务器看日志",
		"部署到线上",
		"开发一个贪吃蛇游戏",
		"",
	}
	for _, text := range cases {
		cfg := newCodingToolGateConfig(text, LoopKindChat)
		if !cfg.active {
			t.Errorf("text=%q: without classifiers, expected active=true (fail-closed), got false; reason=%s", text, cfg.reason)
		}
	}
}

// 4.6 Test newCodingToolGateConfig returns active=false for LoopKindBackground.
// Background loops always bypass the gate, even in fail-closed mode.
func TestCodingGate_InactiveForBackground(t *testing.T) {
	cfg := newCodingToolGateConfig("帮我写代码", LoopKindBackground)
	if cfg.active {
		t.Errorf("expected active=false for background loop, got true; reason=%s", cfg.reason)
	}
}

// 4.7 Test skip signals are NOT honored in fail-closed mode.
// Skip signal detection requires the classifier (GateIntentContinuation).
// Without a classifier, the gate cannot verify the skip signal is legitimate.
func TestCodingGate_SkipSignalIgnoredWithoutClassifier(t *testing.T) {
	signals := []string{
		"帮我写代码，直接做",
		"写个爬虫，不用问了",
		"write a script, just do it",
		"build a tool, go ahead",
	}
	for _, text := range signals {
		cfg := newCodingToolGateConfig(text, LoopKindChat)
		if !cfg.active {
			t.Errorf("text=%q: without classifier, expected active=true (fail-closed), got false; reason=%s", text, cfg.reason)
		}
	}
}

// 4.8 Test gate strips coding tools but preserves delivery tools in a mixed list.
func TestCodingGate_MixedToolList(t *testing.T) {
	calls := []llm.ToolCall{
		makeToolCall("bash"),
		makeToolCall("generate_pdf"),
		makeToolCall("write_file"),
		makeToolCall("send_file"),
		makeToolCall("create_session"),
	}
	result := applyCodingToolGate(calls)
	if !result.applied {
		t.Fatal("expected applied=true")
	}
	if len(result.stripped) != 3 {
		t.Errorf("expected 3 stripped, got %d", len(result.stripped))
	}
	if len(result.remaining) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(result.remaining))
	}
	// Verify order preserved.
	if result.remaining[0].Function.Name != "generate_pdf" {
		t.Errorf("expected remaining[0]=generate_pdf, got %s", result.remaining[0].Function.Name)
	}
	if result.remaining[1].Function.Name != "send_file" {
		t.Errorf("expected remaining[1]=send_file, got %s", result.remaining[1].Function.Name)
	}
}

// 4.9 Test gate handles empty tool call list (no-op).
func TestCodingGate_EmptyToolList(t *testing.T) {
	result := applyCodingToolGate(nil)
	if result.applied {
		t.Error("expected applied=false for empty list")
	}
	if len(result.stripped) != 0 || len(result.remaining) != 0 {
		t.Error("expected empty stripped and remaining")
	}
}

// 4.10 Test gate handles list with only delivery tools (no stripping).
func TestCodingGate_OnlyDeliveryTools(t *testing.T) {
	calls := []llm.ToolCall{
		makeToolCall("generate_pdf"),
		makeToolCall("send_file"),
		makeToolCall("memory"),
	}
	result := applyCodingToolGate(calls)
	if result.applied {
		t.Error("expected applied=false for delivery-only list")
	}
	if len(result.remaining) != 3 {
		t.Errorf("expected 3 remaining, got %d", len(result.remaining))
	}
}

// 4.11 Test gate handles list with only coding tools (all stripped).
func TestCodingGate_OnlyCodingTools(t *testing.T) {
	calls := []llm.ToolCall{
		makeToolCall("bash"),
		makeToolCall("write_file"),
		makeToolCall("create_session"),
	}
	result := applyCodingToolGate(calls)
	if !result.applied {
		t.Fatal("expected applied=true")
	}
	if len(result.stripped) != 3 {
		t.Errorf("expected 3 stripped, got %d", len(result.stripped))
	}
	if len(result.remaining) != 0 {
		t.Errorf("expected 0 remaining, got %d", len(result.remaining))
	}
}

// ---------------------------------------------------------------------------
// Bug-fix keyword detection tests — isBugFixOnly is still used by the
// classifier path (GIC/UIC), not by the degraded mode.
// ---------------------------------------------------------------------------

// Test isBugFixOnly returns true for pure bug-fix messages.
func TestCodingGate_IsBugFixOnly_PureBugFix(t *testing.T) {
	cases := []string{
		"有bug，一直显示加载中",
		"修复加载错误",
		"修bug",
		"修复 bug",
		"页面白屏了",
		"程序崩溃了",
		"调试一下这个问题",
		"排查一下报错",
		"fix the loading issue",
		"debug this crash",
		"这个功能不显示",
		"加载中卡住了",
	}
	for _, text := range cases {
		if !isBugFixOnly(text) {
			t.Errorf("isBugFixOnly(%q) = false, want true", text)
		}
	}
}

// Test isBugFixOnly returns false when creation keywords are also present.
func TestCodingGate_IsBugFixOnly_MixedWithCreation(t *testing.T) {
	cases := []string{
		"开发一个游戏，修复之前的bug",
		"写代码实现登录功能，顺便修复报错",
		"开发前端页面，有个bug要修",
	}
	for _, text := range cases {
		if isBugFixOnly(text) {
			t.Errorf("isBugFixOnly(%q) = true, want false (has creation keywords)", text)
		}
	}
}

// Test isBugFixOnly returns false for non-bug-fix messages.
func TestCodingGate_IsBugFixOnly_NoBugFixKeywords(t *testing.T) {
	cases := []string{
		"帮我写代码",
		"开发一个贪吃蛇游戏",
		"翻译这段话",
		"",
	}
	for _, text := range cases {
		if isBugFixOnly(text) {
			t.Errorf("isBugFixOnly(%q) = true, want false", text)
		}
	}
}

// Test fail-closed applies to bug-fix messages too when classifiers are unavailable.
// Bug-fix detection requires the classifier (GateIntentBugFix).
func TestCodingGate_BugFixNotDetectedWithoutClassifier(t *testing.T) {
	cases := []string{
		"有bug，一直显示加载中",
		"修复加载错误",
		"调试一下这个崩溃",
		"排查报错原因",
	}
	for _, text := range cases {
		cfg := newCodingToolGateConfig(text, LoopKindChat)
		if !cfg.active {
			t.Errorf("text=%q: without classifier, expected active=true (fail-closed), got false; reason=%s", text, cfg.reason)
		}
		// bugFix is NOT set in fail-closed mode — only the classifier can determine this.
		if cfg.bugFix {
			t.Errorf("text=%q: expected bugFix=false in fail-closed mode", text)
		}
	}
}

// Test fail-closed applies to mixed tasks too when classifiers are unavailable.
func TestCodingGate_WithoutClassifiers_MixedTaskFailClosed(t *testing.T) {
	cfg := newCodingToolGateConfig("开发一个bug追踪系统", LoopKindChat)
	if !cfg.active {
		t.Errorf("without classifiers, expected active=true (fail-closed), got false; reason=%s", cfg.reason)
	}
}
