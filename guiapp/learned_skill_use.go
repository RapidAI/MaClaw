package guiapp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// learnedSkillHostTaskBanners are the openings this package writes when it
// turns a task into the user message a coding turn records. The task is the
// text after the banner, not the banner.
var learnedSkillHostTaskBanners = []string{
	"请执行以下编码任务：",
	"请执行以下主机操作任务（执行命令，不要改项目代码）：",
	"请执行以下操作任务（运行/构建/演示，不要改代码）：",
}

// learnedSkillHostTrailers are the blocks this package appends after that
// task: checklists, plan-step instructions, and session continuity. They are
// instructions to the model, not the skill's identity.
var learnedSkillHostTrailers = []string{
	"**before editing**:",
	"**before finalizing**:",
	"**操作任务要求**：",
	"**当前安全兼容模式**：",
	"**前置任务上下文**",
	"**files in scope**:",
	"**验收标准**：",
	"**验收验证要求**：",
	"[session continuity",
	"session plan / overall goal:",
	"you are executing plan step ",
	"## overall user request",
	// The host block is "Plan outline (titles only; ...". The shorter
	// "plan outline " also occurs in a real task sentence.
	learnedSkillPlanOutlineMarker,
}

// learnedSkillPlanOutlineMarker is the host's titles-only plan block.
// extractPlanStepFocusFromTaskText cuts on the same marker. A task sentence
// that merely says "plan outline" is part of the task.
const learnedSkillPlanOutlineMarker = "plan outline (titles only"

func learnedSkillSource(source string) bool {
	return strings.EqualFold(strings.TrimSpace(source), "learned")
}

// learnedSkillTaskFromHostPrompt returns the task inside a recorded user
// message. A trajectory stores the prompt the host sent, which wraps the task
// in plan-step and coding-turn scaffolding. The skill's identity is that task.
// When the scaffolding is all that remains, there is no task to publish.
func learnedSkillTaskFromHostPrompt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if title, description, ok := extractPlanStepFocusFromTaskText(raw); ok {
		return cutLearnedSkillHostTrailer(joinLearnedSkillTask(title, description))
	}
	return stripLearnedSkillHostTaskBanner(cutLearnedSkillHostTrailer(raw))
}

// learnedSkillIdentityTriggers are the stored triggers that name the task.
// Legacy drafts sliced the host wrapper into triggers, so "##" and the coding
// banner match an unrelated turn and bind the skill before vector recall.
// A trigger that does not occur in the task is that wrapper. Stored triggers
// that do name the task stay. An empty trigger list stays empty: extracting
// new keywords here would turn a paraphrase into an identity hit.
// learnedSkillScoringText is the identity text of a learned skill. The stored
// description is often the host wrapper. Matching uses the task inside it,
// and only the stored triggers that occur in that task. Scaffolding with no
// task contributes nothing, so the wrapper cannot match another turn.
func learnedSkillScoringText(description string, stored []string) (string, []string) {
	task := strings.TrimSpace(learnedSkillTaskFromHostPrompt(description))
	if task == "" {
		return "", nil
	}
	return task, learnedSkillIdentityTriggers(description, stored)
}

func learnedSkillIdentityTriggers(description string, stored []string) []string {
	task := strings.TrimSpace(learnedSkillTaskFromHostPrompt(description))
	if task == "" || len(stored) == 0 {
		return nil
	}
	taskLower := strings.ToLower(task)
	kept := make([]string, 0, len(stored))
	for _, trigger := range stored {
		trigger = strings.TrimSpace(trigger)
		if trigger == "" || !tool.TriggerIsSpecific(trigger) {
			continue
		}
		if skillDocPhraseOccurs(taskLower, strings.ToLower(trigger)) {
			kept = append(kept, trigger)
		}
	}
	return kept
}

func joinLearnedSkillTask(title, description string) string {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	switch {
	case title == "":
		return description
	case description == "":
		return title
	default:
		return title + "\n" + description
	}
}

