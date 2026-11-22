package computeruse

import (
	"strings"
	"unicode/utf8"
)

// appAlias binds equivalent window-title names for one application, plus the
// observe-playbook family that should steer Computer Use when that app is focused.
type appAlias struct {
	id    string
	kind  AdapterKind
	names []string
}

// aliasGroups are exact normalized names only. Short or overlapping needles
// ("word" vs "wordpad", "微信" vs "微信开发者工具", "qq" vs "qq浏览器") stay in
// different groups so a hint cannot focus the wrong app.
var aliasGroups = []appAlias{
	{id: "notepad", kind: AdapterEditor, names: []string{"记事本", "notepad"}},
	{id: "wordpad", kind: AdapterEditor, names: []string{"写字板", "wordpad"}},
	{id: "notepadpp", kind: AdapterEditor, names: []string{"notepad++"}},
	{id: "sublime", kind: AdapterEditor, names: []string{"sublime text", "sublime"}},
	{id: "gedit", kind: AdapterEditor, names: []string{"gedit"}},
	{id: "textedit", kind: AdapterEditor, names: []string{"textedit"}},
	{id: "vscode", kind: AdapterEditor, names: []string{"visual studio code", "vscode", "vs code"}},

	{id: "word", kind: AdapterOffice, names: []string{"word", "microsoft word", "winword"}},
	{id: "excel", kind: AdapterOffice, names: []string{"excel", "microsoft excel"}},
	{id: "powerpoint", kind: AdapterOffice, names: []string{"powerpoint", "microsoft powerpoint", "ppt"}},
	// One group so "wps" focuses WPS文字 / WPS表格 / WPS演示. They share a process
	// family; a bare "wps" hint should not miss the only WPS window on screen.
	{id: "wps", kind: AdapterOffice, names: []string{"wps文字", "wps 文字", "wps表格", "wps 表格", "wps演示", "wps 演示", "wps", "wps office"}},
	{id: "libreoffice", kind: AdapterOffice, names: []string{"libreoffice", "libreoffice writer", "libreoffice calc"}},
	{id: "iwork", kind: AdapterOffice, names: []string{"pages", "numbers", "keynote"}},

	{id: "explorer", kind: AdapterShell, names: []string{"文件资源管理器", "资源管理器", "file explorer", "windows explorer", "此电脑", "this pc"}},
	{id: "finder", kind: AdapterShell, names: []string{"finder"}},
	{id: "nautilus", kind: AdapterShell, names: []string{"nautilus", "dolphin"}},

	{id: "chrome", kind: AdapterBrowser, names: []string{"chrome", "google chrome", "谷歌浏览器"}},
	{id: "edge", kind: AdapterBrowser, names: []string{"edge", "microsoft edge", "msedge"}},
	{id: "firefox", kind: AdapterBrowser, names: []string{"firefox", "mozilla firefox", "火狐"}},
	{id: "safari", kind: AdapterBrowser, names: []string{"safari"}},
	{id: "chromium", kind: AdapterBrowser, names: []string{"chromium"}},
	{id: "iexplore", kind: AdapterBrowser, names: []string{"internet explorer", "iexplore"}},
	{id: "qqbrowser", kind: AdapterBrowser, names: []string{"qq浏览器", "qq browser"}},
	{id: "brave", kind: AdapterBrowser, names: []string{"brave"}},

	{id: "wechat", kind: AdapterIM, names: []string{"微信", "wechat", "weixin"}},
	{id: "wechat-dev", kind: AdapterGeneric, names: []string{"微信开发者工具", "wechat devtools"}},
	{id: "wecom", kind: AdapterIM, names: []string{"企业微信", "wxwork", "wecom"}},
	{id: "dingtalk", kind: AdapterIM, names: []string{"钉钉", "dingtalk"}},
	{id: "feishu", kind: AdapterIM, names: []string{"飞书", "feishu", "lark"}},
	{id: "lanxin", kind: AdapterIM, names: []string{"蓝信", "lanxin"}},
	{id: "qq", kind: AdapterIM, names: []string{"qq"}},
	{id: "slack", kind: AdapterIM, names: []string{"slack"}},
	{id: "telegram", kind: AdapterIM, names: []string{"telegram"}},
	{id: "discord", kind: AdapterIM, names: []string{"discord"}},
	{id: "teams", kind: AdapterIM, names: []string{"teams", "microsoft teams"}},
	{id: "calculator", kind: AdapterGeneric, names: []string{"计算器", "calculator"}},
}

