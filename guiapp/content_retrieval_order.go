package guiapp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// Content retrieval on a model-discovery turn is a fixed ladder: memory,
// then the knowledge base, then the network. A contradiction between the
// first two uses the knowledge base. The host lists those two reads and
// refuses the network, local scripts, and remote commands until both have run.

const contentRetrievalOrderMark = "【内容检索】"

const contentRetrievalOrderPrompt = "\n【内容检索】仅当请求需要已有事实时，先调用 memory_recall，再调用 knowledge_search。两边都没有所需事实时才用网络。记忆与知识库不一致时，以知识库为准，不要问用户选哪一份。不要用 craft_tool、本机文件或 ssh 代替这两次检索。如果目标是保存新内容，不要先做这两次检索：调用 tools_search，在 needs 中只写要使用的知识库写入工具名，加载后直接保存。用户给出的 URL 直接抓取该地址。\n"

const contentRetrievalNeedMemory = "内容检索先调用 memory_recall，query 用用户的原话。顺序是先记忆，再知识库，后网络。记忆与知识库不一致时，以知识库为准。不要用 craft_tool、本机文件、ssh 或网络代替。"

const contentRetrievalNeedKnowledge = "记忆已经查过。先调用 knowledge_search，query 用用户的原话。与记忆不一致时以知识库为准，不要问用户选哪一份。两边都没有所需事实时再用网络。"

const contentRetrievalMemoryFooter = "\n下一步调用 knowledge_search，query 用用户的原话。与记忆不一致时以知识库为准，不要问用户选哪一份。这次知识库检索完成前，不要用网络、craft_tool、本机文件或 ssh。"

const contentRetrievalKnowledgeFooter = "\n若与记忆不一致，以本条知识库结果为准，不要问用户选哪一份。两边都没有所需事实时，再用网络。"

func contentRetrievalUtterance(text string) bool {
	text = strings.TrimSpace(text)
	// A URL the user already named is fetched directly. It is not a stored-fact lookup.
	if text == "" || discoveryPictureCreationUtterance(text) || contentRetrievalSuppliedURL(text) {
		return false
	}
	return contentRetrievalLookupVerb(text) || contentRetrievalStoredCue(text)
}

// Phrases that arm the ladder. A query made only of these is the gate talking
// to itself, not the subject the user asked about.
var contentRetrievalLookupPhrases = []string{
	"查一下", "是什么", "什么是",
	"查询", "查查", "查找", "检索", "状态", "天气",
	"look up", "status",
}

var contentRetrievalStoredPhrases = []string{
	"服务器", "主机", "ssh",
}

func contentRetrievalLookupVerb(text string) bool {
	for _, cue := range contentRetrievalLookupPhrases {
		if contentRetrievalContainsFold(text, cue) {
			return true
		}
	}
	return false
}

func contentRetrievalStoredCue(text string) bool {
	for _, cue := range contentRetrievalStoredPhrases {
		if contentRetrievalContainsFold(text, cue) {
			return true
		}
	}
	return false
}

func contentRetrievalSuppliedURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "https://") || strings.Contains(lower, "http://")
}

func contentRetrievalActive(surface *semanticCallSurface, userText string) bool {
	return surface != nil && surface.replan != nil && intent.ClassificationIsModelDiscovery(surface.replan.Classification) && contentRetrievalUtterance(userText)
}

func contentRetrievalKnowledgeWriteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "knowledge_save_text", "knowledge_save_url", "knowledge_save_urls", "knowledge_import_directory", "knowledge_import_files", "knowledge_import_snapshot", semanticTrustedKnowledgeIngestAdapter:
		return true
	default:
		return false
	}
}

func contentRetrievalExempt(name string) bool {
	switch strings.TrimSpace(name) {
	case chatTaskToolSteer, chatTaskToolStop, chatTaskToolRequestInput, "current_datetime":
		return true
	default:
		return contentRetrievalKnowledgeWriteTool(name)
	}
}

