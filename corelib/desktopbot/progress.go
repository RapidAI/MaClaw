package desktopbot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
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
	// A blank line separates a preamble from the card. Empty slots stay with
	// the one-line conclusion above that blank. The card is the paragraph
	// that the 文件 line closes. "1." "2." "3." are optional; the host asked
	// for three lines, then the file line.
	var paragraphs [][]string
	var cur []string
	cardPara := -1
	// A blank before a new sentence starts another paragraph. A blank before
	// 无 keeps that empty slot with the one-line conclusion above it.
	pendingBreak := false
	flush := func() {
		if len(cur) == 0 {
			return
		}
		paragraphs = append(paragraphs, cur)
		cur = nil
	}
	closeCard := func() {
		flush()
		if cardPara < 0 && len(paragraphs) > 0 {
			cardPara = len(paragraphs) - 1
		}
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			if len(cur) > 0 {
				pendingBreak = true
			}
			continue
		}
		if pendingBreak && !emptySlotContinues(cur, line) {
			flush()
		}
		pendingBreak = false
		if m := helperConclusion.FindStringSubmatch(line); m != nil {
			// The number also selects a slot. The line stays in the paragraph,
			// so "2. 无" is still the empty slot in front of 文件. Once 文件
			// has closed the card, a later number fills an empty slot and
			// does not replace a conclusion already written.
			idx := int(m[1][0] - '1')
			if cardPara < 0 || strings.TrimSpace(lines[idx]) == "" {
				lines[idx] = cleanProgressText(m[2])
			}
			cur = append(cur, line)
			continue
		}
		if m := helperFileLine.FindStringSubmatch(line); m != nil {
			// One path. Fields would split "演讲稿 完整版.docx" into two names.
			path := normalizeHelperPath(m[1])
			closeCard()
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
		cur = append(cur, line)
		leftovers = append(leftovers, line)
	}
	for _, path := range extraPaths {
		add(path)
	}
	// A failed send clears the slots before adopt. Otherwise "2. 无" would
	// already be a finished card and the failure sentence would drop out.
	cleared := false
	if cardPara >= 0 {
		cleared = applyCardParagraph(&lines, paragraphs[cardPara])
	}
	// Slot names come off before the send fold. "已完成：已发出" is the done
	// token, and "剩余：无" is an empty slot. Folding first would miss that
	// token and leave 发给 / 原文 under the leftover labels.
	stripHelperSlotRoles(&lines)
	// A 文件：无 line already makes this a card. The send facts are the
	// conclusion when they are labeled 已发出 / 发给谁 / 发出原文 and not
	// numbered. Empty 1. 2. 3. slots must not hide that report.
	// A cleared failure stays cleared: a preamble send must not refill it.
	if !cleared {
		adoptLabeledSendFacts(&lines, leftovers)
	}
	if helperSlotsNone(lines) && len(paths) == 0 {
		return "", &HelperReportError{FileMarkedNone: fileMarkedNone, Detail: helperReportDetails(leftovers)}
	}
	foldSentMessageConclusion(&lines)
	file := ProgressOpenNone
	if len(paths) > 0 {
		file = strings.Join(paths, " ")
	}
	shown := fmt.Sprintf("1. %s\n2. %s\n3. %s\n文件：%s", lines[0], lines[1], lines[2], file)
	return cleanProgressText(shown), nil
}

// foldSentMessageConclusion moves a sent-message report into the finished
// slot. The card's three lines are finished, left, and blocked. A report that
// puts the recipient on line 2 and the text on line 3 is one finished send.
func foldSentMessageConclusion(lines *[3]string) {
	if lines == nil {
		return
	}
	recipient, okRecipient := sentMessageFact(lines[1], sentMessageRecipientPrefixes...)
	body, okBody := sentMessageFact(lines[2], sentMessageBodyPrefixes...)
	if !okRecipient || !okBody || !sentMessageAlreadyDone(lines[0]) {
		return
	}
	finished := strings.TrimSpace(lines[0])
	if !finishedHasLabeledFact(finished, recipient, body) {
		finished += "；发给：" + recipient + "；原文：" + body
	}
	lines[0] = finished
	lines[1] = ProgressOpenNone
	lines[2] = ProgressOpenNone
}

