package guiapp

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	petSilenceCommitSec = 1.0
	petThinkPoseSec     = 1.0
	petThinkCueSec      = 4.0
	petCaptureWaitSec   = 8.0
)

type petUtteranceKind int

const (
	petUtteranceChat petUtteranceKind = iota
	petUtteranceCommand
	petUtteranceAmbiguous
)

type petConfirmKind int

const (
	petConfirmNone petConfirmKind = iota
	petConfirmDelete
	petConfirmSend
	petConfirmExec
	petConfirmVague
)

func petWakeHit(text string) bool {
	compact := compactPetSpeech(text)
	if compact == "" {
		return false
	}
	if strings.Contains(compact, "马卡龙") {
		return false
	}
	return strings.Contains(compact, "码卡龙")
}

func petStripWake(text string) string {
	out := text
	for _, word := range []string{"码卡龙", "，", ",", "。"} {
		out = strings.ReplaceAll(out, word, " ")
	}
	return strings.TrimSpace(out)
}

func petUtteranceKindOf(text string) petUtteranceKind {
	compact := compactPetSpeech(text)
	if compact == "" {
		return petUtteranceAmbiguous
	}
	if petIsClearlyChat(compact) && !petHasCommandCue(compact) {
		return petUtteranceChat
	}
	if petHasCommandCue(compact) {
		return petUtteranceCommand
	}
	return petUtteranceAmbiguous
}

func petNeedsSpokenConfirm(text string) petConfirmKind {
	compact := compactPetSpeech(text)
	switch {
	case petContainsAny(compact, "删除", "删掉", "删了", "rm ", "remove "):
		return petConfirmDelete
	case petContainsAny(compact, "发给", "发到", "发送", "寄给"):
		return petConfirmSend
	case petContainsAny(compact, "执行命令", "跑命令", "运行命令", "shell", "终端里跑"):
		return petConfirmExec
	case petObjectIsVague(compact):
		return petConfirmVague
	default:
		return petConfirmNone
	}
}

func petConfirmQuestion(kind petConfirmKind, text string) string {
	switch kind {
	case petConfirmDelete:
		return "删掉这个，对吗？"
	case petConfirmSend:
		return "现在发出去，对吗？"
	case petConfirmExec:
		return "在终端里跑，对吗？"
	case petConfirmVague:
		return "你说的是刚才那个，对吗？"
	default:
		return ""
	}
}

func petAffirmative(text string) bool {
	compact := compactPetSpeech(text)
	if compact == "" || petNegative(text) {
		return false
	}
	return petContainsAny(compact, "好", "对", "是", "嗯", "可以", "发吧", "删吧", "行", "执行")
}

func petNegative(text string) bool {
	compact := compactPetSpeech(text)
	return petContainsAny(compact, "不用", "不要", "算了", "取消", "别")
}

func petStopPhrase(text string) bool {
	compact := compactPetSpeech(text)
	return petContainsAny(compact, "停下", "别说", "停一下", "取消", "算了", "闭嘴")
}

func petGoodbye(text string) bool {
	compact := compactPetSpeech(text)
	return petContainsAny(compact, "再见", "拜拜", "睡觉", "先这样", "先到这")
}

func petCreatesDocument(text string) bool {
	compact := compactPetSpeech(text)
	if petContainsAny(compact, "写一份", "生成一份", "写成", "总结成", "写篇", "写一封", "做一份") {
		return true
	}
	if !petAskedForDocument(text) {
		return false
	}
	return petContainsAny(compact, "生成", "导出", "整理成", "总结", "起草", "写个", "写成", "做个", "做成", "做一份")
}

func petAskedForDocument(text string) bool {
	compact := compactPetSpeech(text)
	return petContainsAny(compact, "文档", "文稿", "报告", "纪要", "总结成", "写成", "写一份", "生成一份", "pdf", "ppt", "幻灯")
}

func petShouldOfferDocument(userText, reply string, files []string) bool {
	if len(petDocumentFiles(files)) > 0 {
		return true
	}
	return petAskedForDocument(userText) && len([]rune(strings.TrimSpace(reply))) >= 40
}

func petDocumentFiles(files []string) []string {
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok || !petLooksLikeDocument(file) {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, file)
	}
	return out
}

func petLooksLikeDocument(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".txt", ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".csv", ".html", ".htm", ".rtf", ".odt":
		return true
	default:
		return false
	}
}

