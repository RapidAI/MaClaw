package agent

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// HandoffRecoveryMarker is the same literal as guiapp's
// codingSubAgentRecoveryMarker. corelib must not import guiapp. A field that
// is only this marker is empty: there is no file behind a measurement.
const HandoffRecoveryMarker = "…（已截断）"

// HandoffInlineCutMarker is the cut written into a kept tool tail when a
// real toolresult handle remains. A memo field that is only this marker is
// empty. Path bullets may still carry HandoffRecoveryMarker.
const HandoffInlineCutMarker = "…(截断)…"

const (
	handoffOpen  = "<handoff-memo>"
	handoffClose = "</handoff-memo>"

	handoffTaskRunes       = 600
	handoffProgressRunes   = 1400
	handoffFilesRunes      = 900
	handoffNextRunes       = 300
	handoffDeadRunes       = 600
	handoffDeadMax         = 8
	handoffDeadLine        = 160
	handoffShelfRunes      = 400
	handoffShelfMax        = 8
	handoffPathRunes       = 180
	handoffReadMax         = 30
	handoffModifiedMax     = 30
	handoffCreatedMax      = 20
	handoffRenderRunes     = 5000
	handoffAttemptRunes    = 100
	handoffOutcomeRunes    = 120
	handoffOutcomeKeyRunes = 80
	handoffVerifyRunes     = 200

	handoffStripTask     = 200
	handoffStripProgress = 900
	handoffStripFiles    = 300
	handoffStripNext     = 160
	handoffStripDead     = 240
	handoffStripDeadMax  = 2
	handoffStripShelf    = 200
	handoffStripShelfMax = 3

	handoffRemoteBodyRunes = 400

	sessionRoundUserPrefix = "[系统续轮]"
)

// HandoffMemo is the only continuity note the next model call is given for
// covered history. File and tool bodies stay on the shelf.
type HandoffMemo struct {
	Task          string
	Progress      string
	FilesRead     []string
	FilesModified []string
	FilesCreated  []string
	Next          string
	DeadEnds      []DeadEnd
	ShelfHandles  []ShelfHandle
}

// DeadEnd is one already-tried attempt. Outcome is one line, never a log.
type DeadEnd struct {
	Attempt string
	Outcome string
	Tool    string
	Path    string
}

// ShelfHandle names a spilled body the next shift may reopen. Note is one
// line and is never the body.
type ShelfHandle struct {
	Kind   string
	ID     string
	Path   string
	Offset int
	Limit  int
	Note   string
}

// HandoffCommand is a structured tool or shell result. User prose is not a
// command, so a sentence that says not to rerun does not become a dead end.
type HandoffCommand struct {
	Tool      string
	Path      string
	Command   string
	Succeeded bool
	Outcome   string
}

// HandoffFill is the deterministic input. UserTexts are chronological,
// oldest first. Prior is the previous memo, if one was already on the table.
type HandoffFill struct {
	UserTexts   []string
	Read        []string
	Modified    []string
	Created     []string
	Commands    []HandoffCommand
	Prior       HandoffMemo
	Next        string
	Measurement string
	Verify      string
	Handles     []ShelfHandle
	Strip       bool
}

// HandoffMemoMode is off, coding, or all. Unset and any other value are off.
func HandoffMemoMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_HANDOFF_MEMO"))) {
	case "coding", "all":
		return strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_HANDOFF_MEMO")))
	default:
		return "off"
	}
}

// HandoffMemoCoding reports whether the coding loop, plan step, and resume
// replay switch are on. Flag all includes this.
func HandoffMemoCoding() bool {
	mode := HandoffMemoMode()
	return mode == "coding" || mode == "all"
}

// HandoffMemoAll reports whether chat, TUI, and WeChat trim also switch.
func HandoffMemoAll() bool {
	return HandoffMemoMode() == "all"
}

// HandoffMemoPerRound is the extreme per-tool-round clear. It stays off
// unless the main flag is coding or all and MACLAW_CODING_MEMO_PER_ROUND is
// 1, on, or true.
func HandoffMemoPerRound() bool {
	if !HandoffMemoCoding() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_CODING_MEMO_PER_ROUND"))) {
	case "1", "on", "true":
		return true
	default:
		return false
	}
}

// StripOneHandoffRecoveryMarker removes one trailing recovery marker.
// A second copy, and the inline cut marker, stay.
func StripOneHandoffRecoveryMarker(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || !strings.HasSuffix(s, HandoffRecoveryMarker) {
		return s
	}
	return strings.TrimSpace(strings.TrimSuffix(s, HandoffRecoveryMarker))
}

// HandoffTextEmpty reports whether a field must not count as load-bearing.
// A path bullet that still names a file is not empty.
func HandoffTextEmpty(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == HandoffRecoveryMarker || s == HandoffInlineCutMarker {
		return true
	}
	stripped := StripOneHandoffRecoveryMarker(s)
	return stripped == "" || stripped == HandoffRecoveryMarker || stripped == HandoffInlineCutMarker
}