// adoptLabeledSendFacts copies an unnumbered send report into the three
// slots. A recipient or a body counts only when its label is followed by a
// colon, so a sentence that merely starts with 发给 or 内容 stays prose.
// The done line is the status token at the start (已发出。, 已发出了,
// 已发送成功), with or without a colon. 已提交审核 and 已发出去 are
// different words. A leading slot name is removed first, and one line that
// already carries several labeled facts is split on ；. The longest label
// wins. All three facts must be present. A line that says the send failed
// is not a finished send. A numbered conclusion that is not a send is left
// as the helper wrote it.
func adoptLabeledSendFacts(lines *[3]string, leftovers []string) {
	if lines == nil {
		return
	}
	if strings.TrimSpace(lines[0]) != "" && !sentMessageAlreadyDone(lines[0]) {
		return
	}
	slotDone := sentMessageAlreadyDone(lines[0])
	done := ""
	if slotDone {
		done = strings.TrimSpace(lines[0])
	}
	recipient, slotRecipient := sentMessageFact(lines[1], sentMessageRecipientPrefixes...)
	body, slotBody := sentMessageFact(lines[2], sentMessageBodyPrefixes...)
	doneRank, recipientRank, bodyRank := 0, 0, 0
	for _, line := range leftovers {
		line = stripLeadingHelperSlotRoles(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		for _, part := range helperFactParts(line) {
			part = stripLeadingHelperSlotRoles(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if !slotDone {
				if rank, ok := sentMessageDoneRank(part); ok && rank > doneRank {
					done = part
					doneRank = rank
				}
			}
			if !slotRecipient {
				if fact, rank, ok := sentMessageLabel(part, sentMessageRecipientPrefixes); ok && rank > recipientRank {
					recipient = fact
					recipientRank = rank
				}
			}
			if !slotBody {
				if fact, rank, ok := sentMessageLabel(part, sentMessageBodyPrefixes); ok && rank > bodyRank {
					body = fact
					bodyRank = rank
				}
			}
		}
	}
	if done == "" || recipient == "" || body == "" {
		return
	}
	if strings.TrimSpace(lines[0]) == "" {
		lines[0] = done
	}
	if strings.TrimSpace(lines[1]) == "" {
		lines[1] = "发给：" + recipient
	}
	if strings.TrimSpace(lines[2]) == "" {
		lines[2] = "原文：" + body
	}
}

var (
	sentMessageDonePrefixes      = []string{"已经发送", "已发送", "已发出", "已提交"}
	sentMessageRecipientPrefixes = []string{"发给谁", "发给", "收件人"}
	sentMessageBodyPrefixes      = []string{"发出的原文", "发出原文", "原文", "内容"}
	helperSlotRolePrefixes       = [3][]string{
		{"已完成", "完成", "What finished"},
		{"还剩", "剩余", "剩餘", "還剩", "What is left"},
		{"卡住", "阻塞", "What is blocked"},
	}
)

// sentMessageLabel reads one label. The prefix must be the whole label:
// optional space, then a colon. 原文 does not match 原文件, and 内容 does
// not match 内容提要. The longest prefix wins.
func sentMessageLabel(line string, prefixes []string) (string, int, bool) {
	line = strings.TrimSpace(line)
	best := 0
	value := ""
	for _, prefix := range prefixes {
		rest, ok := labelRest(line, prefix)
		if !ok || len(prefix) <= best {
			continue
		}
		best = len(prefix)
		value = rest
	}
	if best == 0 {
		return "", 0, false
	}
	return value, best, true
}

func labelRest(line, prefix string) (string, bool) {
	if prefix == "" || !strings.HasPrefix(line, prefix) {
		return "", false
	}
	tail := strings.TrimLeft(line[len(prefix):], " \t\r\n　")
	if tail == "" {
		return "", false
	}
	r, size := utf8.DecodeRuneInString(tail)
	if r != ':' && r != '：' {
		return "", false
	}
	rest := strings.TrimSpace(tail[size:])
	if rest == "" || helperPathIsNone(rest) {
		return "", false
	}
	return rest, true
}

func finishedHasLabeledFact(finished, recipient, body string) bool {
	return labeledValue(finished, sentMessageRecipientPrefixes, recipient) &&
		labeledValue(finished, sentMessageBodyPrefixes, body)
}

// applyCardParagraph writes the three-line card the 文件 line closes.
// The line before the two empty slots is the conclusion and replaces an
// earlier numbered slot. A failed send clears those slots. A numbered
// conclusion stays only when its text is this card's own conclusion line.
// The bool reports that the slots were cleared.
func applyCardParagraph(lines *[3]string, para []string) bool {
	if lines == nil {
		return false
	}
	slots, ok, refused := unnumberedCardSlots(para)
	if refused {
		// Keep the numbered conclusion only when it is this card's line.
		// An earlier "1. 已发出：发送失败" is a different sentence.
		head := cardConclusion(para)
		current := stripHelperSlotRole(cardLineBody(lines[0]), helperSlotRolePrefixes[0])
		if strings.TrimSpace(current) != "" && cleanProgressText(current) == cleanProgressText(head) {
			return false
		}
		*lines = [3]string{}
		return true
	}
	if ok {
		for i := 0; i < 3; i++ {
			lines[i] = cleanProgressText(slots[i])
		}
	}
	return false
}

// unnumberedCardSlots is the three-line card the 文件 line closes.
// The host asks for a conclusion, then an empty slot, then an empty slot,
// then the file line. Numbers are optional. The two empty slots mark the
// card, so a preamble in the same paragraph is not the conclusion. An empty
// conclusion is not a card. refused is a conclusion whose status word says
// the send failed; the sentence stays in the detail.
func unnumberedCardSlots(para []string) (slots [3]string, ok bool, refused bool) {
	if len(para) < 3 || !helperSlotNone(para[len(para)-1]) || !helperSlotNone(para[len(para)-2]) {
		return slots, false, false
	}
	// The line in front of 无 is a 发给 or 原文 label, and a send fact is
	// already above it. Those 无 lines belong to that labeled report.
	if cardHasEarlierSendFact(para) {
		return slots, false, false
	}
	para = para[len(para)-3:]
	head := stripHelperSlotRole(cardLineBody(para[0]), helperSlotRolePrefixes[0])
	if helperSlotNone(head) {
		return slots, false, false
	}
	if negatedSendStatus(head) {
		return slots, false, true
	}
	for i := 0; i < 3; i++ {
		slots[i] = cardLineBody(para[i])
	}
	return slots, true, false
}

// cardConclusion is the line in front of the two empty slots.
func cardConclusion(para []string) string {
	if len(para) < 3 {
		return ""
	}
	return stripHelperSlotRole(cardLineBody(para[len(para)-3]), helperSlotRolePrefixes[0])
}

// cardHasEarlierSendFact reports that the line in front of the empty slots
// is only a 发给 or 原文 label, and a send fact already appears above it.
// The 无 lines then belong to that labeled report. A conclusion that is not
// itself a label stays the conclusion, even when an earlier line says 已发出。
func cardHasEarlierSendFact(para []string) bool {
	if len(para) < 4 {
		return false
	}
	head := cardConclusion(para)
	if _, ok := sentMessageFact(head, sentMessageRecipientPrefixes...); !ok {
		if _, ok := sentMessageFact(head, sentMessageBodyPrefixes...); !ok {
			return false
		}
	}
	for _, line := range para[:len(para)-3] {
		line = stripLeadingHelperSlotRoles(cardLineBody(line))
		if line == "" {
			continue
		}
		if sentMessageAlreadyDone(line) {
			return true
		}
		if _, ok := sentMessageFact(line, sentMessageRecipientPrefixes...); ok {
			return true
		}
		if _, ok := sentMessageFact(line, sentMessageBodyPrefixes...); ok {
			return true
		}
	}
	return false
}

// emptySlotContinues keeps 无 on the one-line conclusion above a blank.
// A paragraph that already has several lines stays separate, so a labeled
// 发给 / 原文 report is not pulled into the empty slots below it.
func emptySlotContinues(cur []string, line string) bool {
	if len(cur) == 0 || !helperSlotNone(line) {
		return false
	}
	if len(cur) == 1 {
		return true
	}
	return helperSlotNone(cur[len(cur)-1])
}

// cardLineBody drops an optional "1." "2." "3." prefix. The number is how a
// slot is chosen; it is not part of the conclusion or the empty-slot word.
func cardLineBody(line string) string {
	line = strings.TrimSpace(line)
	if m := helperConclusion.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[2])
	}
	return line
}