func contentRetrievalKind(name string) string {
	switch strings.TrimSpace(name) {
	case "memory_recall", "memory":
		return "memory"
	case "knowledge_search":
		return "knowledge"
	default:
		return ""
	}
}

// contentRetrievalOwned is the desktop discovery ladder. A group policy has
// its own allow list; running the reads here would skip that check, and
// waiting on a forbidden read would close every other tool.
func (c *sharedAgentLoopCallbacks) contentRetrievalOwned() bool {
	if c == nil || (c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil) {
		return false
	}
	return contentRetrievalActive(c.semanticSurface, c.userText)
}

// contentRetrievalGate reports whether this discovery content turn still owns
// the call. handled is false once memory and knowledge have both run, so the
// ordinary grant rules decide ssh and the network. reason is empty when the
// call may run.
func (c *sharedAgentLoopCallbacks) contentRetrievalGate(name string) (handled bool, allowed bool, reason string) {
	// tools_search is decided from its arguments. IsToolAllowed runs first and
	// only sees the name, so blocking it here removes the tool and the
	// knowledge-write load never reaches IsToolCallAllowed.
	if strings.TrimSpace(name) == semanticToolsSearchName {
		return false, false, ""
	}
	return c.contentRetrievalGateStep(name)
}

func (c *sharedAgentLoopCallbacks) contentRetrievalGateStep(name string) (handled bool, allowed bool, reason string) {
	if !c.contentRetrievalOwned() || contentRetrievalExempt(name) {
		return false, false, ""
	}
	memory, knowledge := c.contentRetrievalProgress()
	switch contentRetrievalKind(name) {
	case "memory":
		return true, true, ""
	case "knowledge":
		if !memory {
			return true, false, contentRetrievalNeedMemory
		}
		return true, true, ""
	default:
		if !memory {
			return true, false, contentRetrievalNeedMemory
		}
		if !knowledge {
			return true, false, contentRetrievalNeedKnowledge
		}
		return false, false, ""
	}
}

// contentRetrievalGateCall lets discovery load exactly one knowledge-write
// tool without interpreting the user's language. Other capability loads stay
// behind the reads, including a write name batched with a network tool.
func (c *sharedAgentLoopCallbacks) contentRetrievalGateCall(name, argsJSON string) (handled bool, allowed bool, reason string) {
	if strings.TrimSpace(name) == semanticToolsSearchName {
		if contentRetrievalKnowledgeWriteDiscovery(argsJSON) {
			return false, false, ""
		}
		return c.contentRetrievalGateStep(name)
	}
	return c.contentRetrievalGateStep(name)
}

func contentRetrievalKnowledgeWriteDiscovery(argsJSON string) bool {
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil || args == nil {
		return false
	}
	needs, err := parseExactSearchNeeds(args["needs"])
	if err != nil || len(needs) != 1 {
		return false
	}
	for need := range needs {
		name := strings.TrimSpace(string(need))
		if !contentRetrievalKnowledgeWriteTool(name) {
			return false
		}
		_, petitionable := semanticPetitionableCapabilities[name]
		return petitionable
	}
	return false
}

func (c *sharedAgentLoopCallbacks) contentRetrievalProgress() (memory bool, knowledge bool) {
	if c == nil {
		return false, false
	}
	memory = c.contentRetrievalMemoryDone
	knowledge = c.contentRetrievalKnowledgeDone
	for _, entry := range c.contentRetrievalTurnTools() {
		text := conversationEntryText(entry.Content)
		switch strings.TrimSpace(entry.ToolName) {
		case "memory", "memory_recall":
			if strings.Contains(text, contentRetrievalMemoryFooter) {
				memory = true
			}
		case "knowledge_search":
			if strings.Contains(text, contentRetrievalKnowledgeFooter) {
				knowledge = true
			}
		}
	}
	return memory, knowledge
}

