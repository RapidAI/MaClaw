package agent

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// shouldNudgeStoredRetrieval is the one-shot gate for a full turn that was
// given a side-effecting tool and then stopped before using any tool. Memory
// or knowledge search is still listed and unused. Read-only surfaces, including
// file reads, are left alone.
//
// A pure social greeting is not nudged. A self-contained reply is not nudged
// either, when the user turn itself does not need stored connection facts and
// the reply does not ask the user to supply any. Forcing a lookup then makes
// the model answer the system instruction, and the chat keeps that receipt
// instead of the deliverable: a finished translation was hidden under 思考过程.
// An empty reply, a reply that asks for missing facts, or a user turn that
// itself needs stored connection facts is still nudged.
func shouldNudgeStoredRetrieval(light bool, tools []map[string]interface{}, history []ConversationEntry, nudges int, userText, assistantReply string) bool {
	if light || nudges >= 1 || turnUsedAnyTool(history) || !surfaceHasExecutionTool(tools) || IsPureSocialGreeting(userText) {
		return false
	}
	if !retrievalToolsStillPending(tools, history) {
		return false
	}
	if strings.TrimSpace(assistantReply) == "" || answerSolicitsMissingFacts(assistantReply) || userTurnNeedsStoredFacts(userText) {
		return true
	}
	return false
}

// answerSolicitsMissingFacts reports a reply that asks the user for a stored
// connection fact (host, account, password). A finished translation does not.
// Only the closing stretch of a long reply is checked, so a quoted "请提供"
// inside the source text does not force another model round.
func answerSolicitsMissingFacts(content string) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	if runes := []rune(text); len(runes) > 240 {
		text = string(runes[len(runes)-240:])
	}
	lower := strings.ToLower(text)
	if !containsAny(lower, storedFactRequestVerbs) {
		return false
	}
	return containsAny(lower, storedFactNounsCJK) || containsAnyASCIIWord(lower, storedFactNounsASCII)
}

var storedFactRequestVerbs = []string{
	"请提供", "请补充", "请告诉", "麻烦提供", "还请提供",
	"需要你提供", "需要您提供", "需要你告诉", "需要您告诉",
	"能否提供", "可否提供",
	"please provide", "could you provide", "can you provide", "please tell me",
}

var storedFactNounsCJK = []string{
	"主机", "端口", "账号", "帐户", "账户", "密码", "用户名", "连接方式", "服务器", "地址",
}

// ASCII nouns are whole words. "port" must not match inside important or report.
var storedFactNounsASCII = []string{"host", "port", "password", "account", "username", "ssh"}

var storedFactUserCuesCJK = []string{
	"连上", "登录", "登陆", "服务器", "主机", "端口", "账号", "帐户", "账户", "密码",
	"跳板", "数据库",
}

var storedFactUserCuesASCII = []string{"ssh", "mysql", "postgres", "redis"}

func containsAny(text string, cues []string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func containsAnyASCIIWord(text string, words []string) bool {
	for _, word := range words {
		if containsASCIIWord(text, word) {
			return true
		}
	}
	return false
}

func containsASCIIWord(text, word string) bool {
	if word == "" {
		return false
	}
	for from := 0; from < len(text); {
		at := strings.Index(text[from:], word)
		if at < 0 {
			return false
		}
		at += from
		end := at + len(word)
		beforeOK := at == 0 || !isASCIIWordByte(text[at-1])
		afterOK := end >= len(text) || !isASCIIWordByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		from = at + 1
	}
	return false
}

func isASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// userTurnNeedsStoredFacts reports a user turn whose missing host, account,
// or service facts should be read from memory and the knowledge base before
// the model asks the user. An empty turn stays on that path. Only the
// instruction head is checked, so a pasted article that mentions a host does
// not force a lookup.
func userTurnNeedsStoredFacts(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return true
	}
	if runes := []rune(text); len(runes) > 80 {
		text = string(runes[:80])
	}
	lower := strings.ToLower(text)
	return containsAny(lower, storedFactUserCuesCJK) || containsAnyASCIIWord(lower, storedFactUserCuesASCII)
}

