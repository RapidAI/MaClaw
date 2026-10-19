package guiapp

import (
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifyTaskResponseTray pops a system tray balloon notification when an
// interactive agent task reaches a terminal state: failed, awaiting user
// confirmation, waiting for an ask_user reply, or completed. A named task says
// "xx任务完成了" as the balloon title. The name is an explicit label or the
// tasks/<slug>-<id> directory, including when the session is a subdirectory.
// The reply stays in the body. A turn that is still asking, recording, or
// waiting for the person is not called complete.
// Completion follows the general-settings switch and shows even while the
// main window is open. A named task uses "xx任务完成了" as the title; a reply
// with no task name keeps the reply text. Other notices still wait until the
// window is hidden or minimised. User-initiated cancellations are skipped.
// It must never panic: callers treat a panic in this emit path as a task
// failure and would surface a bogus error response.
func (a *App) notifyTaskResponseTray(resp *IMAgentResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tray-notify] panic suppressed: %v", r)
		}
	}()
	if resp == nil || ShowNotification == nil {
		return
	}
	source := canonicalIMResponseSourceKind(resp.ResponseSource)
	if source == imResponseSourceCancel {
		// User-initiated cancellation; the user already knows.
		return
	}
	if trayResponseIsCompletion(resp) {
		if !a.trayTaskCompletionNotifyEnabled() {
			return
		}
	} else if !a.shouldTrayNotify() {
		return
	}
	// A desktop-bot balloon speaks the same sentence as the chat. Text is
	// assistant prose and must not become a second notice. No sentence, and
	// no real error, means no balloon.
	sentence, desktopBot := desktopBotTraySentence(resp)
	replyText := resp.Text
	if desktopBot {
		replyText = sentence
	}
	// A desktop run can fail and still be waiting on this person: a password,
	// a choice, or the desktop it just handed over. That notice comes before
	// the bare error, which would otherwise hide it. A placeholder question
	// is not that wait: the chat already shows the error or the handoff instead.
	waits := trayResponseWaitsForReply(resp)
	if desktopBot {
		// A real choice is a wait even when the question string is blank.
		// A placeholder question, or a placeholder label, is not.
		waits = desktopBotTrayAsk(resp)
	}
	personStillNeeded := desktopBot && (waits || trayResponseNeedsConfirm(resp))
	switch {
	case strings.TrimSpace(resp.Error) != "" && sentence == "" && !personStillNeeded:
		if desktopBot && desktopbot.Filler(resp.Error) {
			return
		}
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		msg := trayBalloonBody(resp.Error)
		if msg == "" {
			msg = "任务出现异常，请返回窗口查看"
		}
		ShowNotification("任务异常", msg, 3)
	case waits:
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		msg := trayBalloonBody(resp.AskUserQuestion)
		if desktopBot && desktopbot.Filler(resp.AskUserQuestion) {
			msg = ""
		}
		if msg == "" {
			msg = trayBalloonBody(replyText)
		}
		if msg == "" {
			if secret := strings.TrimSpace(resp.AskUserSecretName); secret != "" {
				msg = trayBalloonBody("需要提供 " + secret)
			}
		}
		if msg == "" {
			msg = "任务等待回复，请返回窗口查看"
		}
		ShowNotification("等待回复", msg, 2)
	case trayResponseNeedsConfirm(resp):
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		msg := ""
		if resp.Confirmation != nil {
			msg = trayBalloonBody(resp.Confirmation.Summary)
		}
		if msg == "" {
			msg = trayBalloonBody(replyText)
		}
		if msg == "" {
			msg = "任务等待确认，请返回窗口处理"
		}
		ShowNotification("需要确认", msg, 2)
	default:
		if desktopBot && sentence == "" {
			return
		}
		msg := trayBalloonBody(replyText)
		title := "任务完成"
		if name := trayTaskCompletionName(resp); name != "" {
			title = trayTaskCompletedMessage(name)
			if msg == "" {
				// Windows hides a balloon whose body is empty.
				msg = title
			}
		} else if msg == "" {
			msg = "任务完成了"
		}
		ShowNotification(title, msg, 1)
	}
}