// contentRetrievalTurnTools is this turn's tool rows. Checkpoint history also
// holds earlier turns, and a tool name alone is not a consultation: an empty
// query is rejected without the footer the completed call appends.
func (c *sharedAgentLoopCallbacks) contentRetrievalTurnTools() []agent.ConversationEntry {
	if c == nil || len(c.checkpointHistory) == 0 {
		return nil
	}
	start := 0
	for i := len(c.checkpointHistory) - 1; i >= 0; i-- {
		entry := c.checkpointHistory[i]
		if strings.EqualFold(strings.TrimSpace(entry.Role), "user") && !entry.HostContinuation {
			start = i + 1
			break
		}
	}
	out := make([]agent.ConversationEntry, 0)
	for _, entry := range c.checkpointHistory[start:] {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "tool") {
			out = append(out, entry)
		}
	}
	return out
}

func appendContentRetrievalTools(userText string, tools []map[string]interface{}) []map[string]interface{} {
	if !contentRetrievalUtterance(userText) {
		return tools
	}
	return appendUniqueToolDefs(tools, contentRetrievalToolDefs())
}

func contentRetrievalToolDefs() []map[string]interface{} {
	query := map[string]interface{}{
		"type":        "string",
		"description": "用户的原话，或从中抽出的检索词。",
	}
	schema := func() map[string]interface{} {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": query,
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		}
	}
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "memory_recall",
				"description": "先查记忆。只读。内容检索的第一步，完成后再查知识库。",
				"parameters":  schema(),
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "knowledge_search",
				"description": "再查知识库。只读。与记忆不一致时以这里的结果为准。两边都没有再用网络。",
				"parameters":  schema(),
			},
		},
	}
}

func ensureContentRetrievalOrderPrompt(prompt, userText string, discovery bool) string {
	if !discovery || !contentRetrievalUtterance(userText) || strings.Contains(prompt, contentRetrievalOrderMark) {
		return prompt
	}
	return prompt + contentRetrievalOrderPrompt
}

func (c *sharedAgentLoopCallbacks) executeContentRetrievalCall(name, argsJSON string) (string, bool) {
	if !c.contentRetrievalOwned() {
		return "", false
	}
	switch contentRetrievalKind(name) {
	case "memory":
		return c.runContentMemoryRecall(argsJSON), true
	case "knowledge":
		return c.runContentKnowledgeSearch(argsJSON), true
	default:
		return "", false
	}
}

func (c *sharedAgentLoopCallbacks) runContentMemoryRecall(argsJSON string) string {
	query, err := contentRetrievalQuery(argsJSON)
	if err != nil {
		return "[system rejected] memory_recall_query_required"
	}
	if c.handler == nil {
		return "[system rejected] trusted_memory_recall_unavailable"
	}
	principal := strings.TrimSpace(c.userID)
	if principal == "" {
		principal = desktopUserID
	}
	query = contentRetrievalConsultQuery(c.userText, query)
	var text string
	if c.handler.semanticTrustedMemoryRecall != nil {
		text, err = c.handler.semanticTrustedMemoryRecall(principal, query)
	} else {
		text, err = c.handler.recallTrustedMemory(principal, query)
	}
	c.contentRetrievalMemoryDone = true
	if err != nil {
		return "[system rejected] " + err.Error() + contentRetrievalMemoryFooter
	}
	if strings.TrimSpace(text) == "" {
		text = "No matching memory found."
	}
	return text + contentRetrievalMemoryFooter
}