// foldASCIILower lowercases A-Z and leaves every other byte alone.
// Host markers are ASCII. strings.ToLower can change the byte length of a
// rune such as U+0130, and that index is then not an index into text.
func foldASCIILower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func cutLearnedSkillHostTrailer(text string) string {
	lower := foldASCIILower(text)
	best := -1
	for _, marker := range learnedSkillHostTrailers {
		if i := strings.Index(lower, marker); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if idx := indexOfTruncatedHostTrailer(lower); idx >= 0 && (best < 0 || idx < best) {
		best = idx
	}
	if best >= 0 {
		return strings.TrimSpace(text[:best])
	}
	return strings.TrimSpace(text)
}

// indexOfTruncatedHostTrailer catches a host trailer that the description cap
// sliced before the marker finished. The stored text then ends in "…" and the
// tail is only the start of the marker, as in "**当前…". A prose sentence that
// ends in "plan outline…" is not a marker: the prefix has to contain "**" or
// start with "[". lower must be foldASCIILower(text), so the index is valid
// in text.
func indexOfTruncatedHostTrailer(lower string) int {
	end := len(lower)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(lower[:end])
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			end -= size
			continue
		}
		break
	}
	ellipsisLen := 0
	switch {
	case strings.HasSuffix(lower[:end], "…"):
		ellipsisLen = len("…")
	case strings.HasSuffix(lower[:end], "..."):
		ellipsisLen = 3
	default:
		return -1
	}
	headEnd := end - ellipsisLen
	for headEnd > 0 {
		r, size := utf8.DecodeLastRuneInString(lower[:headEnd])
		if r == ' ' || r == '\t' {
			headEnd -= size
			continue
		}
		break
	}
	head := lower[:headEnd]
	headRunes := []rune(head)
	best := -1
	for _, marker := range learnedSkillHostTrailers {
		m := []rune(foldASCIILower(marker))
		limit := len(m)
		if limit > len(headRunes) {
			limit = len(headRunes)
		}
		for n := limit; n >= 4; n-- {
			prefix := string(m[:n])
			if !strings.Contains(prefix, "**") && !strings.HasPrefix(prefix, "[") {
				continue
			}
			if !strings.HasSuffix(head, prefix) {
				continue
			}
			idx := len(head) - len(prefix)
			if best < 0 || idx < best {
				best = idx
			}
			break
		}
	}
	return best
}

func stripLearnedSkillHostTaskBanner(text string) string {
	text = strings.TrimSpace(text)
	for _, banner := range learnedSkillHostTaskBanners {
		if strings.HasPrefix(text, banner) {
			text = strings.TrimSpace(strings.TrimPrefix(text, banner))
			break
		}
	}
	return stripLearnedSkillHostTaskHeading(text)
}

func stripLearnedSkillHostTaskHeading(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lineEnd := strings.IndexAny(text, "\r\n")
	first := text
	rest := ""
	if lineEnd >= 0 {
		first = text[:lineEnd]
		rest = strings.TrimSpace(text[lineEnd:])
	}
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, "##") {
		return text
	}
	colon := strings.Index(first, ":")
	if colon < 0 {
		return text
	}
	head := strings.TrimSpace(first[2:colon])
	if len(head) < 2 || (head[0] != 'T' && head[0] != 't') {
		return text
	}
	for _, r := range head[1:] {
		if r < '0' || r > '9' {
			return text
		}
	}
	title := strings.TrimSpace(first[colon+1:])
	return joinLearnedSkillTask(title, rest)
}

// learnedSkillMissingParameterContract reports that a learned skill still
// embeds a session operand in its procedure. Description redaction already
// classifies those operands — URL, email, Windows path, Unix path — and the
// runner only retargets {{placeholders}}. A literal left in a step would
// replay the session that produced it. Skills from any other source keep the
// procedure their author wrote.
func learnedSkillMissingParameterContract(source string, steps []corelib.NLSkillStep) bool {
	if !learnedSkillSource(source) {
		return false
	}
	for _, step := range steps {
		if learnedSkillValueHasSessionOperand(step.Params) {
			return true
		}
	}
	return false
}