// mentionAlias is one window-title name used when a sentence names an app.
// Longer names are tried first so 微信开发者工具 does not also count as 微信,
// and qq浏览器 does not also count as qq.
type mentionAlias struct {
	name string
	kind AdapterKind
}

var (
	groupByName        = map[string]string{}
	kindByName         = map[string]AdapterKind{}
	desktopLaunchNames []string
	browserLaunchNames []string
	mentionAliases     []mentionAlias
)

func init() {
	for _, g := range aliasGroups {
		for _, name := range g.names {
			key := NormalizeWindowTitle(name)
			if key == "" {
				continue
			}
			groupByName[key] = g.id
			kindByName[key] = g.kind
			mentionAliases = append(mentionAliases, mentionAlias{name: key, kind: g.kind})
			if g.kind == AdapterBrowser {
				browserLaunchNames = append(browserLaunchNames, key)
			} else {
				desktopLaunchNames = append(desktopLaunchNames, key)
			}
		}
	}
	// Longer names first so 企业微信 wins over 微信 and qq浏览器 wins over qq.
	sortLongestFirst(desktopLaunchNames)
	sortLongestFirst(browserLaunchNames)
	sortMentionAliases(mentionAliases)
}

func sortMentionAliases(names []mentionAlias) {
	for i := 1; i < len(names); i++ {
		name := names[i]
		j := i
		for j > 0 && len(names[j-1].name) < len(name.name) {
			names[j] = names[j-1]
			j--
		}
		names[j] = name
	}
}

// longestMentionAlias is the longest alias at the start of span.
// A shorter alias inside a longer one is not a separate mention: qq does not
// match inside qq浏览器, and 微信 does not match inside 微信开发者工具.
// An alias glued to more ASCII letters is not a mention either.
func longestMentionAlias(span string) (mentionAlias, bool) {
	var blocked []string
	for _, alias := range mentionAliases {
		if alias.name == "" || !strings.HasPrefix(span, alias.name) {
			continue
		}
		insideLonger := false
		for _, longer := range blocked {
			if strings.HasPrefix(longer, alias.name) {
				insideLonger = true
				break
			}
		}
		if insideLonger {
			continue
		}
		if len(span) > len(alias.name) && span[len(alias.name)] <= 127 && isASCIIAlphaNumByte(span[len(alias.name)]) {
			blocked = append(blocked, alias.name)
			continue
		}
		return alias, true
	}
	return mentionAlias{}, false
}

func sortLongestFirst(names []string) {
	for i := 1; i < len(names); i++ {
		name := names[i]
		j := i
		for j > 0 && len(names[j-1]) < len(name) {
			names[j] = names[j-1]
			j--
		}
		names[j] = name
	}
}

// NormalizeWindowTitle folds case, strips a dirty leading star and zero-width
// characters, and collapses whitespace so "*Untitled - Notepad" matches
// "untitled - notepad".
func NormalizeWindowTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060':
			return -1
		default:
			return r
		}
	}, s)
	s = strings.TrimLeft(s, "*")
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

func splitWindowTitle(normalized string) (prefix, suffix string, split bool) {
	seps := []string{" - ", " — ", " – ", " | "}
	best := -1
	sepLen := 0
	for _, sep := range seps {
		if i := strings.LastIndex(normalized, sep); i > best {
			best = i
			sepLen = len(sep)
		}
	}
	if best < 0 {
		return normalized, normalized, false
	}
	return strings.TrimSpace(normalized[:best]), strings.TrimSpace(normalized[best+sepLen:]), true
}

func groupOf(name string) (string, bool) {
	id, ok := groupByName[NormalizeWindowTitle(name)]
	return id, ok
}

func sameGroup(a, b string) bool {
	ga, oka := groupOf(a)
	gb, okb := groupOf(b)
	return oka && okb && ga == gb
}

func conflictsGroup(hint, part string) bool {
	gh, okh := groupOf(hint)
	gp, okp := groupOf(part)
	return okh && okp && gh != gp
}

// kindOfTitle reports the playbook family for one window title.
// The app name is the title suffix ("报告.docx - Word") or, for chat apps, the
// title prefix ("蓝信 - 张三"). A document extension or a background word such
// as 文字 does not decide the family.
func kindOfTitle(title string) AdapterKind {
	n := NormalizeWindowTitle(title)
	if n == "" {
		return AdapterGeneric
	}
	if k, ok := kindByName[n]; ok {
		return k
	}
	pre, suf, split := splitWindowTitle(n)
	if k, ok := kindByName[suf]; ok {
		return k
	}
	if split {
		if k, ok := kindByName[pre]; ok {
			return k
		}
	}
	return AdapterGeneric
}

