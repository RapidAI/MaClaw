package intent

import "testing"

func TestClassifyByKeywords_NoLegacyPreservationForRouting(t *testing.T) {
	cases := []string{
		"把报告发给我",
		"导出 PDF",
		"登录服务器",
		"打开浏览器帮我截图",
		"你好吗",
	}

	for _, text := range cases {
		result, confident := classifyByKeywords(NewKeywordRegistry(), NewToolAffinityRegistry(), MessageContext{Text: text})
		if confident {
			t.Fatalf("classifyByKeywords(%q) confident=true, want false", text)
		}
		if result.Primary != LabelUnknown {
			t.Fatalf("classifyByKeywords(%q) primary=%s, want unknown", text, result.Primary)
		}
	}
}
