package notification

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"unicode/utf8"
)

var errEmptyTranslation = errors.New("translation is empty")

// GUI languages a notification can be stored in. They match the desktop
// interface codes: Simplified Chinese, Traditional Chinese, English, and
// Cyrillic Mongolian.
const (
	LangZhHans = "zh-Hans"
	LangZhHant = "zh-Hant"
	LangEn     = "en"
	LangMn     = "mn"
)

// NotificationLangs is the default target set on an admin publish form.
var NotificationLangs = []string{LangZhHans, LangZhHant, LangEn, LangMn}

// LocaleCopy is one language of a notification. Link URLs stay on the parent
// row and are never translated.
type LocaleCopy struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	ButtonText string `json:"button_text,omitempty"`
}

// Translator turns one notification into another language. A nil translator
// stores only the language the admin wrote.
type Translator interface {
	TranslateFields(ctx context.Context, from, to string, fields LocaleCopy) (LocaleCopy, error)
}

// NormalizeNotificationLang maps an admin or client tag onto a stored code.
// An unknown tag is empty so the caller can apply its own default.
func NormalizeNotificationLang(raw string) string {
	tag := strings.TrimSpace(strings.ReplaceAll(raw, "_", "-"))
	switch strings.ToLower(tag) {
	case "zh", "zh-cn", "zh-sg", "zh-hans", "zh-hans-cn", "chinese", "chs":
		return LangZhHans
	case "zh-tw", "zh-hk", "zh-mo", "zh-hant", "zh-hant-tw", "cht":
		return LangZhHant
	case "en", "en-us", "en-gb":
		return LangEn
	case "mn", "mn-mn", "mn-cyrl", "mn-cyrl-mn":
		return LangMn
	default:
		return ""
	}
}

// DefaultPinnedButtonForLang is the action label stored with a pinned notice
// when the admin leaves the button empty. The source language keeps this
// label; the other selected languages are translations of it. These four
// strings are the desktop labels for "View details". An unknown source stays
// Simplified Chinese, matching the empty-source default.
func DefaultPinnedButtonForLang(lang string) string {
	switch NormalizeNotificationLang(lang) {
	case LangZhHant:
		return "查看詳情"
	case LangEn:
		return "View details"
	case LangMn:
		return "Дэлгэрэнгүй харах"
	default:
		return DefaultPinnedButtonText
	}
}

// fillNotificationLocales stores the admin's text under sourceLang and asks
// the translator for every other selected language. A failed language is left
// out so the desktop shows the original text for that interface.
func fillNotificationLocales(ctx context.Context, sourceLang string, targets []string, src LocaleCopy, tr Translator) (string, map[string]LocaleCopy) {
	sourceLang = NormalizeNotificationLang(sourceLang)
	if sourceLang == "" {
		sourceLang = LangZhHans
	}
	src = clampLocale(src)
	out := map[string]LocaleCopy{sourceLang: src}
	if tr == nil {
		return sourceLang, out
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, lang := range normalizeTargetLangs(targets, sourceLang) {
		if lang == sourceLang || ctx.Err() != nil {
			continue
		}
		wg.Add(1)
		go func(lang string) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			next, err := tr.TranslateFields(ctx, sourceLang, lang, src)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[notification] translate %s -> %s: %v", sourceLang, lang, err)
				}
				return
			}
			next = clampLocale(next)
			if strings.TrimSpace(next.Title) == "" {
				next.Title = src.Title
			}
			if strings.TrimSpace(next.Content) == "" {
				next.Content = src.Content
			}
			next.ButtonText = finishTranslatedButton(sourceLang, lang, src, next)
			mu.Lock()
			out[lang] = next
			mu.Unlock()
		}(lang)
	}
	wg.Wait()
	return sourceLang, out
}

