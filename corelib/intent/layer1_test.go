package intent

import "testing"

func TestLocalLexicalClassifier_EmptyMessageFailsClosed(t *testing.T) {
	result, confident := classifyByKeywords(NewKeywordRegistry(), NewToolAffinityRegistry(), MessageContext{Text: ""})
	if confident {
		t.Fatal("empty local lexical classification must not be confident")
	}
	if result.Primary != LabelUnknown || result.Confidence != 0 || result.Layer != 1 {
		t.Fatalf("result = %+v, want unknown confidence=0 layer=1", result)
	}
	if result.Degraded {
		t.Fatal("empty message should be unknown, not degraded")
	}
}

func TestLocalLexicalClassifier_DisabledForExecutableWording(t *testing.T) {
	cases := []string{
		"登录服务器查看日志",
		"打开浏览器帮我截图",
		"帮我写代码实现一个功能",
		"在页面上点击按钮",
		"ssh到服务器",
		"继续",
	}

	for _, text := range cases {
		result, confident := classifyByKeywords(NewKeywordRegistry(), NewToolAffinityRegistry(), MessageContext{
			Text:          text,
			RecentHistory: []string{"帮我开发一个游戏"},
		})
		if confident {
			t.Fatalf("classifyByKeywords(%q) confident=true, want false", text)
		}
		if result.Primary != LabelUnknown {
			t.Fatalf("classifyByKeywords(%q) primary=%s, want unknown", text, result.Primary)
		}
		if result.Confidence != 0 || !result.Degraded {
			t.Fatalf("classifyByKeywords(%q) result=%+v, want confidence=0 degraded=true", text, result)
		}
		if len(result.ToolNames) != 0 {
			t.Fatalf("classifyByKeywords(%q) tool names=%v, want none", text, result.ToolNames)
		}
	}
}