// answerEchoesStoredFactNudge reports a follow-up that discusses the injected
// lookup instruction instead of continuing the user's task.
func answerEchoesStoredFactNudge(content string) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	if strings.Contains(text, "查询优先级") {
		return true
	}
	return strings.Contains(text, "先查记忆") && strings.Contains(text, "knowledge_search")
}

// preferPreNudgeAnswer keeps the deliverable written before the lookup nudge
// when the follow-up only acknowledges that instruction. A follow-up that
// still asks for a missing fact, states a connection value, or carries the
// deliverable forward and adds to it, stands. A finished answer that mentions
// the lookup phrase stays even when the acknowledgement is longer. An earlier
// reply that is only the acknowledgement still yields to a later answer.
func preferPreNudgeAnswer(preNudge, followUp string) string {
	pre := strings.TrimSpace(preNudge)
	follow := strings.TrimSpace(followUp)
	if pre == "" {
		return follow
	}
	if follow == "" || follow == pre {
		return pre
	}
	if answerSolicitsMissingFacts(follow) || !answerEchoesStoredFactNudge(follow) || followReportsConnectionFact(follow) {
		return follow
	}
	if followExtendsDeliverable(pre, follow) {
		return follow
	}
	if answerEchoesStoredFactNudge(pre) && !deliverableBesidesEcho(pre) {
		return follow
	}
	return pre
}

// storedFactDeliverableMinRunes is a finished sentence, not a fragment that
// happens to occur inside the lookup acknowledgement.
const storedFactDeliverableMinRunes = 20

// storedFactExtendLeadRunes is how much text may sit in front of a repeated
// deliverable. A long acknowledgement that quotes the sentence much later
// does not carry it forward.
const storedFactExtendLeadRunes = 40

// followExtendsDeliverable reports a follow-up that repeats the earlier
// answer and adds to it. A short fragment merely occurring inside the
// acknowledgement does not. A quote that begins only after a long lead-in
// does not either.
func followExtendsDeliverable(pre, follow string) bool {
	if utf8.RuneCountInString(follow) <= utf8.RuneCountInString(pre)+40 {
		return false
	}
	if strings.HasPrefix(follow, pre) {
		return true
	}
	if utf8.RuneCountInString(pre) < storedFactDeliverableMinRunes {
		return false
	}
	at := strings.Index(follow, pre)
	if at < 0 {
		return false
	}
	return utf8.RuneCountInString(strings.TrimSpace(follow[:at])) <= storedFactExtendLeadRunes
}

// storedFactValueNounsCJK are connection nouns that can introduce a stated
// value. 连接方式 stays a request noun only: it does not name a value.
var storedFactValueNounsCJK = []string{"主机", "端口", "密码", "账号", "帐户", "账户", "用户名", "服务器", "地址"}

var storedFactValueNounsASCII = []string{"host", "port", "password", "username", "account"}

var storedFactValueNounRunes = cjkNounRunes(storedFactValueNounsCJK)

func cjkNounRunes(nouns []string) [][]rune {
	out := make([][]rune, len(nouns))
	for i, noun := range nouns {
		out[i] = []rune(noun)
	}
	return out
}

// followReportsConnectionFact reports a follow-up that states a connection
// value (主机是 api2, 主机是本地, 地址 10.0.0.1, 端口 22, host is api2).
// 主机是否, 主机是不是, 服务器为了, 服务器为翻译, 主机是原词, 主机是需要保留,
// 主机是论文中的原词, and a copula or colon with nothing after it leave the
// earlier answer in place. The paper-translation receipt states no value.
func followReportsConnectionFact(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return statedCJKConnection(lower) || statedASCIIConnection(lower)
}

func statedCJKConnection(text string) bool {
	if text == "" || !containsAny(text, storedFactValueNounsCJK) {
		return false
	}
	for i, noun := range storedFactValueNounsCJK {
		nr := storedFactValueNounRunes[i]
		from := 0
		for from < len(text) {
			rel := strings.Index(text[from:], noun)
			if rel < 0 {
				break
			}
			if statedAfterNoun(nr, text[from+rel+len(noun):]) {
				return true
			}
			next := from + rel + len(noun)
			if next <= from {
				break
			}
			from = next
		}
	}
	return false
}

