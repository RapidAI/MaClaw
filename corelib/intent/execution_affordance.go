package intent

import "strings"

// BrowserPublicationAffordance detects tasks whose primary work may be search,
// writing, or analysis, but whose delivery step requires operating a web UI.
// This is an execution affordance: it adds Browser as a secondary intent rather
// than replacing the primary intent.
func BrowserPublicationAffordance(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}

	hasPlatform := false
	for _, marker := range []string{
		"\u77e5\u4e4e", "\u5c0f\u7ea2\u4e66", "\u5fae\u535a", "\u516c\u4f17\u53f7", "\u5fae\u4fe1\u516c\u4f17\u53f7", "b\u7ad9", "bilibili",
		"medium", "reddit", "linkedin", "twitter", "x.com", "zhihu",
	} {
		if strings.Contains(msg, marker) {
			hasPlatform = true
			break
		}
	}
	if !hasPlatform {
		return false
	}

	for _, marker := range []string{
		"\u53d1\u8868", "\u53d1\u5e03", "\u53d1\u5e16", "\u53d1\u8d34", "\u53d1\u6587", "\u53d1\u6587\u7ae0", "\u53d1\u5230", "\u53d1\u5728", "\u6295\u7a3f", "\u63d0\u4ea4", "\u767b\u5f55", "\u767b\u5165", "\u6253\u5f00",
		"publish", "post", "submit", "login", "log in", "sign in", "open",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func applyExecutionAffordances(text string, result *ClassificationResult) {
	if result == nil || result.Primary == LabelBrowser || !BrowserPublicationAffordance(text) {
		return
	}
	for _, label := range result.Secondary {
		if label == LabelBrowser {
			return
		}
	}
	result.Secondary = append(result.Secondary, LabelBrowser)
}