func petDocumentOfferLine(reply string) string {
	const ask = "发给聊天，还是放进移动文稿库？"
	lead := clipPetSpeech(firstPetSentence(petPlainSpeech(reply)), 32)
	lead = strings.Trim(lead, "。！？!? ")
	if lead == "" || lead == "做好了" {
		return petDocumentQuestion
	}
	line := lead + "。" + ask
	if len([]rune(line)) > 80 {
		return petDocumentQuestion
	}
	return line
}

// petDocumentChoice reports where a finished document should go.
// choice is "im", "library", "decline", or "" when the answer is unclear.
// target is the spoken chat name when the user already named one.
func petDocumentChoice(text string) (choice, target string) {
	compact := compactPetSpeech(text)
	if compact == "" {
		return "", ""
	}
	if petDocumentKept(compact) {
		return "decline", ""
	}
	library := petWantsLibrary(compact)
	im := petWantsIM(compact)
	dontSend := petContainsAny(compact, "不要发", "别发", "不用发", "先不发")
	target = petDocumentTarget(text)
	if !dontSend && petContainsAny(compact, "存到", "保存到", "放到") && !petDocumentLibraryName(target) {
		if target != "" && petUsableChatName(target) {
			return "im", target
		}
		if !im && !petContainsAny(compact, "文稿库", "移动文稿", "手机") {
			return "", ""
		}
	}
	if target != "" && !petDocumentLibraryName(target) && !dontSend && im {
		return "im", target
	}
	if library && (!im || dontSend || target == "" || petDocumentLibraryName(target)) {
		return "library", ""
	}
	if im && !dontSend && target != "" {
		return "im", target
	}
	if im && !dontSend && !petDocumentRefused(compact) {
		return "im", ""
	}
	if petDocumentRefused(compact) || dontSend {
		return "decline", ""
	}
	return "", ""
}

func petWantsIM(compact string) bool {
	if petContainsAny(compact, "发给", "发到", "发出去", "送到") {
		return true
	}
	if compact == "发吧" || strings.HasPrefix(compact, "发吧") || strings.Contains(compact, "就发吧") {
		return true
	}
	switch compact {
	case "聊天", "微信", "蓝信", "电报", "qq", "im":
		return true
	default:
		return petContainsAny(compact, "发微信", "发蓝信", "发电报", "发qq")
	}
}

func petDocumentKept(compact string) bool {
	return petContainsAny(compact, "先放着", "留在这", "留着", "都不用", "保存着", "存着吧")
}

func petWantsLibrary(compact string) bool {
	if petContainsAny(compact, "不存", "别存", "不要存", "不保存", "别保存") {
		return false
	}
	return petContainsAny(compact, "文稿库", "移动文稿", "手机上", "存到手机", "保存到手机", "放进手机", "保存", "存起来", "存一下")
}

func petDocumentRefused(compact string) bool {
	return petContainsAny(compact, "不用", "不要", "算了", "取消", "先放着", "留在这", "留着", "都不用")
}

func petDocumentLibraryName(name string) bool {
	compact := compactPetSpeech(name)
	return compact == "文稿库" || compact == "移动文稿库" || compact == "移动文稿" || compact == "手机" || compact == "手机上"
}

func petDocumentTarget(text string) string {
	raw := strings.TrimSpace(text)
	prefixes := []string{"帮我发到", "帮我发给", "发出去", "发到", "发给", "发去", "送到", "放到", "存到", "保存到", "发吧"}
	bestAt, bestLen := -1, 0
	for _, prefix := range prefixes {
		at := strings.Index(raw, prefix)
		if at < 0 {
			continue
		}
		if bestAt < 0 || at < bestAt || (at == bestAt && len(prefix) > bestLen) {
			bestAt, bestLen = at, len(prefix)
		}
	}
	if bestAt < 0 {
		return ""
	}
	raw = strings.Trim(strings.TrimSpace(raw[bestAt+bestLen:]), "，。！？?、 ")
	raw = strings.TrimRight(raw, "吧啊呀呢嘛了哦")
	raw = strings.TrimSpace(raw)
	if petDocumentChannelOnly(raw) || petDocumentVagueName(raw) || len([]rune(raw)) < 2 {
		return ""
	}
	if channel, group := petSplitChatTarget(raw); channel != "" && group == "" {
		return ""
	}
	if !petUsableChatName(raw) {
		return ""
	}
	return raw
}

