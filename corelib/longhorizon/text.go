package longhorizon

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

func clipRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func DigestOf(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func Clip(s string, max int) string {
	return clipRunes(s, max)
}

func ClipCarryover(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	clipped := make([]string, 0, len(items))
	for _, item := range items {
		item = clipRunes(item, CarryoverItemCap)
		if item == "" {
			continue
		}
		clipped = append(clipped, item)
	}
	if len(clipped) == 0 {
		return nil
	}
	if len(clipped) > CarryoverMaxItems {
		clipped = clipped[len(clipped)-CarryoverMaxItems:]
	}
	total := 0
	start := len(clipped)
	for i := len(clipped) - 1; i >= 0; i-- {
		n := utf8.RuneCountInString(clipped[i])
		if start < len(clipped) && total+n > CarryoverCapRunes {
			break
		}
		total += n
		start = i
	}
	return append([]string(nil), clipped[start:]...)
}

func SanitizeExperienceText(s string) string {
	s = StripUntrustedMedia(s)
	s = clipRunes(s, 1200)
	if s == "" || s == "[image omitted]" {
		return ""
	}
	return s
}

var dataImageRe = regexp.MustCompile(`(?i)data:image\/[a-z0-9.+-]+;base64,[a-z0-9+/=]+`)
var base64PayloadRe = regexp.MustCompile(`(?i)base64,[a-z0-9+/=]{32,}`)

func StripUntrustedMedia(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = dataImageRe.ReplaceAllString(s, "[image omitted]")
	s = base64PayloadRe.ReplaceAllString(s, "base64,[omitted]")
	return strings.TrimSpace(s)
}

var forbiddenPromptNeedles = []string{
	"buildsystempromptwithmemory",
	"conversationmemory",
	"相关编码经验",
	"编程知识库",
	"steering rules",
	"candidate knowledge",
	"coding_knowledge_search",
	"knowledge_search",
	"call_mcp_tool",
	"spawn_coding_agent",
}

var cliOnlyPromptNeedles = []string{
	"computer_observe",
	"computer_action",
}

func ContainsForbiddenPrompt(text string) bool {
	return containsForbiddenNeedles(text, forbiddenPromptNeedles) || containsForbiddenNeedles(text, cliOnlyPromptNeedles)
}

func ContainsForbiddenPromptForRole(role, text string) bool {
	_ = role
	return containsForbiddenNeedles(text, forbiddenPromptNeedles)
}

func containsForbiddenNeedles(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) || strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
