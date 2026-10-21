package guiapp

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// desktopBotMaterialNames are the file names of a trailing [素材] list.
func desktopBotMaterialNames(message string) []string {
	if !desktopBotHasMaterials(message) {
		return nil
	}
	at := strings.LastIndex(message, "[素材]\n")
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(message[at+len("[素材]\n"):]), "\n") {
		p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if name := path.Base(p); name != "" && name != "." && name != "/" {
			names = append(names, name)
		}
	}
	return names
}

// desktopBotWordsBeforeMaterials is the typed text without the [素材] list.
func desktopBotWordsBeforeMaterials(message string) string {
	if !desktopBotHasMaterials(message) {
		return strings.TrimSpace(message)
	}
	return strings.TrimSpace(message[:strings.LastIndex(message, "[素材]\n")])
}

// desktopBotLooksEnglish: no Han characters in the typed words.
func desktopBotLooksEnglish(words string) bool {
	for _, r := range words {
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return strings.TrimSpace(words) != ""
}

// desktopBotFileList names up to three files: 《a》、《b》, or 《a》、《b》等 5 个文件.
func desktopBotFileList(names []string, en bool) string {
	shown := names
	if len(shown) > 3 {
		shown = shown[:2]
	}
	if en {
		quoted := make([]string, len(shown))
		for i, n := range shown {
			quoted[i] = "\"" + n + "\""
		}
		out := strings.Join(quoted, ", ")
		if len(names) > len(shown) {
			out += fmt.Sprintf(" and %d more", len(names)-len(shown))
		}
		return out
	}
	quoted := make([]string, len(shown))
	for i, n := range shown {
		quoted[i] = "《" + n + "》"
	}
	out := strings.Join(quoted, "、")
	if len(names) > len(shown) {
		out += fmt.Sprintf("等 %d 个文件", len(names))
	}
	return out
}

func desktopBotMentionsAny(told string, names []string) bool {
	for _, n := range names {
		stem := strings.TrimSuffix(n, path.Ext(n))
		if strings.Contains(told, n) || (utf8.RuneCountInString(stem) >= 4 && strings.Contains(told, stem)) {
			return true
		}
	}
	return false
}

// desktopBotShortGoal trims a goal or task card to a phrase that fits in
// one spoken sentence, or "" when it will not.
func desktopBotShortGoal(goal string) string {
	goal = strings.TrimSpace(strings.SplitN(strings.TrimSpace(goal), "\n", 2)[0])
	for _, prefix := range []string{"目标：", "目标:", "goal:", "Goal:", "去"} {
		goal = strings.TrimSpace(strings.TrimPrefix(goal, prefix))
	}
	goal = strings.TrimRight(goal, "。.!！，, ")
	if goal == "" || utf8.RuneCountInString(goal) > 24 {
		return ""
	}
	return goal
}

// desktopBotMaterialAcceptTold is the first line for a new job that came
// with files: it names them and says what they are for.
func desktopBotMaterialAcceptTold(u *DesktopBotUnderstanding, names []string, en bool, base string) string {
	files := desktopBotFileList(names, en)
	if u.Fallback != "" {
		if en {
			return fmt.Sprintf("Got %s. I'll try it exactly as you put it; stop me anytime if I've got it wrong.", files)
		}
		return fmt.Sprintf("收到%s，我先照你的原话去试试，哪里理解偏了随时叫停我。", files)
	}
	if len(u.Options) >= 2 || (strings.TrimSpace(u.Unclear) != "" && strings.TrimSpace(u.Assumption) != "") {
		if en {
			return fmt.Sprintf("Got %s. %s", files, base)
		}
		return fmt.Sprintf("收到%s。%s", files, base)
	}
	if desktopBotMentionsAny(base, names) {
		return base
	}
	goal := desktopBotShortGoal(u.Goal)
	if en {
		if goal != "" {
			return fmt.Sprintf("Got %s. I'll use it for %s and start now.", files, goal)
		}
		return fmt.Sprintf("Got %s. Starting with it now.", files)
	}
	if goal != "" {
		return fmt.Sprintf("收到%s，我拿它当%s的素材，这就开始。", files, goal)
	}
	return fmt.Sprintf("收到%s，这就用它开始。", files)
}

// desktopBotMaterialKeepTold answers files sent with no job: they are kept
// for later.
func desktopBotMaterialKeepTold(told string, names []string, en bool) string {
	if strings.TrimSpace(told) != "" && desktopBotMentionsAny(told, names) {
		return told
	}
	files := desktopBotFileList(names, en)
	if en {
		return fmt.Sprintf("Got %s. It's in ~/素材 on the desktop; tell me when you want it used.", files)
	}
	return fmt.Sprintf("收到%s，先放在桌面的 ~/素材 里，要用的时候说一声。", files)
}

// desktopBotMaterialSupplementAck answers files added to a job already
// running, naming that job when it is short enough to say.
func desktopBotMaterialSupplementAck(names []string, taskText string, en bool) string {
	files := desktopBotFileList(names, en)
	goal := desktopBotShortGoal(desktopBotWordsBeforeMaterials(taskText))
	if en {
		if goal != "" {
			return fmt.Sprintf("Got %s. I'll use it for %s next.", files, goal)
		}
		return fmt.Sprintf("Got %s. I'll use it in what I'm working on.", files)
	}
	if goal != "" {
		return fmt.Sprintf("收到附件%s，接下来会用它来%s。", files, goal)
	}
	return fmt.Sprintf("收到附件%s，我会用在手上这件事里。", files)
}