func statedAfterNoun(nr []rune, suffix string) bool {
	runes := []rune(suffix)
	j := 0
	if j < len(runes) && runes[j] == '号' {
		j++
	}
	j = skipSpaceRunes(runes, j)
	if j >= len(runes) {
		return false
	}
	switch runes[j] {
	case '是':
		return cjkCopulaStatesValue(runes, j)
	case '不':
		k := skipSpaceRunes(runes, j+1)
		return k < len(runes) && runes[k] == '是' && connectionValueAt(runes, k+1)
	case '为':
		k := skipSpaceRunes(runes, j+1)
		if k < len(runes) && runes[k] == '了' {
			return false
		}
		return connectionValueAt(runes, j+1)
	case '：', ':':
		return connectionValueAt(runes, j+1)
	default:
		return isConnectionDigit(runes[j]) && (isPortNoun(nr) || (isHostNoun(nr) && ipv4At(runes, j)))
	}
}

// cjkCopulaStatesValue accepts 主机是 api2 and rejects 是否, 是不是, 是什么,
// and a copula that ends the sentence.
func cjkCopulaStatesValue(runes []rune, copula int) bool {
	k := skipSpaceRunes(runes, copula+1)
	if k >= len(runes) {
		return false
	}
	switch runes[k] {
	case '否', '什', '哪', '谁', '多', '不':
		return false
	default:
		return connectionValueAt(runes, k)
	}
}

// connectionTaskLabels are paper-task words. 服务器为翻译 and 主机是原词 name
// the task, while 本地 and 张三 name a connection value.
var connectionTaskLabels = []string{
	"译", "保持", "核对", "核验", "保留", "原样", "原词", "术语", "完成",
	"使用", "连接", "摘要", "论文", "需要", "原文", "任务", "处理", "修改", "省略", "沿用",
}

// connectionValueAt accepts an identifier (api2, 10.0.0.1, root) or a short
// CJK label (本地, 张三, 空). A clause such as 论文中的原词, or a task word
// such as 翻译, 原词, or 需要保留, does not state a connection value.
func connectionValueAt(runes []rune, j int) bool {
	j = skipSpaceRunes(runes, j)
	if j < len(runes) && (runes[j] == ':' || runes[j] == '：') {
		j = skipSpaceRunes(runes, j+1)
	}
	if j >= len(runes) || isConnectionPunct(runes[j]) {
		return false
	}
	start := j
	ident := false
	cjk := 0
	glue := false
	for ; j < len(runes); j++ {
		r := runes[j]
		if unicode.IsSpace(r) || isConnectionPunct(r) {
			break
		}
		switch {
		case isASCIIIdentRune(r) || isConnectionDigit(r):
			ident = true
		case r == '.' || r == '．' || r == '-' || r == '_':
		case isClauseGlue(r):
			glue = true
			cjk++
		default:
			cjk++
		}
	}
	if glue {
		return false
	}
	if ident {
		return true
	}
	if cjk == 0 || cjk > 4 {
		return false
	}
	return !isTaskLabel(string(runes[start:j]))
}

func isTaskLabel(token string) bool {
	for _, stem := range connectionTaskLabels {
		if strings.Contains(token, stem) {
			return true
		}
	}
	return false
}

func skipSpaceRunes(runes []rune, j int) int {
	for j < len(runes) && unicode.IsSpace(runes[j]) {
		j++
	}
	return j
}

func isConnectionDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= '０' && r <= '９')
}

func isASCIIIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isClauseGlue(r rune) bool {
	switch r {
	case '的', '了', '过', '而', '并', '且', '或', '与', '中', '里', '用', '着', '所', '把', '被', '在', '和', '及', '么', '吗', '呢':
		return true
	default:
		return false
	}
}

func isPortNoun(nr []rune) bool {
	return len(nr) == 2 && nr[0] == '端' && nr[1] == '口'
}

func isHostNoun(nr []rune) bool {
	if len(nr) == 2 && ((nr[0] == '主' && nr[1] == '机') || (nr[0] == '地' && nr[1] == '址')) {
		return true
	}
	return len(nr) == 3 && nr[0] == '服' && nr[1] == '务' && nr[2] == '器'
}

