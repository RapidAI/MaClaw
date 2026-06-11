package agent

import (
	"strings"
	"testing"
)

func TestExtractStructuralSkeleton_NumberedList(t *testing.T) {
	text := `基于 git commit 记录分析，发现以下 4 个高频打点健壮性问题：

1. 缺少重试机制：当网络不稳定时，打点请求直接丢失，没有任何重试逻辑
2. 异常未捕获：JSON 序列化失败时抛出未捕获异常，导致后续打点全部中断
3. 时间戳精度不足：使用秒级时间戳，无法区分 1 秒内的多次操作
4. 批量上报缺失：每次事件都单独发送 HTTP 请求，高频场景下严重影响性能

建议逐一修复以上问题，优先处理第 1 和第 2 个（影响数据完整性）。`

	skeleton := extractStructuralSkeleton(text)

	// All 4 numbered items must be present in the skeleton.
	for _, marker := range []string{"1.", "2.", "3.", "4."} {
		if !strings.Contains(skeleton, marker) {
			t.Errorf("skeleton missing numbered item %q\nskeleton:\n%s", marker, skeleton)
		}
	}
	// Key titles must be preserved.
	for _, title := range []string{"缺少重试机制", "异常未捕获", "时间戳精度不足", "批量上报缺失"} {
		if !strings.Contains(skeleton, title) {
			t.Errorf("skeleton missing title %q\nskeleton:\n%s", title, skeleton)
		}
	}
	// The preamble should be included.
	if !strings.Contains(skeleton, "高频打点健壮性") {
		t.Errorf("skeleton missing preamble context\nskeleton:\n%s", skeleton)
	}
}

func TestExtractStructuralSkeleton_MarkdownHeadings(t *testing.T) {
	text := `# 分析结果

## 问题一：内存泄漏
在 EventBus 的 subscribe 方法中，闭包持有外部引用...

## 问题二：竞态条件
多线程环境下 counter 没有加锁保护...

## 问题三：资源未释放
文件句柄在异常路径下未 close...`

	skeleton := extractStructuralSkeleton(text)

	for _, heading := range []string{"# 分析结果", "## 问题一", "## 问题二", "## 问题三"} {
		if !strings.Contains(skeleton, heading) {
			t.Errorf("skeleton missing heading %q\nskeleton:\n%s", heading, skeleton)
		}
	}
}

func TestExtractStructuralSkeleton_BulletPoints(t *testing.T) {
	text := `修改建议如下：
- 添加指数退避重试（最多 3 次）
- 用 try-catch 包裹 JSON.stringify 调用
- 切换到毫秒级时间戳
- 实现 BatchQueue 批量上报`

	skeleton := extractStructuralSkeleton(text)

	for _, bullet := range []string{"添加指数退避重试", "try-catch", "毫秒级时间戳", "BatchQueue"} {
		if !strings.Contains(skeleton, bullet) {
			t.Errorf("skeleton missing bullet content %q\nskeleton:\n%s", bullet, skeleton)
		}
	}
}

func TestExtractStructuralSkeleton_PlainProse(t *testing.T) {
	text := `经过分析，这段代码的主要问题在于错误处理逻辑不完善。当 HTTP 请求返回非 200 状态码时，代码直接忽略了响应体中可能包含的错误信息，导致调试困难。同时日志级别设置不当，生产环境中 DEBUG 级别日志过多影响性能。`

	skeleton := extractStructuralSkeleton(text)

	// For plain prose, should get generous truncation (500 runes).
	if len([]rune(skeleton)) > 500 {
		t.Errorf("prose skeleton exceeds 500 rune budget: %d runes", len([]rune(skeleton)))
	}
	// Should contain the beginning of the text.
	if !strings.Contains(skeleton, "错误处理逻辑不完善") {
		t.Errorf("prose skeleton missing key content\nskeleton:\n%s", skeleton)
	}
}

func TestExtractStructuralSkeleton_ChineseNumberedList(t *testing.T) {
	text := `发现以下问题：
1、重复发送相同事件
2、缓存未设置过期时间
3、线程安全问题`

	skeleton := extractStructuralSkeleton(text)

	for _, marker := range []string{"1、", "2、", "3、"} {
		if !strings.Contains(skeleton, marker) {
			t.Errorf("skeleton missing Chinese numbered item %q\nskeleton:\n%s", marker, skeleton)
		}
	}
}

func TestBuildCurrentTaskSummary_PreservesStructuredOutput(t *testing.T) {
	history := []ConversationEntry{
		{Role: "user", Content: "基于git commit记录分析高频打点健壮性"},
		{Role: "assistant", Content: "分析完成，发现 4 个问题：\n1. 缺少重试机制\n2. 异常未捕获\n3. 时间戳精度\n4. 批量上报缺失"},
	}

	summary := buildCurrentTaskSummary(history)

	// The summary must contain all 4 items so the classifier can match
	// "将4个问题做修复" to the current task.
	if !strings.Contains(summary, "1.") || !strings.Contains(summary, "4.") {
		t.Errorf("summary lost structured items:\n%s", summary)
	}
	if !strings.Contains(summary, "缺少重试机制") {
		t.Errorf("summary lost item title:\n%s", summary)
	}
}

func TestBuildCurrentTaskSummary_StructuredNotLastMessage(t *testing.T) {
	// Simulates the case where the structured output (4 issues) is followed
	// by a short closing remark from the assistant. The summary should still
	// pick up the structured message, not the short closing.
	history := []ConversationEntry{
		{Role: "user", Content: "基于git commit记录分析高频打点健壮性"},
		{Role: "assistant", Content: "分析完成，发现 4 个问题：\n1. 缺少重试机制\n2. 异常未捕获\n3. 时间戳精度不足\n4. 批量上报缺失"},
		{Role: "assistant", Content: "建议优先修复第 1 和第 2 个问题。"},
	}

	summary := buildCurrentTaskSummary(history)

	// The structured items must be present even though the last assistant
	// message is a short prose sentence.
	if !strings.Contains(summary, "1.") || !strings.Contains(summary, "4.") {
		t.Errorf("summary should prefer structured message over short closing:\n%s", summary)
	}
	if !strings.Contains(summary, "缺少重试机制") {
		t.Errorf("summary lost structured item title:\n%s", summary)
	}
}

func TestIsStructuredLine(t *testing.T) {
	positives := []string{
		"1. First item",
		"2) Second item",
		"1、中文编号",
		"- bullet point",
		"* star bullet",
		"• unicode bullet",
		"# Heading",
		"## Sub heading",
		"### Third level",
		"(1) Parenthesized",
	}
	for _, line := range positives {
		if !isStructuredLine(line) {
			t.Errorf("expected isStructuredLine(%q) = true", line)
		}
	}

	negatives := []string{
		"This is plain text.",
		"No structure here, just prose.",
		"",
		"a) not a number",        // starts with letter, not digit
		"import React from 'react'", // code
	}
	for _, line := range negatives {
		if isStructuredLine(line) {
			t.Errorf("expected isStructuredLine(%q) = false", line)
		}
	}
}