func normalizeTargetLangs(targets []string, sourceLang string) []string {
	// Nil means the caller omitted the list, so every desktop language is a
	// target. An empty list is the admin unchecking every box.
	if targets == nil {
		targets = NotificationLangs
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(targets)+1)
	add := func(lang string) {
		lang = NormalizeNotificationLang(lang)
		if lang == "" {
			return
		}
		if _, ok := seen[lang]; ok {
			return
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	add(sourceLang)
	for _, lang := range targets {
		add(lang)
	}
	return out
}

// finishTranslatedButton keeps a model-written button. The four default
// "View details" labels are known, so a missing or echoed default becomes
// the target language's own label instead of the source language's.
func finishTranslatedButton(sourceLang, targetLang string, src, got LocaleCopy) string {
	button := strings.TrimSpace(got.ButtonText)
	canonical := DefaultPinnedButtonForLang(sourceLang)
	if src.ButtonText != canonical {
		return button
	}
	if button == "" || button == src.ButtonText {
		return DefaultPinnedButtonForLang(targetLang)
	}
	return button
}

func clampLocale(copy LocaleCopy) LocaleCopy {
	copy.Title = clampRunes(strings.TrimSpace(copy.Title), 100)
	copy.Content = clampRunes(strings.TrimSpace(copy.Content), 2000)
	copy.ButtonText = clampRunes(strings.TrimSpace(copy.ButtonText), 20)
	return copy
}

func clampRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func cloneLocales(in map[string]LocaleCopy) map[string]LocaleCopy {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]LocaleCopy, len(in))
	for lang, copy := range in {
		code := NormalizeNotificationLang(lang)
		if code == "" {
			continue
		}
		out[code] = clampLocale(copy)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func marshalLocales(locales map[string]LocaleCopy) string {
	if len(locales) == 0 {
		return ""
	}
	raw, err := json.Marshal(locales)
	if err != nil {
		return ""
	}
	return string(raw)
}

func unmarshalLocales(raw string) map[string]LocaleCopy {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var locales map[string]LocaleCopy
	if err := json.Unmarshal([]byte(raw), &locales); err != nil {
		return nil
	}
	return cloneLocales(locales)
}

// TranslationSystemPrompt is the instruction sent with TranslationUserPrompt.
const TranslationSystemPrompt = "You translate a short product notification. Reply with one JSON object only, no markdown. Keys are title, content, and button_text. Keep the meaning, product names, and numbers. Leave button_text empty when the source button is empty. Do not add a link."

// TranslationUserPrompt is the user message for one target language.
func TranslationUserPrompt(from, to string, src LocaleCopy) string {
	// button_text stays present when empty. LocaleCopy omits it, and a missing
	// key makes the model invent a label.
	payload, _ := json.Marshal(struct {
		Title      string `json:"title"`
		Content    string `json:"content"`
		ButtonText string `json:"button_text"`
	}{src.Title, src.Content, src.ButtonText})
	return "Translate from " + languageName(from) + " into " + languageName(to) + ".\n" + string(payload)
}

func languageName(code string) string {
	switch NormalizeNotificationLang(code) {
	case LangZhHans:
		return "Simplified Chinese"
	case LangZhHant:
		return "Traditional Chinese"
	case LangEn:
		return "English"
	case LangMn:
		return "Cyrillic Mongolian"
	default:
		return code
	}
}

// ParseTranslatedLocale reads the model JSON. A fenced reply is accepted.
func ParseTranslatedLocale(raw string) (LocaleCopy, error) {
	text := strings.TrimSpace(raw)
	text = stripJSONFence(text)
	var copy LocaleCopy
	if err := json.Unmarshal([]byte(text), &copy); err != nil {
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start < 0 || end <= start {
			return LocaleCopy{}, err
		}
		if err2 := json.Unmarshal([]byte(text[start:end+1]), &copy); err2 != nil {
			return LocaleCopy{}, err
		}
	}
	copy = clampLocale(copy)
	if copy.Title == "" && copy.Content == "" {
		return LocaleCopy{}, errEmptyTranslation
	}
	return copy, nil
}

func stripJSONFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSpace(text)
	if nl := strings.IndexByte(text, '\n'); nl >= 0 && !strings.Contains(text[:nl], "{") {
		text = strings.TrimSpace(text[nl+1:])
	}
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