// ipv4At reports a dotted address such as 10.0.0.1. A lone figure number
// such as 地址 1 does not match.
func ipv4At(runes []rune, j int) bool {
	for octet := 0; octet < 4; octet++ {
		n := 0
		for n < 3 && j < len(runes) && isConnectionDigit(runes[j]) {
			j++
			n++
		}
		if n == 0 {
			return false
		}
		if octet == 3 {
			return true
		}
		if j >= len(runes) || (runes[j] != '.' && runes[j] != '．') {
			return false
		}
		j++
	}
	return false
}

func isConnectionPunct(r rune) bool {
	switch r {
	case '。', '，', '、', '；', '：', '！', '？', '…', '·', '.', ',', ';', ':', '!', '?', ')', ']', '}', '"', '\'', '）', '】', '』', '”', '’':
		return true
	default:
		return false
	}
}

func statedASCIIConnection(text string) bool {
	for _, word := range storedFactValueNounsASCII {
		if asciiConnectionValue(text, word) {
			return true
		}
	}
	return false
}

func asciiConnectionValue(text, word string) bool {
	for from := 0; from < len(text); {
		at := strings.Index(text[from:], word)
		if at < 0 {
			return false
		}
		at += from
		end := at + len(word)
		if at > 0 && isASCIIWordByte(text[at-1]) {
			from = at + 1
			continue
		}
		if end < len(text) && isASCIIWordByte(text[end]) {
			if word == "port" && isASCIIDigitByte(text[end]) {
				return true
			}
			if word == "host" && isASCIIDigitByte(text[end]) && ipv4At([]rune(text[end:]), 0) {
				return true
			}
			from = at + 1
			continue
		}
		rest := strings.TrimLeftFunc(text[end:], unicode.IsSpace)
		if word == "port" && rest != "" && isASCIIDigitByte(rest[0]) {
			return true
		}
		if word == "host" && ipv4At([]rune(rest), 0) {
			return true
		}
		if asciiCopulaValue(text[end:]) {
			return true
		}
		from = at + 1
	}
	return false
}

func asciiCopulaValue(rest string) bool {
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	if strings.HasPrefix(rest, "is") && (len(rest) == 2 || !isASCIIWordByte(rest[2])) {
		return asciiValueAfter(rest[2:])
	}
	if strings.HasPrefix(rest, ":") {
		return asciiValueAfter(rest[1:])
	}
	if strings.HasPrefix(rest, "：") {
		return asciiValueAfter(rest[len("："):])
	}
	return false
}

func asciiValueAfter(rest string) bool {
	return connectionValueAt([]rune(rest), 0)
}

func isASCIIDigitByte(b byte) bool {
	return b >= '0' && b <= '9'
}

// deliverableBesidesEcho reports text that still says something after the
// lookup-instruction phrase is removed. A bare "已收到查询优先级指令" does not.
func deliverableBesidesEcho(text string) bool {
	stripped := strings.ReplaceAll(text, "查询优先级", "")
	stripped = strings.ReplaceAll(stripped, "先查记忆", "")
	stripped = strings.ReplaceAll(stripped, "knowledge_search", "")
	return utf8.RuneCountInString(strings.TrimSpace(stripped)) >= storedFactDeliverableMinRunes
}

// dropStoredFactNudgeSuffix removes the injected lookup instruction and the
// receipt after it. The assistant reply that triggered the nudge stays. A
// later tool call is kept, including its result.
func dropStoredFactNudgeSuffix(history []ConversationEntry) []ConversationEntry {
	at := storedFactNudgeIndex(history)
	if at < 0 || historyHasToolAfter(history, at) {
		return history
	}
	return history[:at]
}

func storedFactNudgeIndex(history []ConversationEntry) int {
	nudge := strings.TrimSpace(StoredOperationalFactNudge())
	for i := len(history) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(history[i].Role), "user") && strings.TrimSpace(entryContentString(history[i])) == nudge {
			return i
		}
	}
	return -1
}

func historyHasToolAfterStoredFactNudge(history []ConversationEntry) bool {
	at := storedFactNudgeIndex(history)
	return at >= 0 && historyHasToolAfter(history, at)
}

func historyHasToolAfter(history []ConversationEntry, at int) bool {
	for _, entry := range history[at+1:] {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "tool") || strings.TrimSpace(entry.ToolName) != "" {
			return true
		}
	}
	return false
}

