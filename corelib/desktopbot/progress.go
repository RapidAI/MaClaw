package desktopbot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// Progress file for one background task. The three sections are replaced.
// A write does not append a log, and the body is not a second copy of the
// conversation. The summary that points at the file is the path alone.

const (
	ProgressKindTask   = "tasks"
	ProgressKindHelper = "helpers"
	ProgressFileName   = "PROGRESS.md"
	ProgressOpenNone   = "无"

	progressWhere = "## where we are"
	progressNext  = "## next step"
	progressOpen  = "## open problems"
)

// Sections is the whole progress file. OpenProblems is 无 when nothing is unsettled.
type Sections struct {
	WhereWeAre   string
	NextStep     string
	OpenProblems string
}

// ProgressRead is what a person gets when they ask how the work is going.
// SendToUser stays empty: this read is not a new progress bubble.
type ProgressRead struct {
	Text       string
	SendToUser string
}

// ProgressCopy is the last successful write, kept on the run so a sleeping
// machine does not have to wake to answer.
type ProgressCopy struct {
	Sections Sections
	Path     string
}

var (
	untrustedFence = regexp.MustCompile(`(?s)\[untrusted [a-z]+\](.*?)\[/untrusted\]`)
	progressCopies sync.Map
)

// TaskProgressPath is the container path of the main agent's own background task.
func TaskProgressPath(taskID string) string {
	return progressCanonical(ProgressKindTask, taskID)
}

// HelperProgressPath is the container path of one helper. It is not the task file.
func HelperProgressPath(helperID string) string {
	return progressCanonical(ProgressKindHelper, helperID)
}

func progressCanonical(kind, id string) string {
	if kind != ProgressKindTask && kind != ProgressKindHelper {
		kind = ProgressKindTask
	}
	return "/home/desktop/" + kind + "/" + SanitizeTaskID(id) + "/" + ProgressFileName
}

// CleanSections drops secrets and the original text inside an untrusted fence.
func CleanSections(s Sections) Sections {
	s.WhereWeAre = cleanProgressText(s.WhereWeAre)
	s.NextStep = cleanProgressText(s.NextStep)
	s.OpenProblems = cleanProgressText(s.OpenProblems)
	if strings.TrimSpace(s.OpenProblems) == "" {
		s.OpenProblems = ProgressOpenNone
	}
	return s
}

func cleanProgressText(s string) string {
	s = stripUntrustedFences(s)
	s = ScrubSecrets(s)
	return strings.TrimSpace(s)
}

func stripUntrustedFences(s string) string {
	s = untrustedFence.ReplaceAllString(s, "")
	if i := strings.Index(s, "[untrusted "); i >= 0 {
		s = s[:i]
	}
	return s
}

// RenderProgress is the file body. The previous body is not included.
func RenderProgress(s Sections) string {
	s = CleanSections(s)
	return progressWhere + "\n" + s.WhereWeAre + "\n\n" +
		progressNext + "\n" + s.NextStep + "\n\n" +
		progressOpen + "\n" + s.OpenProblems + "\n"
}

// ParseProgress reads a file that starts with the three headings.
// A tool result that only mentions a heading later is not a progress file.
func ParseProgress(text string) (Sections, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, progressWhere) {
		return Sections{}, false
	}
	where := progressBetween(text, progressWhere, progressNext)
	next := progressBetween(text, progressNext, progressOpen)
	open := progressBetween(text, progressOpen, "")
	if strings.TrimSpace(where) == "" && strings.TrimSpace(next) == "" && strings.TrimSpace(open) == "" {
		return Sections{}, false
	}
	return Sections{WhereWeAre: where, NextStep: next, OpenProblems: open}, true
}

func progressBetween(text, heading, nextHeading string) string {
	i := strings.Index(text, heading)
	if i < 0 {
		return ""
	}
	rest := strings.TrimPrefix(text[i+len(heading):], "\n")
	if nextHeading != "" {
		if j := strings.Index(rest, nextHeading); j >= 0 {
			rest = rest[:j]
		}
	}
	return strings.TrimSpace(rest)
}

