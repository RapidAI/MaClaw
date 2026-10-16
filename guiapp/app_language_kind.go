package guiapp

import (
	"log"
	"strings"
)

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

// languageFromLANGID maps a Windows LANGID to the app language tag.
// Primary language 0x04 is Chinese; traditional scripts are Taiwan / HK / Macau.
func languageFromLANGID(id uint16) string {
	if id == 0 {
		return string(appLanguageEnglish)
	}
	primary := id & 0x3ff
	if primary != 0x04 {
		return string(appLanguageEnglish)
	}
	sub := (id >> 10) & 0x3f
	switch sub {
	case 0x01, 0x03, 0x05: // Traditional, Hong Kong, Macau
		return string(appLanguageZhHant)
	default:
		return string(appLanguageZhHans)
	}
}

// GetCurrentLanguage is the language the tray and UI should use right now.
// Config wins; otherwise the OS UI language (not WebView2 navigator.language,
// which is often en-US on Chinese Windows).
func (a *App) GetCurrentLanguage() string {
	if a != nil && strings.TrimSpace(a.CurrentLanguage) != "" {
		return string(normalizeAppLanguageKind(a.CurrentLanguage))
	}
	return detectOSAppLanguage()
}

// applyInitialLanguage sets CurrentLanguage before the tray and WebView start
// so the tray is not stuck on the hardcoded English labels.
func (a *App) applyInitialLanguage() {
	if a == nil {
		return
	}
	lang := detectOSAppLanguage()
	if cfg, err := a.LoadConfig(); err == nil {
		if trimmed := strings.TrimSpace(cfg.Language); trimmed != "" {
			lang = string(normalizeAppLanguageKind(trimmed))
		}
	}
	a.SetLanguage(lang)
	log.Printf("[i18n] initial language=%s", lang)
}