// desktopBotTraySentence is the send_to_user line a desktop-bot balloon may
// show. Other chats are unchanged. A filler or a missing call is not a sentence.
func desktopBotTraySentence(resp *IMAgentResponse) (sentence string, desktop bool) {
	if resp == nil || !strings.HasPrefix(strings.TrimSpace(resp.RequestID), "desktop-bot-") {
		return "", false
	}
	visible := desktopbot.Visible(desktopbot.Turn{
		SendToUser: resp.SendToUser,
		Error:      resp.Error,
	})
	if visible.Bubble && !visible.Failed {
		return visible.Sentence, true
	}
	return "", true
}

// desktopBotTrayAsk is a question this person can answer. A placeholder such
// as 无变化 is not one, so it must not hide the error or the handoff.
// A secret name or a real choice still is, even when the question is that placeholder.
func desktopBotTrayAsk(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	if strings.TrimSpace(resp.AskUserSecretName) != "" {
		return true
	}
	if desktopAskHasOptions(resp.AskUserOptionsJSON) {
		return true
	}
	question := strings.TrimSpace(resp.AskUserQuestion)
	return question != "" && !desktopbot.Filler(question)
}

// trayResponseIsCompletion is a finished turn, not an error or a pause for the person.
func trayResponseIsCompletion(resp *IMAgentResponse) bool {
	if resp == nil || canonicalIMResponseSourceKind(resp.ResponseSource) == imResponseSourceCancel {
		return false
	}
	if strings.TrimSpace(resp.Error) != "" || trayResponseWaitsForReply(resp) || trayResponseNeedsConfirm(resp) {
		return false
	}
	return true
}

// trayTaskCompletionNotifyEnabled is the general-settings switch.
// A missing value stays on.
func (a *App) trayTaskCompletionNotifyEnabled() bool {
	if a == nil {
		return true
	}
	cfg := a.PeekConfig()
	if cfg == nil || cfg.TrayTaskCompletionNotify == nil {
		return true
	}
	return *cfg.TrayTaskCompletionNotify
}

// trayResponseWaitsForReply is a turn that stopped to ask this person something.
// The reply text is not a finished task, even when ResponseSource was not set.
func trayResponseWaitsForReply(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	if strings.TrimSpace(resp.AskUserQuestion) != "" || strings.TrimSpace(resp.AskUserSecretName) != "" {
		return true
	}
	switch canonicalIMResponseSourceKind(resp.ResponseSource) {
	case imResponseSourceAskUser, imResponseSourceRecordAudio:
		return true
	default:
		return false
	}
}

// trayResponseNeedsConfirm is a turn the person still has to accept or take over.
func trayResponseNeedsConfirm(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	return resp.Confirmation != nil || resp.UnfinishedTask != nil || resp.UnfinishedSlot != nil ||
		resp.CodingRuntimeRecovery != nil || resp.DesktopUserControl ||
		strings.TrimSpace(resp.DesktopAttentionReason) != ""
}

// trayTaskCompletionName is the label in "xx任务完成了". An explicit tray label
// wins. A session path contributes a name only when it is a managed task directory.
func trayTaskCompletionName(resp *IMAgentResponse) string {
	if resp == nil {
		return ""
	}
	if name := strings.TrimSpace(resp.trayTaskName); name != "" {
		return name
	}
	return managedTaskTitleFromSessionPath(projectPathFromSessionOwnerID(strings.TrimSpace(resp.SessionKey)))
}

// managedTaskTitleFromSessionPath reads the tasks/<slug>-<id> segment.
// A workspace subdirectory stays on that task. A project folder, or a
// directory that only happens to sit under data/tasks, is not a task name.
func managedTaskTitleFromSessionPath(path string) string {
	n := filepath.ToSlash(normalizeProjectSessionPath(path))
	if n == "" {
		return ""
	}
	const marker = "/data/tasks/"
	i := strings.Index(strings.ToLower(n), marker)
	if i < 0 {
		return ""
	}
	rest := n[i+len(marker):]
	slug, _, _ := strings.Cut(rest, "/")
	if !looksLikeManagedTaskDirName(slug) {
		return ""
	}
	return stripTaskDirTimestampSuffix(slug)
}

