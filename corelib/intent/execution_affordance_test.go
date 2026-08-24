package intent

import "testing"

func TestBrowserPublicationAffordance(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"\u627e\u7bc7\u6700\u65b0\u7684 agentic RL \u76f8\u5173\u8bba\u6587\uff0c\u505a\u5b8c\u7efc\u8ff0\uff0c\u53d1\u8868\u5230\u77e5\u4e4e\uff0c\u4f5c\u4e3a\u6b63\u5f0f\u6587\u7ae0", true},
		{"\u5728\u77e5\u4e4e\u53d1\u5e16", true},
		{"\u6253\u5f00\u77e5\u4e4e\u5e76\u767b\u5f55", true},
		{"log into Zhihu and publish a post", true},
		{"\u641c\u7d22\u77e5\u4e4e\u4e0a\u5173\u4e8e agentic RL \u7684\u6587\u7ae0", false},
		{"\u5199\u4e00\u7bc7 agentic RL \u7efc\u8ff0", false},
	}
	for _, tc := range cases {
		if got := BrowserPublicationAffordance(tc.text); got != tc.want {
			t.Fatalf("BrowserPublicationAffordance(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestExecutionAffordanceDoesNotAddCapabilityLabelsFromWording(t *testing.T) {
	result := ClassificationResult{Primary: LabelSearch, Confidence: 0.91}
	applyExecutionAffordances("\u627e\u7bc7\u6700\u65b0\u8bba\u6587\uff0c\u5199\u5b8c\u540e\u53d1\u5e03\u5230\u77e5\u4e4e", &result)
	if len(result.Secondary) != 0 {
		t.Fatalf("wording added capability secondary = %#v", result.Secondary)
	}
}
