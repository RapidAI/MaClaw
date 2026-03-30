package main

import (
	"fmt"
	"os"
	"strings"
)

// replaceFunc replaces a top-level function and its preceding comment block.
// It finds "func funcName(" and looks backward for the comment block start,
// then forward for the next top-level func or EOF.
func replaceFunc(content, funcName, replacement string) string {
	marker := "func " + funcName + "("
	idx := strings.Index(content, marker)
	if idx < 0 {
		fmt.Fprintf(os.Stderr, "func %s not found\n", funcName)
		return content
	}

	// Find start of comment block before the function
	// Look for the "// ---" line that starts the comment block
	blockStart := idx
	for blockStart > 0 {
		nlPos := strings.LastIndex(content[:blockStart-1], "\n")
		if nlPos < 0 {
			break
		}
		line := content[nlPos+1 : blockStart-1]
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "//") || line == "" {
			blockStart = nlPos + 1
		} else {
			break
		}
	}

	// Find end of function: next top-level "func " or "// ---" block
	rest := content[idx:]
	// Skip past the opening brace
	braceIdx := strings.Index(rest, "{\n")
	if braceIdx < 0 {
		braceIdx = strings.Index(rest, "{\r\n")
	}
	if braceIdx < 0 {
		fmt.Fprintf(os.Stderr, "opening brace not found for %s\n", funcName)
		return content
	}

	// Find the matching closing brace (top-level, no indent)
	searchFrom := idx + braceIdx + 1
	funcEnd := -1
	for i := searchFrom; i < len(content); i++ {
		if content[i] == '}' && (i == 0 || content[i-1] == '\n') {
			funcEnd = i + 1
			// Skip trailing newlines
			for funcEnd < len(content) && (content[funcEnd] == '\r' || content[funcEnd] == '\n') {
				funcEnd++
			}
			break
		}
	}
	if funcEnd < 0 {
		fmt.Fprintf(os.Stderr, "closing brace not found for %s\n", funcName)
		return content
	}

	fmt.Printf("Replacing %s: pos %d..%d (%d bytes)\n", funcName, blockStart, funcEnd, funcEnd-blockStart)
	return content[:blockStart] + replacement + content[funcEnd:]
}

func nl(s string, crlf bool) string {
	if crlf {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func main() {
	data, err := os.ReadFile("gui/im_message_handler_coding_workflow_test.go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}
	c := string(data)
	crlf := strings.Contains(c, "\r\n")

	new2 := nl(`// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 2: Requirements Phase document components
//
// Validates: Requirements 2.1 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty2_ConfirmationContainsAllComponents(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !strings.Contains(prompt, "需求背景与目标") {
			t.Logf("missing 需求背景与目标"); return false
		}
		if !strings.Contains(prompt, "功能需求列表") {
			t.Logf("missing 功能需求列表"); return false
		}
		if !strings.Contains(prompt, "非功能需求") {
			t.Logf("missing 非功能需求"); return false
		}
		if !strings.Contains(prompt, "约束与假设") {
			t.Logf("missing 约束与假设"); return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}
`, crlf)

	new5 := nl(`// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 5: Verification Phase completeness
//
// Validates: Requirements 6.1, 6.2, 6.3 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty5_RFOWorkflowCompleteness(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !strings.Contains(prompt, "全量回归测试") {
			t.Logf("missing 全量回归测试"); return false
		}
		if !(strings.Contains(prompt, "总任务数") || strings.Contains(prompt, "成功/失败数")) {
			t.Logf("missing report task count"); return false
		}
		if !strings.Contains(prompt, "每个任务的执行结果") {
			t.Logf("missing per-task result"); return false
		}
		if !strings.Contains(prompt, "全量测试运行结果") {
			t.Logf("missing full test result"); return false
		}
		if !strings.Contains(prompt, "全部通过") {
			t.Logf("missing success report"); return false
		}
		if !(strings.Contains(prompt, "有失败") || strings.Contains(prompt, "列出失败项")) {
			t.Logf("missing failure report"); return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 5 failed: %v", err)
	}
}
`, crlf)

	new6 := nl(`// ---------------------------------------------------------------------------
// Feature: coding-interaction-workflow, Property 6: Task failure handling with retry
//
// Validates: Requirements 5.4, 5.5 (maclaw-spec-driven-workflow)
// ---------------------------------------------------------------------------
func TestCodingWorkflowProperty6_SkipRFOOnTaskFailure(t *testing.T) {
	f := func(cfg randomAppConfig) bool {
		prompt := buildPromptForConfig(cfg)
		if !(strings.Contains(prompt, "最多 3 次") || strings.Contains(prompt, "最多3次")) {
			t.Logf("missing retry limit"); return false
		}
		if !strings.Contains(prompt, "跳到下一个任务") {
			t.Logf("missing skip-to-next"); return false
		}
		if !(strings.Contains(prompt, "完成 ✅") || strings.Contains(prompt, "失败 ❌")) {
			t.Logf("missing progress format"); return false
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 6 failed: %v", err)
	}
}
`, crlf)

	c = replaceFunc(c, "TestCodingWorkflowProperty2_ConfirmationContainsAllComponents", new2)
	c = replaceFunc(c, "TestCodingWorkflowProperty5_RFOWorkflowCompleteness", new5)
	c = replaceFunc(c, "TestCodingWorkflowProperty6_SkipRFOOnTaskFailure", new6)

	if err := os.WriteFile("gui/im_message_handler_coding_workflow_test.go", []byte(c), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Done.")
}