// ApplyToolReturn replaces the three sections from a tool result.
// A result that is itself a progress file replaces all three.
// Any other result replaces where we are and keeps the previous next step.
func ApplyToolReturn(prev Sections, toolName, body string) Sections {
	if parsed, ok := ParseProgress(body); ok {
		return CleanSections(parsed)
	}
	prev = CleanSections(prev)
	prev.WhereWeAre = toolProgressLine(toolName, body)
	if strings.TrimSpace(prev.OpenProblems) == "" {
		prev.OpenProblems = ProgressOpenNone
	}
	return prev
}

func toolProgressLine(name, body string) string {
	body = cleanProgressText(body)
	if i := strings.IndexAny(body, "\r\n"); i >= 0 {
		body = body[:i]
	}
	body = strings.Join(strings.Fields(body), " ")
	name = strings.TrimSpace(name)
	line := body
	if name != "" && body != "" {
		line = name + ": " + body
	} else if line == "" {
		line = name
	}
	if utf8.RuneCountInString(line) > 200 {
		line = string([]rune(line)[:200])
	}
	return strings.TrimSpace(line)
}

// WriteProgressFile replaces the file under root/kind/id. The bytes are the
// three sections only. root is the host directory; the canonical path is the
// container path and is not a second file in the GUI data directory.
func WriteProgressFile(root, kind, taskID string, s Sections) (string, Sections, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", Sections{}, fmt.Errorf("progress directory is empty")
	}
	if kind != ProgressKindTask && kind != ProgressKindHelper {
		return "", Sections{}, fmt.Errorf("progress kind is invalid")
	}
	cleaned := CleanSections(s)
	dir := filepath.Join(root, kind, SanitizeTaskID(taskID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Sections{}, err
	}
	host := filepath.Join(dir, ProgressFileName)
	if err := os.WriteFile(host, []byte(RenderProgress(cleaned)), 0o644); err != nil {
		return "", Sections{}, err
	}
	return host, cleaned, nil
}

// RememberProgress stores the three sections on the run after a successful write.
func RememberProgress(taskID string, s Sections, path string) {
	id := SanitizeTaskID(taskID)
	progressCopies.Store(id, ProgressCopy{Sections: CleanSections(s), Path: strings.TrimSpace(path)})
}

// RecallProgress returns the last successful copy. A missing copy means this
// process has not written the file yet.
func RecallProgress(taskID string) (ProgressCopy, bool) {
	value, ok := progressCopies.Load(SanitizeTaskID(taskID))
	if !ok {
		return ProgressCopy{}, false
	}
	copy, ok := value.(ProgressCopy)
	return copy, ok
}

// ResetProgressCopies drops the in-process run copies. Tests use it.
func ResetProgressCopies() {
	progressCopies.Range(func(key, _ any) bool {
		progressCopies.Delete(key)
		return true
	})
}

// ResumeFromNextStep is the continuation after sleep, summary, or interrupted.
// The file wins when it is in hand. Otherwise the run copy is the next step.
func ResumeFromNextStep(fileText string, copy Sections) string {
	if parsed, ok := ParseProgress(fileText); ok && strings.TrimSpace(parsed.NextStep) != "" {
		return strings.TrimSpace(parsed.NextStep)
	}
	return strings.TrimSpace(CleanSections(copy).NextStep)
}

// ProgressSummary is the only text a summary stores for this file: the path.
func ProgressSummary(path string) string {
	return strings.TrimSpace(path)
}

// ReadProgress answers a progress question from the file when the machine is
// awake, and from the run copy when it is asleep. It does not set send_to_user.
func ReadProgress(fileText string, copy Sections, awake bool) ProgressRead {
	chosen := copy
	if awake {
		if parsed, ok := ParseProgress(fileText); ok {
			chosen = parsed
		}
	}
	chosen = CleanSections(chosen)
	return ProgressRead{Text: chosen.WhereWeAre + "\n" + chosen.NextStep + "\n" + chosen.OpenProblems}
}

