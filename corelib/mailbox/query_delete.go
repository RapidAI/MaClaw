package mailbox

import "strings"

// QueryWantsMessageDelete reports a request to delete mailbox messages now.
// Removing a mailbox account, a question, and a named file stay on their
// own labels. A delete that is only the body of a schedule being created or
// changed is not this request. Call it after a local file-delete check so
// "删除邮件.md" stays a file delete.
func QueryWantsMessageDelete(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" || queryAsksAboutMailDelete(q) || queryNamesMailFile(q) || queryNegatesMailDelete(q) {
		return false
	}
	if QuerySchedulesMessageDelete(userMessage) {
		return false
	}
	if mailboxAccountRemoval(q) && !mentionsMailMessage(q) {
		return false
	}
	if mentionsMailMessage(q) && hasMailDeleteVerb(q) {
		return true
	}
	return hasMailEmptyVerb(q) && mentionsMailbox(q)
}

// QuerySchedulesMessageDelete reports that the mail-delete verbs are the
// body of a schedule being created or changed, not a deletion to perform now.
// A question, a named file, and a governed refusal stay false, same as
// QueryWantsMessageDelete. Bare 定时, 定时器, and 每小时 are not a schedule act.
func QuerySchedulesMessageDelete(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" || queryAsksAboutMailDelete(q) || queryNamesMailFile(q) || queryNegatesMailDelete(q) {
		return false
	}
	if !mailDeleteIsScheduledPayload(q) {
		return false
	}
	if mentionsMailMessage(q) && hasMailDeleteVerb(q) {
		return true
	}
	return hasMailEmptyVerb(q) && mentionsMailbox(q)
}

// mailDeleteIsScheduledPayload is clause government. A schedule-act clause
// holds a schedule noun and an administration verb before that noun. Delete
// verbs in earlier clauses, and delete verbs in the act clause that start
// before the administration verb, are still immediate. Delete verbs at or
// after that verb, and delete verbs in later clauses, are the payload. Any
// immediate delete verb means this is not only a scheduled payload.
func mailDeleteIsScheduledPayload(q string) bool {
	clauses := mailClauses(q)
	act := -1
	adminAt := 0
	for i, clause := range clauses {
		at, ok := scheduleActAdminIndex(clause)
		if !ok {
			continue
		}
		act = i
		adminAt = at
		break
	}
	if act < 0 {
		return false
	}
	sawPayload := false
	for i, clause := range clauses {
		for _, span := range mailDeleteVerbSpans(stripMailNonDeleteCompounds(clause)) {
			if i < act || (i == act && span.start < adminAt) {
				return false
			}
			sawPayload = true
		}
	}
	return sawPayload
}

// scheduleNounMarkers are whole schedule records. 定时 and 定时器 are not
// records. Longer nouns are listed first so a shorter noun cannot claim the
// same span.
var scheduleNounMarkers = []string{
	"定时任务", "定時任務", "定时提醒", "定時提醒", "计划任务", "計劃任務", "日程",
}

var scheduleNounPhrases = []string{"scheduled tasks", "scheduled task"}

// scheduleAdminVerbs name creating or changing the record. They are not the
// payload verb. English verbs are bounded so "schedule" does not match inside
// "scheduled".
var scheduleAdminVerbs = []string{"创建", "新建", "新增", "添加", "设置", "設定", "安排", "建立"}

var scheduleAdminEnglish = []string{"set up", "create", "schedule", "add"}

func scheduleActAdminIndex(clause string) (int, bool) {
	for _, nounAt := range scheduleNounIndexes(clause) {
		if adminAt := lastScheduleAdminVerbBefore(clause[:nounAt]); adminAt >= 0 {
			return adminAt, true
		}
	}
	return 0, false
}

