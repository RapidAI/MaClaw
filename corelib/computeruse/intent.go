package computeruse

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToolNames is the full Computer Use tool surface exposed to the agent.
var ToolNames = []string{
	"computer_observe",
	"computer_click",
	"computer_type",
	"computer_key",
	"computer_scroll",
	"computer_select",
	"computer_scroll_into_view",
	"computer_drag",
	"computer_wait",
	"computer_focus",
	"computer_find",
	"computer_done",
	"computer_playbook",
}

// LegacyGUICompeteTools are older raw GUI tools that should be de-prioritized
// when Computer Use is active (prefer ref-based computer_* instead).
var LegacyGUICompeteTools = map[string]bool{
	"gui_click":      true,
	"gui_type":       true,
	"gui_screenshot": true,
}

// RequestsDesktopAppOperation reports whether the user asked to drive a
// desktop application (open a program, click a window, look at the screen).
// Mentioning Word, 软件, or a file path is not enough: fresh Computer Use
// activation requires this cue in addition to the classifier, so a document
// or chat request does not open the desktop-control surface.
func RequestsDesktopAppOperation(userText string) bool {
	s := strings.ToLower(strings.TrimSpace(userText))
	if s == "" || browserTaskWithoutDesktop(s) {
		return false
	}
	return hasDesktopOperationCue(s)
}

func hasDesktopOperationCue(s string) bool {
	s = stripNonDesktopCompounds(s)
	for _, phrase := range []string{
		"屏幕上", "螢幕上", "看看屏幕", "看一下屏幕", "看屏幕", "看看螢幕",
		"当前窗口", "當前視窗", "当前视窗", "窗口上", "視窗上", "窗口里", "視窗裡", "视窗里",
		"桌面上", "桌面软件", "桌面軟體", "桌面应用", "桌面應用",
		"快捷键", "快捷鍵", "操作桌面",
		"on the screen", "the screen", "my screen",
		"on the desktop", "desktop app", "desktop application", "the desktop",
	} {
		if phraseBounded(s, phrase) {
			return true
		}
	}
	if hasClickOnUI(s) || hasEnglishClickOnUI(s) {
		return true
	}
	if hasKnownAppLaunch(s) {
		return true
	}
	return hasAppLaunchCue(s) || hasEnglishAppLaunchCue(s)
}

// hasKnownAppLaunch accepts "打开微信" / "打开记事本" without requiring the
// word 程序. The app name has to come right after the verb (fillers like
// 一个/这个 allowed). "打开昨天那个关于微信的文件" is a file, not WeChat.
// A browser name such as Chrome or QQ浏览器 stays on the browser tools.
func hasKnownAppLaunch(s string) bool {
	verbs := []string{"打开", "打開", "启动", "啟動", "切换到", "切換到", "切到"}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		for _, verb := range verbs {
			vr := []rune(verb)
			if !hasRunePrefix(rs[i:], vr) {
				continue
			}
			span := stripLaunchFiller(string(rs[i+len(vr):]))
			if span == "" || hasAliasPrefix(span, browserLaunchNames) {
				continue
			}
			if hasAliasPrefix(span, desktopLaunchNames) {
				return true
			}
		}
	}
	return false
}

func stripLaunchFiller(span string) string {
	fillers := []string{"一下", "一个", "這個", "这个", "那個", "那个", "我的"}
	for {
		next := strings.TrimSpace(span)
		for _, filler := range fillers {
			next = strings.TrimSpace(strings.TrimPrefix(next, filler))
		}
		if next == span {
			return span
		}
		span = next
	}
}

func hasAliasPrefix(span string, names []string) bool {
	for _, name := range names {
		if name == "" || !strings.HasPrefix(span, name) {
			continue
		}
		if len(span) > len(name) && span[len(name)] <= 127 && isASCIIAlphaNumByte(span[len(name)]) {
			continue
		}
		return true
	}
	return false
}

// stripNonDesktopCompounds removes compounds that only look like a desktop
// window or a screen. "时间窗口里" and "the screenshot" are not GUI control.
func stripNonDesktopCompounds(s string) string {
	for _, phrase := range []string{"时间窗口", "滑动窗口", "滑動窗口", "窗口函数", "窗口期", "screen shot", "screenshot"} {
		s = strings.ReplaceAll(s, phrase, " ")
	}
	return s
}

// hasClickOnUI accepts 点击 only next to a control. "点击查看详情" is a link,
// not a request to drive the desktop. "鼠标" alone is not a click.
func hasClickOnUI(s string) bool {
	verbs := []string{"点击", "双击", "雙擊", "右键", "右鍵"}
	targets := []string{"窗口", "視窗", "按钮", "按鈕", "屏幕", "螢幕", "图标", "圖標", "桌面", "菜单", "菜單"}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		for _, verb := range verbs {
			vr := []rune(verb)
			if !hasRunePrefix(rs[i:], vr) {
				continue
			}
			rest := string(rs[i+len(vr):])
			if strings.HasPrefix(rest, "确定") || strings.HasPrefix(rest, "確定") || strings.HasPrefix(rest, "一下确定") || strings.HasPrefix(rest, "一下確定") {
				return true
			}
			// Look forward only. A window mentioned before the verb
			// ("关闭窗口后点击这里") is not the click target.
			to := i + len(vr) + 8
			if to > len(rs) {
				to = len(rs)
			}
			span := string(rs[i+len(vr) : to])
			for _, target := range targets {
				if strings.Contains(span, target) {
					return true
				}
			}
		}
	}
	return false
}