var (
	helperConclusion = regexp.MustCompile(`^\s*([123])[\.、]\s*(.*)$`)
	helperFileLine   = regexp.MustCompile(`^\s*文件\s*[:：]\s*(.*)$`)
	helperHomePath   = regexp.MustCompile(`/home/desktop/\S+`)
	// Hyphens stay in the token. ScrubSecrets stops at the first one, so
	// sk-live-should-not-show would otherwise remain and the whole reason would be dropped.
	// The boundary keeps "ask-user" intact.
	helperDetailSecret = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9_-]{4,}|\bbearer\s+\S+`)
	helperTraceToken   = regexp.MustCompile(`TRACE_[A-Za-z0-9_-]+`)
)

// HelperReport reports whether body is the helper card.
// The card is a 文件： line, or at least two of the three conclusion lines.
// One numbered line in a normal answer is not a card.
func HelperReport(body string) bool {
	var seen [3]bool
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if helperFileLine.MatchString(line) {
			return true
		}
		if m := helperConclusion.FindStringSubmatch(line); m != nil {
			seen[m[1][0]-'1'] = true
		}
	}
	n := 0
	for _, ok := range seen {
		if ok {
			n++
		}
	}
	return n >= 2
}

// HelperReportError is a helper card that named nothing the person can use.
// Error() is a stable record the UI localizes. It names which parts were
// empty and, when the helper wrote one, the scrubbed reason. A tool trace
// and a secret stay out of that text.
type HelperReportError struct {
	FileMarkedNone bool
	Detail         string
}

func (e *HelperReportError) Error() string {
	file := "missing"
	if e != nil && e.FileMarkedNone {
		file = "none"
	}
	out := "helper_report_empty\nfile: " + file
	if e != nil {
		if detail := strings.TrimSpace(e.Detail); detail != "" {
			out += "\ndetail:\n" + detail
		}
	}
	return out
}

// CollapseHelperReport keeps three conclusion lines and the named paths.
// Tool traces stay out of the text. An empty conclusion and no path is a failure.
func CollapseHelperReport(body string, extraPaths []string) (string, error) {
	var lines [3]string
	var paths []string
	fileMarkedNone := false
	seen := map[string]struct{}{}
	add := func(path string) {
		path = normalizeHelperPath(path)
		if helperPathIsNone(path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	var leftovers []string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if m := helperConclusion.FindStringSubmatch(line); m != nil {
			lines[m[1][0]-'1'] = cleanProgressText(m[2])
			continue
		}
		if m := helperFileLine.FindStringSubmatch(line); m != nil {
			// One path. Fields would split "演讲稿 完整版.docx" into two names.
			path := normalizeHelperPath(m[1])
			if helperPathIsNone(path) {
				fileMarkedNone = true
				continue
			}
			add(path)
			continue
		}
		for _, path := range helperHomePath.FindAllString(line, -1) {
			add(path)
		}
		if line != "" {
			leftovers = append(leftovers, line)
		}
	}
	for _, path := range extraPaths {
		add(path)
	}
	if strings.TrimSpace(lines[0]+lines[1]+lines[2]) == "" && len(paths) == 0 {
		return "", &HelperReportError{FileMarkedNone: fileMarkedNone, Detail: helperReportDetails(leftovers)}
	}
	file := ProgressOpenNone
	if len(paths) > 0 {
		file = strings.Join(paths, " ")
	}
	shown := fmt.Sprintf("1. %s\n2. %s\n3. %s\n文件：%s", lines[0], lines[1], lines[2], file)
	return cleanProgressText(shown), nil
}

// normalizeHelperPath is the path a file line contributes. Punctuation wrapped
// around 无 still counts as no path. A quote around the whole name is not part of it.
func normalizeHelperPath(path string) string {
	return strings.Trim(strings.TrimSpace(cleanProgressText(path)), "，。,;；\"'`")
}

func helperPathIsNone(path string) bool {
	switch strings.ToLower(strings.TrimSpace(path)) {
	case "", ProgressOpenNone, "無", "none":
		return true
	default:
		return false
	}
}