func helperSlotNone(line string) bool {
	line = stripLeadingHelperSlotRoles(cardLineBody(line))
	return helperPathIsNone(normalizeHelperPath(line))
}

func helperSlotsNone(lines [3]string) bool {
	for _, line := range lines {
		if !helperSlotNone(line) {
			return false
		}
	}
	return true
}

func negatedSendStatus(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || !sentMessageNegated(line) {
		return false
	}
	for _, prefix := range sentMessageDonePrefixes {
		if doneStatusAtStart(line, prefix) {
			return true
		}
	}
	return false
}

func labeledValue(line string, labels []string, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, label := range labels {
		if strings.Contains(line, label+"："+value) || strings.Contains(line, label+":"+value) {
			return true
		}
	}
	return false
}

// stripHelperSlotRoles removes a leading slot name. The card labels those
// slots again when the person reads them, so "剩余：无" is just 无.
func stripHelperSlotRoles(lines *[3]string) {
	if lines == nil {
		return
	}
	for i := range lines {
		lines[i] = stripHelperSlotRole(lines[i], helperSlotRolePrefixes[i])
	}
}

func stripHelperSlotRole(line string, roles []string) string {
	line = strings.TrimSpace(line)
	for {
		next, ok := cutHelperSlotRole(line, roles)
		if !ok {
			return line
		}
		line = next
	}
}

