package docgen

import (
	"fmt"
	"strings"
)

// Content limits for markdown handed to Generate.
//
// Validation lives here rather than in each caller so every path into this
// package is guarded: SwarmDocGenerator used to be the only one checking, and
// the desktop companion export (guiapp) called GenerateToFile directly with
// unbounded model output. Layout cost grows faster than content size (blocks
// are re-measured by binary search during pagination), so an unbounded reply
// turns one export into a multi-second stall.
const (
	maxContentBytes     = 180 * 1024
	maxParagraphBytes   = 48 * 1024
	maxSuspiciousB64Run = 8192
	filePayloadMarker   = "[file_base64|"
)

// Limits exposes the content bounds so callers and their tests assert against
// the values that actually apply instead of a copy that can drift.
func Limits() (maxContent, maxParagraph, maxBase64Run int) {
	return maxContentBytes, maxParagraphBytes, maxSuspiciousB64Run
}

// ValidateContent rejects markdown that cannot be laid out into a readable
// PDF: outsized content, a single never-wrapping paragraph, or an accidental
// file/blob payload pasted into the reply.
//
// Every error names what to do about it, because the caller is often the model
// itself: a bare "failed" would just be retried verbatim.
func ValidateContent(content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return fmt.Errorf("缺少 content 参数（Markdown 格式的文档内容）")
	}
	// Data-URL images are real pictures, not pasted blobs: judge length and
	// base64 runs against the document with those pixels collapsed, so one
	// embedded chart does not read as an oversized payload.
	measured := dataURLViewForValidation(content)
	if len(measured) > maxContentBytes {
		return fmt.Errorf("PDF 内容过长（%d 字节），请缩短后再生成", len(measured))
	}
	if strings.Contains(measured, filePayloadMarker) {
		return fmt.Errorf("PDF 内容疑似包含文件载荷，请先整理正文再生成")
	}
	if longestSuspiciousBase64Run(measured) >= maxSuspiciousB64Run {
		return fmt.Errorf("PDF 内容疑似包含超长 base64 或二进制载荷，请先整理正文再生成")
	}
	for _, block := range splitMarkdownParagraphs(measured) {
		if len(block) > maxParagraphBytes {
			return fmt.Errorf("PDF 中存在过长段落（%d 字节），请拆分后再生成", len(block))
		}
	}
	return nil
}

// splitMarkdownParagraphs splits on blank lines, which is how the renderer
// decides where a block starts and ends.
func splitMarkdownParagraphs(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	parts := strings.Split(normalized, "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			blocks = append(blocks, trimmed)
		}
	}
	return blocks
}

// longestSuspiciousBase64Run reports the longest unbroken run of base64
// characters. Real prose breaks long before any of these thresholds; only a
// pasted blob or encoded file reaches them.
func longestSuspiciousBase64Run(content string) int {
	best, run := 0, 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		isB64 := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
		if isB64 {
			run++
			if run > best {
				best = run
			}
			continue
		}
		run = 0
	}
	return best
}
