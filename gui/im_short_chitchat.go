package main

import (
	"regexp"
	"strings"
)

func isShortChitChatMessage(text string) bool {
	return normalizeShortChitChatToken(text) != ""
}

var shortChitChatEdgePunctuationPattern = regexp.MustCompile(`^[\s"'` + "`" + `\(\)\[\]<>.,!?;:，。！？；：、-]+|[\s"'` + "`" + `\(\)\[\]<>.,!?;:，。！？；：、-]+$`)
var shortChitChatChineseIdlePattern = regexp.MustCompile(`^(没事|没事了|没有)(啊|呀|哦|啦|哈|呢|的)?$`)
var shortChitChatChineseThanksPattern = regexp.MustCompile(`^(谢谢)(啊|呀|哦|啦|哈|呢)?$`)
var shortChitChatChineseGreetingPattern = regexp.MustCompile(`^(你好|你好呀|你好啊|哈喽)(啊|呀|哦|啦|哈|呢)?$`)

func normalizeShortChitChatToken(text string) string {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	if cleaned == "" {
		return ""
	}
	for {
		next := strings.TrimSpace(shortChitChatEdgePunctuationPattern.ReplaceAllString(cleaned, ""))
		if next == cleaned {
			break
		}
		cleaned = next
	}
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return ""
	}
	switch {
	case shortChitChatChineseIdlePattern.MatchString(cleaned):
		return "没事"
	case shortChitChatChineseThanksPattern.MatchString(cleaned):
		return "谢谢"
	case shortChitChatChineseGreetingPattern.MatchString(cleaned):
		return "你好"
	}
	shortPhrases := map[string]struct{}{
		"hi":        {},
		"hello":     {},
		"hey":       {},
		"你好":        {},
		"没事":        {},
		"nothing":   {},
		"none":      {},
		"thanks":    {},
		"thank you": {},
		"谢谢":        {},
	}
	if _, ok := shortPhrases[cleaned]; ok {
		return cleaned
	}
	return ""
}

func buildShortChitChatResponse(text, lang string) string {
	normalized := normalizeShortChitChatToken(text)
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(text))
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		switch normalized {
		case "hi", "hello", "hey", "nothing", "none", "thanks", "thank you":
			lang = "en"
		default:
			lang = "zh"
		}
	}
	if strings.HasPrefix(lang, "en") {
		switch normalized {
		case "thanks", "thank you":
			return "You're welcome. I'm here if you want to continue."
		case "nothing", "none":
			return "No problem. I'm here if you need anything."
		default:
			return "Hi! I'm here if you need anything."
		}
	}
	switch normalized {
	case "谢谢":
		return "不客气。我在这，有需要随时叫我。"
	case "没事", "nothing", "none":
		return "好，没问题。我在这，有需要随时叫我。"
	default:
		return "你好，我在。有需要随时叫我。"
	}
}
