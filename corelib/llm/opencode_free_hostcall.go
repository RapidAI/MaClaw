package llm

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// The free endpoint rejects a request whose system prompt or tools differ
// from the captured OpenCode client. Host tools such as goal cannot be added
// to that list. The model still has execute, so a host call travels inside
// that one official tool and is rewritten back to the host name before
// admission. The note is appended to the latest real user turn of the
// outbound body. A tool result flattened onto the user role is not that
// turn. The system prompt bytes are part of the fingerprint and must stay
// intact. A tail that already ends with the same note is left alone. A
// failed execute is a short tool result, not a second copy of the note. A
// host-only call already in the transcript is shown back as execute plus
// MACLAW_CALL, so the next turn repeats the channel instead of a direct
// host name. That same sentence is an execute delivery on the way back.
// Calls that were adapted only for local execution cross the
// bridge in the opposite direction and reappear under their official names.
//
// The goal example follows the host continuation phase of the latest user
// message (guiapp goalContinuationPhase): the tag is a prefix, an execute
// tag wins over a later planning sentence, and a bare [系统续接] is the
// step phase. A newer user message without that prefix clears it. Every
// taught step example records progress, so copying it does not finish the
// step. One execute can carry several host calls. A goal action from the
// other phase is dropped, and a string value that is only the example dots
// is not a delivery. A MACLAW_CALL of shell, read, write, edit, glob, grep, webfetch, or
// websearch is the same official call as a direct function and uses that
// name bridge. tools.shell stays inside execute.

const openCodeFreeHostCallToken = "MACLAW_CALL"

// openCodeFreeOfficialTools is derived from the embedded fingerprint catalog.
// Deriving it keeps a profile refresh from splitting the wire schema and the
// host-extra classifier into two disagreeing tool lists.
var openCodeFreeOfficialTools = loadOpenCodeFreeOfficialToolNames()