func hasEnglishClickOnUI(s string) bool {
	clicks := englishClickAt(s)
	if len(clicks) == 0 {
		return false
	}
	targets := []string{"button", "window", "dialog", "screen", "desktop", "icon", "menu", "ok"}
	for _, at := range clicks {
		from := at - 32
		if from < 0 {
			from = 0
		}
		to := at + 32
		if to > len(s) {
			to = len(s)
		}
		window := s[from:to]
		for _, target := range targets {
			if containsASCIIWord(window, target) {
				return true
			}
		}
	}
	return false
}

func englishClickAt(s string) []int {
	var at []int
	for _, word := range []string{"double-click", "double click", "right-click", "right click", "click"} {
		for start := 0; start < len(s); {
			i := strings.Index(s[start:], word)
			if i < 0 {
				break
			}
			i += start
			end := i + len(word)
			if (i == 0 || !isASCIIAlphaNumByte(s[i-1])) && (end == len(s) || !isASCIIAlphaNumByte(s[end])) {
				at = append(at, i)
			}
			start = end
		}
	}
	return at
}

// hasAppLaunchCue matches 打开/操作/控制 next to 程序/软件/窗口/界面.
// 打开网页 and 打开浏览器 are not desktop-app launches.
func hasAppLaunchCue(s string) bool {
	verbs := []string{"打开", "打開", "启动", "啟動", "运行", "運行", "操作", "控制", "切换到", "切換到", "切到", "聚焦"}
	targets := []string{"应用程序", "應用程式", "程序", "軟體", "软件", "应用", "應用", "窗口", "視窗", "界面", "介面"}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		for _, verb := range verbs {
			vr := []rune(verb)
			if !hasRunePrefix(rs[i:], vr) {
				continue
			}
			rest := rs[i+len(vr):]
			if len(rest) > 16 {
				rest = rest[:16]
			}
			span := string(rest)
			if strings.Contains(span, "网页") || strings.Contains(span, "網頁") ||
				strings.Contains(span, "网站") || strings.Contains(span, "網站") ||
				strings.Contains(span, "浏览器") || strings.Contains(span, "瀏覽器") ||
				strings.Contains(span, "链接") || strings.Contains(span, "連結") {
				continue
			}
			for _, target := range targets {
				if !launchTargetMatches(span, target) {
					continue
				}
				// "运行这个程序" / "控制程序流程" is code, not a desktop app.
				// Keep the match when the same span names a window or the desktop.
				if (verb == "运行" || verb == "運行" || verb == "控制") &&
					(target == "程序" || target == "软件" || target == "軟體" || target == "应用" || target == "應用" || target == "应用程序" || target == "應用程式") &&
					!strings.Contains(span, "窗口") && !strings.Contains(span, "視窗") &&
					!strings.Contains(span, "界面") && !strings.Contains(span, "介面") &&
					!strings.Contains(span, "桌面") && !strings.Contains(span, "按钮") && !strings.Contains(span, "按鈕") {
					continue
				}
				// "打开应用层" / "打开软件工程" / "打开程序设计" name a subject,
				// not an application window.
				if launchCompoundIsTopic(span, target) {
					continue
				}
				return true
			}
		}
	}
	return false
}

// launchTargetMatches keeps 界面 as a surface the user is driving
// (界面上/这个界面). "操作界面说明" is a document title.
func launchCompoundIsTopic(span, target string) bool {
	var suffixes []string
	switch target {
	case "应用", "應用":
		suffixes = []string{"层", "数学", "场景", "范围"}
	case "软件", "軟體":
		suffixes = []string{"工程", "开发", "测试", "測試", "设计", "架构", "架構"}
	case "程序":
		suffixes = []string{"设计", "流程", "员"}
	default:
		return false
	}
	idx := strings.Index(span, target)
	if idx < 0 {
		return false
	}
	rest := span[idx+len(target):]
	for _, suffix := range suffixes {
		if strings.HasPrefix(rest, suffix) {
			return true
		}
	}
	return false
}