// pureSocialGreetings are whole utterances, not substrings. A greeting plus
// a task ("你好，帮我连上那台机器") must still retrieve stored facts.
var pureSocialGreetings = map[string]struct{}{
	"你好": {}, "您好": {}, "嗨": {}, "哈喽": {}, "哈啰": {},
	"hello": {}, "hi": {}, "hey": {}, "thanks": {}, "thank you": {},
	"谢谢": {}, "谢谢你": {}, "感谢": {}, "在吗": {}, "在么": {}, "在不在": {},
	"ok": {}, "okay": {}, "收到": {}, "好的": {}, "好呀": {},
	"早安": {}, "晚安": {}, "早上好": {}, "晚上好": {}, "下午好": {},
	"good morning": {}, "good night": {}, "good evening": {},
	"嗯": {}, "嗯嗯": {}, "哈哈": {},
}

// conversationalAcks are social utterances that can mean "go ahead" on an
// open task. They skip the intent tree and the stored-fact nudge, and they
// keep the parent tools. A greeting does not.
var conversationalAcks = map[string]struct{}{
	"ok": {}, "okay": {}, "收到": {}, "好的": {}, "好呀": {}, "嗯": {}, "嗯嗯": {},
}

// IsPureSocialGreeting reports a short social utterance with no task in it.
func IsPureSocialGreeting(text string) bool {
	_, ok := socialUtteranceBase(text)
	return ok
}

// IsAnswerOnlySocialTurn reports a greeting that should be answered in
// place. Acknowledgements such as 好的 stay on the open task.
func IsAnswerOnlySocialTurn(text string) bool {
	base, ok := socialUtteranceBase(text)
	if !ok {
		return false
	}
	_, ack := conversationalAcks[base]
	return !ack
}

func socialUtteranceBase(text string) (string, bool) {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, " \t\r\n！!。.?？~～,，、…")
	if text == "" || utf8.RuneCountInString(text) > 16 {
		return "", false
	}
	lower := strings.ToLower(text)
	if _, ok := pureSocialGreetings[lower]; ok {
		return lower, true
	}
	for _, particle := range []string{"呀", "啊", "哇", "哦", "哟", "哈", "呢"} {
		if strings.HasSuffix(lower, particle) {
			base := strings.TrimSuffix(lower, particle)
			if _, ok := pureSocialGreetings[base]; ok {
				return base, true
			}
		}
	}
	return "", false
}

func surfaceHasExecutionTool(tools []map[string]interface{}) bool {
	now := time.Now().UTC()
	for _, def := range tools {
		name := toolDefName(def)
		if name == "" || IsLightTurnToolAllowed(name) {
			continue
		}
		provision, ok := tool.LegacyAdapterProvisionForTool(name, now)
		if !ok {
			continue
		}
		for _, effect := range provision.Effects {
			if effect != tool.EffectReadOnly {
				return true
			}
		}
	}
	return false
}

func turnUsedAnyTool(history []ConversationEntry) bool {
	for _, entry := range history {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "tool") || strings.TrimSpace(entry.ToolName) != "" {
			return true
		}
	}
	return false
}

// StoredOperationalFactNudge tells the model to read stored facts before
// asking the user, then continue with the tools already listed.
func StoredOperationalFactNudge() string {
	return "[系统] 先查记忆（memory），再查知识库（knowledge_search）。两边都没有，才向用户索要还缺的事实。只有一方有结果，或两边一致，直接用查到的内容继续原任务，不要再问用户。两边冲突时，只问用户确认哪一份。"
}

// retrievalToolsStillPending is true when this request still lists memory or
// knowledge_search and that tool has not run yet. With neither tool on the
// surface there is nothing to force.
func retrievalToolsStillPending(tools []map[string]interface{}, history []ConversationEntry) bool {
	wanted := map[string]bool{}
	for _, def := range tools {
		switch toolDefName(def) {
		case "memory", "memory_recall", "knowledge_search":
			wanted[toolDefName(def)] = true
		}
	}
	if len(wanted) == 0 {
		return false
	}
	for _, entry := range history {
		delete(wanted, strings.TrimSpace(entry.ToolName))
	}
	return len(wanted) > 0
}