// Usable is false when task, progress, next, files, and dead ends are all
// empty. Shelf handles alone do not make a memo a shift note.
func (m HandoffMemo) Usable() bool {
	if !HandoffTextEmpty(m.Task) || !HandoffTextEmpty(m.Progress) || !HandoffTextEmpty(m.Next) {
		return true
	}
	for _, path := range m.FilesRead {
		if !HandoffTextEmpty(path) {
			return true
		}
	}
	for _, path := range m.FilesModified {
		if !HandoffTextEmpty(path) {
			return true
		}
	}
	for _, path := range m.FilesCreated {
		if !HandoffTextEmpty(path) {
			return true
		}
	}
	for _, end := range m.DeadEnds {
		if !HandoffTextEmpty(deadLine(end)) {
			return true
		}
	}
	return false
}

// LengthNote is safe to log. It has sizes, not the progress body.
func (m HandoffMemo) LengthNote() string {
	return "task=" + strconv.Itoa(runeLen(m.Task)) +
		" progress=" + strconv.Itoa(runeLen(m.Progress)) +
		" files=" + strconv.Itoa(m.fileCount()) +
		" next=" + strconv.Itoa(runeLen(m.Next)) +
		" dead=" + strconv.Itoa(len(m.DeadEnds)) +
		" shelf=" + strconv.Itoa(len(m.ShelfHandles)) +
		" usable=" + strconv.FormatBool(m.Usable())
}

func (m HandoffMemo) fileCount() int {
	return len(m.FilesRead) + len(m.FilesModified) + len(m.FilesCreated)
}

// FillHandoffMemo builds one capped memo from anchors and the previous memo.
func FillHandoffMemo(in HandoffFill) HandoffMemo {
	memo := HandoffMemo{}
	if task := packTaskNewestFirst(in.UserTexts, taskCap(in.Strip)); !HandoffTextEmpty(task) {
		memo.Task = task
	} else if !HandoffTextEmpty(in.Prior.Task) {
		memo.Task = headOrTail(in.Prior.Task, taskCap(in.Strip), false)
	}
	memo.FilesRead = capPaths(mergePathLists(in.Prior.FilesRead, in.Read), handoffReadMax, handoffPathRunes)
	memo.FilesModified = capPaths(mergePathLists(in.Prior.FilesModified, in.Modified), handoffModifiedMax, handoffPathRunes)
	memo.FilesCreated = capPaths(mergePathLists(in.Prior.FilesCreated, in.Created), handoffCreatedMax, handoffPathRunes)
	memo.fitFileBudget(fileCap(in.Strip))

	progress := assembleProgress(in)
	if HandoffTextEmpty(progress) {
		progress = in.Prior.Progress
	}
	memo.Progress = tailCut(strings.TrimSpace(progress), progressCap(in.Strip))
	if HandoffTextEmpty(memo.Progress) {
		memo.Progress = ""
	}

	next := strings.TrimSpace(in.Next)
	if HandoffTextEmpty(next) {
		next = in.Prior.Next
	}
	memo.Next = headCut(strings.TrimSpace(next), nextCap(in.Strip))
	if HandoffTextEmpty(memo.Next) {
		memo.Next = ""
	}

	memo.DeadEnds = capDeadEnds(mergeDeadEnds(in.Prior.DeadEnds, deadEndsFromCommands(in.Commands)), deadCap(in.Strip), deadMax(in.Strip))
	memo.ShelfHandles = capHandles(mergeHandles(in.Prior.ShelfHandles, in.Handles), shelfCap(in.Strip), shelfMax(in.Strip))
	memo.shrinkToRenderCap()
	return memo
}

func taskCap(strip bool) int {
	if strip {
		return handoffStripTask
	}
	return handoffTaskRunes
}

func progressCap(strip bool) int {
	if strip {
		return handoffStripProgress
	}
	return handoffProgressRunes
}

func fileCap(strip bool) int {
	if strip {
		return handoffStripFiles
	}
	return handoffFilesRunes
}

func nextCap(strip bool) int {
	if strip {
		return handoffStripNext
	}
	return handoffNextRunes
}

func deadCap(strip bool) int {
	if strip {
		return handoffStripDead
	}
	return handoffDeadRunes
}

func deadMax(strip bool) int {
	if strip {
		return handoffStripDeadMax
	}
	return handoffDeadMax
}

func shelfCap(strip bool) int {
	if strip {
		return handoffStripShelf
	}
	return handoffShelfRunes
}

func shelfMax(strip bool) int {
	if strip {
		return handoffStripShelfMax
	}
	return handoffShelfMax
}

func headOrTail(s string, n int, tail bool) string {
	if tail {
		return tailCut(s, n)
	}
	return headCut(s, n)
}