func loadOpenCodeFreeOfficialToolNames() map[string]struct{} {
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(openCodeFreeToolsJSON), &tools); err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if name := strings.ToLower(strings.TrimSpace(tool.Function.Name)); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func openCodeFreeOfficialToolName(name string) bool {
	_, ok := openCodeFreeOfficialTools[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// OpenCodeFreeHostExtras lists caller tools the official catalog cannot
// express. shell/read/write/edit/glob/grep/webfetch/websearch already rewrite
// onto their Maclaw executors, so those names are not repeated here.
func OpenCodeFreeHostExtras(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if openCodeFreeOfficialToolName(name) || openCodeFreeMappedHost(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func openCodeFreeCallerHostNote(caller []byte) string {
	var envelope struct {
		Tools json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(caller, &envelope) != nil || len(strings.TrimSpace(string(envelope.Tools))) == 0 {
		return ""
	}
	var tools any
	if json.Unmarshal(envelope.Tools, &tools) != nil {
		return ""
	}
	extras := OpenCodeFreeHostExtras(OpenAIDeclaredToolNames(tools))
	phase, stepIndex := openCodeFreeGoalPhase(caller)
	note := openCodeFreeHostChannelNote(extras, phase, stepIndex)
	if note == "" {
		return ""
	}
	extra := openCodeFreeHostSupplement(tools, extras)
	if extra == "" {
		return note
	}
	return strings.TrimSuffix(note, "</system-reminder>") + extra + "</system-reminder>"
}

// parseOpenCodeFreeContentToolCalls recognizes the serializations
// openCodeFreeConversation writes back into the transcript. Official tools
// come back as "websearch {...}". Host extras come back as
// "execute return MACLAW_CALL(...)". Both are context for the model and
// both are imitation surfaces. A copied host replay is the execute delivery
// this channel already accepts; leaving it as assistant text is how the
// call becomes the answer. A sentence that only mentions the form stays
// text. Names such as read/write stay on this boundary because they are
// too generic for the cross-provider parser.
func parseOpenCodeFreeContentToolCalls(content string) ([]ToolCall, bool) {
	call, adopted, parsed := adoptOpenCodeFreeHostReplay(content)
	if adopted {
		return []ToolCall{call}, false
	}
	if !parsed && openCodeFreeBrokenHostSyntax(content) {
		return nil, true
	}
	return parseContentToolCallsDetailed(content, openCodeFreeCompactToolName)
}

// adoptOpenCodeFreeHostReplay accepts a transcript replay of one or more
// host extras. The recovered call is execute with {"code": sentence}, the
// envelope a real execute call carries, so the stream assembler keeps it
// and expandOpenCodeFreeHostCalls reads the code. parsed is false when that
// syntax did not close. Placeholder dots are not a delivery. A residue that
// is a real sentence is not a replay.
func adoptOpenCodeFreeHostReplay(content string) (call ToolCall, adopted, parsed bool) {
	adopted, parsed = classifyOpenCodeFreeHostReplay(content)
	if !adopted {
		return ToolCall{}, false, parsed
	}
	encoded, err := json.Marshal(map[string]string{"code": content})
	if err != nil {
		return ToolCall{}, false, true
	}
	return makeContentToolCall("execute", string(encoded)), true, true
}

// classifyOpenCodeFreeHostReplay reports whether content is a host replay.
// The stream filter calls this on the way to flush, so it does not build
// the execute envelope until a caller actually adopts the call.
func classifyOpenCodeFreeHostReplay(content string) (adopted, parsed bool) {
	found, residue, ok := splitOpenCodeFreeHostReplay(content)
	if !ok {
		return false, false
	}
	if !openCodeFreeHostCallsDeliver(found, "") || !openCodeFreeReplayResidue(residue) {
		return false, true
	}
	return true, true
}

// openCodeFreeBrokenHostSyntax is a replay whose host call never closed.
// Call it only after adoptOpenCodeFreeHostReplay reports parsed=false: a
// call that parsed is a delivery or an example, not this case.
func openCodeFreeBrokenHostSyntax(content string) bool {
	token := openCodeFreeHostCallToken
	for _, at := range indexOpenCodeFreeHostCalls(content) {
		if at < 0 || at >= len(content) || !openCodeFreeReplayResidue(content[:at]) {
			continue
		}
		tail := content[at:]
		if strings.HasPrefix(tail, token) {
			if openCodeFreeBrokenCallTail(tail) {
				return true
			}
			continue
		}
		if openCodeFreeBrokenToolsTail(tail) {
			return true
		}
	}
	return false
}

// openCodeFreeBrokenCallTail reports that everything after a MACLAW_CALL
// token is an unclosed call plus replay residue. A later word is an answer.
func openCodeFreeBrokenCallTail(src string) bool {
	token := openCodeFreeHostCallToken
	if !strings.HasPrefix(src, token) {
		return false
	}
	i := len(token)
	if i < len(src) && isJSIdentCont(src[i]) {
		return false
	}
	i = skipASCIISpace(src, i)
	if i >= len(src) || src[i] != '(' {
		return false
	}
	return openCodeFreeBrokenAfterOpen(src, i)
}

// openCodeFreeBrokenToolsTail is the tools.name( form of the same cutoff.
// Official names are not hits, so tools.shell( stays text.
func openCodeFreeBrokenToolsTail(src string) bool {
	if _, _, ok := matchToolsHostCall(src, 0); !ok {
		return false
	}
	open := strings.IndexByte(src, '(')
	if open < 0 {
		return false
	}
	return openCodeFreeBrokenAfterOpen(src, open)
}

// openCodeFreeBrokenAfterOpen reports that the argument list opened at open
// never finishes, and the bytes after it are only replay residue.
func openCodeFreeBrokenAfterOpen(src string, open int) bool {
	i := open + 1
	for i < len(src) {
		if src[i] == '"' || src[i] == '\'' || src[i] == '`' {
			next, closed := openCodeFreeQuotedSpan(src, i)
			// An unclosed quote that runs onto the next line is the answer
			// continuing, not a cut-off call.
			if !closed && strings.IndexByte(src[i:], '\n') >= 0 {
				return false
			}
			if next <= i {
				i++
				continue
			}
			i = next
			continue
		}
		if next, ok := skipJSCodeNoise(src, i); ok {
			if next <= i {
				i++
				continue
			}
			i = next
			continue
		}
		c := src[i]
		if isJSIdentStart(c) {
			ident, n, ok := readJSIdent(src[i:])
			if !ok {
				break
			}
			switch ident {
			case "true", "false", "null":
				i += n
				continue
			default:
			}
			break
		}
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || isASCIISpaceByte(c) || openCodeFreeHostCallPunct(c) {
			i++
			continue
		}
		break
	}
	return openCodeFreeReplayResidue(src[i:])
}

func openCodeFreeHostCallPunct(c byte) bool {
	switch c {
	case '(', ')', '{', '}', '[', ']', ',', ':', ';':
		return true
	default:
		return false
	}
}

func openCodeFreeQuotedSpan(src string, i int) (int, bool) {
	if i >= len(src) {
		return i, true
	}
	var next int
	switch src[i] {
	case '`':
		next = skipJSTemplate(src, i)
	case '"', '\'':
		next = skipJSString(src, i)
	default:
		return i, true
	}
	if next <= i {
		return i + 1, false
	}
	return next, src[next-1] == src[i]
}

// openCodeFreeReplayResidue is the wrapper openCodeFreeConversation puts
// around a host call: the words execute and return, separators, whitespace,
// and a trailing tag the model sometimes appends. A tag or one of those
// words that is still unfinished at the end counts too, because the next
// byte can finish the wrapper. Any other byte is an answer.
func openCodeFreeReplayResidue(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case ' ', '\n', '\r', '\t', ';', ',':
			i++
			continue
		case '<':
			n := openCodeFreeReplayTagLen(s[i:])
			if n > 0 {
				i += n
				continue
			}
			return openCodeFreeReplayTagOpen(s[i:])
		}
		if isJSIdentStart(c) {
			j := i + 1
			for j < len(s) && isJSIdentCont(s[j]) {
				j++
			}
			word := s[i:j]
			if strings.EqualFold(word, "execute") || strings.EqualFold(word, "return") {
				i = j
				continue
			}
			return j == len(s) && openCodeFreeReplayWordOpen(word)
		}
		return false
	}
	return true
}

func openCodeFreeReplayTagLen(s string) int {
	if len(s) < 3 || s[0] != '<' {
		return 0
	}
	i := 1
	if s[i] == '/' {
		i++
	}
	if i >= len(s) || !isJSIdentStart(s[i]) {
		return 0
	}
	i++
	for i < len(s) {
		c := s[i]
		if isJSIdentCont(c) || c == ':' || c == '-' {
			i++
			continue
		}
		break
	}
	if i >= len(s) || s[i] != '>' {
		return 0
	}
	return i + 1
}

// openCodeFreeReplayTagOpen reports that s is the start of a tag that has
// not closed. A byte that cannot appear in a tag is text.
func openCodeFreeReplayTagOpen(s string) bool {
	if s == "" || s[0] != '<' {
		return false
	}
	i := 1
	if i < len(s) && s[i] == '/' {
		i++
	}
	if i >= len(s) {
		return true
	}
	if !isJSIdentStart(s[i]) {
		return false
	}
	for i < len(s) {
		c := s[i]
		if isJSIdentCont(c) || c == ':' || c == '-' {
			i++
			continue
		}
		return false
	}
	return true
}

// openCodeFreeReplayWordOpen reports that word can still grow into execute
// or return. A finished different word is text.
func openCodeFreeReplayWordOpen(word string) bool {
	return openCodeFreeFoldPrefix("execute", word) || openCodeFreeFoldPrefix("return", word)
}

func openCodeFreeFoldPrefix(lowerTarget, word string) bool {
	if word == "" || len(word) >= len(lowerTarget) {
		return false
	}
	return asciiFoldEqual(word, lowerTarget[:len(word)])
}

const (
	openCodeFreeReplayNone = iota
	openCodeFreeReplayPending
	openCodeFreeReplayCall
)

// openCodeFreeHostReplayStreamHold keeps a host-extra replay off the chat
// until flush. answerStarted means an earlier chunk already showed prose, so
// this buffer is part of that answer and is released as it arrives. A finished
// mention is released before flush: more tokens cannot turn it into a replay.
// Suppress waits for flush so a later sentence is not dropped while the
// buffer still holds it. The free stream is the only caller.
func openCodeFreeHostReplayStreamHold(s string, force, answerStarted bool) (visible, hold string, suppress, ok bool) {
	// Prose already reached the chat, so this buffer cannot be a pure replay.
	// Holding it would delay the rest of the answer until the stream ends.
	if answerStarted {
		return "", "", false, false
	}
	kind, start := openCodeFreeHostReplayHoldSpan(s)
	if kind == openCodeFreeReplayNone {
		return "", "", false, false
	}
	if start < 0 || start > len(s) {
		start = 0
	}
	if !force {
		if kind == openCodeFreeReplayCall && strings.ContainsAny(s, ")}]") {
			if adopted, parsed := classifyOpenCodeFreeHostReplay(s); parsed && !adopted {
				return "", "", false, false
			}
		}
		return s[:start], s[start:], false, true
	}
	if adopted, parsed := classifyOpenCodeFreeHostReplay(s); adopted || (!parsed && openCodeFreeBrokenHostSyntax(s)) {
		return "", "", true, true
	}
	return "", "", false, false
}

func openCodeFreeReplayHint(s string) bool {
	if strings.Contains(s, "MAC") || strings.Contains(s, "tools") {
		return true
	}
	// A Chinese answer has no ASCII letter, so the word scan can stop here.
	if !openCodeFreeHasASCIILetter(s) {
		return false
	}
	return openCodeFreeContainsFold(s, "return") || openCodeFreeContainsFold(s, "execute")
}

func openCodeFreeHasASCIILetter(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

func openCodeFreeContainsFold(s, lower string) bool {
	n := len(lower)
	if n == 0 || len(s) < n {
		return false
	}
	for i := 0; i+n <= len(s); i++ {
		if asciiFoldEqual(s[i:i+n], lower) {
			return true
		}
	}
	return false
}

func openCodeFreeHostReplayHoldSpan(s string) (int, int) {
	// A chunk that is only the replay wrapper (a tag, "execute", "return",
	// whitespace) can still grow into the call. Hold it so it is not emitted
	// before the next chunk arrives.
	if s != "" && openCodeFreeReplayResidue(s) {
		return openCodeFreeReplayPending, 0
	}
	if !openCodeFreeReplayHint(s) {
		return openCodeFreeReplayNone, 0
	}
	token := openCodeFreeHostCallToken
	for i := 0; i < len(s); {
		if next, ok := skipJSCodeNoise(s, i); ok {
			if next <= i {
				i++
				continue
			}
			i = next
			continue
		}
		atBoundary := i == 0 || !isJSIdentCont(s[i-1])
		if atBoundary && strings.HasPrefix(s[i:], token) {
			end := i + len(token)
			if end < len(s) && isJSIdentCont(s[end]) {
				i++
				continue
			}
			j := skipASCIISpace(s, end)
			if j >= len(s) || s[j] == '(' {
				kind := openCodeFreeReplayPending
				if j < len(s) && s[j] == '(' {
					kind = openCodeFreeReplayCall
				}
				start := openCodeFreeReplayHoldStart(s, openCodeFreeHostCallPreambleStart(s, i))
				return kind, start
			}
			i = end
			continue
		}
		// tools.memory_recall( is the other transcript shape. Hold the call
		// once the parenthesis is present, and hold an unfinished head
		// (tools / tools.memory_recall) so the name does not flash first.
		if atBoundary && s[i] == 't' {
			if at, _, ok := matchToolsHostCall(s, i); ok {
				start := openCodeFreeReplayHoldStart(s, openCodeFreeHostCallPreambleStart(s, at))
				return openCodeFreeReplayCall, start
			}
			if openCodeFreeOpenToolsHead(s[i:]) && openCodeFreeReplayResidue(s[:i]) {
				return openCodeFreeReplayPending, openCodeFreeReplayHoldStart(s, i)
			}
		}
		i++
	}
	if n, at, ok := openCodeFreeHostTokenPrefix(s); ok {
		start := openCodeFreeHostCallPreambleStart(s, at)
		// Hold MACLAW, an execute/return preamble, or a prefix that is itself
		// only replay noise. "The server MAC" is ordinary text.
		if n >= 6 || start < at || openCodeFreeReplayResidue(s[:at]) {
			return openCodeFreeReplayPending, openCodeFreeReplayHoldStart(s, start)
		}
	}
	if start, ok := openCodeFreeExecuteReturnTail(s); ok {
		return openCodeFreeReplayPending, openCodeFreeReplayHoldStart(s, start)
	}
	return openCodeFreeReplayNone, 0
}

func openCodeFreeHostTokenPrefix(s string) (int, int, bool) {
	token := openCodeFreeHostCallToken
	max := len(token) - 1
	if len(s) < max {
		max = len(s)
	}
	for n := max; n >= 1; n-- {
		at := len(s) - n
		if !strings.HasPrefix(token, s[at:]) {
			continue
		}
		if at > 0 && isJSIdentCont(s[at-1]) {
			continue
		}
		if !openCodeFreeAtCode(s, at) {
			continue
		}
		return n, at, true
	}
	return 0, 0, false
}

// openCodeFreeHostCallPreambleStart walks back from the token across
// "return" and then "execute". The word boundary has to be whitespace, so
// executereturn stays one word.
func openCodeFreeHostCallPreambleStart(s string, tokenAt int) int {
	if tokenAt <= 0 || tokenAt > len(s) {
		return tokenAt
	}
	retEnd := skipASCIISpaceBack(s, tokenAt)
	if retEnd == tokenAt {
		return tokenAt
	}
	retStart := trimBackWord(s, retEnd, "return")
	if retStart < 0 {
		return tokenAt
	}
	execEnd := skipASCIISpaceBack(s, retStart)
	if execEnd == retStart {
		return retStart
	}
	execStart := trimBackWord(s, execEnd, "execute")
	if execStart < 0 {
		return retStart
	}
	return execStart
}

// openCodeFreeExecuteReturnTail holds a finished "execute return" that
// follows other text, including its trailing space. The word has to be its
// own identifier, so "please return" stays visible. A leading "return" is
// replay residue and is held before this check.
func openCodeFreeExecuteReturnTail(s string) (int, bool) {
	end := len(s)
	for end > 0 && isASCIISpaceByte(s[end-1]) {
		end--
	}
	start := trimBackWord(s, end, "return")
	if start < 0 || !openCodeFreeAtCode(s, start) {
		return 0, false
	}
	before := skipASCIISpaceBack(s, start)
	if before == start {
		return 0, false
	}
	execStart := trimBackWord(s, before, "execute")
	if execStart < 0 {
		return 0, false
	}
	return execStart, true
}

// openCodeFreeOpenToolsHead reports that s is an unfinished tools.name or
// tools["name"] head running to the end of the buffer. A parenthesis is a
// call, handled by matchToolsHostCall, so this stops before one.
func openCodeFreeOpenToolsHead(s string) bool {
	const token = "tools"
	if !strings.HasPrefix(s, token) {
		return false
	}
	if len(s) > len(token) && isJSIdentCont(s[len(token)]) {
		return false
	}
	i := skipASCIISpace(s, len(token))
	if i >= len(s) {
		return true
	}
	switch {
	case s[i] == '.':
		i++
	case s[i] == '?' && i+1 < len(s) && s[i+1] == '.':
		i += 2
	case s[i] == '?' && i+1 == len(s):
		return true
	case s[i] == '[':
		return openCodeFreeOpenToolsBracket(s[i:])
	default:
		return false
	}
	i = skipASCIISpace(s, i)
	if i >= len(s) {
		return true
	}
	if !isJSIdentStart(s[i]) {
		return false
	}
	i++
	for i < len(s) && isJSIdentCont(s[i]) {
		i++
	}
	i = skipASCIISpace(s, i)
	return i >= len(s)
}

func openCodeFreeOpenToolsBracket(s string) bool {
	if s == "" || s[0] != '[' {
		return false
	}
	i := skipASCIISpace(s, 1)
	if i >= len(s) {
		return true
	}
	if s[i] != '"' && s[i] != '\'' {
		return false
	}
	next, closed := openCodeFreeQuotedSpan(s, i)
	if !closed {
		return next >= len(s)
	}
	i = skipASCIISpace(s, next)
	if i >= len(s) {
		return true
	}
	if s[i] != ']' {
		return false
	}
	i = skipASCIISpace(s, i+1)
	return i >= len(s)
}

func openCodeFreeReplayHoldStart(s string, start int) int {
	if start <= 0 || start > len(s) {
		return 0
	}
	// A leading tag or "execute return" is part of the replay. Leaving it
	// visible would mark the answer as started and force the call to stream.
	if openCodeFreeReplayResidue(s[:start]) {
		return 0
	}
	return start
}

func trimBackWord(s string, end int, word string) int {
	n := len(word)
	if end < n || end > len(s) {
		return -1
	}
	start := end - n
	if !strings.EqualFold(s[start:end], word) {
		return -1
	}
	if start > 0 && isJSIdentCont(s[start-1]) {
		return -1
	}
	return start
}

func skipASCIISpaceBack(s string, i int) int {
	for i > 0 && isASCIISpaceByte(s[i-1]) {
		i--
	}
	return i
}

func isASCIISpaceByte(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

// openCodeFreeAtCode reports whether index at sits outside a string,
// template, or comment.
func openCodeFreeAtCode(s string, at int) bool {
	if at < 0 || at > len(s) {
		return false
	}
	for i := 0; i < at; {
		if next, ok := skipJSCodeNoise(s, i); ok {
			if next <= i {
				i++
				continue
			}
			if at < next {
				return false
			}
			i = next
			continue
		}
		i++
	}
	return true
}

func openCodeFreeCompactToolName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if _, ok := openCodeFreeOfficialTools[lower]; ok {
		return true
	}
	if openCodeFreeMappedHost(lower) {
		return true
	}
	return looksLikeLineOrientedToolName(name)
}

func openCodeFreeMappedHost(name string) bool {
	for _, host := range openCodeFreeToolTargets {
		if strings.EqualFold(host, name) {
			return true
		}
	}
	return false
}

// OpenCodeFreeHostChannelNote tells the model how to deliver a host tool
// through execute. An empty extra list needs no note. The note is wire-only:
// the tool result must not repeat it, or the next body stacks a second copy.
func OpenCodeFreeHostChannelNote(extras []string) string {
	return openCodeFreeHostChannelNote(extras, "", 0)
}

func openCodeFreeHostChannelNote(extras []string, phase string, stepIndex int) string {
	form := openCodeFreeHostCallForm(extras, phase, stepIndex)
	if form == "" {
		return ""
	}
	return "<system-reminder>\n" + form + "</system-reminder>"
}

func openCodeFreeHostCallForm(extras []string, phase string, stepIndex int) string {
	if len(extras) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The function list in this request is fixed. It has no entry for the host tools named below. Do not call those names directly, and do not tell the user they are missing.\n")
	b.WriteString("Deliver each host tool by calling execute. The code argument is data, not a catalog lookup. Do not call tools.* and do not inspect the catalog. Use one return statement.\n")
	b.WriteString("Host tools on this turn: ")
	b.WriteString(openCodeFreeHostToolList(extras))
	b.WriteString(".\n")
	if openCodeFreeHostExampleName(extras) == "goal" {
		switch phase {
		case "plan":
			b.WriteString("A goal plan is one execute call: return MACLAW_CALL(\"goal\", {\"action\":\"plan\",\"understanding\":\"...\",\"steps\":[{\"title\":\"...\",\"detail\":\"...\"}],\"acceptance_criteria\":[\"...\"]}).\n")
		case "execute":
			if stepIndex < 1 {
				stepIndex = 1
			}
			b.WriteString("A step update is one execute call: return MACLAW_CALL(\"goal\", {\"action\":\"step\",\"index\":")
			b.WriteString(strconv.Itoa(stepIndex))
			b.WriteString(",\"status\":\"progress\",\"result\":\"...\"}).\n")
		default:
			b.WriteString("A goal plan is one execute call: return MACLAW_CALL(\"goal\", {\"action\":\"plan\",\"understanding\":\"...\",\"steps\":[{\"title\":\"...\",\"detail\":\"...\"}],\"acceptance_criteria\":[\"...\"]}).\n")
			b.WriteString("A step update is return MACLAW_CALL(\"goal\", {\"action\":\"step\",\"index\":1,\"status\":\"progress\",\"result\":\"...\"}).\n")
		}
		b.WriteString("Replace every ... from the user task. Submitting the dots does not state the plan.\n")
		b.WriteString("Writing the plan as prose does not submit it.\n")
	}
	return b.String()
}

// openCodeFreeGoalPhase reads the host phase from the latest user message.
// Older turns do not keep a phase, and an assistant echo of the marker is
// not a phase change. The step index is the last index= in that message.
func openCodeFreeGoalPhase(caller []byte) (string, int) {
	var envelope struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(caller, &envelope) != nil {
		return "", 0
	}
	for i := len(envelope.Messages) - 1; i >= 0; i-- {
		msg := envelope.Messages[i]
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		return openCodeFreeGoalPhaseFromText(openCodeFreeMessageText(msg.Content))
	}
	return "", 0
}

// openCodeFreeGoalPhaseFromText matches guiapp goalContinuationPhase.
// The tag has to be the prefix of this turn. An execute tag is decided
// before the plan sentence, because a step title can repeat 本轮只做规划.
func openCodeFreeGoalPhaseFromText(text string) (string, int) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[系统续接]") {
		return "", 0
	}
	if strings.HasPrefix(trimmed, "[系统续接][execute]") {
		return "execute", openCodeFreeGoalStepIndex(text)
	}
	if strings.HasPrefix(trimmed, "[系统续接][plan]") || strings.Contains(trimmed, "本轮只做规划") {
		return "plan", 0
	}
	return "execute", openCodeFreeGoalStepIndex(text)
}

func openCodeFreeGoalStepIndex(text string) int {
	const key = "index="
	n := 0
	for from := 0; from < len(text); {
		at := strings.Index(text[from:], key)
		if at < 0 {
			return n
		}
		i := from + at + len(key)
		v := 0
		digits := 0
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			v = v*10 + int(text[i]-'0')
			digits++
			i++
			if v > 100000 {
				v = 0
				digits = 0
				break
			}
		}
		if digits > 0 && v > 0 {
			n = v
		}
		from = i
	}
	return n
}

func openCodeFreeHostExampleName(extras []string) string {
	for _, name := range extras {
		if name == "goal" {
			return name
		}
	}
	if len(extras) == 0 {
		return ""
	}
	return extras[0]
}

func openCodeFreeHostToolList(extras []string) string {
	shown := extras
	more := 0
	if len(shown) > 32 {
		more = len(shown) - 32
		shown = shown[:32]
	}
	list := strings.Join(shown, ", ")
	if more > 0 {
		list += " …"
	}
	return list
}

// joinOpenCodeFreeHostNote appends the wire note once. A message that already
// ends with it (a replayed body, or a tail built from an earlier append) is
// unchanged.
func joinOpenCodeFreeHostNote(content, note string) string {
	if note == "" {
		return content
	}
	body := strings.TrimRight(content, "\n")
	if strings.HasSuffix(body, note) {
		return body
	}
	if body == "" {
		return note
	}
	return body + "\n\n" + note
}

// OpenCodeFreeHostChannelRetry is the tool result for an execute call that
// did not deliver a host tool. It must not tell the model to stop retrying
// execute, and it must not embed the wire reminder.
func OpenCodeFreeHostChannelRetry(rendered []string) string {
	extras := OpenCodeFreeHostExtras(rendered)
	if len(extras) == 0 {
		return "Error: execute is not a host runtime on this turn. Call shell, read, write, edit, glob, grep, webfetch, or websearch directly. Do not inspect the catalog."
	}
	return "Error: this execute call did not deliver a host tool. Submit one return MACLAW_CALL statement. Host tools: " + openCodeFreeHostToolList(extras) + ". Do not inspect the catalog."
}

// OpenCodeFreeHostChannelRetryForTurn is the execute denial for one turn.
// A copied example and a goal action from the other phase are different
// failures, so the result names the one that blocked delivery.
func OpenCodeFreeHostChannelRetryForTurn(rendered []string, userText, executeArgs string) string {
	phase, _ := openCodeFreeGoalPhaseFromText(userText)
	code := openCodeFreeExecuteCode(executeArgs)
	parsed, ok := parseOpenCodeFreeHostCalls(code)
	if !ok || len(parsed) == 0 || openCodeFreeHostCallsDeliver(parsed, phase) {
		return OpenCodeFreeHostChannelRetry(rendered)
	}
	placeholder := false
	conflict := false
	for _, call := range parsed {
		if openCodeFreeGoalPhaseConflicts(phase, call) {
			conflict = true
			continue
		}
		if openCodeFreeArgsUsePlaceholder(call.Args) {
			placeholder = true
		}
	}
	if placeholder {
		return openCodeFreeHostRetry(rendered, "this execute call copied the example dots. Replace every ... from the user task, then return one MACLAW_CALL.")
	}
	if conflict && phase == "plan" {
		return openCodeFreeHostRetry(rendered, "this turn submits a goal plan. A step update is not this turn. Return one MACLAW_CALL for action plan.")
	}
	if conflict && phase == "execute" {
		return openCodeFreeHostRetry(rendered, "this turn submits the current step. A new plan is not this turn. Return one MACLAW_CALL for action step.")
	}
	return OpenCodeFreeHostChannelRetry(rendered)
}

func openCodeFreeHostRetry(rendered []string, detail string) string {
	extras := OpenCodeFreeHostExtras(rendered)
	if len(extras) == 0 || strings.TrimSpace(detail) == "" {
		return OpenCodeFreeHostChannelRetry(rendered)
	}
	return "Error: " + detail + " Host tools: " + openCodeFreeHostToolList(extras) + ". Do not inspect the catalog."
}

// presentOpenCodeFreeHostCall renders a stored host call as the execute
// statement the model has to repeat. Official and mapped names stay as they
// are: those already have a direct function.
func presentOpenCodeFreeHostCall(name, args string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || !validOpenCodeFreeHostToolName(name) {
		return "", false
	}
	if openCodeFreeOfficialToolName(name) || openCodeFreeMappedHost(name) {
		return "", false
	}
	args = strings.TrimSpace(args)
	if args == "" {
		args = "{}"
	}
	// Raw statement, not a JSON-encoded arguments object. The chat body
	// escapes quotes once for transport; pre-escaping here shows \" to the
	// model, and a literal copy no longer parses.
	return "return MACLAW_CALL(" + strconv.Quote(name) + ", " + args + ")", true
}

type openCodeFreeHostCall struct {
	Name string
	Args string
}

func expandOpenCodeFreeHostCalls(call ToolCall, phase string) ([]ToolCall, bool) {
	if !strings.EqualFold(strings.TrimSpace(call.Function.Name), "execute") {
		return nil, false
	}
	code := openCodeFreeExecuteCode(call.Function.Arguments)
	parsed, ok := parseOpenCodeFreeHostCalls(code)
	if !ok || len(parsed) == 0 {
		return nil, false
	}
	kept := make([]openCodeFreeHostCall, 0, len(parsed))
	for _, item := range parsed {
		if openCodeFreeHostCallAllowed(item, phase) {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		return nil, false
	}
	out := make([]ToolCall, 0, len(kept))
	for i, item := range kept {
		next := call
		if i > 0 {
			next.ID = openCodeFreeHostCallID(call.ID, i+1)
		}
		next.Function.Name = item.Name
		next.Function.Arguments = item.Args
		out = append(out, next)
	}
	return out, true
}

func openCodeFreeHostCallsDeliver(calls []openCodeFreeHostCall, phase string) bool {
	for _, call := range calls {
		if openCodeFreeHostCallAllowed(call, phase) {
			return true
		}
	}
	return false
}

func openCodeFreeHostCallAllowed(call openCodeFreeHostCall, phase string) bool {
	if openCodeFreeArgsUsePlaceholder(call.Args) {
		return false
	}
	return !openCodeFreeGoalPhaseConflicts(phase, call)
}

func openCodeFreeGoalPhaseConflicts(phase string, call openCodeFreeHostCall) bool {
	if phase == "" || call.Name != "goal" {
		return false
	}
	action := openCodeFreeGoalAction(call.Args)
	switch phase {
	case "plan":
		return action == "step"
	case "execute":
		return action == "plan"
	default:
		return false
	}
}

func openCodeFreeGoalAction(args string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(args), &fields) != nil || fields == nil {
		return ""
	}
	if action, _ := fields["action"].(string); strings.TrimSpace(action) != "" {
		return strings.ToLower(strings.TrimSpace(action))
	}
	if _, ok := fields["steps"]; ok {
		return "plan"
	}
	if _, ok := fields["understanding"]; ok {
		return "plan"
	}
	if _, ok := fields["index"]; ok {
		return "step"
	}
	return ""
}

func openCodeFreeArgsUsePlaceholder(args string) bool {
	var value any
	if json.Unmarshal([]byte(args), &value) != nil {
		return false
	}
	return openCodeFreeValueUsesPlaceholder(value)
}

func openCodeFreeValueUsesPlaceholder(value any) bool {
	switch typed := value.(type) {
	case string:
		return openCodeFreeDotPlaceholder(typed)
	case []any:
		for _, item := range typed {
			if openCodeFreeValueUsesPlaceholder(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if openCodeFreeValueUsesPlaceholder(item) {
				return true
			}
		}
	}
	return false
}

// openCodeFreeDotPlaceholder is the example token "..." submitted as a whole
// value. A longer sentence that happens to end with an ellipsis is real text.
func openCodeFreeDotPlaceholder(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, r := range text {
		if r != '.' && r != '…' {
			return false
		}
	}
	return true
}

func openCodeFreeHostCallID(id string, n int) string {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "maclaw-call"
	}
	return id + "#" + strconv.Itoa(n)
}

func openCodeFreeExecuteCode(argsJSON string) string {
	raw := strings.TrimSpace(argsJSON)
	for i := 0; i < 3 && raw != ""; i++ {
		if strings.HasPrefix(raw, "\"") {
			var text string
			if json.Unmarshal([]byte(raw), &text) != nil {
				break
			}
			raw = strings.TrimSpace(text)
			continue
		}
		if strings.HasPrefix(raw, "{") {
			var args map[string]any
			if json.Unmarshal([]byte(raw), &args) != nil || args == nil {
				break
			}
			code, _ := args["code"].(string)
			return code
		}
		break
	}
	// Not a {"code":...} envelope. The statement may be the arguments
	// themselves (a copied replay, or tools.name(...)). The parser decides
	// whether it delivers a host tool; dropping it here would skip that.
	return raw
}

// openCodeFreeHostSupplement is the copyable call for every host extra other
// than goal, plus the argument-name line. goal already has a plan example.
// The object lists required parameters only, using a literal of the schema
// type, so a copied call is not {"argument":"value"}.
func openCodeFreeHostSupplement(tools any, extras []string) string {
	schemas := make(map[string]struct {
		props    map[string]interface{}
		required []string
	}, len(extras))
	for _, tool := range openAIChatTools(tools) {
		name, props, required := openCodeFreeFunctionSchema(tool)
		if name == "" {
			continue
		}
		schemas[name] = struct {
			props    map[string]interface{}
			required []string
		}{props, required}
	}
	examples := make([]string, 0, len(extras))
	lines := make([]string, 0, len(extras))
	for _, name := range extras {
		if name == "" || name == "goal" || !openCodeFreeHostExtraName(name) {
			continue
		}
		schema := schemas[name]
		examples = append(examples, openCodeFreeCallExample(name, schema.props, schema.required))
		if len(schema.props) > 0 {
			lines = append(lines, name+"("+openCodeFreeParamList(schema.props, schema.required)+")")
		}
		if len(examples) >= 32 {
			break
		}
	}
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	for _, example := range examples {
		b.WriteString(example)
		b.WriteString(".\n")
	}
	if len(lines) > 0 {
		sort.Strings(lines)
		b.WriteString("Argument names: ")
		b.WriteString(strings.Join(lines, "; "))
		b.WriteString(".\n")
	}
	return b.String()
}

func openCodeFreeCallExample(name string, props map[string]interface{}, required []string) string {
	var b strings.Builder
	b.WriteString("return MACLAW_CALL(")
	b.WriteString(strconv.Quote(name))
	b.WriteString(", {")
	seen := map[string]struct{}{}
	written := 0
	for _, key := range required {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok || written >= 8 {
			continue
		}
		seen[key] = struct{}{}
		if written > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(key))
		b.WriteByte(':')
		var prop any
		if props != nil {
			prop = props[key]
		}
		b.WriteString(openCodeFreeExampleLiteral(prop))
		written++
	}
	b.WriteString("})")
	return b.String()
}

