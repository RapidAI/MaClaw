package main

import "sync/atomic"

// agentViewI18n provides localized strings for AgentView builder functions.
// The language is set by App.SetLanguage() and read by builder functions
// without needing an App reference.

var agentViewCurrentLang atomic.Value // stores string

func init() {
	agentViewCurrentLang.Store("zh-Hans")
}

// setAgentViewLang is called by App.SetLanguage to keep AgentView builders in sync.
func setAgentViewLang(lang string) {
	if lang == "" {
		lang = "zh-Hans"
	}
	agentViewCurrentLang.Store(lang)
}

// avTr returns the localized string for AgentView UI text.
// When the current language is Chinese (default), returns zh; otherwise returns en.
func avTr(en, zh string) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	if lang == "en" {
		return en
	}
	return zh
}