func (c *sharedAgentLoopCallbacks) runContentKnowledgeSearch(argsJSON string) string {
	if memory, _ := c.contentRetrievalProgress(); !memory {
		return "[system rejected] " + contentRetrievalNeedMemory
	}
	query, err := contentRetrievalQuery(argsJSON)
	if err != nil {
		return "[system rejected] memory_recall 已经完成。knowledge_search 需要 query。"
	}
	if c.handler == nil {
		return "[system rejected] trusted_knowledge_read_unavailable"
	}
	principal := strings.TrimSpace(c.userID)
	if principal == "" {
		principal = desktopUserID
	}
	query = contentRetrievalConsultQuery(c.userText, query)
	text, err := c.handler.readTrustedKnowledge(principal, query)
	if err != nil {
		// An error is not an empty library. Recording it as consulted used to
		// open ssh and the network, which is the skip this ladder exists to stop.
		// The no-progress stop ends a store that keeps failing.
		return "[system rejected] " + err.Error() + "\n知识库这一步没有完成。请重试 knowledge_search。完成前不要用网络、craft_tool、本机文件或 ssh。"
	}
	c.contentRetrievalKnowledgeDone = true
	if strings.TrimSpace(text) == "" {
		text = "No matching knowledge found."
	}
	return text + contentRetrievalKnowledgeFooter
}

// ContentRetrievalStopNudge implements agent.ContentRetrievalStopGuard.
// Hiding craft_tool and ssh until the two reads finish also removes the
// side-effecting tool the stored-fact nudge requires, so a prose answer
// would skip the stores. Each missing step is asked twice. A third prose
// stop on the same step ends that reminder, and finishing memory still
// leaves the knowledge reminder unused.
func (c *sharedAgentLoopCallbacks) ContentRetrievalStopNudge() string {
	if !c.contentRetrievalOwned() {
		return ""
	}
	memory, knowledge := c.contentRetrievalProgress()
	if !memory {
		if c.contentRetrievalMemoryNudges >= 2 {
			return ""
		}
		c.contentRetrievalMemoryNudges++
		return "[系统] " + contentRetrievalNeedMemory
	}
	if !knowledge {
		if c.contentRetrievalKnowledgeNudges >= 2 {
			return ""
		}
		c.contentRetrievalKnowledgeNudges++
		return "[系统] " + contentRetrievalNeedKnowledge
	}
	return ""
}

// contentRetrievalConsultQuery is the text actually searched. The model query
// is kept when it names the subject and does not still contain a phrase that
// opened the ladder. Those phrases are not the fact. A short Han sentence
// that still contains one is matched as an AND of every anchor, so 查一下
// becomes a required 一下 and the card is reported missing. An empty hit
// then opens ssh. The search uses the subject that remains.
func contentRetrievalConsultQuery(userText, modelQuery string) string {
	userText = strings.TrimSpace(userText)
	modelQuery = strings.TrimSpace(modelQuery)
	if userText == "" {
		return modelQuery
	}
	if !contentRetrievalNamesSubject(userText, modelQuery) {
		return contentRetrievalFallbackQuery(userText)
	}
	// A port is not a gate phrase, but the lexical builder treats
	// api2.maclaw.top:22 as one phrase and requires the port. The host
	// identifier is the consultation.
	if contentRetrievalContainsGatePhrase(modelQuery) || contentRetrievalHasHostPort(modelQuery) {
		fallback := contentRetrievalFallbackQuery(userText)
		subject := contentRetrievalSubjectQuery(modelQuery)
		if subject != "" && contentRetrievalNamesSubject(userText, subject) {
			// Stripping ssh/主机 leaves a particle such as 到那台. That is not
			// the topic. The user text still contains the phrase, and FTS ORs
			// it, so the user text is the consultation.
			if fallback == userText && contentRetrievalHasASCIIIdent(userText) && !contentRetrievalHasASCIIIdent(subject) {
				return userText
			}
			return subject
		}
		return fallback
	}
	// "the backup" still names backup, but the lexical builder ANDs the extra
	// word. api2.maclaw.top likewise names api2. Search that subject.
	if bare := contentRetrievalSubjectQuery(modelQuery); bare != "" && bare != modelQuery && contentRetrievalNamesSubject(userText, bare) {
		return bare
	}
	return modelQuery
}