func openCodeFreeExampleLiteral(prop any) string {
	switch openCodeFreeSchemaType(prop) {
	case "integer", "number":
		return "0"
	case "boolean":
		return "false"
	case "array":
		return "[]"
	case "object":
		return "{}"
	default:
		return `"..."`
	}
}

func openCodeFreeSchemaType(prop any) string {
	schema, _ := prop.(map[string]interface{})
	if schema == nil {
		return ""
	}
	switch typed := schema["type"].(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(typed))
	case []any:
		for _, item := range typed {
			text, _ := item.(string)
			text = strings.ToLower(strings.TrimSpace(text))
			if text != "" && text != "null" {
				return text
			}
		}
	}
	return ""
}

func openCodeFreeFunctionSchema(tool map[string]interface{}) (string, map[string]interface{}, []string) {
	if tool == nil {
		return "", nil, nil
	}
	fn, _ := tool["function"].(map[string]interface{})
	if fn == nil {
		fn = tool
	}
	name := strings.TrimSpace(stringValue(fn["name"]))
	params, _ := fn["parameters"].(map[string]interface{})
	if params == nil {
		return name, nil, nil
	}
	props, _ := params["properties"].(map[string]interface{})
	return name, props, openAIChatStringList(params["required"])
}

func openCodeFreeParamList(props map[string]interface{}, required []string) string {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(props))
	add := func(name string, optional bool) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok || len(names) >= 8 {
			return
		}
		seen[name] = struct{}{}
		if optional {
			name += "?"
		}
		names = append(names, name)
	}
	for _, name := range required {
		add(name, false)
	}
	rest := make([]string, 0, len(props))
	for name := range props {
		if _, ok := seen[name]; !ok {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name, true)
	}
	return strings.Join(names, ", ")
}