func scheduleNounIndexes(clause string) []int {
	seen := map[int]bool{}
	var hits []int
	add := func(at int) {
		if at < 0 || seen[at] {
			return
		}
		seen[at] = true
		hits = append(hits, at)
	}
	for _, noun := range scheduleNounMarkers {
		start := 0
		for start < len(clause) {
			idx := strings.Index(clause[start:], noun)
			if idx < 0 {
				break
			}
			add(start + idx)
			start = start + idx + len(noun)
		}
	}
	for _, phrase := range scheduleNounPhrases {
		start := 0
		for start < len(clause) {
			idx := strings.Index(clause[start:], phrase)
			if idx < 0 {
				break
			}
			at := start + idx
			if asciiBoundedAt(clause, at, len(phrase)) {
				add(at)
			}
			start = at + 1
		}
	}
	start := 0
	for start < len(clause) {
		idx := strings.Index(clause[start:], "cron")
		if idx < 0 {
			break
		}
		at := start + idx
		if asciiBoundedAt(clause, at, len("cron")) {
			add(at)
		}
		start = at + 1
	}
	// Earliest noun is the one an administration verb has to precede.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j] < hits[j-1]; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	return hits
}

func lastScheduleAdminVerbBefore(prefix string) int {
	best := -1
	for _, verb := range scheduleAdminVerbs {
		if at := strings.LastIndex(prefix, verb); at > best {
			best = at
		}
	}
	for _, phrase := range scheduleAdminEnglish {
		if at := lastBoundedPhrase(prefix, phrase); at > best {
			best = at
		}
	}
	return best
}

func lastBoundedPhrase(q, phrase string) int {
	best := -1
	start := 0
	for start < len(q) {
		idx := strings.Index(q[start:], phrase)
		if idx < 0 {
			return best
		}
		at := start + idx
		if asciiBoundedAt(q, at, len(phrase)) {
			best = at
		}
		start = at + 1
	}
	return best
}

// queryNegatesMailDelete is true when every delete verb is governed by a
// refusal, or is only a question. Questions are a perfective tail such as
// 删了没, an A-not-A pair such as 删不删, or a frame such as 怎么/如何 that
// actually governs the verb. Each verb is judged on its own, including a
// later verb in the same clause, so 「把没有删的邮件删了」 still asks for a
// deletion. The cue closest to that verb wins.
func queryNegatesMailDelete(q string) bool {
	sawVerb := false
	for _, clause := range mailClauses(q) {
		stripped := stripMailNonDeleteCompounds(clause)
		spans := mailDeleteVerbSpans(stripped)
		for i, span := range spans {
			sawVerb = true
			if mailANotAParticipant(stripped, spans, i) {
				continue
			}
			if !mailSpanRefused(stripped, span) {
				return false
			}
		}
	}
	return sawVerb
}

// mailANotAParticipant is one side of 删不删 / 删除不删除. The 不 sits
// between the two verbs, so a prefix check never sees the second verb as
// refused. 删除不了解 and 删不了 stay requests: the text after 不 is not
// another delete verb.
func mailANotAParticipant(clause string, spans []mailVerbSpan, i int) bool {
	if i > 0 && mailVerbANotA(clause, spans[i-1], spans[i]) {
		return true
	}
	return i+1 < len(spans) && mailVerbANotA(clause, spans[i], spans[i+1])
}

func mailVerbANotA(clause string, left, right mailVerbSpan) bool {
	if left.end > right.start {
		return false
	}
	return strings.Trim(clause[left.end:right.start], " \t") == "不"
}

func mailSpanRefused(clause string, span mailVerbSpan) bool {
	if mailNegationGoverns(clause[:span.start]) {
		return true
	}
	return mailVerbQuestionTail(clause[span.end:])
}