// bindLearnedSkillOperands replaces session operands in a drafted procedure
// with {{placeholders}} and declares each one as a required parameter.
// Identical literals share one placeholder. A value that still matches after
// replacement is not a runnable skill.
func bindLearnedSkillOperands(steps []skill.SkillYAMLStep) ([]skill.SkillYAMLStep, []skill.SkillYAMLParam, []string, error) {
	type hit struct {
		class   string
		literal string
	}
	var hits []hit
	var walk func(interface{})
	walk = func(value interface{}) {
		switch v := value.(type) {
		case string:
			for _, span := range learnedSkillOperandSpans(v) {
				hits = append(hits, hit{class: span.class, literal: span.literal})
			}
		case []interface{}:
			for _, item := range v {
				walk(item)
			}
		case map[string]interface{}:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(v[key])
			}
		}
	}
	for i := range steps {
		walk(steps[i].Params)
	}

	names := make(map[string]string, len(hits))
	classes := make(map[string]string, len(hits))
	var order []string
	counts := map[string]int{}
	for _, item := range hits {
		if _, ok := names[item.literal]; ok {
			continue
		}
		counts[item.class]++
		name := fmt.Sprintf("%s_%d", item.class, counts[item.class])
		names[item.literal] = name
		classes[name] = item.class
		order = append(order, name)
	}
	if len(order) == 0 {
		return steps, nil, nil, nil
	}

	bound := make([]skill.SkillYAMLStep, len(steps))
	for i, step := range steps {
		replaced := bindLearnedSkillValue(step.Params, names)
		if replaced == nil {
			step.Params = nil
		} else if params, ok := replaced.(map[string]interface{}); ok {
			step.Params = params
		} else {
			return nil, nil, nil, fmt.Errorf("session operand was not bound to a parameter")
		}
		bound[i] = step
	}
	for _, step := range bound {
		if learnedSkillValueHasSessionOperand(step.Params) {
			return nil, nil, nil, fmt.Errorf("session operand was not bound to a parameter")
		}
	}
	params := make([]skill.SkillYAMLParam, len(order))
	required := make([]string, len(order))
	for i, name := range order {
		required[i] = name
		params[i] = skill.SkillYAMLParam{
			Name:        name,
			Type:        "string",
			Required:    true,
			Description: learnedOperandParamDescription(classes[name]),
		}
	}
	return bound, params, required, nil
}

// pruneLearnedSkillUnusedContract drops required arguments that no surviving
// step still references. Runner adaptation can remove the only step that used
// a placeholder; the declaration has to follow the procedure that remains.
func pruneLearnedSkillUnusedContract(draft *skill.SkillYAMLFile) {
	if draft == nil || (len(draft.RequiredArgs) == 0 && len(draft.Params) == 0) {
		return
	}
	used := map[string]struct{}{}
	for _, step := range draft.Steps {
		collectLearnedSkillPlaceholderNames(step.Params, used)
	}
	var required []string
	for _, name := range draft.RequiredArgs {
		if _, ok := used[strings.ToLower(strings.TrimSpace(name))]; ok {
			required = append(required, name)
		}
	}
	draft.RequiredArgs = required
	var params []skill.SkillYAMLParam
	for _, param := range draft.Params {
		if _, ok := used[strings.ToLower(strings.TrimSpace(param.Name))]; ok {
			params = append(params, param)
		}
	}
	draft.Params = params
}

func collectLearnedSkillPlaceholderNames(value interface{}, used map[string]struct{}) {
	switch v := value.(type) {
	case string:
		for _, key := range skill.ExtractPlaceholderKeys(v) {
			if key = strings.ToLower(strings.TrimSpace(key)); key != "" {
				used[key] = struct{}{}
			}
		}
	case []interface{}:
		for _, item := range v {
			collectLearnedSkillPlaceholderNames(item, used)
		}
	case map[string]interface{}:
		for _, item := range v {
			collectLearnedSkillPlaceholderNames(item, used)
		}
	}
}

func learnedOperandParamDescription(class string) string {
	switch class {
	case "url":
		return "URL supplied when the skill runs"
	case "email":
		return "Email address supplied when the skill runs"
	default:
		return "Path supplied when the skill runs"
	}
}

func bindLearnedSkillValue(value interface{}, names map[string]string) interface{} {
	switch v := value.(type) {
	case string:
		return bindLearnedSkillString(v, names)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = bindLearnedSkillValue(item, names)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = bindLearnedSkillValue(item, names)
		}
		return out
	default:
		return value
	}
}