func parseOpenCodeFreeHostCalls(code string) ([]openCodeFreeHostCall, bool) {
	calls, _, ok := splitOpenCodeFreeHostReplay(code)
	return calls, ok
}

// splitOpenCodeFreeHostReplay parses each host-extra hit once. residue is
// the text outside those spans. A hit that does not parse fails the whole
// sentence. A later hit inside an earlier span stays in the call list and
// is left out of the residue.
func splitOpenCodeFreeHostReplay(code string) ([]openCodeFreeHostCall, string, bool) {
	hits := indexOpenCodeFreeHostCalls(code)
	if len(hits) == 0 {
		return nil, "", false
	}
	calls := make([]openCodeFreeHostCall, len(hits))
	ends := make([]int, len(hits))
	for i, at := range hits {
		if at < 0 || at > len(code) {
			return nil, "", false
		}
		call, end, parsed := parseOneOpenCodeFreeHostCallSpan(code[at:])
		if !parsed || end <= 0 || at+end > len(code) {
			return nil, "", false
		}
		calls[i] = call
		ends[i] = at + end
	}
	var b strings.Builder
	b.Grow(len(code))
	prev := 0
	for i, at := range hits {
		if at < prev {
			continue
		}
		b.WriteString(code[prev:at])
		prev = ends[i]
	}
	if prev < len(code) {
		b.WriteString(code[prev:])
	}
	return calls, b.String(), true
}