// mailVerbQuestionTail is an optional 了, then 没/没有/么/嘛/吗, then only
// 呢啊呀吧哦. The tail has to finish the clause. 「删了没有用的邮件」 stays a
// request because 用 follows 没有, and a bare 「删了」 stays a request too.
func mailVerbQuestionTail(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	if strings.HasPrefix(rest, "了") {
		rest = strings.TrimLeft(rest[len("了"):], " \t")
	}
	matched := false
	for _, word := range []string{"没有", "沒有", "没", "沒", "么", "嘛", "吗", "嗎"} {
		if strings.HasPrefix(rest, word) {
			rest = strings.TrimLeft(rest[len(word):], " \t")
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for rest != "" {
		cut := false
		for _, particle := range []string{"呢", "啊", "呀", "吧", "哦"} {
			if strings.HasPrefix(rest, particle) {
				rest = strings.TrimLeft(rest[len(particle):], " \t")
				cut = true
				break
			}
		}
		if !cut {
			return false
		}
	}
	return true
}

func mailClauses(q string) []string {
	parts := strings.FieldsFunc(q, func(r rune) bool {
		switch r {
		case '，', '。', '；', '、', '！', '？', ',', ';', '!', '?', '\n':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return []string{q}
	}
	return parts
}

func mailNegationGoverns(prefix string) bool {
	bestPos := -1
	bestNeg := false
	consider := func(pos int, neg bool) {
		if pos > bestPos {
			bestPos = pos
			bestNeg = neg
		}
	}
	for _, cue := range []struct {
		text string
		neg  bool
	}{
		{"但是", false}, {"不過", false}, {"不过", false}, {"然而", false},
		{"不要", true}, {"不用", true}, {"无需", true}, {"無需", true}, {"勿", true},
	} {
		consider(strings.LastIndex(prefix, cue.text), cue.neg)
	}
	for _, word := range []string{"don't", "don’t", "dont", "do not", "cannot", "can't", "can’t", "cant", "unable", "not", "but"} {
		consider(lastASCIIToken(prefix, word), word != "but")
	}
	consider(lastGoverningBie(prefix, "别"), true)
	consider(lastGoverningBie(prefix, "別"), true)
	for _, phrase := range mailVerbRefusalPhrases {
		consider(lastGoverningRefusal(prefix, phrase), true)
	}
	for _, phrase := range mailQuestionFrames {
		consider(lastQuestionFrame(prefix, phrase), true)
	}
	consider(lastEnglishHow(prefix), true)
	return bestNeg
}

// mailQuestionFrames ask about the verb only when they govern it. 把怎么写的邮件删了
// stays a request because 写 sits between the frame and the verb.
var mailQuestionFrames = []string{
	"怎么样", "怎麼樣", "怎样", "怎樣", "如何", "怎么", "怎麼",
	"有没有", "有沒有",
}

func lastQuestionFrame(prefix, phrase string) int {
	if phrase == "" {
		return -1
	}
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], phrase)
		if idx < 0 {
			return best
		}
		pos := start + idx
		if questionFrameGoverns(prefix[pos+len(phrase):]) {
			best = pos
		}
		start = pos + len(phrase)
	}
	return best
}

// questionFrameGoverns accepts an empty gap, a chain of auxiliaries such as
// 才能/想, or 把/将/给 after those auxiliaries. A noun in the gap keeps the
// verb a request.
func questionFrameGoverns(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	for {
		stripped := false
		for _, modal := range []string{"才能够", "才能", "可以", "能够", "可能", "要", "想", "去", "才", "能", "会", "會"} {
			if strings.HasPrefix(rest, modal) {
				rest = strings.TrimLeft(rest[len(modal):], " \t")
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
		if rest == "" {
			return true
		}
	}
	if rest == "" {
		return true
	}
	for _, particle := range []string{"把", "将", "將", "给", "給"} {
		if strings.HasPrefix(rest, particle) {
			return true
		}
	}
	return false
}

func lastEnglishHow(prefix string) int {
	const word = "how"
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], word)
		if idx < 0 {
			return best
		}
		at := start + idx
		beforeOK := at == 0 || !isASCIIAlphaNum(prefix[at-1])
		after := at + len(word)
		afterOK := after >= len(prefix) || !isASCIIAlphaNum(prefix[after])
		if beforeOK && afterOK && englishHowGoverns(prefix[after:]) {
			best = at
		}
		start = at + 1
	}
	return best
}

