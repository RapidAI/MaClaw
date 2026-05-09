package intent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestLocalLexicalClassifier_DescriptiveTermsDoNotRoute(t *testing.T) {
	cases := []string{
		"介绍 ssh/gui 操作能力",
		"生成一份 ppt，介绍 docker/nginx 部署能力",
		"ssh 到服务器查看日志",
		"介绍代码能力",
	}

	for _, text := range cases {
		result, confident := classifyByKeywords(NewKeywordRegistry(), NewToolAffinityRegistry(), MessageContext{Text: text})
		if confident {
			t.Fatalf("classifyByKeywords(%q) confident=true, want false", text)
		}
		if result.Primary != LabelUnknown {
			t.Fatalf("classifyByKeywords(%q) primary=%s, want unknown; reason=%s", text, result.Primary, result.Reason)
		}
		if result.Reason != "local lexical classification disabled; semantic classifier required" {
			t.Fatalf("classifyByKeywords(%q) reason=%q", text, result.Reason)
		}
	}
}

func TestUnifiedIntentClassifier_DescriptiveSSHInPPTRequestPrefersOffice(t *testing.T) {
	uic := New(Config{Embedder: embedding.NoopEmbedder{}})

	result := uic.Classify(MessageContext{
		Text: "阅读 d:\\workprj\\aicoder 代码，生成一份 面向用户的 ppt, 介绍 maclaw 的功能特点，优势等（比如 ssh/gui 操作）",
	})

	if result.Primary != LabelUnknown {
		t.Fatalf("primary = %s, want %s; reason=%s secondary=%v", result.Primary, LabelUnknown, result.Reason, result.Secondary)
	}
	if result.Reason != "semantic classifiers unavailable" {
		t.Fatalf("reason = %q, want semantic classifiers unavailable", result.Reason)
	}
}
