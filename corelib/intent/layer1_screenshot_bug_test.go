package intent

import "testing"

func TestClassifyByKeywords_ScreenshotWordingDoesNotRoute(t *testing.T) {
	cases := []string{
		"帮我截屏桌面发给我图片",
		"截图桌面发给我",
		"帮我截屏",
		"截图发给我",
	}

	for _, text := range cases {
		result, confident := classifyByKeywords(NewKeywordRegistry(), NewToolAffinityRegistry(), MessageContext{Text: text})
		if confident {
			t.Fatalf("classifyByKeywords(%q) confident=true, want false", text)
		}
		if result.Primary != LabelUnknown {
			t.Fatalf("classifyByKeywords(%q) primary=%s, want unknown", text, result.Primary)
		}
		if result.ToolNames != nil {
			t.Fatalf("classifyByKeywords(%q) tool names=%v, want none", text, result.ToolNames)
		}
	}
}