func cutHelperSlotRole(line string, roles []string) (string, bool) {
	for _, role := range roles {
		if role == "" || !strings.HasPrefix(line, role) {
			continue
		}
		tail := strings.TrimSpace(line[len(role):])
		if tail == "" {
			continue
		}
		r, size := utf8.DecodeRuneInString(tail)
		if r != ':' && r != '：' {
			continue
		}
		rest := strings.TrimSpace(tail[size:])
		if rest == "" {
			continue
		}
		return rest, true
	}
	return line, false
}

func sentMessageFact(line string, prefixes ...string) (string, bool) {
	rest, _, ok := sentMessageLabel(line, prefixes)
	return rest, ok
}

func sentMessageAlreadyDone(line string) bool {
	_, ok := sentMessageDoneRank(line)
	return ok
}

func sentMessageDoneRank(line string) (int, bool) {
	line = strings.TrimSpace(line)
	if line == "" || sentMessageNegated(line) {
		return 0, false
	}
	// A mention later in the sentence is not the slot. "报告已提交" stays a
	// normal conclusion. The status token has to start the line and end there:
	// 已发出, 已发出。, 已发出了, 已发送成功, 已发出：成功, or 已发出；发给：….
	// 已发出去 and 已提交给 are different words. 失败 on this fact is not a send.
	best := 0
	for _, prefix := range sentMessageDonePrefixes {
		if doneStatusAtStart(line, prefix) && len(prefix) > best {
			best = len(prefix)
		}
	}
	if best == 0 {
		return 0, false
	}
	return best, true
}