func indexOpenCodeFreeHostCalls(code string) []int {
	var hits []int
	token := openCodeFreeHostCallToken
	for i := 0; i < len(code); {
		if next, ok := skipJSCodeNoise(code, i); ok {
			if next <= i {
				i++
				continue
			}
			i = next
			continue
		}
		if i == 0 || !isJSIdentCont(code[i-1]) {
			if strings.HasPrefix(code[i:], token) {
				end := i + len(token)
				if end >= len(code) || !isJSIdentCont(code[end]) {
					hits = append(hits, i)
				}
				i = end
				continue
			}
			if at, end, ok := matchToolsHostCall(code, i); ok {
				hits = append(hits, at)
				i = end
				continue
			}
		}
		i++
	}
	return hits
}

// matchToolsHostCall recognizes tools.name( and tools["name"]( at i.
// Official and mapped names are not hits: those already have a direct
// function, and tools.opencode.* is the catalog, not a host extra.
func matchToolsHostCall(code string, i int) (int, int, bool) {
	const token = "tools"
	if !strings.HasPrefix(code[i:], token) {
		return 0, 0, false
	}
	end := i + len(token)
	if end < len(code) && isJSIdentCont(code[end]) {
		return 0, 0, false
	}
	j := skipASCIISpace(code, end)
	if j >= len(code) {
		return 0, 0, false
	}
	var name string
	switch {
	case code[j] == '.' || (code[j] == '?' && j+1 < len(code) && code[j+1] == '.'):
		if code[j] == '?' {
			j += 2
		} else {
			j++
		}
		j = skipASCIISpace(code, j)
		ident, n, ok := readJSIdent(code[j:])
		if !ok || !openCodeFreeHostExtraName(ident) {
			return 0, 0, false
		}
		name = ident
		j += n
	case code[j] == '[':
		bracket, n, ok := parseToolsBracketName(code[j:])
		if !ok || !openCodeFreeHostExtraName(bracket) {
			return 0, 0, false
		}
		name = bracket
		j += n
	default:
		return 0, 0, false
	}
	j = skipASCIISpace(code, j)
	if j >= len(code) || code[j] != '(' {
		return 0, 0, false
	}
	if name == "" {
		return 0, 0, false
	}
	return i, end, true
}