func petSplitChatTarget(name string) (channel, group string) {
	name = strings.TrimSpace(name)
	folded := strings.ToLower(name)
	for _, label := range []string{"企业微信", "微信", "蓝信", "电报", "qq"} {
		if !strings.HasPrefix(folded, label) {
			continue
		}
		rest := strings.TrimSpace(name[len(label):])
		for _, lead := range []string{"里面的", "里边的", "里头的", "里的", "的"} {
			if strings.HasPrefix(rest, lead) {
				rest = strings.TrimSpace(strings.TrimPrefix(rest, lead))
				break
			}
		}
		if len([]rune(rest)) < 2 {
			return label, ""
		}
		return label, rest
	}
	return "", name
}

func petIsPromptEcho(heard, prompt string) bool {
	h := compactPetSpeech(heard)
	p := compactPetSpeech(prompt)
	if h == "" || p == "" {
		return false
	}
	if h == p {
		return true
	}
	return strings.Contains(h, "还是") && strings.Contains(p, "还是") && strings.Contains(p, h)
}

func petGuardPlayback(phase string, until time.Time, now time.Time) bool {
	return phase == "speak" || now.Before(until)
}

// petMainWindowConfirm is true when the only way to finish the step is a click
// in the main window. The pet says so and stops; the card is not a spoken answer.
func petMainWindowConfirm(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	return resp.Confirmation != nil || resp.CodingRuntimeRecovery != nil || resp.DesktopUserControl
}

func petDocumentChannelOnly(raw string) bool {
	switch compactPetSpeech(raw) {
	case "", "聊天", "聊天里", "微信", "微信里", "蓝信", "蓝信里", "电报", "qq", "im":
		return true
	default:
		return false
	}
}

func petDocumentVagueName(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "他", "她", "它", "别人", "他们", "她们":
		return true
	default:
		return false
	}
}

func petUsableChatName(name string) bool {
	if !petPlausibleChatName(name) {
		return false
	}
	switch compactPetSpeech(name) {
	case "一半", "这里", "那里", "这儿", "那儿", "本地", "电脑", "这边", "那边", "哪里", "哪儿":
		return false
	default:
		return true
	}
}

func petPlausibleChatName(name string) bool {
	name = strings.TrimSpace(name)
	n := len([]rune(name))
	if n < 2 || n > 32 {
		return false
	}
	switch compactPetSpeech(name) {
	case "嗯", "啊", "哦", "好", "对", "是", "行", "那个", "这个", "什么", "不知道", "聊天", "微信", "蓝信":
		return false
	default:
		return !petDocumentChannelOnly(name) && !petDocumentVagueName(name)
	}
}

func petDocumentTitle(userText string) string {
	title := petPlainSpeech(userText)
	for _, prefix := range []string{"请帮我写一份", "帮我写一份", "帮我生成一份", "写一份", "生成一份", "做一份", "帮我写", "帮我"} {
		if strings.HasPrefix(title, prefix) {
			title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
			break
		}
	}
	title = clipPetSpeech(title, 24)
	if title == "" {
		return "宠物文稿"
	}
	return title
}

func petIsFollowUp(text string) bool {
	compact := compactPetSpeech(text)
	return petContainsAny(compact, "再", "刚才", "那个", "继续", "短一点", "长一点", "换一种")
}

func petHeardNothing(rms float64, sampleCount int) bool {
	return sampleCount < 16000/5 || rms < 0.008
}

func petSpokenLine(cfg corelib.AppConfig, kind petUtteranceKind, text string) string {
	original := text
	text = strings.TrimLeft(petPlainSpeech(text), "。！？.!? \t")
	if text == "" {
		if petBareCompletion(original) {
			text = "做好了。"
		} else {
			text = "这步没做成。"
		}
	}
	mode := strings.TrimSpace(cfg.PetReadbackMode)
	if mode == "" {
		if cfg.PetVoiceReadback {
			mode = "summary"
		} else {
			mode = "off"
		}
	}
	if kind == petUtteranceChat {
		return clipPetSpeech(text, 80)
	}
	body := petDropLeadingCompletion(text)
	switch mode {
	case "off":
		return clipPetSpeech(body, 48)
	case "full":
		return clipPetSpeech(body, 160)
	case "done-only", "summary":
		return clipPetSpeech(petResultLead(body), 48)
	default:
		return clipPetSpeech(petResultLead(body), 48)
	}
}

