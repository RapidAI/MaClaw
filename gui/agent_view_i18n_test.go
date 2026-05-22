package main

import "testing"

func TestAgentViewTranslationRecognizesEnglishVariants(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	setAgentViewLang("en-US")
	if got := avTr("Submit", "提交"); got != "Submit" {
		t.Fatalf("en-US avTr = %q, want Submit", got)
	}

	setAgentViewLang("zh-CN")
	if got := avTr("Submit", "提交"); got != "提交" {
		t.Fatalf("zh-CN avTr = %q, want 提交", got)
	}
}