func parseToolsBracketName(src string) (string, int, bool) {
	if src == "" || src[0] != '[' {
		return "", 0, false
	}
	j := skipASCIISpace(src, 1)
	if j >= len(src) || (src[j] != '"' && src[j] != '\'') {
		return "", 0, false
	}
	decoded, n, ok := decodeJSString(src[j:])
	if !ok {
		return "", 0, false
	}
	j += n
	j = skipASCIISpace(src, j)
	if j >= len(src) || src[j] != ']' {
		return "", 0, false
	}
	return decoded, j + 1, true
}

func readJSIdent(src string) (string, int, bool) {
	if src == "" || !isJSIdentStart(src[0]) {
		return "", 0, false
	}
	j := 1
	for j < len(src) && isJSIdentCont(src[j]) {
		j++
	}
	return src[:j], j, true
}

func skipASCIISpace(src string, i int) int {
	for i < len(src) {
		switch src[i] {
		case ' ', '\n', '\r', '\t':
			i++
		default:
			return i
		}
	}
	return i
}

// parseOneOpenCodeFreeHostCallSpan returns the call and how many leading
// bytes it consumed, including the optional closing parenthesis. Residue
// stripping uses that end, so it stays the same span this parser accepts.
func parseOneOpenCodeFreeHostCallSpan(src string) (openCodeFreeHostCall, int, bool) {
	if strings.HasPrefix(src, openCodeFreeHostCallToken) {
		endTok := len(openCodeFreeHostCallToken)
		if endTok < len(src) && isJSIdentCont(src[endTok]) {
			return openCodeFreeHostCall{}, 0, false
		}
		j := skipASCIISpace(src, endTok)
		if j >= len(src) || src[j] != '(' {
			return openCodeFreeHostCall{}, 0, false
		}
		j = skipASCIISpace(src, j+1)
		name, rest, ok := parseHostCallName(src[j:])
		if !ok || !validOpenCodeFreeHostToolName(name) {
			return openCodeFreeHostCall{}, 0, false
		}
		j += len(src[j:]) - len(rest)
		j = skipASCIISpace(src, j)
		if j >= len(src) || src[j] != ',' {
			return openCodeFreeHostCall{}, 0, false
		}
		j = skipASCIISpace(src, j+1)
		call, n, ok := finishHostCallArgsSpan(name, src[j:])
		if !ok {
			return openCodeFreeHostCall{}, 0, false
		}
		j += n
		j = skipASCIISpace(src, j)
		if j < len(src) && src[j] == ')' {
			j++
		}
		return call, j, true
	}
	name, rest, ok := splitToolsHostCall(src)
	if !ok || !openCodeFreeHostExtraName(name) {
		return openCodeFreeHostCall{}, 0, false
	}
	if len(rest) > len(src) || !strings.HasSuffix(src, rest) {
		return openCodeFreeHostCall{}, 0, false
	}
	j := len(src) - len(rest)
	call, n, ok := finishHostCallArgsSpan(name, rest)
	if !ok {
		return openCodeFreeHostCall{}, 0, false
	}
	j += n
	j = skipASCIISpace(src, j)
	if j < len(src) && src[j] == ')' {
		j++
	}
	return call, j, true
}