func englishHowGoverns(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	for rest != "" {
		word, next, ok := firstASCIIWord(rest)
		if !ok {
			return false
		}
		switch word {
		case "to", "do", "does", "did", "can", "could", "should", "would", "about", "i", "you", "we", "one", "me":
			rest = strings.TrimLeft(next, " \t")
		default:
			return false
		}
	}
	return true
}

func firstASCIIWord(s string) (word, rest string, ok bool) {
	i := 0
	for i < len(s) && isASCIIAlphaNum(s[i]) {
		i++
	}
	if i == 0 {
		return "", s, false
	}
	return s[:i], s[i:], true
}

// mailVerbRefusalPhrases refuse the verb only when they govern it. A noun
// phrase such as 没有主题 or 不能识别 stays a delete request.
var mailVerbRefusalPhrases = []string{
	"没有办法", "沒有辦法", "没办法", "沒辦法",
	"不可能", "不可以", "不是", "不能", "无法", "無法", "没法", "沒法", "不可",
	"并未", "並未", "并没", "並沒", "并不", "並不",
	"没有", "沒有", "没", "沒",
}

// lastGoverningRefusal finds a phrase that refuses the verb. It refuses when
// the phrase sits immediately before the verb, or before 要/想/去 and 把/将/给.
func lastGoverningRefusal(prefix, phrase string) int {
	if phrase == "" {
		return -1
	}
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], phrase)
		if idx < 0 {
			return best
		}
		pos := start + idx
		if buShiGoverns(prefix[pos+len(phrase):]) {
			best = pos
		}
		start = pos + len(phrase)
	}
	return best
}

func buShiGoverns(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	for {
		stripped := false
		for _, modal := range []string{"要", "想", "去"} {
			if strings.HasPrefix(rest, modal) {
				rest = strings.TrimLeft(rest[len(modal):], " \t")
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
		if rest == "" {
			return true
		}
	}
	if rest == "" {
		return true
	}
	for _, particle := range []string{"把", "将", "將", "给", "給"} {
		if strings.HasPrefix(rest, particle) {
			return true
		}
	}
	return false
}

// lastGoverningBie finds a 别 that refuses the following verb. 特别, 区别,
// 别的 and 别人 are not refusals. 别把 / 别将 / 别给 are, and so is 别 sitting
// immediately before the verb.
func lastGoverningBie(prefix, phrase string) int {
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], phrase)
		if idx < 0 {
			return best
		}
		pos := start + idx
		if bieGoverns(prefix, pos, phrase) {
			best = pos
		}
		start = pos + len(phrase)
	}
	return best
}

func bieGoverns(prefix string, pos int, phrase string) bool {
	if pos >= len("特") && prefix[pos-len("特"):pos] == "特" {
		return false
	}
	if pos >= len("区") && prefix[pos-len("区"):pos] == "区" {
		return false
	}
	if pos >= len("區") && prefix[pos-len("區"):pos] == "區" {
		return false
	}
	rest := strings.TrimLeft(prefix[pos+len(phrase):], " \t")
	if rest == "" {
		return true
	}
	for _, particle := range []string{"把", "将", "將", "给", "給"} {
		if strings.HasPrefix(rest, particle) {
			return true
		}
	}
	return false
}

func lastASCIIToken(q, word string) int {
	best := -1
	for start := 0; start < len(q); {
		idx := strings.Index(q[start:], word)
		if idx < 0 {
			return best
		}
		at := start + idx
		beforeOK := at == 0 || !isASCIIAlphaNum(q[at-1])
		after := at + len(word)
		afterOK := after >= len(q) || !isASCIIAlphaNum(q[after])
		if beforeOK && afterOK {
			best = at
		}
		start = at + 1
	}
	return best
}

