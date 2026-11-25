package codingagent

import (
	"regexp"
	"strings"
)

var finalAnswerPlanStepHeading = regexp.MustCompile(`(?m)^### T\d+`)

var finalAnswerInternalSummaryHeadings = map[string]bool{
	"质量审计": true, "验证状态": true, "探索状态": true, "diff 自检": true,
	"安全边界": true, "命令验证": true, "动态工具": true, "步骤清单": true,
	"远程 diff 自检": true, "确认状态": true, "命令状态": true, "无改动证据": true,
	"执行报告": true, "執行報告": true, "execution report": true,
	"coding task execution report": true, "coding execution report": true, "execution stats": true,
	"quality audit": true, "exploration status": true, "verification status": true,
	"diff self-check": true, "security boundary": true, "command verification": true,
	"dynamic tools": true, "step checklist": true, "remote diff self-check": true,
	"confirmation status": true, "command status": true, "no-change evidence": true,
	"failed tasks": true, "计划执行结果": true, "按已批准计划执行": true, "plan execution result": true,
	// Bare file-ledger sections are host audit evidence, not a readable
	// explanation. The host fact fallback adds a compact sentence when needed.
	"涉及文件": true, "文件变更": true, "文件改动": true, "改动文件": true,
	"involved files": true, "files involved": true, "files changed": true, "file changes": true,
}

var finalAnswerSummaryHeadingAliases = map[string]string{
	"验证结果": "验证", "验证命令": "验证",
	"摘要": "结果",
}

var finalAnswerAdaptiveSummaryHeadings = map[string]bool{
	"实现": true, "接入": true, "接入位置": true, "行为变化": true,
	"修复的问题": true, "根因与改法": true, "优化": true, "审查过但不改的点": true,
	"发现": true, "证据": true, "建议": true, "验证": true, "执行结果": true,
	"命令与输出": true, "当前状态": true, "阻塞": true, "阻塞原因": true, "下一步": true,
	"残留风险": true, "未覆盖范围": true,
	// English counterparts keep an English adaptive summary structured when the
	// user's language is English. Without these a bold semantic heading after an
	// internal audit section was swallowed as audit content.
	"implementation": true, "integration": true, "integration points": true, "behavior changes": true,
	"fixed issues": true, "root cause and fix": true, "optimizations": true, "reviewed but unchanged": true,
	"findings": true, "evidence": true, "recommendations": true, "verification": true, "execution result": true,
	"commands and output": true, "current status": true, "blocked": true, "blocker": true, "next steps": true,
	"residual risk": true, "remaining risk": true, "coverage gaps": true,
}

// StripFinalAnswerAuditSections removes host-facing internal audit sections
// from a model-written final answer while preserving semantic result sections.
// It is shared by local, remote and cloud coding surfaces so review/fix/
// optimize reports have the same user-visible shape everywhere.
func StripFinalAnswerAuditSections(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if finalAnswerTextIsPlanApproval(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skippingInternalSection := false
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			if !skippingInternalSection {
				out = append(out, line)
			}
			continue
		}
		if !inFence && finalAnswerPlanStepHeading.MatchString(line) {
			break
		}
		if !inFence {
			if _, isMarkdownHeading := finalAnswerMarkdownHeadingTitle(line); isMarkdownHeading {
				if replacement, skipSection, known := finalAnswerSummaryHeadingAction(line); known {
					skippingInternalSection = skipSection
					if replacement != "" {
						out = append(out, "## "+replacement)
					}
					continue
				}
				// A semantic heading the filter does not own still ends an internal
				// audit section. Treating it as ordinary content would otherwise
				// delete the useful remainder of an adaptive summary.
				skippingInternalSection = false
				out = append(out, line)
				continue
			}
			if _, isBoldHeading := finalAnswerBoldSummaryHeadingTitle(line); isBoldHeading {
				if replacement, skipSection, known := finalAnswerSummaryHeadingAction(line); known {
					skippingInternalSection = skipSection
					if replacement != "" {
						out = append(out, "## "+replacement)
					}
					continue
				}
				// A bold semantic heading has the same role as a Markdown heading:
				// it ends any internal audit section instead of being swallowed by it.
				skippingInternalSection = false
				out = append(out, line)
				continue
			}
			if strings.HasPrefix(trimmed, "执行步骤：") {
				skippingInternalSection = true
				continue
			}
		}
		if skippingInternalSection {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func finalAnswerTextIsPlanApproval(text string) bool {
	return strings.Contains(text, "## 需求确认执行计划") || strings.Contains(text, "/plan approve")
}

func finalAnswerSummaryHeadingAction(line string) (replacement string, skipSection bool, known bool) {
	title, ok := finalAnswerMarkdownHeadingTitle(line)
	if !ok {
		title, ok = finalAnswerBoldSummaryHeadingTitle(line)
	}
	if !ok {
		return "", false, false
	}
	canonical := canonicalFinalAnswerSummaryHeading(title)
	if finalAnswerInternalSummaryHeadings[canonical] {
		return "", true, true
	}
	if mapped := finalAnswerSummaryHeadingAliases[canonical]; mapped != "" {
		return mapped, false, true
	}
	if finalAnswerAdaptiveSummaryHeadings[canonical] && strings.HasPrefix(strings.TrimSpace(line), "**") {
		return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), ":：")), false, true
	}
	return "", false, false
}

func finalAnswerMarkdownHeadingTitle(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level < 2 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(trimmed[level+1:])
	return title, title != ""
}

func finalAnswerBoldSummaryHeadingTitle(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	heading := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(trimmed, ":"), "："))
	if !strings.HasPrefix(heading, "**") || !strings.HasSuffix(heading, "**") || len(heading) <= 4 {
		return "", false
	}
	title := strings.TrimSpace(heading[2 : len(heading)-2])
	if title == "" {
		return "", false
	}
	canonical := canonicalFinalAnswerSummaryHeading(title)
	if finalAnswerInternalSummaryHeadings[canonical] || finalAnswerSummaryHeadingAliases[canonical] != "" || finalAnswerAdaptiveSummaryHeadings[canonical] {
		return title, true
	}
	return "", false
}

func canonicalFinalAnswerSummaryHeading(title string) string {
	title = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), ":："))
	title = strings.Join(strings.Fields(title), " ")
	return strings.ToLower(title)
}