func splitToolsHostCall(src string) (string, string, bool) {
	const token = "tools"
	if !strings.HasPrefix(src, token) {
		return "", "", false
	}
	if len(src) > len(token) && isJSIdentCont(src[len(token)]) {
		return "", "", false
	}
	rest := trimLeftSpace(src[len(token):])
	if strings.HasPrefix(rest, "?.") {
		rest = trimLeftSpace(rest[2:])
	} else if strings.HasPrefix(rest, ".") {
		rest = trimLeftSpace(rest[1:])
	} else if strings.HasPrefix(rest, "[") {
		name, n, ok := parseToolsBracketName(rest)
		if !ok {
			return "", "", false
		}
		rest = trimLeftSpace(rest[n:])
		if !strings.HasPrefix(rest, "(") {
			return "", "", false
		}
		return name, trimLeftSpace(rest[1:]), true
	} else {
		return "", "", false
	}
	name, n, ok := readJSIdent(rest)
	if !ok {
		return "", "", false
	}
	rest = trimLeftSpace(rest[n:])
	if !strings.HasPrefix(rest, "(") {
		return "", "", false
	}
	return name, trimLeftSpace(rest[1:]), true
}

func finishHostCallArgsSpan(name, rest string) (openCodeFreeHostCall, int, bool) {
	if rest == "" || (rest[0] != '{' && rest[0] != '[') {
		return openCodeFreeHostCall{}, 0, false
	}
	raw, ok := extractBalancedJSON(rest)
	if !ok {
		return openCodeFreeHostCall{}, 0, false
	}
	args, ok := jsValueToJSON(raw)
	if !ok {
		return openCodeFreeHostCall{}, 0, false
	}
	return openCodeFreeHostCall{Name: name, Args: args}, len(raw), true
}

func parseHostCallName(src string) (string, string, bool) {
	src = trimLeftSpace(src)
	if src == "" {
		return "", "", false
	}
	if src[0] == '"' {
		dec := json.NewDecoder(strings.NewReader(src))
		var name string
		if err := dec.Decode(&name); err != nil {
			return "", "", false
		}
		return name, src[int(dec.InputOffset()):], true
	}
	if src[0] != '\'' {
		return "", "", false
	}
	decoded, n, ok := decodeJSString(src)
	if !ok {
		return "", "", false
	}
	return decoded, src[n:], true
}

func validOpenCodeFreeHostToolName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 && !isJSIdentStart(c) {
			return false
		}
		if i > 0 && !isJSIdentCont(c) && c != '-' {
			return false
		}
	}
	return true
}

// openCodeFreeHostExtraName is a caller tool the official catalog cannot
// name. shell/read and the mapped Maclaw executors already have a direct
// function, so a tools.shell(...) inside execute is not a host delivery.
func openCodeFreeHostExtraName(name string) bool {
	if !validOpenCodeFreeHostToolName(name) {
		return false
	}
	if openCodeFreeOfficialToolName(name) || openCodeFreeMappedHost(name) {
		return false
	}
	return true
}