func bindLearnedSkillString(text string, names map[string]string) string {
	spans := learnedSkillOperandSpans(text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, span := range spans {
		name := names[span.literal]
		if name == "" || span.start < last || span.end > len(text) {
			return text
		}
		b.WriteString(text[last:span.start])
		b.WriteString("{{")
		b.WriteString(name)
		b.WriteString("}}")
		last = span.end
	}
	b.WriteString(text[last:])
	return b.String()
}

func learnedSkillValueHasSessionOperand(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return len(learnedSkillOperandSpans(v)) > 0
	case []interface{}:
		for _, item := range v {
			if learnedSkillValueHasSessionOperand(item) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range v {
			if learnedSkillValueHasSessionOperand(item) {
				return true
			}
		}
	}
	return false
}

type learnedOperandSpan struct {
	start   int
	end     int
	class   string
	literal string
}

// learnedSkillOperandSpans finds session operands using the same classes as
// redactLearnedSkillIntent, in the same order. Text already inside {{...}} is
// a parameter, not another operand. Overlapping matches keep the earliest,
// then the longest, so a URL is not also recorded as the path inside it.
//
// The Windows-path class stops at whitespace, so a spaced path would otherwise
// be published with the tail still in the command. A match that begins a
// quoted argument is that whole argument. Only a path match absorbs the next
// token, and only when that token contains a slash, is not itself an operand,
// and does not begin another path. URL and email matches already consume
// their whole token. An unquoted "C:\Program Files" stays split: the next
// token has no slash, and a word list would be a second classifier.
func learnedSkillOperandSpans(text string) []learnedOperandSpan {
	return collectLearnedSkillOperandSpans(text, true)
}

// learnedSkillProseOperandSpans is the description form of the same classes.
// A spaced path absorbs a continuation token that contains a slash, as a
// command does. The gap may be any whitespace the gist later collapses,
// including a newline; otherwise the tail is glued back into a trigger.
// It does not expand a quoted interior: the words after a path are the task.
// Continuation stops at the punctuation the path class and the document-title
// rule already treat as a boundary, so "app，写报告" does not become part of
// the path. A command does not cross a newline: that break ends the line.
func learnedSkillProseOperandSpans(text string) []learnedOperandSpan {
	return collectLearnedSkillOperandSpans(text, false)
}

func collectLearnedSkillOperandSpans(text string, command bool) []learnedOperandSpan {
	if text == "" {
		return nil
	}
	masked := maskLearnedSkillPlaceholders(text)
	specs := []struct {
		class string
		re    *regexp.Regexp
	}{
		{"url", learnedSkillURLRe},
		{"email", learnedSkillEmailRe},
		{"path", learnedSkillWinPathRe},
		{"path", learnedSkillUnixPathRe},
	}
	var spans []learnedOperandSpan
	for _, spec := range specs {
		for _, loc := range spec.re.FindAllStringIndex(masked, -1) {
			if len(loc) != 2 || loc[0] < 0 || loc[1] <= loc[0] || loc[1] > len(text) {
				continue
			}
			spans = append(spans, expandLearnedSkillOperandSpan(text, learnedOperandSpan{
				start: loc[0], end: loc[1], class: spec.class, literal: text[loc[0]:loc[1]],
			}, command))
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	kept := spans[:0]
	last := 0
	for _, span := range spans {
		if span.start < last {
			continue
		}
		kept = append(kept, span)
		last = span.end
	}
	return kept
}

// expandLearnedSkillOperandSpan widens one regex hit to the operand the
// command actually names. Quoted arguments expand only when the hit begins
// the argument, so a path mentioned inside a larger quoted program stays the
// path. A path hit then grows by continuation tokens. URL and email hits do
// not: their class already includes every non-space character. Description
// prose passes command=false and skips the quote expansion.
func expandLearnedSkillOperandSpan(text string, span learnedOperandSpan, command bool) learnedOperandSpan {
	if span.start < 0 || span.end > len(text) || span.start >= span.end {
		return span
	}
	if command {
		if start, end, ok := learnedSkillQuotedArgument(text, span.start, span.end); ok {
			span.start = start
			span.end = end
			span.literal = text[start:end]
			return span
		}
	}
	if span.class != "path" {
		return span
	}
	end := span.end
	for {
		next, ok := learnedSkillSlashContinuation(text, end, command)
		if !ok {
			break
		}
		end = next
	}
	if end != span.end && end <= len(text) {
		span.end = end
		span.literal = text[span.start:span.end]
	}
	return span
}

// learnedSkillQuotedArgument reports the interior of the quote pair that
// contains [start, end) when that interior begins with the match. The quotes
// themselves stay in the command.
func learnedSkillQuotedArgument(text string, start, end int) (int, int, bool) {
	open := byte(0)
	openAt := -1
	for i := 0; i < len(text); i++ {
		if !learnedSkillQuoteDelim(text, i) {
			continue
		}
		c := text[i]
		if open == 0 {
			open = c
			openAt = i
			continue
		}
		if c != open {
			continue
		}
		interiorStart := openAt + 1
		interiorEnd := i
		if start >= interiorStart && end <= interiorEnd &&
			strings.Trim(text[interiorStart:start], " \t") == "" &&
			!strings.Contains(text[interiorStart:interiorEnd], "{{") {
			return interiorStart, interiorEnd, true
		}
		open = 0
		openAt = -1
	}
	return 0, 0, false
}

// learnedSkillSlashContinuation extends end across one following token when
// that token is a continuation of the same path. A token that itself matches
// an operand class is a separate value. command=false stops the token at the
// punctuation a description already treats as the end of a path or a title,
// and treats a newline as the same gap as a space. A command line stops at
// the newline.
func learnedSkillSlashContinuation(text string, end int, command bool) (int, bool) {
	if end < 0 || end >= len(text) {
		return 0, false
	}
	i := end
	if command {
		if text[end] != ' ' && text[end] != '\t' {
			return 0, false
		}
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
	} else if !learnedSkillCollapsedSpace(text[end]) {
		return 0, false
	} else {
		for i < len(text) && learnedSkillCollapsedSpace(text[i]) {
			i++
		}
	}
	if i >= len(text) {
		return 0, false
	}
	j := i
	for j < len(text) {
		if command {
			c := text[j]
			if c == ' ' || c == '\t' || c == '"' || c == '\'' {
				break
			}
			j++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[j:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if learnedSkillProsePathBreak(r) {
			break
		}
		j += size
	}
	if j == i {
		return 0, false
	}
	// A backslash immediately before the quote escapes that quote. It is not
	// the last character of the path.
	if j > i && text[j-1] == '\\' && j < len(text) && (text[j] == '"' || text[j] == '\'') {
		j--
	}
	if j == i {
		return 0, false
	}
	if !learnedSkillTokenContinuesPath(text[i:j]) {
		return 0, false
	}
	return j, true
}

// learnedSkillQuoteDelim reports a quote that opens or closes an argument.
// A double quote preceded by an odd number of backslashes is escaped, which
// is how a command embeds a quote inside a double-quoted program. A
// backslash in a Windows path does not precede a quote, so it is unaffected.
func learnedSkillQuoteDelim(text string, i int) bool {
	if i < 0 || i >= len(text) {
		return false
	}
	c := text[i]
	if c != '"' && c != '\'' {
		return false
	}
	if c != '"' {
		return true
	}
	slashes := 0
	for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
		slashes++
	}
	return slashes%2 == 0
}

// learnedSkillCollapsedSpace reports a byte the description redaction
// collapses. It is the same set as the path class's \s, so a path broken
// across a line is still one path after the line break is removed.
func learnedSkillCollapsedSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	default:
		return false
	}
}

// learnedSkillProsePathBreak reports a rune that ends a path token in a
// description. It is the path class's own terminator set, plus the quote
// marks the document-title rule uses, so a clause after the path stays the task.
func learnedSkillProsePathBreak(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '"', '\'', '，', '。', '；', '：', '、', '）', ')', ']', '】',
		'“', '”', '「', '」', '『', '』':
		return true
	default:
		return false
	}
}

// learnedSkillTokenContinuesPath reports that token is the tail of the path
// match before it. "Files\app" and ".hidden\app" continue. "..\backup",
// ".\out", "/tmp/out", and "\Windows" begin another path.
func learnedSkillTokenContinuesPath(token string) bool {
	if token == "" || strings.Contains(token, "{{") || strings.Contains(token, "}}") {
		return false
	}
	if !strings.Contains(token, `\`) && !strings.Contains(token, "/") {
		return false
	}
	if learnedSkillTokenStartsOperand(token) || learnedSkillTokenStartsAnotherPath(token) {
		return false
	}
	return true
}

func learnedSkillTokenStartsAnotherPath(token string) bool {
	if token == "" {
		return false
	}
	switch token[0] {
	case '/', '\\', '~':
		return true
	case '.':
		rest := token[1:]
		if strings.HasPrefix(rest, ".") {
			rest = rest[1:]
		}
		return rest == "" || rest[0] == '/' || rest[0] == '\\'
	default:
		return false
	}
}

func learnedSkillTokenStartsOperand(token string) bool {
	if token == "" {
		return false
	}
	for _, re := range []*regexp.Regexp{
		learnedSkillURLRe, learnedSkillEmailRe, learnedSkillWinPathRe, learnedSkillUnixPathRe,
	} {
		loc := re.FindStringIndex(token)
		if len(loc) == 2 && loc[0] == 0 {
			return true
		}
	}
	return false
}

func maskLearnedSkillPlaceholders(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		if i+1 < len(text) && text[i] == '{' && text[i+1] == '{' {
			if end := strings.Index(text[i+2:], "}}"); end >= 0 {
				n := end + 4
				for k := 0; k < n; k++ {
					b.WriteByte(' ')
				}
				i += n
				continue
			}
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}
