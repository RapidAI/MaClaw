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

func TestCropIsForegroundRequiresAReadableCaption(t *testing.T) {
	if CropIsForeground("蓝信", "") || CropIsForeground("", "蓝信") || CropIsForeground("蓝信", "   ") {
		t.Fatal("an empty caption is not the operated window")
	}
	if !WindowTitlesMatch("", "蓝信") {
		t.Fatal("unknown titles still match for callers without a crop")
	}
	if !CropIsForeground("蓝信", "蓝信") {
		t.Fatal("the same caption is the crop")
	}
	if CropIsForeground("蓝信", "蓝信 - 马勇") != WindowTitlesMatch("蓝信", "蓝信 - 马勇") {
		t.Fatal("a readable caption uses the same app match")
	}
	if CropIsForeground("蓝信", "记事本") || CropIsForeground("微信", "微信开发者工具") {
		t.Fatal("a different app is not the crop")
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

func TestBestAppFramePrefersAVisibleFrame(t *testing.T) {
	frames := []AppFrame{
		{Title: "Program Manager", Visible: true},
		{Title: "dist - 文件资源管理器", Visible: true},
		{Title: "蓝信", Visible: false},
		{Title: "蓝信", Visible: true, Owned: true},
	}
	idx, ok := BestAppFrame("蓝信", frames)
	if !ok || frames[idx].Title != "蓝信" || !frames[idx].Visible {
		t.Fatalf("visible 蓝信 should win, got %+v ok=%v", frames[idx], ok)
	}

	hidden := []AppFrame{
		{Title: "Program Manager", Visible: true},
		{Title: "微信开发者工具", Visible: true},
		{Title: "微信", Visible: false, Owned: true},
		{Title: "微信", Visible: false},
	}
	idx, ok = BestAppFrame("微信", hidden)
	if !ok || hidden[idx].Owned || hidden[idx].Visible || hidden[idx].Title != "微信" {
		t.Fatalf("unowned hidden 微信 should win, got %+v ok=%v", hidden[idx], ok)
	}
	if _, ok := BestAppFrame("微信", []AppFrame{{Title: "微信开发者工具", Visible: true}}); ok {
		t.Fatal("wechat hint must not select wechat devtools")
	}
	if _, ok := BestAppFrame("蓝信", []AppFrame{{Title: "码卡龙 8 MaClaw（企缘）", Visible: true}}); ok {
		t.Fatal("lanxin hint must not select another app")
	}
	openChat := []AppFrame{
		{Title: "蓝信", Visible: false},
		{Title: "蓝信 - 张三", Visible: true},
	}
	idx, ok = BestAppFrame("蓝信", openChat)
	if !ok || openChat[idx].Title != "蓝信 - 张三" {
		t.Fatalf("an open chat should beat a hidden main window, got %+v ok=%v", openChat[idx], ok)
	}
}

func TestBestAppFrameDoesNotRaiseAnExecutablePathConsole(t *testing.T) {
	const console = `C:\Program Files (x86)\LanxinSoftCustom\main\LxMainNew.exe`
	if !ExecutablePathTitle(console) {
		t.Fatal("a program path is not an application window")
	}
	if ExecutablePathTitle("蓝信") || ExecutablePathTitle(`readme.txt - Notepad`) || ExecutablePathTitle(`C:\notes\密码学笔记.txt`) {
		t.Fatal("an application title must stay selectable")
	}
	frames := []AppFrame{
		{Title: console, Visible: true},
		{Title: "蓝信", Visible: true},
	}
	if _, ok := BestAppFrame(console, frames); ok {
		t.Fatal("focus must not raise the console titled with the program path")
	}
	idx, ok := BestAppFrame("蓝信", frames)
	if !ok || frames[idx].Title != "蓝信" {
		t.Fatalf("蓝信 should still match, got %+v ok=%v", frames[idx], ok)
	}
	kept := WithoutExecutablePathTitles([]string{"蓝信", console, "记事本"})
	if len(kept) != 2 || kept[0] != "蓝信" || kept[1] != "记事本" {
		t.Fatalf("window list = %#v", kept)
	}
}

func TestChooseAppLaunchNameUsesTheInstalledShortcut(t *testing.T) {
	names := []string{"卸载微信", "微信", "蓝信卸载", "蓝信"}
	got, ok := ChooseAppLaunchName("蓝信", names)
	if !ok || got != "蓝信" {
		t.Fatalf("蓝信 shortcut = %q ok=%v", got, ok)
	}
	got, ok = ChooseAppLaunchName("lanxin", names)
	if !ok || got != "蓝信" {
		t.Fatalf("lanxin shortcut = %q ok=%v", got, ok)
	}
	got, ok = ChooseAppLaunchName("蓝信 - 张三", names)
	if !ok || got != "蓝信" {
		t.Fatalf("chat title shortcut = %q ok=%v", got, ok)
	}
	got, ok = ChooseAppLaunchName("微信", names)
	if !ok || got != "微信" {
		t.Fatalf("wechat shortcut = %q ok=%v", got, ok)
	}
	if _, ok := ChooseAppLaunchName("微信", []string{"微信开发者工具"}); ok {
		t.Fatal("wechat must not launch wechat devtools")
	}
	if _, ok := ChooseAppLaunchName("chrome", []string{"Google Chrome", "蓝信"}); ok {
		t.Fatal("a browser hint must not launch a desktop shortcut")
	}
	if _, ok := ChooseAppLaunchName("Program Manager", names); ok {
		t.Fatal("a window that is not an app must not launch")
	}
	if _, ok := ChooseAppLaunchName("码卡龙", names); ok {
		t.Fatal("an unknown title must not launch")
	}
}