func extractBalancedJSON(src string) (string, bool) {
	if src == "" {
		return "", false
	}
	open := src[0]
	var close byte
	switch open {
	case '{':
		close = '}'
	case '[':
		close = ']'
	default:
		return "", false
	}
	depth := 0
	for i := 0; i < len(src); {
		if next, ok := skipJSCodeNoise(src, i); ok {
			i = next
			continue
		}
		switch src[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return src[:i+1], true
			}
		}
		i++
	}
	return "", false
}

func jsValueToJSON(src string) (string, bool) {
	src = strings.TrimSpace(src)
	if json.Valid([]byte(src)) {
		return compactJSON(src)
	}
	norm, ok := normalizeJSValue(src)
	if !ok || !json.Valid([]byte(norm)) {
		return "", false
	}
	return compactJSON(norm)
}

func compactJSON(src string) (string, bool) {
	var value any
	if json.Unmarshal([]byte(src), &value) != nil {
		return "", false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func normalizeJSValue(src string) (string, bool) {
	var b strings.Builder
	var stack []byte
	expectKey := false
	for i := 0; i < len(src); {
		c := src[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			b.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
			i = skipJSComment(src, i)
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			lit, n, ok := copyJSStringAsJSON(src[i:])
			if !ok {
				return "", false
			}
			b.WriteString(lit)
			expectKey = false
			i += n
			continue
		}
		if c == ',' {
			j := skipSpaceAndComments(src, i+1)
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				i++
				continue
			}
			b.WriteByte(',')
			expectKey = len(stack) > 0 && stack[len(stack)-1] == '{'
			i++
			continue
		}
		switch c {
		case '{':
			b.WriteByte(c)
			stack = append(stack, '{')
			expectKey = true
			i++
			continue
		case '[':
			b.WriteByte(c)
			stack = append(stack, '[')
			expectKey = false
			i++
			continue
		case '}', ']':
			b.WriteByte(c)
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			expectKey = false
			i++
			continue
		case ':':
			b.WriteByte(c)
			expectKey = false
			i++
			continue
		}
		if isJSIdentStart(c) {
			j := i + 1
			for j < len(src) && isJSIdentCont(src[j]) {
				j++
			}
			word := src[i:j]
			if expectKey || (word != "true" && word != "false" && word != "null") {
				b.WriteString(strconv.Quote(word))
			} else {
				b.WriteString(word)
			}
			expectKey = false
			i = j
			continue
		}
		if c >= 0x80 {
			j := i + 1
			for j < len(src) && !isBareValueStop(src[j]) {
				j++
			}
			b.WriteString(strconv.Quote(src[i:j]))
			expectKey = false
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), true
}

func copyJSStringAsJSON(src string) (string, int, bool) {
	if src != "" && src[0] == '`' {
		decoded, n, ok := decodeJSTemplate(src)
		if !ok {
			return "", 0, false
		}
		return strconv.Quote(decoded), n, true
	}
	decoded, n, ok := decodeJSString(src)
	if !ok {
		return "", 0, false
	}
	return strconv.Quote(decoded), n, true
}

// decodeJSTemplate reads one static template literal. ${} is a real
// expression; guessing its value would submit a different plan, so the
// call fails closed instead.
func decodeJSTemplate(src string) (string, int, bool) {
	if src == "" || src[0] != '`' {
		return "", 0, false
	}
	var b strings.Builder
	escape := false
	for i := 1; i < len(src); i++ {
		c := src[i]
		if escape {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(c)
			}
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '$' && i+1 < len(src) && src[i+1] == '{' {
			return "", 0, false
		}
		if c == '`' {
			return b.String(), i + 1, true
		}
		b.WriteByte(c)
	}
	return "", 0, false
}

func decodeJSString(src string) (string, int, bool) {
	if src == "" || (src[0] != '"' && src[0] != '\'') {
		return "", 0, false
	}
	quote := src[0]
	var b strings.Builder
	escape := false
	for i := 1; i < len(src); i++ {
		c := src[i]
		if escape {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if i+4 >= len(src) {
					return "", 0, false
				}
				r, err := strconv.ParseUint(src[i+1:i+5], 16, 32)
				if err != nil {
					return "", 0, false
				}
				b.WriteRune(rune(r))
				i += 4
			default:
				b.WriteByte(c)
			}
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == quote {
			return b.String(), i + 1, true
		}
		b.WriteByte(c)
	}
	return "", 0, false
}

func trimLeftSpace(s string) string {
	return strings.TrimLeft(s, " \t\r\n")
}

// skipJSCodeNoise jumps over one string, template, or comment. Those spans
// are not calls and must not end a surrounding object.
func skipJSCodeNoise(src string, i int) (int, bool) {
	if i >= len(src) {
		return i, false
	}
	switch src[i] {
	case '"', '\'':
		return skipJSString(src, i), true
	case '`':
		return skipJSTemplate(src, i), true
	}
	if src[i] == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
		return skipJSComment(src, i), true
	}
	return i, false
}

func skipJSString(src string, i int) int {
	if i >= len(src) {
		return i
	}
	quote := src[i]
	i++
	for i < len(src) {
		c := src[i]
		if c == '\\' {
			i += 2
			if i > len(src) {
				return len(src)
			}
			continue
		}
		i++
		if c == quote {
			return i
		}
	}
	return len(src)
}

func skipJSTemplate(src string, i int) int {
	if i >= len(src) || src[i] != '`' {
		return i
	}
	i++
	for i < len(src) {
		c := src[i]
		if c == '\\' {
			i += 2
			if i > len(src) {
				return len(src)
			}
			continue
		}
		if c == '`' {
			return i + 1
		}
		if c == '$' && i+1 < len(src) && src[i+1] == '{' {
			i = skipBalancedCode(src, i+1)
			continue
		}
		i++
	}
	return len(src)
}

// skipBalancedCode consumes one `{...}` span, including strings, comments,
// and nested templates inside it. A `}` in that span must not close the
// object that contains the template.
func skipBalancedCode(src string, i int) int {
	if i >= len(src) || src[i] != '{' {
		return i
	}
	depth := 0
	for i < len(src) {
		if next, ok := skipJSCodeNoise(src, i); ok {
			i = next
			continue
		}
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			i++
			if depth == 0 {
				return i
			}
			continue
		}
		i++
	}
	return len(src)
}

func skipJSComment(src string, i int) int {
	if i+1 >= len(src) || src[i] != '/' {
		if i < len(src) {
			return i + 1
		}
		return i
	}
	if src[i+1] == '/' {
		i += 2
		for i < len(src) && src[i] != '\n' {
			i++
		}
		return i
	}
	if src[i+1] != '*' {
		return i + 1
	}
	i += 2
	for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
		i++
	}
	if i+1 < len(src) {
		return i + 2
	}
	return len(src)
}

func skipSpaceAndComments(src string, i int) int {
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
			i = skipJSComment(src, i)
			continue
		}
		break
	}
	return i
}

func isBareValueStop(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', ',', ':', '{', '}', '[', ']', '/', '"', '\'', '`':
		return true
	default:
		return false
	}
}

func isJSIdentStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isJSIdentCont(c byte) bool {
	return isJSIdentStart(c) || (c >= '0' && c <= '9')
}
