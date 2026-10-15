package main

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopActivityThinking is the gap after one desktop step, while the model
// chooses the next one. It is a real pause, not a stand-in for the step.
const desktopActivityThinking = "正在思考"

// desktopActivityPhrases are the only words a desktop run may show on the
// working row. The typed text, a window title, and a tool name are not in
// this set, so a password or a path cannot become the label.
var desktopActivityPhrases = map[string]struct{}{
	"正在输入":   {},
	"正在点击":   {},
	"正在按键":   {},
	"正在翻页":   {},
	"正在看屏幕":  {},
	"正在打开":   {},
	"正在打开网页": {},
	"正在查看":   {},
	"正在读取":   {},
	"正在编辑":   {},
	"正在执行命令": {},
	"正在安装":   {},
	"正在交付":   {},
	"正在切换窗口": {},
	"正在查找":   {},
	"正在选择":   {},
	"正在操作网页": {},
	"正在等待":   {},
	desktopActivityThinking: {},
}

var desktopActivities sync.Map

func desktopActivityAllowed(phrase string) bool {
	_, ok := desktopActivityPhrases[strings.TrimSpace(phrase)]
	return ok
}

func setDesktopActivity(runID, phrase string) {
	runID = strings.TrimSpace(runID)
	phrase = strings.TrimSpace(phrase)
	if runID == "" || !desktopActivityAllowed(phrase) {
		return
	}
	desktopActivities.Store(runID, phrase)
}

func loadDesktopActivity(runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ""
	}
	value, ok := desktopActivities.Load(runID)
	if !ok {
		return ""
	}
	text, _ := value.(string)
	if !desktopActivityAllowed(text) {
		return ""
	}
	return text
}

func clearDesktopActivity(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopActivities.Delete(runID)
}

// beginDesktopActivity shows phrase for the step that is about to run, then
// leaves the run on 正在思考 when that step returns. The cleanup is what the
// caller defers. An empty run id changes nothing.
func beginDesktopActivity(runID, phrase string) func() {
	setDesktopActivity(runID, phrase)
	return func() { setDesktopActivity(runID, desktopActivityThinking) }
}

func desktopContainerPhrase(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "file_read", "file_list":
		return "正在读取"
	case "file_write", "file_edit":
		return "正在编辑"
	case "bash":
		return "正在执行命令"
	default:
		return ""
	}
}

func desktopKeyPhrase(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "正在按键"
	}
	if browser.KeyInsertsText(key) {
		return "正在输入"
	}
	switch strings.ToLower(key) {
	case "return", "enter", "kp_enter", "linefeed":
		return "正在输入"
	default:
		return "正在按键"
	}
}

func desktopNamedActionPhrase(action, key string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "type":
		return "正在输入"
	case "click":
		return "正在点击"
	case "select":
		return "正在选择"
	case "scroll":
		return "正在翻页"
	case "navigate":
		return "正在打开网页"
	case "press", "key":
		return desktopKeyPhrase(key)
	case "wait":
		return "正在等待"
	case "probe", "screenshot", "observe":
		return "正在看屏幕"
	case "focus":
		return "正在切换窗口"
	case "find", "search":
		return "正在查找"
	default:
		return ""
	}
}

// desktopArgvPhrase names one xdotool step. The words are the verb only.
// The text being typed is an argument and stays out of the phrase.
func desktopArgvPhrase(step []string) string {
	if len(step) == 0 {
		return ""
	}
	switch step[0] {
	case "type":
		return "正在输入"
	case "key":
		key := ""
		if len(step) >= 2 {
			key = step[len(step)-1]
		}
		return desktopKeyPhrase(key)
	case "mousemove":
		return "正在点击"
	case "search", "windowactivate":
		return "正在切换窗口"
	default:
		return ""
	}
}

func desktopClickPhrase(argv []string) string {
	for _, part := range argv {
		if part == "click" {
			return "正在点击"
		}
	}
	return ""
}

func desktopBrowserStepsPhrase(steps []browser.StepSpec) string {
	phrase := ""
	for _, step := range steps {
		next := desktopNamedActionPhrase(step.Action, step.Params["key"])
		if next == "" {
			continue
		}
		if phrase == "" {
			phrase = next
			continue
		}
		if next != phrase {
			return "正在操作网页"
		}
	}
	if phrase == "" {
		return "正在操作网页"
	}
	return phrase
}

// desktopScriptCalls reports a page-script call. A CSS pseudo such as
// nth-of-type( or a method such as prototype( is not that call, and the
// argument, including anything the call types, is not part of the phrase.
func desktopScriptCalls(script, name string) bool {
	lower := strings.ToLower(script)
	needle := name + "("
	from := 0
	for {
		at := strings.Index(lower[from:], needle)
		if at < 0 {
			return false
		}
		at += from
		if at == 0 {
			return true
		}
		prev := lower[at-1]
		if (prev < 'a' || prev > 'z') && (prev < '0' || prev > '9') && prev != '_' && prev != '-' {
			return true
		}
		from = at + len(needle)
	}
}

// desktopScriptPhrase reads the verbs in one page script. The script body,
// including anything it types, is not part of the phrase.
func desktopScriptPhrase(script string) string {
	has := func(name string) bool {
		return desktopScriptCalls(script, name)
	}
	found := make([]string, 0, 4)
	if has("type") {
		found = append(found, "正在输入")
	}
	if has("click") {
		found = append(found, "正在点击")
	}
	if has("press") {
		found = append(found, "正在按键")
	}
	if has("scroll") {
		found = append(found, "正在翻页")
	}
	if has("select") {
		found = append(found, "正在选择")
	}
	if len(found) == 1 {
		return found[0]
	}
	return "正在操作网页"
}