func trayTaskLabelFromCommand(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shortenTrayTaskName(name string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	name = strings.TrimSpace(reTrayHeading.ReplaceAllString(name, ""))
	name = strings.Join(strings.Fields(stripTrayInline(name)), " ")
	runes := []rune(name)
	if len(runes) > trayTitleRunes {
		return string(runes[:trayTitleRunes]) + "…"
	}
	return name
}

func trayTaskCompletedMessage(name string) string {
	name = shortenTrayTaskName(name)
	if name == "" {
		return "任务完成了"
	}
	suffix := "任务完成了"
	if strings.HasSuffix(name, "任务") {
		suffix = "完成了"
	}
	// szInfoTitle holds 63 UTF-16 units plus a NUL. Emoji take two units.
	budget := trayTitleUnits - utf16Units(suffix)
	if utf16Units(name) > budget {
		name = truncateTrayUnits(name, budget)
	}
	return name + suffix
}

const (
	// trayInfoUnits is NOTIFYICONDATA.szInfo without the terminating NUL.
	trayInfoUnits = 255
	// trayTitleUnits is NOTIFYICONDATA.szInfoTitle without the terminating NUL.
	trayTitleUnits = 63
	// trayTitleRunes keeps a task name short before the "任务完成了" suffix.
	trayTitleRunes = 40
	// trayScanRunes bounds markdown cleanup. The balloon only shows trayInfoUnits.
	trayScanRunes = 4000
)

// trayBalloonBody is the plain, length-capped text placed in a tray balloon.
func trayBalloonBody(text string) string {
	return fitTrayInfo(trayBalloonPlain(text), trayInfoUnits)
}

// trayNotifyBody is "name: summary", shortened so the whole line fits the balloon.
// A trailing delivery warning stays intact when the summary is shortened.
func trayNotifyBody(name, summary string) string {
	name = shortenTrayTaskName(name)
	summary = trayBalloonPlain(summary)
	if name == "" {
		return fitTrayInfo(summary, trayInfoUnits)
	}
	if summary == "" {
		return name
	}
	lead := name + ": "
	budget := trayInfoUnits - utf16Units(lead)
	if budget < 24 {
		return fitTrayInfo(lead+"\n"+summary, trayInfoUnits)
	}
	return lead + fitTrayInfo(summary, budget)
}

// trayBalloonPlain turns Markdown into text a balloon can show. Fenced code is
// left out so a snippet does not crowd out the sentence around it. Underscores
// stay, because identifiers such as __init__ and snake_case are not emphasis.
// A trailing "[投递警告]" note is kept separate from the prose.
func trayBalloonPlain(text string) string {
	body, warn := splitTrayDeliveryWarning(text)
	prose, snippet := splitTrayFences(body)
	prose = collapseTrayLines(prose)
	if prose == "" {
		prose = strings.Join(strings.Fields(snippet), " ")
	}
	if warn == "" {
		return prose
	}
	if prose == "" {
		return warn
	}
	return prose + "\n\n" + warn
}

func splitTrayDeliveryWarning(text string) (body, warn string) {
	idx := strings.LastIndex(text, "[投递警告]")
	if idx < 0 {
		return text, ""
	}
	head := strings.TrimRight(text[:idx], " \t")
	if head != "" && !strings.HasSuffix(head, "\n") && !strings.HasSuffix(head, "\r") {
		return text, ""
	}
	return head, strings.TrimSpace(text[idx:])
}

// splitTrayFences separates prose from fenced code. A ```markdown or ```md
// block is the answer itself, so its contents stay. Other fences are code and
// stay out of the balloon except for the first line, used when there is no prose.
// A bare ~~~ line is a rule, not a fence, so it cannot hide the lines after it.
func splitTrayFences(text string) (prose, snippet string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var b strings.Builder
	var stack []bool // true when the open fence is prose (markdown/md)
	for _, line := range strings.Split(text, "\n") {
		if isTrayFenceLine(line) {
			lang := fenceLanguage(line)
			if len(stack) > 0 && (lang == "" || !stack[len(stack)-1]) {
				stack = stack[:len(stack)-1]
				continue
			}
			stack = append(stack, fenceIsProse(lang))
			continue
		}
		if len(stack) > 0 && !stack[len(stack)-1] {
			if snippet == "" {
				snippet = strings.TrimSpace(line)
			}
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), snippet
}

func isTrayFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func fenceLanguage(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "```") {
		return ""
	}
	line = strings.TrimSpace(line[3:])
	lang, _, _ := strings.Cut(line, " ")
	return strings.ToLower(lang)
}

func fenceIsProse(lang string) bool {
	switch lang {
	case "markdown", "md":
		return true
	default:
		return false
	}
}

func collapseTrayLines(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if trayLooksPlain(text) {
		return strings.Join(strings.Fields(text), " ")
	}
	text = reTrayBreak.ReplaceAllString(text, "\n")
	if len(text) > trayScanRunes {
		text = capTrayRunes(text, trayScanRunes)
	}
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = normalizeTrayLine(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func trayLooksPlain(text string) bool {
	if strings.ContainsAny(text, "`*~#[]|>!<") {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line[0] {
		case '-', '+':
			return false
		}
		if line[0] >= '0' && line[0] <= '9' {
			return false
		}
	}
	return true
}

func normalizeTrayLine(line string) string {
	for {
		line = strings.TrimSpace(line)
		if line == "" {
			return ""
		}
		if strings.HasPrefix(line, ">") {
			line = line[1:]
			continue
		}
		if reTrayHeading.MatchString(line) {
			line = reTrayHeading.ReplaceAllString(line, "")
			continue
		}
		break
	}
	line = strings.TrimSpace(reTrayList.ReplaceAllString(line, ""))
	line = strings.TrimSpace(reTrayBox.ReplaceAllString(line, ""))
	// Only a row wrapped in pipes is a table. Inline code such as `a|b|c` stays.
	if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && strings.Count(line, "|") >= 2 {
		cells := strings.ReplaceAll(line, "|", " ")
		if trayLineIsDecoration(cells) {
			return ""
		}
		line = cells
	}
	if trayLineIsDecoration(line) {
		return ""
	}
	line = stripTrayInline(line)
	if strings.Trim(line, "*_~` ") == "" {
		return ""
	}
	return strings.Join(strings.Fields(line), " ")
}

func trayLineIsDecoration(line string) bool {
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) < 3 {
		return false
	}
	for _, r := range line {
		switch r {
		case '-', ':', '=', '*', '_', '~', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

func stripTrayInline(text string) string {
	if text == "" || !strings.ContainsAny(text, "`*~[!<") {
		return text
	}
	text = reTrayCode.ReplaceAllString(text, "$1")
	text = reTrayImage.ReplaceAllString(text, "$1")
	text = reTrayLink.ReplaceAllString(text, "$1")
	text = reTrayAutoLink.ReplaceAllString(text, "$1")
	text = reTrayStrike.ReplaceAllString(text, "$1")
	text = reTrayBoldItalic.ReplaceAllString(text, "$1")
	text = reTrayBold.ReplaceAllString(text, "$1")
	text = reTrayItalic.ReplaceAllString(text, "$1")
	return text
}

func fitTrayInfo(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" || max <= 0 {
		return ""
	}
	body, warn := splitTrayDeliveryWarning(text)
	body = strings.TrimSpace(body)
	if warn == "" {
		return truncateTrayUnits(body, max)
	}
	warnUnits := utf16Units(warn)
	if warnUnits >= max {
		return truncateTrayUnits(warn, max)
	}
	// Two newlines sit between the prose and the warning.
	body = truncateTrayUnits(body, max-warnUnits-utf16Units("\n\n"))
	if body == "" {
		return warn
	}
	return body + "\n\n" + warn
}

// utf16Units counts UTF-16 code units, matching NOTIFYICONDATA's wchar buffer.
// A rune above U+FFFF takes a surrogate pair.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// truncateTrayUnits shortens s so its UTF-16 length, including an ellipsis when
// cut, stays within max units. A rune is never split.
func truncateTrayUnits(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if utf16Units(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	units := 0
	end := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > max-1 {
			break
		}
		units += w
		i += size
		end = i
	}
	if end == 0 {
		return "…"
	}
	return s[:end] + "…"
}

func capTrayRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

var (
	reTrayHeading    = regexp.MustCompile(`^#{1,6}\s+`)
	reTrayList       = regexp.MustCompile(`^(?:[-*+]|\d{1,3}[.)])\s+`)
	reTrayBox        = regexp.MustCompile(`^\[[ xX]\]\s*`)
	reTrayBreak      = regexp.MustCompile(`(?i)<br\s*/?>`)
	reTrayCode       = regexp.MustCompile("`([^`]+)`")
	reTrayImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reTrayLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reTrayAutoLink   = regexp.MustCompile(`<((?:https?://|mailto:)[^>\s]+)>`)
	reTrayStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reTrayBoldItalic = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reTrayBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reTrayItalic     = regexp.MustCompile(`\*([^*\n]+)\*`)
)

func (a *App) shouldTrayNotify() bool {
	if IsMainWindowVisible != nil && !IsMainWindowVisible() {
		return true
	}
	if a != nil && a.ctx != nil {
		return runtime.WindowIsMinimised(a.ctx)
	}
	return false
}
