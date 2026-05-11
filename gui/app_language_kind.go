package main

import "strings"

type appLanguageKind string

const (
	appLanguageEnglish appLanguageKind = "en"
	appLanguageZhHans  appLanguageKind = "zh-Hans"
	appLanguageZhHant  appLanguageKind = "zh-Hant"
)

func normalizeAppLanguageKind(value string) appLanguageKind {
	lang := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(lang, "zh-hant"), strings.HasPrefix(lang, "zh-tw"), strings.HasPrefix(lang, "zh-hk"):
		return appLanguageZhHant
	case strings.HasPrefix(lang, "zh-hans"), strings.HasPrefix(lang, "zh-cn"), strings.HasPrefix(lang, "zh"):
		return appLanguageZhHans
	default:
		return appLanguageEnglish
	}
}

func (lang appLanguageKind) IsEnglish() bool {
	return lang == appLanguageEnglish
}

func (lang appLanguageKind) IsChinese() bool {
	return lang == appLanguageZhHans || lang == appLanguageZhHant
}

func (lang appLanguageKind) TranslationTag() string {
	return string(lang)
}
