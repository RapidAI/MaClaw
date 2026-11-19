package computeruse

import "testing"

func TestWindowTitlesMatch(t *testing.T) {
	if !WindowTitlesMatch("Untitled - Notepad", "*Untitled - Notepad") {
		t.Fatal("notepad dirty title should match")
	}
	if WindowTitlesMatch("WeChat", "Slack") {
		t.Fatal("distinct apps must not match")
	}
	if !WindowTitlesMatch("", "anything") {
		t.Fatal("empty is unknown and must not invalidate")
	}
	if WindowTitlesMatch("WeChat", "Chat") {
		t.Fatal("wechat must not match a title that merely shares a substring")
	}
	if WindowTitlesMatch("微信", "微信开发者工具") {
		t.Fatal("wechat must not match wechat devtools")
	}
	if !WindowTitlesMatch("hello.txt - Notepad", "Untitled - Notepad") {
		t.Fatal("same app suffix should match after the document title changes")
	}
	if !WindowTitlesMatch("无标题 - 记事本", "Untitled - Notepad") {
		t.Fatal("localized notepad titles should match")
	}
	if WindowTitlesMatch("简历.docx - Word", "Untitled - WordPad") {
		t.Fatal("word must not match wordpad")
	}
}

func TestSameControlWindowDoesNotMergeSameApp(t *testing.T) {
	if !sameControlWindow("", "收件箱 - Google Chrome") {
		t.Fatal("empty stays unknown")
	}
	if !sameControlWindow("记事本", "无标题 - 记事本") {
		t.Fatal("a bare app name still matches that app's window")
	}
	if !sameControlWindow("订单", "订单 - Google Chrome") {
		t.Fatal("a bare document name still matches its full title")
	}
	if !sameControlWindow("订单 - Google Chrome", "订单 — Google Chrome") {
		t.Fatal("the same document and app still match across separators")
	}
	if sameControlWindow("订单 - Google Chrome", "收件箱 - Google Chrome") {
		t.Fatal("two chrome windows must not match")
	}
	if sameControlWindow("Chrome", "订单 - Google Chrome") {
		t.Fatal("a short token must not match a longer app suffix")
	}
	if sameControlWindow("微信", "微信开发者工具") {
		t.Fatal("wechat must not match wechat devtools")
	}
	if sameControlWindow("hello.txt - Notepad", "Untitled - Notepad") {
		t.Fatal("two documents of one app must not match")
	}
	if !WindowTitlesMatch("hello.txt - Notepad", "Untitled - Notepad") {
		t.Fatal("app-family matching must stay on WindowTitlesMatch")
	}
}

func TestSameDocumentWindowIgnoresUnreadCount(t *testing.T) {
	if !sameDocumentWindow("收件箱 (1) - Google Chrome", "收件箱 (2) - Google Chrome") {
		t.Fatal("an unread count is still the same document")
	}
	if !sameDocumentWindow("收件箱 (1) - Gmail - Google Chrome", "收件箱 (2) - Gmail - Google Chrome") {
		t.Fatal("a count before the product name is still the same document")
	}
	if sameDocumentWindow("搜索 - Google Chrome", "订单 - Google Chrome") {
		t.Fatal("a new page is a different document")
	}
	if !sameDocumentWindow("记事本", "无标题 - 记事本") {
		t.Fatal("a bare app name is still that window's document")
	}
	if sameDocumentWindow("订单 - Google Chrome", "收件箱 - Google Chrome") {
		t.Fatal("two chrome documents must not match")
	}
}

func TestBestWindowTitleDisambiguatesApps(t *testing.T) {
	cases := []struct {
		hint   string
		titles []string
		want   string
	}{
		{"微信", []string{"微信开发者工具", "微信"}, "微信"},
		{"word", []string{"Untitled - WordPad", "简历.docx - Word"}, "简历.docx - Word"},
		{"chrome", []string{"Chrome Remote Desktop", "New Tab - Google Chrome"}, "New Tab - Google Chrome"},
		{"notepad", []string{"无标题 - 记事本"}, "无标题 - 记事本"},
		{"qq", []string{"QQ浏览器", "QQ"}, "QQ"},
		{"微信", []string{"企业微信", "微信"}, "微信"},
		{"资源管理器", []string{"文件资源管理器"}, "文件资源管理器"},
		{"wps", []string{"WPS文字"}, "WPS文字"},
		{"记事本", []string{"说明文字 - 记事本", "文字讨论 - 微信"}, "说明文字 - 记事本"},
	}
	for _, tc := range cases {
		got, ok := BestWindowTitle(tc.hint, tc.titles)
		if !ok || got != tc.want {
			t.Errorf("hint %q → %q ok=%v, want %q", tc.hint, got, ok, tc.want)
		}
	}
	if _, ok := BestWindowTitle("a", []string{"WeChat", "a-not-exact"}); ok {
		t.Fatal("single-character hint must not substring-match")
	}
	if _, ok := BestWindowTitle("微信", []string{"微信开发者工具"}); ok {
		t.Fatal("wechat hint must not fall through to devtools")
	}
	if _, ok := BestWindowTitle("chrome", []string{"Chrome Remote Desktop"}); ok {
		t.Fatal("chrome hint must not focus chrome remote desktop")
	}
}

func TestKindOfTitleUsesAppNotDocumentWords(t *testing.T) {
	if kindOfTitle("说明文字 - 微信") != AdapterIM {
		t.Fatal("文字 inside a wechat title must stay IM")
	}
	if kindOfTitle("report.docx - Google Chrome") != AdapterBrowser {
		t.Fatal("a docx open in chrome is still the browser")
	}
	if kindOfTitle("QQ浏览器") != AdapterBrowser {
		t.Fatal("qq browser must not be classified as qq chat")
	}
	if kindOfTitle("Internet Explorer") != AdapterBrowser {
		t.Fatal("internet explorer must not be classified as the file manager")
	}
	if kindOfTitle("无标题 - 记事本") != AdapterEditor {
		t.Fatal("记事本 should be an editor")
	}
	if kindOfTitle("文件资源管理器") != AdapterShell {
		t.Fatal("文件资源管理器 should be the file manager")
	}
	if kindOfTitle("WPS文字") != AdapterOffice {
		t.Fatal("wps writer should be office")
	}
	if kindOfTitle("蓝信 - 张三") != AdapterIM {
		t.Fatal("lanxin chat should be IM")
	}
	if kindOfTitle("Untitled - WordPad") != AdapterEditor {
		t.Fatal("wordpad should be an editor, not office")
	}
}