func queryAsksAboutMailDelete(q string) bool {
	for _, marker := range []string{
		"吗", "嗎", "是否", "要不要", "有没有删", "有沒有刪", "删过", "刪過", "被删", "被刪",
	} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	for _, phrase := range []string{"was deleted", "been deleted", "did you delete", "have you deleted", "?"} {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

func queryNamesMailFile(q string) bool {
	for _, ext := range []string{".md", ".txt", ".go", ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".csv", ".json", ".png", ".jpg", ".jpeg"} {
		if strings.Contains(q, ext) {
			return true
		}
	}
	return false
}

func mailboxAccountRemoval(q string) bool {
	for _, marker := range []string{"账号", "帐号", "帐户", "账户", "配置", "account"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

func mentionsMailMessage(q string) bool {
	for _, marker := range []string{"邮件", "郵件", "收件"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	if strings.Contains(q, "e-mail") {
		return true
	}
	for _, word := range []string{"inbox", "email", "mail", "message"} {
		if asciiToken(q, word) || asciiToken(q, word+"s") {
			return true
		}
	}
	return false
}

func mentionsMailbox(q string) bool {
	for _, marker := range []string{"邮箱", "郵箱", "信箱"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return asciiToken(q, "mailbox")
}

func hasMailDeleteVerb(q string) bool {
	return len(mailDeleteVerbSpans(stripMailNonDeleteCompounds(q))) > 0
}

func stripMailNonDeleteCompounds(q string) string {
	for _, marker := range []string{"删除线", "刪除線"} {
		q = strings.ReplaceAll(q, marker, " ")
	}
	return q
}

type mailVerbSpan struct {
	start int
	end   int
}

// mailDeleteVerbMarkers are longest-first so 删除 is one span, not 删 plus 除.
var mailDeleteVerbMarkers = []string{
	"彻底删除", "徹底刪除",
	"删除", "刪除", "删掉", "刪掉", "删了", "刪了",
	"清空", "清除",
}

func mailDeleteVerbSpans(q string) []mailVerbSpan {
	var spans []mailVerbSpan
	for i := 0; i < len(q); {
		span, ok := longestMailVerbAt(q, i)
		if !ok {
			i++
			continue
		}
		spans = append(spans, span)
		i = span.end
	}
	return spans
}

func longestMailVerbAt(q string, i int) (mailVerbSpan, bool) {
	for _, marker := range mailDeleteVerbMarkers {
		if strings.HasPrefix(q[i:], marker) {
			return mailVerbSpan{start: i, end: i + len(marker)}, true
		}
	}
	// A lone 删 is a verb. 删改, 删节 and 删减 are edits.
	for _, marker := range []string{"删", "刪"} {
		if strings.HasPrefix(q[i:], marker) && !chanIsCompound(q[i+len(marker):]) {
			return mailVerbSpan{start: i, end: i + len(marker)}, true
		}
	}
	for _, word := range []string{"delete", "remove", "trash", "empty"} {
		if strings.HasPrefix(q[i:], word) && asciiBoundedAt(q, i, len(word)) {
			return mailVerbSpan{start: i, end: i + len(word)}, true
		}
	}
	return mailVerbSpan{}, false
}

func asciiBoundedAt(q string, at, n int) bool {
	beforeOK := at == 0 || !isASCIIAlphaNum(q[at-1])
	after := at + n
	afterOK := after >= len(q) || !isASCIIAlphaNum(q[after])
	return beforeOK && afterOK
}

func chanIsCompound(rest string) bool {
	for _, next := range []string{"改", "节", "節", "减", "減"} {
		if strings.HasPrefix(rest, next) {
			return true
		}
	}
	return false
}

func hasMailEmptyVerb(q string) bool {
	for _, marker := range []string{"清空", "清除"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return asciiToken(q, "empty")
}

func asciiToken(q, word string) bool {
	return asciiTokenIndex(q, word) >= 0
}

func asciiTokenIndex(q, word string) int {
	if word == "" {
		return -1
	}
	start := 0
	for {
		idx := strings.Index(q[start:], word)
		if idx < 0 {
			return -1
		}
		at := start + idx
		beforeOK := at == 0 || !isASCIIAlphaNum(q[at-1])
		after := at + len(word)
		afterOK := after >= len(q) || !isASCIIAlphaNum(q[after])
		if beforeOK && afterOK {
			return at
		}
		start = at + 1
		if start >= len(q) {
			return -1
		}
	}
}

func isASCIIAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