func assembleProgress(in HandoffFill) string {
	var parts []string
	if m := strings.TrimSpace(in.Measurement); !HandoffTextEmpty(m) {
		parts = append(parts, m)
	}
	if len(in.Modified) > 0 {
		parts = append(parts, in.Modified[len(in.Modified)-1])
	}
	verify := strings.TrimSpace(in.Verify)
	if HandoffTextEmpty(verify) {
		verify = verifyFromCommands(in.Commands)
	}
	if !HandoffTextEmpty(verify) {
		parts = append(parts, headCut(verify, handoffVerifyRunes))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func verifyFromCommands(commands []HandoffCommand) string {
	for i := len(commands) - 1; i >= 0; i-- {
		cmd := commands[i]
		if !cmd.Succeeded {
			continue
		}
		line := strings.TrimSpace(cmd.Command)
		if line == "" {
			line = strings.TrimSpace(cmd.Tool)
		}
		if line == "" {
			continue
		}
		return line + " exit 0"
	}
	return ""
}

func packTaskNewestFirst(lines []string, max int) string {
	if max <= 0 {
		return ""
	}
	var newestFirst []string
	used := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := handoffOneLine(lines[i])
		if line == "" || corelib.IsSyntheticUserContent(line) {
			continue
		}
		if strings.Contains(line, handoffOpen) || strings.HasPrefix(line, "[上下文恢复]") || strings.HasPrefix(line, "[对话历史摘要]") {
			continue
		}
		if HandoffTextEmpty(line) {
			continue
		}
		sep := 0
		if len(newestFirst) > 0 {
			sep = 1
		}
		if used+sep+runeLen(line) > max {
			if len(newestFirst) > 0 {
				break
			}
			line = tailCut(line, max)
			if HandoffTextEmpty(line) {
				break
			}
		}
		newestFirst = append(newestFirst, line)
		used += sep + runeLen(line)
	}
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return strings.Join(newestFirst, "\n")
}

func mergePathLists(parts ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, path := range part {
			path = strings.TrimSpace(path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func capPaths(paths []string, maxCount, lineRunes int) []string {
	var out []string
	for _, path := range paths {
		path = capPathBullet(path, lineRunes)
		if path == "" {
			continue
		}
		out = append(out, path)
	}
	if maxCount > 0 && len(out) > maxCount {
		out = out[len(out)-maxCount:]
	}
	return out
}

func capPathBullet(path string, lineRunes int) string {
	path = strings.TrimSpace(path)
	if path == "" || lineRunes <= 0 {
		return ""
	}
	if runeLen(path) <= lineRunes {
		return path
	}
	marker := HandoffRecoveryMarker
	room := lineRunes - runeLen(marker)
	if room < 1 {
		return headCut(path, lineRunes)
	}
	return headCut(path, room) + marker
}

func (m *HandoffMemo) fitFileBudget(budget int) {
	if budget <= 0 {
		return
	}
	for m.fileRunes() > budget {
		switch {
		case len(m.FilesRead) > 0:
			m.FilesRead = m.FilesRead[1:]
		case len(m.FilesModified) > 0:
			m.FilesModified = m.FilesModified[1:]
		case len(m.FilesCreated) > 1:
			m.FilesCreated = m.FilesCreated[1:]
		default:
			return
		}
	}
}

func (m HandoffMemo) fileRunes() int {
	n := 0
	for _, path := range m.FilesRead {
		n += runeLen(path)
	}
	for _, path := range m.FilesModified {
		n += runeLen(path)
	}
	for _, path := range m.FilesCreated {
		n += runeLen(path)
	}
	return n
}

func deadEndsFromCommands(commands []HandoffCommand) []DeadEnd {
	var raw []DeadEnd
	for _, cmd := range commands {
		if cmd.Succeeded {
			continue
		}
		attempt := strings.TrimSpace(cmd.Command)
		if attempt == "" {
			attempt = strings.TrimSpace(cmd.Tool)
		}
		attempt = headCut(attempt, handoffAttemptRunes)
		outcome := firstLine(cmd.Outcome)
		if outcome == "" {
			outcome = "exit != 0"
		}
		outcome = headCut(outcome, handoffOutcomeRunes)
		raw = append(raw, DeadEnd{
			Attempt: attempt,
			Outcome: outcome,
			Tool:    strings.TrimSpace(cmd.Tool),
			Path:    strings.TrimSpace(cmd.Path),
		})
	}
	// Snapshot keys before the repeat prefix mutates Outcome. The key is the
	// first 80 runes of the original outcome, so a later lookup on the
	// prefixed text would miss the count.
	keys := make([]string, len(raw))
	counts := map[string]int{}
	for i := range raw {
		keys[i] = deadKey(raw[i])
		counts[keys[i]]++
	}
	for i := range raw {
		if counts[keys[i]] < 2 {
			continue
		}
		if strings.HasPrefix(raw[i].Outcome, "不要用同一方式再试") {
			continue
		}
		raw[i].Outcome = headCut("不要用同一方式再试 "+raw[i].Outcome, handoffOutcomeRunes)
	}
	return raw
}

func deadKey(end DeadEnd) string {
	return end.Tool + "\x00" + end.Path + "\x00" + headCut(end.Outcome, handoffOutcomeKeyRunes)
}

func mergeDeadEnds(prior, next []DeadEnd) []DeadEnd {
	seen := map[string]bool{}
	var ends []DeadEnd
	add := func(end DeadEnd) {
		end.Attempt = strings.TrimSpace(end.Attempt)
		end.Outcome = strings.TrimSpace(end.Outcome)
		if end.Attempt == "" && end.Outcome == "" {
			return
		}
		key := end.Attempt + "\x00" + end.Outcome
		if seen[key] {
			return
		}
		seen[key] = true
		ends = append(ends, end)
	}
	for _, end := range prior {
		add(end)
	}
	for _, end := range next {
		add(end)
	}
	return ends
}

func capDeadEnds(ends []DeadEnd, budget, maxCount int) []DeadEnd {
	var out []DeadEnd
	for _, end := range ends {
		line := deadLine(end)
		if HandoffTextEmpty(line) {
			continue
		}
		if runeLen(line) > handoffDeadLine {
			end.Outcome = headCut(end.Outcome, handoffDeadLine)
			end.Attempt = headCut(end.Attempt, handoffDeadLine)
			for runeLen(deadLine(end)) > handoffDeadLine && end.Outcome != "" {
				end.Outcome = headCut(end.Outcome, runeLen(end.Outcome)-1)
			}
		}
		out = append(out, end)
	}
	if maxCount > 0 && len(out) > maxCount {
		out = out[len(out)-maxCount:]
	}
	for deadRunes(out) > budget && len(out) > 0 {
		out = out[1:]
	}
	return out
}

func deadLine(end DeadEnd) string {
	attempt := strings.TrimSpace(end.Attempt)
	outcome := strings.TrimSpace(end.Outcome)
	if attempt == "" {
		return outcome
	}
	if outcome == "" {
		return attempt
	}
	return attempt + ": " + outcome
}

func deadRunes(ends []DeadEnd) int {
	n := 0
	for _, end := range ends {
		n += runeLen(deadLine(end))
	}
	return n
}

func mergeHandles(parts ...[]ShelfHandle) []ShelfHandle {
	seen := map[string]bool{}
	var out []ShelfHandle
	for _, part := range parts {
		for _, handle := range part {
			handle.Kind = strings.TrimSpace(handle.Kind)
			handle.ID = strings.TrimSpace(handle.ID)
			handle.Path = strings.TrimSpace(handle.Path)
			handle.Note = handoffOneLine(handle.Note)
			if handle.Kind == "" && handle.ID == "" && handle.Path == "" {
				continue
			}
			key := handle.Kind + "\x00" + handle.ID + "\x00" + handle.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, handle)
		}
	}
	return out
}

func capHandles(handles []ShelfHandle, budget, maxCount int) []ShelfHandle {
	if maxCount > 0 && len(handles) > maxCount {
		handles = handles[len(handles)-maxCount:]
	}
	for shelfRunes(handles) > budget && len(handles) > 0 {
		handles = handles[1:]
	}
	return handles
}

func shelfLine(handle ShelfHandle) string {
	var b strings.Builder
	kind := handle.Kind
	if kind == "" {
		kind = "tool_result"
	}
	b.WriteString(kind)
	if handle.ID != "" {
		b.WriteString(" id=")
		b.WriteString(handle.ID)
	}
	if handle.Path != "" {
		b.WriteString(" path=")
		b.WriteString(handle.Path)
	}
	if handle.Offset != 0 {
		b.WriteString(" offset=")
		b.WriteString(strconv.Itoa(handle.Offset))
	}
	if handle.Limit != 0 {
		b.WriteString(" limit=")
		b.WriteString(strconv.Itoa(handle.Limit))
	}
	if handle.Note != "" {
		b.WriteString(" note=")
		b.WriteString(handle.Note)
	}
	return b.String()
}

func shelfRunes(handles []ShelfHandle) int {
	n := 0
	for _, handle := range handles {
		n += runeLen(shelfLine(handle))
	}
	return n
}

// capFieldsToSchema applies the full-memo field caps before the render cap.
// A parsed summary may be up to handoffRenderRunes, so one field can be
// larger than its schema cap. Task keeps its newest tail. Progress keeps
// its measurement. This does not clear progress.
func (m *HandoffMemo) capFieldsToSchema() {
	if m == nil {
		return
	}
	m.Task = tailCut(strings.TrimSpace(m.Task), handoffTaskRunes)
	if HandoffTextEmpty(m.Task) {
		m.Task = ""
	}
	m.Progress = capProgressKeepingMeasurement(m.Progress)
	m.FilesRead = capPaths(m.FilesRead, handoffReadMax, handoffPathRunes)
	m.FilesModified = capPaths(m.FilesModified, handoffModifiedMax, handoffPathRunes)
	m.FilesCreated = capPaths(m.FilesCreated, handoffCreatedMax, handoffPathRunes)
	m.fitFileBudget(handoffFilesRunes)
	m.Next = headCut(strings.TrimSpace(m.Next), handoffNextRunes)
	if HandoffTextEmpty(m.Next) {
		m.Next = ""
	}
	m.DeadEnds = capDeadEnds(m.DeadEnds, handoffDeadRunes, handoffDeadMax)
	m.ShelfHandles = capHandles(m.ShelfHandles, handoffShelfRunes, handoffShelfMax)
}

func (m *HandoffMemo) shrinkToRenderCap() {
	measurement := progressMeasurement(m.Progress)
	for runeLen(m.Render()) > handoffRenderRunes {
		switch {
		case len(m.ShelfHandles) > 0:
			m.ShelfHandles = m.ShelfHandles[1:]
		case len(m.DeadEnds) > 0:
			m.DeadEnds = m.DeadEnds[1:]
		case len(m.FilesRead) > 0:
			m.FilesRead = m.FilesRead[1:]
		case len(m.FilesModified) > 0:
			m.FilesModified = m.FilesModified[1:]
		case len(m.FilesCreated) > 0:
			m.FilesCreated = m.FilesCreated[1:]
		case runeLen(m.Next) > 1:
			m.Next = headCut(m.Next, runeLen(m.Next)/2)
		case runeLen(m.Task) > 1:
			m.Task = tailCut(m.Task, runeLen(m.Task)/2)
		case measurement != "" && strings.TrimSpace(m.Progress) != measurement:
			m.Progress = measurement
			if runeLen(m.Progress) > handoffProgressRunes {
				m.Progress = capProgressKeepingMeasurement(m.Progress)
			}
		case measurement == "" && runeLen(m.Progress) > 1:
			m.Progress = tailCut(m.Progress, runeLen(m.Progress)/2)
		default:
			return
		}
	}
}

// Render is the inner memo, without the tags. Empty sections are omitted.
func (m HandoffMemo) Render() string {
	var b strings.Builder
	writeSection := func(name, body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("## ")
		b.WriteString(name)
		b.WriteByte('\n')
		b.WriteString(body)
		b.WriteByte('\n')
	}
	writeSection("task", m.Task)
	writeSection("progress", m.Progress)
	writeSection("files", m.renderFiles())
	writeSection("next", m.Next)
	writeSection("dead_ends", m.renderDeadEnds())
	writeSection("shelf_handles", m.renderHandles())
	return strings.TrimSpace(b.String())
}

func (m HandoffMemo) renderFiles() string {
	var b strings.Builder
	write := func(label string, paths []string) {
		if len(paths) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(label)
		b.WriteString(":\n")
		for _, path := range paths {
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteByte('\n')
		}
	}
	write("read", m.FilesRead)
	write("modified", m.FilesModified)
	write("created", m.FilesCreated)
	return strings.TrimSpace(b.String())
}

func (m HandoffMemo) renderDeadEnds() string {
	if len(m.DeadEnds) == 0 {
		return ""
	}
	var b strings.Builder
	for _, end := range m.DeadEnds {
		line := deadLine(end)
		if line == "" {
			continue
		}
		b.WriteString("- [ERR] ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func (m HandoffMemo) renderHandles() string {
	if len(m.ShelfHandles) == 0 {
		return ""
	}
	var b strings.Builder
	for _, handle := range m.ShelfHandles {
		b.WriteString("- ")
		b.WriteString(shelfLine(handle))
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// RenderBlock wraps Render in the single handoff tag pair.
func (m HandoffMemo) RenderBlock() string {
	body := m.Render()
	if body == "" {
		return handoffOpen + "\n" + handoffClose
	}
	return handoffOpen + "\n" + body + "\n" + handoffClose
}

// ParseHandoffMemo reads the first tag pair. ok is false when the open tag
// is missing.
func ParseHandoffMemo(text string) (HandoffMemo, bool) {
	i := strings.Index(text, handoffOpen)
	if i < 0 {
		return HandoffMemo{}, false
	}
	rest := text[i+len(handoffOpen):]
	if j := strings.Index(rest, handoffClose); j >= 0 {
		rest = rest[:j]
	}
	var memo HandoffMemo
	section := ""
	fileLabel := ""
	var dead []string
	for _, line := range strings.Split(rest, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(trim, "## "))
			fileLabel = ""
			continue
		}
		switch section {
		case "task":
			memo.Task = joinBody(memo.Task, trim)
		case "progress":
			memo.Progress = joinBody(memo.Progress, trim)
		case "next":
			memo.Next = joinBody(memo.Next, trim)
		case "files":
			switch trim {
			case "read:", "modified:", "created:":
				fileLabel = strings.TrimSuffix(trim, ":")
				continue
			}
			if !strings.HasPrefix(trim, "- ") {
				continue
			}
			path := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			switch fileLabel {
			case "modified":
				memo.FilesModified = append(memo.FilesModified, path)
			case "created":
				memo.FilesCreated = append(memo.FilesCreated, path)
			default:
				memo.FilesRead = append(memo.FilesRead, path)
			}
		case "dead_ends":
			if strings.HasPrefix(trim, "- ") {
				dead = append(dead, strings.TrimSpace(strings.TrimPrefix(trim, "- ")))
			}
		case "shelf_handles":
			if strings.HasPrefix(trim, "- ") {
				memo.ShelfHandles = append(memo.ShelfHandles, parseShelf(strings.TrimPrefix(trim, "- ")))
			}
		}
	}
	for _, line := range dead {
		line = strings.TrimSpace(strings.TrimPrefix(line, "[ERR]"))
		line = strings.TrimSpace(line)
		attempt, outcome, _ := strings.Cut(line, ": ")
		memo.DeadEnds = append(memo.DeadEnds, DeadEnd{Attempt: attempt, Outcome: outcome})
	}
	return memo, true
}

func joinBody(prev, line string) string {
	if line == "" {
		return prev
	}
	if prev == "" {
		return line
	}
	return prev + "\n" + line
}

func parseShelf(line string) ShelfHandle {
	fields := strings.Fields(line)
	var handle ShelfHandle
	if len(fields) == 0 {
		return handle
	}
	handle.Kind = fields[0]
	for _, field := range fields[1:] {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "id":
			handle.ID = val
		case "path":
			handle.Path = val
		case "offset":
			handle.Offset, _ = strconv.Atoi(val)
		case "limit":
			handle.Limit, _ = strconv.Atoi(val)
		case "note":
			handle.Note = val
		}
	}
	return handle
}

// HandoffOutside returns task text with the memo block removed. A missing
// close tag drops from the open tag through the end.
func HandoffOutside(task string) string {
	i := strings.Index(task, handoffOpen)
	if i < 0 {
		return task
	}
	rest := task[i+len(handoffOpen):]
	j := strings.Index(rest, handoffClose)
	if j < 0 {
		return strings.TrimSpace(task[:i])
	}
	return strings.TrimSpace(task[:i] + rest[j+len(handoffClose):])
}

// ReplaceHandoffBlock puts one memo block into task text. A missing close
// tag replaces from the open tag through the end. A second call still leaves
// one block.
func ReplaceHandoffBlock(task, inner string) string {
	inner = strings.TrimSpace(inner)
	block := handoffOpen + "\n" + inner + "\n" + handoffClose
	i := strings.Index(task, handoffOpen)
	if i < 0 {
		task = strings.TrimRight(task, "\n")
		if task == "" {
			return block
		}
		return task + "\n\n" + block
	}
	rest := task[i+len(handoffOpen):]
	j := strings.Index(rest, handoffClose)
	if j < 0 {
		return task[:i] + block
	}
	return task[:i] + block + rest[j+len(handoffClose):]
}

// PackRemotePlannedBody packs a 400-rune planned-remote body so a verify
// line that sits past rune 400 of a naive head assembly still remains.
// The header formatRemotePlanStepCarrySummary adds is not part of this body.
func PackRemotePlannedBody(verify, measurement, path, deadEnd, task string) string {
	verify = headCut(strings.TrimSpace(verify), handoffVerifyRunes)
	measurement = strings.TrimSpace(measurement)
	path = strings.TrimSpace(path)
	for runeLen(joinNonEmpty(verify, measurement, path)) > handoffRemoteBodyRunes && measurement != "" {
		measurement = shrinkNonNumeric(measurement)
	}
	for runeLen(joinNonEmpty(verify, measurement, path)) > handoffRemoteBodyRunes && path != "" {
		if runeLen(path) == 1 {
			path = ""
			break
		}
		path = tailCut(path, runeLen(path)-1)
	}
	for runeLen(joinNonEmpty(verify, measurement, path)) > handoffRemoteBodyRunes && measurement != "" {
		measurement = ""
	}
	core := joinNonEmpty(verify, measurement, path)
	if runeLen(core) > handoffRemoteBodyRunes {
		core = tailCut(core, handoffRemoteBodyRunes)
	}
	body := core
	appendFit := func(piece string) {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			return
		}
		sep := 0
		if body != "" {
			sep = 1
		}
		room := handoffRemoteBodyRunes - runeLen(body) - sep
		if room <= 0 {
			return
		}
		if runeLen(piece) > room {
			piece = tailCut(piece, room)
		}
		if piece == "" {
			return
		}
		if body != "" {
			body += "\n"
		}
		body += piece
	}
	appendFit(deadEnd)
	appendFit(task)
	return body
}

// PackRemotePlannedFromSummary pulls a verify line, one measurement, and one
// path out of a step summary before packing. It does not head-cut the summary.
func PackRemotePlannedFromSummary(sum string) string {
	sum = strings.TrimSpace(sum)
	if sum == "" {
		return ""
	}
	verify, measurement, path, found := handoffSummaryAnchors(sum)
	if !found {
		return tailCut(sum, handoffRemoteBodyRunes)
	}
	return PackRemotePlannedBody(verify, measurement, path, "", "")
}

// HandoffStripFromText is the status strip for one finished step. It keeps a
// measurement, a path, and the verify line, and leaves the raw log out.
func HandoffStripFromText(text string) string {
	text = strings.TrimSpace(StripOneHandoffRecoveryMarker(text))
	if HandoffTextEmpty(text) {
		return ""
	}
	verify, measurement, path, found := handoffSummaryAnchors(text)
	fill := HandoffFill{Strip: true, Verify: verify, Measurement: measurement}
	if path != "" {
		fill.Modified = []string{path}
	}
	if !found {
		fill.Measurement = text
	}
	memo := FillHandoffMemo(fill)
	if !memo.Usable() {
		return ""
	}
	return memo.Render()
}

func handoffSummaryAnchors(sum string) (verify, measurement, path string, found bool) {
	for _, line := range strings.Split(sum, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if isVerifyLine(trim) {
			verify = trim
			continue
		}
		if path == "" && looksLikePath(trim) {
			path = trim
		}
		if containsDigit(trim) && !isVerifyLine(trim) {
			measurement = trim
		}
	}
	return verify, measurement, path, verify != "" || measurement != "" || path != ""
}

func isVerifyLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "exit 0") || strings.Contains(lower, "step verify")
}

func looksLikePath(line string) bool {
	if strings.ContainsAny(line, "/\\") {
		return true
	}
	for _, ext := range []string{".go", ".md", ".py", ".txt", ".json"} {
		if strings.Contains(strings.ToLower(line), ext) {
			return true
		}
	}
	return false
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func shrinkNonNumeric(s string) string {
	runes := []rune(s)
	i := 0
	for i < len(runes) && (runes[i] < '0' || runes[i] > '9') {
		i++
	}
	if i == 0 {
		if len(runes) <= 1 {
			return ""
		}
		return string(runes[1:])
	}
	if i >= len(runes) {
		return ""
	}
	return string(runes[i:])
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n")
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func headCut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func tailCut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// progressMeasurement is the digit line a tail cut must keep. A numbered
// section heading is not a measurement: the line still has to contain a
// digit after that heading. The first such line is the one a tail cut
// drops. A long line contributes the window around its last number.
func progressMeasurement(progress string) string {
	line := firstSubstantiveDigitLine(progress)
	if line == "" {
		_, anchored, _, found := handoffSummaryAnchors(progress)
		if !found {
			return ""
		}
		line = strings.TrimSpace(anchored)
	}
	line = strings.TrimSpace(line)
	if line == "" || isVerifyLine(line) {
		return ""
	}
	if runeLen(line) <= 180 {
		return line
	}
	return digitWindow(line)
}

// firstSubstantiveDigitLine returns the earliest non-verify line whose
// digits are not only a leading list number such as "1." or "4、".
func firstSubstantiveDigitLine(progress string) string {
	for _, line := range strings.Split(progress, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || isVerifyLine(trim) {
			continue
		}
		if containsDigit(stripLeadingEnumerator(trim)) {
			return trim
		}
	}
	return ""
}

// stripLeadingEnumerator removes one leading "1." / "2)" / "3、" marker.
// A decimal such as "17.4" is left intact.
func stripLeadingEnumerator(line string) string {
	runes := []rune(line)
	i := 0
	for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(runes) {
		return line
	}
	if runes[i] != '.' && runes[i] != ')' && runes[i] != '、' {
		return line
	}
	if runes[i] == '.' && i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
		return line
	}
	j := i + 1
	for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
		j++
	}
	return string(runes[j:])
}

func digitWindow(line string) string {
	runes := []rune(line)
	last := -1
	for i, r := range runes {
		if r >= '0' && r <= '9' {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	end := last + 1
	for end < len(runes) && (runes[end] == '.' || (runes[end] >= '0' && runes[end] <= '9')) {
		end++
	}
	start := last
	for start > 0 && end-start < 80 {
		start--
	}
	return strings.TrimSpace(string(runes[start:end]))
}

// capProgressKeepingMeasurement tail-cuts progress to the field cap after
// moving the measurement to the end. The uncut body is not returned.
func capProgressKeepingMeasurement(progress string) string {
	progress = strings.TrimSpace(progress)
	if progress == "" || HandoffTextEmpty(progress) {
		return ""
	}
	measurement := progressMeasurement(progress)
	if measurement != "" {
		var kept []string
		for _, line := range strings.Split(progress, "\n") {
			if strings.TrimSpace(line) == measurement {
				continue
			}
			kept = append(kept, line)
		}
		progress = strings.TrimSpace(strings.Join(kept, "\n"))
		if progress == "" {
			progress = measurement
		} else {
			progress += "\n" + measurement
		}
	}
	if runeLen(progress) > handoffProgressRunes {
		progress = tailCut(progress, handoffProgressRunes)
	}
	if measurement != "" && !strings.Contains(progress, measurement) {
		meas := measurement
		if runeLen(meas) > handoffProgressRunes {
			meas = tailCut(meas, handoffProgressRunes)
		}
		room := handoffProgressRunes - runeLen(meas) - 1
		if room < 1 {
			progress = meas
		} else {
			progress = strings.TrimSpace(tailCut(progress, room) + "\n" + meas)
		}
		if runeLen(progress) > handoffProgressRunes {
			progress = tailCut(progress, handoffProgressRunes)
		}
	}
	return strings.TrimSpace(progress)
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

// ConversationHasPendingAsk is the exported form of the live-loop check.
// A pending ask means the transcript must not be rewritten.
func ConversationHasPendingAsk(msgs []interface{}) bool {
	return conversationHasPendingAsk(msgs)
}

// SessionRoundPrefix is the stored user prefix of a host continuation wave.
const SessionRoundPrefix = sessionRoundUserPrefix

// MemoFromDroppedProjects builds a deterministic memo from messages that are
// about to leave the model context. It does not call an LLM.
func MemoFromDroppedProjects(msgs []interface{}, prior HandoffMemo, strip bool) HandoffMemo {
	var users []string
	var read, modified []string
	var commands []HandoffCommand
	for _, msg := range msgs {
		role, content := ExtractRoleContent(msg)
		content = strings.TrimSpace(content)
		if role == "user" && content != "" {
			users = append(users, content)
		}
		if role == "assistant" {
			entry := ConversationEntry{Role: role, Content: content}
			if typed, ok := msg.(map[string]interface{}); ok {
				if calls, exists := typed["tool_calls"]; exists {
					entry.ToolCalls = calls
				}
			}
			for _, path := range toolArgumentPaths(entry) {
				read = append(read, path)
			}
		}
		if role == "tool" && content != "" {
			name := ""
			if typed, ok := msg.(map[string]interface{}); ok {
				name, _ = typed["name"].(string)
			}
			failed := strings.Contains(strings.ToLower(content), "error") || strings.Contains(content, "FAIL") || strings.HasPrefix(content, "exit status")
			commands = append(commands, HandoffCommand{
				Tool:      name,
				Command:   name,
				Succeeded: !failed,
				Outcome:   firstLine(content),
			})
		}
	}
	return FillHandoffMemo(HandoffFill{
		UserTexts: users,
		Read:      read,
		Modified:  modified,
		Commands:  commands,
		Prior:     prior,
		Strip:     strip,
	})
}

var (
	memoApplied   atomic.Int64
	memoFallback  atomic.Int64
	memoKeptExtra atomic.Int64
)

// HandoffMemoStats is the process-lifetime count. There is no time window.
type HandoffMemoStats struct {
	Applied        int64 `json:"memo_applied"`
	Fallback       int64 `json:"memo_fallback"`
	KeptExtraGroup int64 `json:"memo_kept_extra_group"`
}

// NoteHandoffApplied records one LLM memo that replaced the covered span.
func NoteHandoffApplied() { memoApplied.Add(1) }

// NoteHandoffFallback records one deterministic fill. A deterministic-only
// run is what raises the doctor warning once fallback/(applied+fallback) > 0.2.
func NoteHandoffFallback() {
	memoFallback.Add(1)
	applied := memoApplied.Load()
	fallback := memoFallback.Load()
	total := applied + fallback
	if total > 0 && float64(fallback)/float64(total) > 0.2 {
		log.Printf("[handoff-memo] fallback_ratio_high applied=%d fallback=%d", applied, fallback)
	}
}

// HandoffCompactionPrompt asks the unique summarizer for one memo. An empty
// previous summary is the first handoff. A previous summary asks for the
// whole six-heading memo again, not the old four sections.
func HandoffCompactionPrompt(previous, material string) string {
	material = strings.TrimSpace(material)
	if strings.TrimSpace(previous) == "" {
		return handoffCompactionInitialPrompt + material
	}
	return fmt.Sprintf(handoffCompactionUpdatePrompt, strings.TrimSpace(previous), material)
}

// SummarizerPromptForMode is the system and user text of the compress
// callbacks. memo is true only on the flag-all path. Flag off keeps the
// free-form summary those callbacks already sent.
func SummarizerPromptForMode(material string, memo bool) (system, user string) {
	if memo {
		return "你是交接备忘录填写器。只返回一个 <handoff-memo>，不要添加其它说明。", HandoffCompactionPrompt("", material)
	}
	return "你是一个对话摘要助手。请将以下对话历史压缩为简洁的摘要，保留关键决策、工具调用结果、文件路径和代码片段。去除冗余的思考过程和重复的失败尝试。", "请压缩以下对话历史:\n\n" + material
}

const handoffCompactionInitialPrompt = `你正在执行上下文检查点压缩（Context Checkpoint Compaction）。
为将要继续此任务的另一个 LLM 生成一份交接备忘录。

只返回一个 <handoff-memo> 块，不要返回其它说明。块内使用这六个标题，空的标题省略:
## task
## progress
## files
## next
## dead_ends
## shelf_handles

约束、用户偏好和技术选型写入 task。关键数字写入 progress。文件路径写入 files，格式为:
read:
- path
modified:
- path
created:
- path

dead_ends 每条一行: - [ERR] 尝试: 结果
shelf_handles 每条一行: - kind id=... path=... note=...

不要包含工具调用的原始输出。直接引用关键短语。

以下是需要压缩的对话内容:
`

const handoffCompactionUpdatePrompt = `你正在更新一份已有的交接备忘录。规则:
- 保留上一份摘要中的目标、约束、关键决定和已完成项
- 只并入新进展
- 返回整份六个标题的一个 <handoff-memo>，不要沿用旧的四段标题
- 标题是 task、progress、files、next、dead_ends、shelf_handles
- 空的标题省略

上一份摘要:
%s

新增的对话材料:
%s
`

// NoteHandoffKeptExtraGroup records one extra kept group after an empty memo.
func NoteHandoffKeptExtraGroup() { memoKeptExtra.Add(1) }

// CurrentHandoffMemoStats returns the counters. Logs must not print progress.
func CurrentHandoffMemoStats() HandoffMemoStats {
	return HandoffMemoStats{
		Applied:        memoApplied.Load(),
		Fallback:       memoFallback.Load(),
		KeptExtraGroup: memoKeptExtra.Load(),
	}
}