// ScoreWindowHint ranks how well title answers a focus/crop hint.
// Higher is better. 0 means the title must not be treated as that app.
// Ties keep the earlier candidate so Z-order (topmost first) wins.
func ScoreWindowHint(hint, title string) int {
	h := NormalizeWindowTitle(hint)
	t := NormalizeWindowTitle(title)
	if h == "" || t == "" {
		return 0
	}
	if h == t {
		return 1000
	}
	// A single letter or character matches almost every title.
	if utf8.RuneCountInString(h) < 2 {
		return 0
	}
	pre, suf, split := splitWindowTitle(t)
	if h == suf || sameGroup(h, suf) {
		if conflictsGroup(h, suf) {
			return 0
		}
		return 920
	}
	if split && (h == pre || sameGroup(h, pre)) && !conflictsGroup(h, pre) {
		return 880
	}
	// "wps" focuses "WPS文字", but "word" must not focus "WordPad" and
	// "chrome" must not focus "Chrome Remote Desktop".
	if asciiCJKPrefix(h, suf) && !conflictsGroup(h, suf) {
		return 640
	}
	return 0
}

// asciiCJKPrefix reports whether suffix starts with an ASCII hint and continues
// in a non-ASCII app name ("wps" + "文字"). A following ASCII letter or word
// is a different token ("wordpad", "chrome remote").
func asciiCJKPrefix(hint, suffix string) bool {
	if len(hint) < 3 || len(suffix) <= len(hint) || !strings.HasPrefix(suffix, hint) {
		return false
	}
	for i := 0; i < len(hint); i++ {
		if hint[i] > 127 {
			return false
		}
	}
	r, _ := utf8.DecodeRuneInString(suffix[len(hint):])
	return r > 127
}

// BestWindowTitle returns the highest-scoring title for hint.
// The input order is the tie break (pass topmost windows first).
func BestWindowTitle(hint string, titles []string) (string, bool) {
	bestScore := 0
	best := ""
	for _, title := range titles {
		score := ScoreWindowHint(hint, title)
		if score > bestScore {
			bestScore = score
			best = title
		}
	}
	if bestScore <= 0 {
		return "", false
	}
	return best, true
}

// WindowTitlesMatch reports whether two window titles likely refer to the
// same top-level window. Empty values are treated as unknown (match).
// A shared app suffix ("hello.txt - Notepad" and "Untitled - Notepad", or
// 记事本 and Notepad) matches. A loose substring ("WeChat" vs "Chat",
// "微信" vs "微信开发者工具") does not.
func WindowTitlesMatch(a, b string) bool {
	na := NormalizeWindowTitle(a)
	nb := NormalizeWindowTitle(b)
	if na == "" || nb == "" {
		return true
	}
	if na == nb {
		return true
	}
	_, sa, _ := splitWindowTitle(na)
	_, sb, _ := splitWindowTitle(nb)
	if sa == "" || sb == "" {
		return false
	}
	if sa == sb || sameGroup(sa, sb) {
		return true
	}
	return false
}

// sameControlWindow reports whether two titles are the same top-level window,
// not merely the same application. It is the fallback when a stable window
// id is missing. Empty values stay unknown and match. "记事本" still matches
// "无标题 - 记事本". Two document titles that only share an app suffix
// ("订单 - Google Chrome" and "收件箱 - Google Chrome") do not.
// WindowTitlesMatch stays the app-family check used when a crop goes stale.
func sameControlWindow(a, b string) bool {
	na := NormalizeWindowTitle(a)
	nb := NormalizeWindowTitle(b)
	if na == "" || nb == "" || na == nb {
		return true
	}
	preA, sufA, splitA := splitWindowTitle(na)
	preB, sufB, splitB := splitWindowTitle(nb)
	if splitA && splitB {
		return sufA != "" && preA == preB && sufA == sufB
	}
	if splitA == splitB {
		return false
	}
	if splitA {
		return nb == preA || nb == sufA
	}
	return na == preB || na == sufB
}