// helperReportDetails is the reason text for a failed card. It runs only
// after the card has already failed. A long log is read from the end.
// Prompt echoes keep the text after the prompt. Status lines are dropped.
// The helper's own sentences are the report. A shell diagnostic such as
// FullyQualifiedErrorId is shown only when those sentences never say what failed.
// A sentence that also mentioned a secret keeps the rest.
func helperReportDetails(lines []string) string {
	const window = 40
	if len(lines) > window {
		lines = lines[len(lines)-window:]
	}
	var sentences, diagnostics []string
	for _, line := range lines {
		line = helperReportStripPrompt(line)
		if line == "" {
			continue
		}
		text := helperReportDetailLine(line)
		if text == "" || helperReportStack(text) || helperReportChatter(text) {
			continue
		}
		if helperReportProse(text) {
			sentences = append(sentences, text)
			continue
		}
		diagnostics = append(diagnostics, text)
	}
	if len(sentences) == 0 {
		if len(diagnostics) > 4 {
			diagnostics = diagnostics[len(diagnostics)-4:]
		}
		return joinHelperReportDetail(diagnostics)
	}
	var earlier string
	if len(sentences) > 4 {
		for _, line := range sentences[:len(sentences)-4] {
			if helperReportFailureLine(line) {
				earlier = line
			}
		}
		sentences = sentences[len(sentences)-4:]
	}
	if earlier == "" && !helperReportHasFailure(sentences) && len(diagnostics) > 0 {
		earlier = diagnostics[len(diagnostics)-1]
	}
	if earlier != "" && !helperReportHasFailure(sentences) {
		sentences = append([]string{earlier}, sentences...)
		if len(sentences) > 4 {
			sentences = append([]string{sentences[0]}, sentences[len(sentences)-3:]...)
		}
	}
	return joinHelperReportDetail(sentences)
}

func helperReportHasFailure(lines []string) bool {
	for _, line := range lines {
		if helperReportFailureLine(line) {
			return true
		}
	}
	return false
}

// helperReportStripPrompt keeps what a person typed or what the shell printed
// after a prompt. A second prompt on the same line is removed too.
// The prompt itself is not the reason.
func helperReportStripPrompt(s string) string {
	s = strings.TrimSpace(s)
	for {
		next := helperReportStripPromptOnce(s)
		if next == s {
			return s
		}
		s = strings.TrimSpace(next)
		if s == "" {
			return ""
		}
	}
}

func helperReportStripPromptOnce(s string) string {
	switch {
	case strings.HasPrefix(s, "PS>"), strings.HasPrefix(s, "PS ") && strings.Contains(s, ">"):
		// "PS the policy blocked it." is a sentence. Only a prompt has ">".
		if i := strings.IndexByte(s, '>'); i >= 0 {
			return strings.TrimSpace(s[i+1:])
		}
		return s
	case strings.HasPrefix(s, ">>>"):
		return strings.TrimSpace(strings.TrimPrefix(s, ">>>"))
	case strings.HasPrefix(s, "$ "):
		return strings.TrimSpace(strings.TrimPrefix(s, "$ "))
	case helperReportCmdPrompt(s):
		i := strings.IndexByte(s, '>')
		return strings.TrimSpace(s[i+1:])
	default:
		return s
	}
}

// helperReportCmdPrompt is a cmd.exe echo such as "C:\Users\ma>". A path with
// no ">" is a file name and stays.
func helperReportCmdPrompt(s string) bool {
	if len(s) < 4 || s[1] != ':' || (s[2] != '\\' && s[2] != '/') {
		return false
	}
	c := s[0]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return false
	}
	return strings.IndexByte(s, '>') > 2
}