func sentMessageNegated(line string) bool {
	// Only the status clause can cancel the send. "已发出：发送失败" is a
	// failure. "已发出：消息发送成功，目标失败原因已修复" is a send; the later
	// clause is about the goal. ； stays inside the clause so "已发出；发送失败"
	// is still a failure.
	head := statusClause(line)
	return strings.Contains(head, "未发") || strings.Contains(head, "没发") || strings.Contains(head, "没有发") || strings.Contains(head, "失败")
}

func statusClause(line string) string {
	for i, r := range line {
		switch r {
		case '，', '。', '！', '？', ',', '!', '?', '\n':
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

// doneClosers finish the status word. 了/过/啦 are aspect particles and
// 成功 is the result. They are not a new word. 去 and 给 are.
var doneClosers = []string{"成功", "了", "过", "啦"}

func doneStatusAtStart(line, prefix string) bool {
	if prefix == "" || !strings.HasPrefix(line, prefix) {
		return false
	}
	tail := consumeDoneClosers(line[len(prefix):])
	if tail == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(tail)
	if r == utf8.RuneError {
		return false
	}
	return !unicode.Is(unicode.Han, r) && !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func consumeDoneClosers(tail string) string {
	for {
		trimmed := strings.TrimLeft(tail, " \t\r\n　")
		rest, ok := cutDoneCloser(trimmed)
		if !ok {
			return trimmed
		}
		tail = rest
	}
}

func cutDoneCloser(tail string) (string, bool) {
	for _, closer := range doneClosers {
		if closer != "" && strings.HasPrefix(tail, closer) {
			return tail[len(closer):], true
		}
	}
	return tail, false
}

// stripLeadingHelperSlotRoles removes a slot name from a leftover. The
// numbered slots are stripped before adopt; an unnumbered "已完成：已发出"
// or "剩余：发给：刘歌" has to lose the same name or the token stays hidden.
func stripLeadingHelperSlotRoles(line string) string {
	line = strings.TrimSpace(line)
	for {
		next := line
		for slot := range helperSlotRolePrefixes {
			cut := stripHelperSlotRole(line, helperSlotRolePrefixes[slot])
			if cut != line {
				next = cut
				break
			}
		}
		if next == line {
			return line
		}
		line = next
	}
}

// helperFactParts splits one leftover only when it carries at least two
// labeled facts. "已发出；发给：刘歌；原文：你好" is three facts. A segment
// that is not a label stays with the previous fact, so "已发出；发送失败"
// is still the status and "原文：你好；世界" is still one body.
func helperFactParts(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" || (!strings.Contains(line, "；") && !strings.Contains(line, ";")) {
		if line == "" {
			return nil
		}
		return []string{line}
	}
	type span struct{ start, end int }
	var spans []span
	start := 0
	for i, r := range line {
		if r != '；' && r != ';' {
			continue
		}
		if i > start {
			spans = append(spans, span{start, i})
		}
		start = i + utf8.RuneLen(r)
	}
	if start < len(line) {
		spans = append(spans, span{start, len(line)})
	}
	if len(spans) < 2 {
		return []string{line}
	}
	groups := []span{spans[0]}
	for _, sp := range spans[1:] {
		part := strings.TrimSpace(line[sp.start:sp.end])
		if part != "" && helperFactPart(part) {
			groups = append(groups, sp)
			continue
		}
		groups[len(groups)-1].end = sp.end
	}
	if len(groups) < 2 {
		return []string{line}
	}
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = strings.TrimSpace(line[g.start:g.end])
	}
	return out
}

func helperFactPart(line string) bool {
	line = stripLeadingHelperSlotRoles(strings.TrimSpace(line))
	if line == "" {
		return false
	}
	if sentMessageAlreadyDone(line) {
		return true
	}
	if _, ok := sentMessageFact(line, sentMessageRecipientPrefixes...); ok {
		return true
	}
	_, ok := sentMessageFact(line, sentMessageBodyPrefixes...)
	return ok
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