func petFailureLine(err error) string {
	if err == nil {
		return "这步没做成。"
	}
	raw := strings.TrimSpace(err.Error())
	if line := petKnownFailure(raw); line != "" {
		return line
	}
	if line := petChineseFailure(raw); line != "" {
		return clipPetSpeech(line, 48)
	}
	return "这步没做成。"
}

func petKnownFailure(raw string) string {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "not configured"):
		return "还没配好模型。"
	case strings.Contains(low, "budget"):
		return "今天的用量到了。"
	case strings.Contains(low, "unavailable"), strings.Contains(low, "missing hub"):
		return "助手这会儿没连上。"
	case strings.Contains(low, "slot occupied"), strings.Contains(low, "long-running"):
		return "有个任务还在跑。"
	default:
		return ""
	}
}

func petChineseFailure(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !petHasChinese(raw) {
		return ""
	}
	low := strings.ToLower(raw)
	if strings.Contains(low, "tool") || strings.Contains(raw, "\\") || strings.Contains(raw, ".go:") || strings.Contains(raw, "{") {
		return ""
	}
	sentence := firstPetSentence(strings.TrimLeft(petPlainSpeech(raw), "。！？.!? \t"))
	if sentence == "" {
		return ""
	}
	last, _ := utf8.DecodeLastRuneInString(sentence)
	switch last {
	case '。', '！', '？':
	default:
		sentence += "。"
	}
	if strings.HasPrefix(sentence, "这步") || strings.HasPrefix(sentence, "没做成") {
		return sentence
	}
	reason := strings.TrimRight(sentence, "。！？")
	return "没做成，" + reason + "。"
}

func petHasChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func petBareCompletion(text string) bool {
	switch compactPetSpeech(text) {
	case "做好了", "完成了", "好了", "任务已完成", "已完成", "done", "taskcompleted":
		return true
	default:
		return false
	}
}

func petDropLeadingCompletion(text string) string {
	rest := strings.TrimSpace(text)
	for rest != "" {
		sentence := firstPetSentence(rest)
		if sentence == "" || !petBareCompletion(sentence) {
			break
		}
		next := strings.TrimSpace(strings.TrimPrefix(rest, sentence))
		if next == "" {
			return rest
		}
		rest = next
	}
	return rest
}

func petResultLead(text string) string {
	lead := firstPetSentence(petDropLeadingCompletion(text))
	if lead == "" {
		return firstPetSentence(text)
	}
	return lead
}

func petPlainSpeech(text string) string {
	text = strings.TrimSpace(text)
	replacer := strings.NewReplacer(
		"```", "",
		"**", "",
		"__", "",
		"`", "",
		"#", "",
		"任务已完成", "",
		"Task completed", "",
	)
	text = strings.TrimSpace(replacer.Replace(text))
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}

func firstPetSentence(text string) string {
	for i, r := range text {
		if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' {
			return strings.TrimSpace(text[:i+utf8.RuneLen(r)])
		}
	}
	return strings.TrimSpace(text)
}

func clipPetSpeech(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func petIsClearlyChat(compact string) bool {
	return petContainsAny(compact, "你好", "您好", "在吗", "早上好", "晚上好", "谢谢", "哈哈", "呵呵", "无聊", "陪我", "讲个笑话", "你是谁", "在干嘛")
}

func petHasCommandCue(compact string) bool {
	return petContainsAny(compact, "帮我", "打开", "查一下", "查查", "搜索", "改成", "修改", "写", "删除", "删掉", "发给", "发送", "运行", "执行", "看看", "处理")
}

func petObjectIsVague(compact string) bool {
	if !petContainsAny(compact, "发给他", "发给她", "删掉它", "删了它", "就这个") {
		return false
	}
	return !petContainsAny(compact, "文件", "邮件", "消息", "群")
}

func compactPetSpeech(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var b strings.Builder
	for _, r := range text {
		switch r {
		case ' ', '\n', '\t', '，', '。', '！', '？', ',', '.', '!', '?', '、', '：', ':':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func petContainsAny(text string, parts ...string) bool {
	for _, part := range parts {
		if part != "" && strings.Contains(text, part) {
			return true
		}
	}
	return false
}