// phraseBounded matches Chinese phrases as substrings. English phrases must
// not continue into another word, so "the screen" does not match "the screensaver".
func phraseBounded(s, phrase string) bool {
	if phrase == "" {
		return false
	}
	if phrase[0] > 127 {
		return strings.Contains(s, phrase)
	}
	for start := 0; start < len(s); {
		i := strings.Index(s[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(phrase)
		leftOK := i == 0 || phrase[0] == ' ' || !isASCIIAlphaNumByte(s[i-1])
		rightOK := end == len(s) || !isASCIIAlphaNumByte(s[end])
		if leftOK && rightOK {
			return true
		}
		start = i + 1
	}
	return false
}

func launchTargetMatches(span, target string) bool {
	if target != "界面" && target != "介面" {
		return strings.Contains(span, target)
	}
	for _, form := range []string{target + "上", target + "里", target + "裡", target + "中", "这个" + target, "這個" + target, "该" + target, "該" + target} {
		if strings.Contains(span, form) {
			return true
		}
	}
	return false
}

func hasEnglishAppLaunchCue(s string) bool {
	// "app"/"application" only follow an open/launch verb. "focus on the
	// application of ..." and "version control application" are not GUI tasks.
	// type/drag still match a window, notepad, or the desktop.
	verbs := []string{"open", "launch", "operate", "minimize", "maximize", "type", "drag"}
	for _, verb := range verbs {
		wide := verb == "open" || verb == "launch" || verb == "operate" || verb == "minimize" || verb == "maximize"
		for start := 0; start < len(s); {
			i := strings.Index(s[start:], verb)
			if i < 0 {
				break
			}
			i += start
			end := i + len(verb)
			if (i > 0 && isASCIIAlphaNumByte(s[i-1])) || (end < len(s) && isASCIIAlphaNumByte(s[end])) {
				start = i + 1
				continue
			}
			spanEnd := end + 48
			if spanEnd > len(s) {
				spanEnd = len(s)
			}
			span := strings.ReplaceAll(s[end:spanEnd], "window function", "")
			// "application of a formula" is the English noun, not an app window.
			span = strings.ReplaceAll(span, "application of", "")
			// " application" contains the letters of " app". A bounded match
			// keeps "open the Word app" and rejects "open the application of ...".
			targets := []string{" window", " notepad", " calculator", " desktop"}
			if wide {
				targets = append(targets, " application", " app")
			}
			for _, target := range targets {
				if phraseBounded(span, target) {
					return true
				}
			}
			start = end
		}
	}
	return false
}

// browserTaskWithoutDesktop keeps web tasks on the browser tools. A desktop
// noun (窗口/桌面/程序) in the same request can still be a native dialog.
func browserTaskWithoutDesktop(s string) bool {
	browser := strings.Contains(s, "浏览器") || strings.Contains(s, "瀏覽器") ||
		strings.Contains(s, "网页") || strings.Contains(s, "網頁") ||
		strings.Contains(s, "网站") || strings.Contains(s, "網站") ||
		strings.Contains(s, "http://") || strings.Contains(s, "https://") ||
		containsASCIIWord(s, "browser")
	if !browser {
		return false
	}
	if strings.Contains(s, "程序") || strings.Contains(s, "软件") || strings.Contains(s, "軟體") ||
		strings.Contains(s, "窗口") || strings.Contains(s, "視窗") ||
		strings.Contains(s, "桌面") || strings.Contains(s, "界面") || strings.Contains(s, "介面") {
		return false
	}
	if strings.Contains(s, "desktop") || strings.Contains(s, " window") || strings.Contains(s, "notepad") {
		return false
	}
	return true
}

func hasRunePrefix(s, prefix []rune) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i, r := range prefix {
		if s[i] != r {
			return false
		}
	}
	return true
}

func containsASCIIWord(text, word string) bool {
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], word)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(word)
		if (i == 0 || !isASCIIAlphaNumByte(text[i-1])) && (end == len(text) || !isASCIIAlphaNumByte(text[end])) {
			return true
		}
		start = i + 1
	}
	return false
}

func isASCIIAlphaNumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// HasExplicitTrigger reports whether the user message explicitly invokes
// Computer Use via the @computer / "computer use" trigger syntax. This is an
// addressing override, not intent detection — semantic activation still
// requires RequestsDesktopAppOperation plus the unified intent classifier.
func HasExplicitTrigger(userText string) bool {
	t := strings.ToLower(strings.TrimSpace(userText))
	if t == "" {
		return false
	}
	return hasComputerMention(t) || hasComputerPhrase(t)
}

func hasComputerMention(text string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], "@computer")
		if i < 0 {
			return false
		}
		i += start
		end := i + len("@computer")
		if (i == 0 || !isMentionRuneBefore(text, i)) && (end == len(text) || !isMentionRuneAfter(text, end)) {
			return true
		}
		start = end
	}
}

func hasComputerPhrase(text string) bool {
	for _, phrase := range []string{"computer use", "computer_use", "computer-use"} {
		for start := 0; ; {
			i := strings.Index(text[start:], phrase)
			if i < 0 {
				break
			}
			i += start
			end := i + len(phrase)
			if (i == 0 || !isWordRuneBefore(text, i)) && (end == len(text) || !isWordRuneAfter(text, end)) {
				return true
			}
			start = end
		}
	}
	return false
}

func isMentionRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func isMentionRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func isWordRuneBefore(text string, index int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func isWordRuneAfter(text string, index int) bool {
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// IsComputerUseTool reports whether name is part of the CU surface.
func IsComputerUseTool(name string) bool {
	name = strings.TrimSpace(name)
	for _, n := range ToolNames {
		if n == name {
			return true
		}
	}
	return false
}