// sameDocumentWindow reports that two titles are the same document, not just
// the same top-level window. A trailing unread count ("收件箱 (1)" and
// "收件箱 (2)") stays one document. A new page title ("搜索" and "订单") does
// not. Callers that already separated windows by HWND use this to see whether
// the page inside that window changed.
func sameDocumentWindow(a, b string) bool {
	if sameControlWindow(a, b) {
		return true
	}
	na := NormalizeWindowTitle(a)
	nb := NormalizeWindowTitle(b)
	if na == "" || nb == "" {
		return true
	}
	preA, sufA, splitA := splitWindowTitle(na)
	preB, sufB, splitB := splitWindowTitle(nb)
	if !splitA || !splitB || sufA == "" || sufA != sufB {
		return false
	}
	docA := stripDocNoise(preA)
	docB := stripDocNoise(preB)
	return docA != "" && docA == docB
}

// stripDocNoise removes parenthetical unread counts so "收件箱 (1) - Gmail"
// and "收件箱 (2) - Gmail" compare as the same document prefix.
func stripDocNoise(prefix string) string {
	var b strings.Builder
	r := []rune(prefix)
	for i := 0; i < len(r); {
		if r[i] == '(' || r[i] == '（' {
			j := i + 1
			digits := false
			for j < len(r) && r[j] >= '0' && r[j] <= '9' {
				digits = true
				j++
			}
			if digits && j < len(r) && (r[j] == ')' || r[j] == '）') {
				i = j + 1
				continue
			}
		}
		b.WriteRune(r[i])
		i++
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// AppFrame is one top-level window the focus matcher may activate.
// Visible is false for a tray app whose frame still exists but is hidden.
// Owned is true when another window owns this one (a tool popup or dialog).
type AppFrame struct {
	Title   string
	Visible bool
	Owned   bool
}

// BestAppFrame returns the index of the frame that best answers hint.
// A visible match wins over every hidden frame, so an open chat stays in
// front of a tray window. Among frames with the same visibility, a higher
// title score wins, then an unowned frame, then the earlier index
// (topmost first). A score of 0 is not a match.
func BestAppFrame(hint string, frames []AppFrame) (int, bool) {
	if idx := bestVisibleFrame(hint, frames, true); idx >= 0 {
		return idx, true
	}
	if idx := bestVisibleFrame(hint, frames, false); idx >= 0 {
		return idx, true
	}
	return 0, false
}

func bestVisibleFrame(hint string, frames []AppFrame, visible bool) int {
	bestScore := 0
	best := -1
	for i, frame := range frames {
		if frame.Visible != visible {
			continue
		}
		score := ScoreWindowHint(hint, frame.Title)
		if score <= 0 {
			continue
		}
		if best < 0 || appFrameBeats(frame, score, frames[best], bestScore) {
			best = i
			bestScore = score
		}
	}
	return best
}

func appFrameBeats(challenger AppFrame, cScore int, holder AppFrame, hScore int) bool {
	if cScore != hScore {
		return cScore > hScore
	}
	if challenger.Visible != holder.Visible {
		return challenger.Visible
	}
	if challenger.Owned != holder.Owned {
		return !challenger.Owned
	}
	return false
}

// DesktopAppFromHint reports the installed desktop application a focus hint
// names. The hint itself may be the app ("蓝信", "lanxin") or a window title
// whose prefix or suffix is that app ("蓝信 - 张三", "无标题 - 记事本").
// Browser aliases are not desktop apps: those windows stay on browser tools.
func DesktopAppFromHint(hint string) (string, bool) {
	h := NormalizeWindowTitle(hint)
	if h == "" {
		return "", false
	}
	if name, ok := desktopAliasName(h); ok {
		return name, true
	}
	pre, suf, split := splitWindowTitle(h)
	if !split {
		return "", false
	}
	if name, ok := desktopAliasName(pre); ok {
		return name, true
	}
	if name, ok := desktopAliasName(suf); ok {
		return name, true
	}
	return "", false
}

func desktopAliasName(name string) (string, bool) {
	key := NormalizeWindowTitle(name)
	kind, ok := kindByName[key]
	if !ok || kind == AdapterBrowser {
		return "", false
	}
	return key, true
}

// ChooseAppLaunchName picks the installed shortcut name for hint.
// names are shortcut display names, in preference order. An uninstaller
// ("蓝信卸载") and a different app ("微信") do not score, so they are not
// chosen. The hint must name a desktop app; a browser hint and a title
// that names no app return false.
func ChooseAppLaunchName(hint string, names []string) (string, bool) {
	app, ok := DesktopAppFromHint(hint)
	if !ok {
		return "", false
	}
	bestScore := 0
	best := ""
	for _, name := range names {
		score := ScoreWindowHint(app, name)
		if score > bestScore {
			bestScore = score
			best = name
		}
	}
	if bestScore <= 0 {
		return "", false
	}
	return best, true
}