// contentRetrievalNamesSubject reports that modelQuery refers to this
// request's subject. A cue fragment such as 一下 does not. An identifier
// such as api2 must be carried whole.
func contentRetrievalNamesSubject(userText, modelQuery string) bool {
	if utf8.RuneCountInString(modelQuery) < 2 || !contentRetrievalContainsFold(userText, modelQuery) {
		return false
	}
	if len(contentRetrievalSubjectSpans(modelQuery)) == 0 {
		return false
	}
	// One hostname's consultation is its identifier or the whole address.
	// A port or a single octet still occurs in the user text, and treating
	// it as the subject completes the step on the wrong card.
	if host := contentRetrievalDottedHostQuery(userText); host != "" {
		return contentRetrievalContainsFold(modelQuery, host)
	}
	digits, ascii, cjk := contentRetrievalClassifySpans(contentRetrievalSubjectSpans(userText))
	if len(digits) > 0 {
		idents := make([]string, 0, len(digits))
		for _, span := range digits {
			if contentRetrievalAlphanumeric(span) {
				idents = append(idents, span)
			}
		}
		if len(idents) > 0 {
			return contentRetrievalContainsAnyFold(modelQuery, idents)
		}
		return contentRetrievalContainsAnyFold(modelQuery, digits)
	}
	if len(ascii) > 0 {
		// "of" and "the" are spans too. Only the longest word is the subject,
		// so a function word cannot complete the step.
		return contentRetrievalContainsFold(modelQuery, contentRetrievalLongestSpan(ascii))
	}
	if len(cjk) == 0 {
		return true
	}
	for _, span := range cjk {
		if contentRetrievalContainsFold(span, modelQuery) || contentRetrievalContainsFold(modelQuery, span) {
			return true
		}
	}
	return false
}

// contentRetrievalFallbackQuery is the consultation when the model did not
// name the subject. A single identifier (api2, 北京) is the query. Several
// identifiers are not joined: the lexical builder ANDs whitespace-separated
// tokens, so a card that has only one of them is reported missing and ssh
// opens. A mixed sentence takes the OR path, so the user text is searched.
// A single-script list uses its longest span. When the only ASCII token was
// itself a gate phrase (ssh) and the leftover is not an identifier, the user
// text is searched so that phrase stays in the query.
func contentRetrievalFallbackQuery(userText string) string {
	digits, ascii, _ := contentRetrievalClassifySpans(contentRetrievalSubjectSpans(userText))
	if len(digits) == 0 && len(ascii) == 0 && contentRetrievalHasASCIIIdent(userText) {
		return userText
	}
	if subject := contentRetrievalSubjectQuery(userText); subject != "" {
		return subject
	}
	return userText
}

// contentRetrievalSubjectQuery is the subject with the ladder's own phrases
// removed. One span is returned as itself. One hostname is one subject even
// when a Chinese sentence makes its labels look like several identifiers:
// the label that mixes letters and digits is the identifier, and a hostname
// with no such label is searched whole. Several separate identifiers in a
// mixed sentence return the sentence, because that query ORs. Several spans
// in one script return the longest digit identifier when one exists,
// otherwise the longest span. The pieces of a hostname must not be ANDed.
func contentRetrievalSubjectQuery(text string) string {
	if host := contentRetrievalDottedHostQuery(text); host != "" {
		return host
	}
	digits, ascii, cjk := contentRetrievalClassifySpans(contentRetrievalSubjectSpans(text))
	spans := cjk
	if len(digits) > 0 || len(ascii) > 0 {
		spans = append(append([]string{}, digits...), ascii...)
	}
	switch len(spans) {
	case 0:
		return ""
	case 1:
		return spans[0]
	default:
		if contentRetrievalMixedScript(text) {
			return strings.TrimSpace(text)
		}
		if len(digits) > 0 {
			return contentRetrievalLongestSpan(digits)
		}
		return contentRetrievalLongestSpan(spans)
	}
}