func helperReportFailureLine(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{"失败", "错误", "无法", "阻止", "异常", "退出码", "error", "failed", "failure", "exception", "denied", "exit code"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// helperReportProse is a sentence the helper wrote, as opposed to a status echo.
// A dotted identifier such as System.Foo.Bar() is not a sentence, and neither
// is a file name. 演讲稿.docx has no sentence punctuation and is not the reason.
func helperReportProse(text string) bool {
	if helperReportStack(text) || helperReportPathLine(text) {
		return false
	}
	han := false
	for _, r := range text {
		if r > 127 {
			han = true
			break
		}
	}
	// 下载失败：策略阻止 and 执行策略阻止了下载，安装包没有落盘 are sentences.
	// A file name has none of these marks and no space.
	if han {
		return strings.ContainsAny(text, "。！？!?，、：:") || strings.Contains(text, " ")
	}
	if !strings.ContainsAny(text, ".!?") {
		return false
	}
	return len(strings.Fields(text)) >= 2
}

// helperReportPathLine is a line that is only a path. A sentence that mentions
// a path, such as 无法写入 C:\Users\ma139\Downloads, stays a sentence.
func helperReportPathLine(text string) bool {
	s := strings.Trim(strings.TrimSpace(text), "\"'`")
	// ASCII ":" is the drive separator in C:\... Fullwidth ： is a sentence.
	if s == "" || strings.ContainsAny(s, "。！？!?，、：") {
		return false
	}
	if strings.HasPrefix(s, "/home/") || strings.HasPrefix(s, `\\`) {
		return true
	}
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		c := s[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}

// helperReportStack is a call frame or a PowerShell location line.
// The dots in the type name are not sentence punctuation.
func helperReportStack(text string) bool {
	s := strings.TrimSpace(text)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "at line:") {
		return true
	}
	// at System.Foo.Bar() has a dot in the token before "(".
	// "at the end the download failed (policy)." does not.
	if !strings.HasPrefix(lower, "at ") || !strings.Contains(s, "(") {
		return false
	}
	rest := strings.TrimSpace(s[len("at "):])
	paren := strings.IndexByte(rest, '(')
	return paren > 0 && strings.Contains(rest[:paren], ".")
}

func helperReportChatter(text string) bool {
	return !helperReportFailureLine(text) && !helperReportProse(text)
}

// helperReportDetailLine keeps one human sentence from a failed card.
// A trace marker stays out. A secret is removed and the rest of the sentence stays.
func helperReportDetailLine(raw string) string {
	traced := strings.Contains(raw, "TRACE_")
	if traced {
		raw = helperTraceToken.ReplaceAllString(raw, " ")
	}
	cleaned := cleanProgressText(raw)
	cleaned = strings.ReplaceAll(cleaned, "sk-<redacted>", "")
	cleaned = strings.ReplaceAll(cleaned, "Bearer <redacted>", "")
	cleaned = strings.ReplaceAll(cleaned, "<redacted>", "")
	cleaned = helperDetailSecret.ReplaceAllString(cleaned, "")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if !helperReportKeepDetail(cleaned, traced) {
		return ""
	}
	if path := strings.TrimSpace(helperHomePath.FindString(cleaned)); path != "" && path == cleaned {
		return ""
	}
	return cleaned
}

func helperReportKeepDetail(cleaned string, traced bool) bool {
	if cleaned == "" || helperPathIsNone(cleaned) || (traced && helperReportTooThin(cleaned)) {
		return false
	}
	if strings.Contains(cleaned, "TRACE_") {
		return false
	}
	// "ask-user" contains the letters sk-. Match the token, not that substring.
	if helperDetailSecret.MatchString(cleaned) {
		return false
	}
	return true
}

// helperReportTooThin drops a leftover that is only a short English word,
// such as the "only" left after a trace token is removed.
func helperReportTooThin(text string) bool {
	for _, r := range text {
		if r > 127 {
			return false
		}
	}
	return len(strings.Fields(text)) == 1 && len(text) <= 4
}

func joinHelperReportDetail(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if utf8.RuneCountInString(text) > 240 {
		text = strings.TrimSpace(string([]rune(text)[:240])) + "…"
	}
	return text
}

// HelperStartEntries is the helper window at the start of a workflow.
// A new id is empty. The same id keeps its own entries. The main session is not copied.
func HelperStartEntries(_ []string, own []string, sameID bool) []string {
	if !sameID {
		return nil
	}
	return append([]string(nil), own...)
}