// contentRetrievalDottedHostQuery collapses the labels of a single hostname.
// A Chinese sentence is mixed, so api2, maclaw, and top would otherwise be
// searched as the whole utterance and 状态 can fill the result. A second
// hostname or any other subject leaves this empty and the caller keeps the
// OR path.
func contentRetrievalDottedHostQuery(text string) string {
	hosts := contentRetrievalDottedHosts(contentRetrievalStripGatePhrases(text))
	if len(hosts) != 1 {
		return ""
	}
	pieces := strings.FieldsFunc(hosts[0], func(r rune) bool { return r == '.' })
	if len(pieces) < 2 {
		return ""
	}
	known := make(map[string]struct{}, len(pieces))
	for _, piece := range pieces {
		known[strings.ToLower(piece)] = struct{}{}
	}
	stripped := strings.ToLower(contentRetrievalStripGatePhrases(text))
	hostKey := strings.ToLower(hosts[0])
	for _, span := range contentRetrievalSubjectSpans(text) {
		if _, ok := known[strings.ToLower(span)]; ok {
			continue
		}
		// :22 is this host's port, not a second subject.
		if contentRetrievalPureDigits(span) && strings.Contains(stripped, hostKey+":"+span) {
			continue
		}
		return ""
	}
	var ident []string
	for _, piece := range pieces {
		if utf8.RuneCountInString(piece) >= 2 && contentRetrievalAlphanumeric(piece) {
			ident = append(ident, piece)
		}
	}
	if len(ident) > 0 {
		return contentRetrievalLongestSpan(ident)
	}
	return hosts[0]
}

func contentRetrievalDottedHosts(text string) []string {
	var hosts []string
	var b strings.Builder
	flush := func() {
		token := strings.Trim(b.String(), ".")
		b.Reset()
		if strings.Count(token, ".") < 1 {
			return
		}
		pieces := strings.FieldsFunc(token, func(r rune) bool { return r == '.' })
		if len(pieces) >= 2 {
			hosts = append(hosts, token)
		}
	}
	for _, r := range text {
		if contentRetrievalASCIIIdentRune(r) || r == '.' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return hosts
}

func contentRetrievalAlphanumeric(text string) bool {
	letter := false
	digit := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	return letter && digit
}

func contentRetrievalPureDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// contentRetrievalHasHostPort reports a single hostname with a :digits suffix.
func contentRetrievalHasHostPort(text string) bool {
	hosts := contentRetrievalDottedHosts(text)
	if len(hosts) != 1 {
		return false
	}
	lower := strings.ToLower(text)
	needle := strings.ToLower(hosts[0]) + ":"
	idx := strings.Index(lower, needle)
	if idx < 0 || idx+len(needle) >= len(lower) {
		return false
	}
	return unicode.IsDigit(rune(lower[idx+len(needle)]))
}

func contentRetrievalLongestSpan(spans []string) string {
	best := ""
	bestN := 0
	for _, span := range spans {
		n := utf8.RuneCountInString(span)
		if n > bestN {
			best = span
			bestN = n
		}
	}
	return best
}

func contentRetrievalMixedScript(text string) bool {
	ascii := false
	other := false
	for _, r := range text {
		switch {
		case contentRetrievalASCIIIdentRune(r):
			ascii = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			other = true
		}
	}
	return ascii && other
}

func contentRetrievalContainsGatePhrase(text string) bool {
	for _, phrase := range contentRetrievalGatePhrases() {
		if contentRetrievalASCIIPhrase(phrase) {
			if contentRetrievalContainsBoundedFold(text, phrase) {
				return true
			}
			continue
		}
		if contentRetrievalContainsFold(text, phrase) {
			return true
		}
	}
	return false
}

func contentRetrievalGatePhrases() []string {
	phrases := make([]string, 0, len(contentRetrievalLookupPhrases)+len(contentRetrievalStoredPhrases))
	phrases = append(phrases, contentRetrievalLookupPhrases...)
	phrases = append(phrases, contentRetrievalStoredPhrases...)
	sort.SliceStable(phrases, func(i, j int) bool {
		return utf8.RuneCountInString(phrases[i]) > utf8.RuneCountInString(phrases[j])
	})
	return phrases
}

func contentRetrievalStripGatePhrases(text string) string {
	for _, phrase := range contentRetrievalGatePhrases() {
		text = contentRetrievalReplaceFold(text, phrase, contentRetrievalASCIIPhrase(phrase))
	}
	return text
}

func contentRetrievalASCIIPhrase(phrase string) bool {
	if phrase == "" {
		return false
	}
	for _, r := range phrase {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func contentRetrievalReplaceFold(text, phrase string, bounded bool) string {
	want := []rune(phrase)
	if len(want) == 0 {
		return text
	}
	src := []rune(text)
	var b strings.Builder
	for i := 0; i < len(src); {
		if i+len(want) <= len(src) && contentRetrievalRunesFoldEqual(src[i:i+len(want)], want) {
			if !bounded || contentRetrievalASCIIBoundary(src, i, i+len(want)) {
				b.WriteByte(' ')
				i += len(want)
				continue
			}
		}
		b.WriteRune(src[i])
		i++
	}
	return b.String()
}

func contentRetrievalContainsBoundedFold(text, phrase string) bool {
	want := []rune(phrase)
	if len(want) == 0 {
		return false
	}
	src := []rune(text)
	for i := 0; i+len(want) <= len(src); i++ {
		if contentRetrievalRunesFoldEqual(src[i:i+len(want)], want) && contentRetrievalASCIIBoundary(src, i, i+len(want)) {
			return true
		}
	}
	return false
}

func contentRetrievalASCIIBoundary(src []rune, start, end int) bool {
	if start > 0 && contentRetrievalASCIIIdentRune(src[start-1]) {
		return false
	}
	if end < len(src) && contentRetrievalASCIIIdentRune(src[end]) {
		return false
	}
	return true
}

func contentRetrievalASCIIIdentRune(r rune) bool {
	return r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func contentRetrievalSubjectSpans(text string) []string {
	var spans []string
	var b strings.Builder
	flush := func() {
		span := b.String()
		b.Reset()
		if utf8.RuneCountInString(span) >= 2 {
			spans = append(spans, span)
		}
	}
	for _, r := range contentRetrievalStripGatePhrases(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return spans
}

func contentRetrievalClassifySpans(spans []string) (digits, ascii, cjk []string) {
	for _, span := range spans {
		switch contentRetrievalSpanClass(span) {
		case "digit":
			digits = append(digits, span)
		case "ascii":
			ascii = append(ascii, span)
		case "cjk":
			cjk = append(cjk, span)
		}
	}
	return digits, ascii, cjk
}

func contentRetrievalSpanClass(span string) string {
	digit := false
	ascii := false
	other := false
	for _, r := range span {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case r <= unicode.MaxASCII && unicode.IsLetter(r):
			ascii = true
		case unicode.IsLetter(r):
			other = true
		}
	}
	if digit {
		return "digit"
	}
	if ascii && !other {
		return "ascii"
	}
	if other {
		return "cjk"
	}
	return ""
}

func contentRetrievalHasASCIIIdent(text string) bool {
	for _, r := range text {
		if contentRetrievalASCIIIdentRune(r) {
			return true
		}
	}
	return false
}

func contentRetrievalContainsAnyFold(text string, parts []string) bool {
	for _, part := range parts {
		if contentRetrievalContainsFold(text, part) {
			return true
		}
	}
	return false
}

func contentRetrievalContainsFold(text, part string) bool {
	if part == "" {
		return false
	}
	src := []rune(text)
	want := []rune(part)
	if len(want) > len(src) {
		return false
	}
	for i := 0; i+len(want) <= len(src); i++ {
		if contentRetrievalRunesFoldEqual(src[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func contentRetrievalRunesFoldEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if unicode.ToLower(a[i]) != unicode.ToLower(b[i]) {
			return false
		}
	}
	return true
}

func contentRetrievalQuery(argsJSON string) (string, error) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil || parsed == nil {
		return "", fmt.Errorf("query required")
	}
	query, _ := parsed["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query required")
	}
	return query, nil
}
