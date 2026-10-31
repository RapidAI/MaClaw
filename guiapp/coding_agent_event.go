package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const codingAgentEventPrefix = "Coding Agent Event:"

// CodingAgentEvent is the structured progress envelope for MaClaw's coding runtime.
// The text form remains line-oriented so existing progress pipes can carry it.
type CodingAgentEvent struct {
	Version     int                     `json:"version"`
	Agent       string                  `json:"agent"`
	Event       string                  `json:"event"`
	Phase       string                  `json:"phase"`
	TaskID      string                  `json:"task_id,omitempty"`
	Title       string                  `json:"title,omitempty"`
	RunID       string                  `json:"run_id,omitempty"`
	TurnID      string                  `json:"turn_id,omitempty"`
	Ts          string                  `json:"ts,omitempty"`
	Detail      string                  `json:"detail,omitempty"`
	Command     string                  `json:"command,omitempty"`
	Target      string                  `json:"target,omitempty"`
	Outcome     string                  `json:"outcome,omitempty"`
	Severity    string                  `json:"severity,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	DurationMS  int64                   `json:"duration_ms,omitempty"`
	Count       int                     `json:"count,omitempty"`
	Files       []string                `json:"files,omitempty"`
	Added       int                     `json:"added,omitempty"`
	Removed     int                     `json:"removed,omitempty"`
	FileChanges []CodingAgentFileChange `json:"file_changes,omitempty"`
}

// CodingAgentFileChange is one row in the OpenCode-style changed-file table.
type CodingAgentFileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

func newCodingAgentTaskEvent(phase codingAgentEventPhaseKind, task *TaskItem, title string, runID string) CodingAgentEvent {
	if task != nil && strings.TrimSpace(title) == "" {
		title = compactSubAgentTaskTitle(task.Title)
	}
	taskID := ""
	if task != nil {
		taskID = fmt.Sprintf("T%d", task.Index)
	}
	return CodingAgentEvent{
		Version: 1,
		Agent:   codingAgentNameCoding.String(),
		Event:   codingAgentEventKindTaskStatus.String(),
		Phase:   phase.String(),
		TaskID:  taskID,
		Title:   strings.TrimSpace(title),
		RunID:   strings.TrimSpace(runID),
	}
}

func parseCodingAgentEventText(text string) (CodingAgentEvent, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, codingAgentEventPrefix) {
		return CodingAgentEvent{}, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, codingAgentEventPrefix))
	if payload == "" {
		return CodingAgentEvent{}, false
	}
	var event CodingAgentEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return CodingAgentEvent{}, false
	}
	if normalizeCodingAgentNameKind(event.Agent) != codingAgentNameCoding {
		return CodingAgentEvent{}, false
	}
	return event, true
}

func formatCodingAgentEvent(event CodingAgentEvent) string {
	if event.Version == 0 {
		event.Version = 1
	}
	if strings.TrimSpace(event.Agent) == "" {
		event.Agent = codingAgentNameCoding.String()
	}
	if strings.TrimSpace(event.Ts) == "" {
		event.Ts = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(codingAgentEventWire(event))
	if err != nil {
		return ""
	}
	return codingAgentEventPrefix + " " + string(payload)
}

type codingAgentEventPayload struct {
	Version     int                     `json:"version"`
	Agent       string                  `json:"agent"`
	Event       string                  `json:"event"`
	Phase       string                  `json:"phase"`
	TaskID      string                  `json:"task_id,omitempty"`
	Title       string                  `json:"title,omitempty"`
	RunID       string                  `json:"run_id,omitempty"`
	TurnID      string                  `json:"turn_id,omitempty"`
	Ts          string                  `json:"ts,omitempty"`
	Detail      string                  `json:"detail,omitempty"`
	Command     string                  `json:"command,omitempty"`
	Target      string                  `json:"target,omitempty"`
	Outcome     string                  `json:"outcome,omitempty"`
	Severity    string                  `json:"severity,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	DurationMS  *int64                  `json:"duration_ms,omitempty"`
	Count       *int                    `json:"count,omitempty"`
	Files       []string                `json:"files,omitempty"`
	Added       *int                    `json:"added,omitempty"`
	Removed     *int                    `json:"removed,omitempty"`
	FileChanges []CodingAgentFileChange `json:"file_changes,omitempty"`
}

func codingAgentEventWire(event CodingAgentEvent) codingAgentEventPayload {
	payload := codingAgentEventPayload{
		Version:     event.Version,
		Agent:       event.Agent,
		Event:       event.Event,
		Phase:       event.Phase,
		TaskID:      event.TaskID,
		Title:       event.Title,
		RunID:       event.RunID,
		TurnID:      event.TurnID,
		Ts:          event.Ts,
		Detail:      event.Detail,
		Command:     event.Command,
		Target:      event.Target,
		Outcome:     event.Outcome,
		Severity:    event.Severity,
		Summary:     event.Summary,
		Files:       event.Files,
		FileChanges: event.FileChanges,
	}
	if event.Added > 0 || event.Removed > 0 || len(event.FileChanges) > 0 {
		added := event.Added
		removed := event.Removed
		payload.Added = &added
		payload.Removed = &removed
	}
	if event.DurationMS > 0 || codingAgentEventCarriesDuration(event.Event) {
		durationMS := event.DurationMS
		payload.DurationMS = &durationMS
	}
	if event.Count > 0 || codingAgentEventCarriesCount(event.Event) {
		count := event.Count
		payload.Count = &count
	}
	return payload
}

func codingAgentEventCarriesDuration(event string) bool {
	return classifyCodingAgentEventKind(event).CarriesDuration()
}

func codingAgentEventCarriesCount(event string) bool {
	return classifyCodingAgentEventKind(event).CarriesCount()
}

func isCodingAgentEventText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), codingAgentEventPrefix)
}

func isCodingAgentUserProgressText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if isCodingAgentEventText(text) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(text), "coding agent:")
}

func emitCodingAgentUserProgress(onProgress func(string), message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if isCodingAgentUserProgressText(message) {
		if onProgress != nil {
			onProgress(message)
		}
		return
	}
	log.Printf("[coding-progress] %s", truncateRunesV2(message, 240))
}

func wrapCodingAgentUserProgress(onProgress func(string)) func(string) {
	return func(message string) {
		emitCodingAgentUserProgress(onProgress, message)
	}
}

// suppressCodingWorkbenchStatusProgress drops generic [Status] milestones on
// local/remote coding workbench turns. Codex shows thinking + Read/Edit/$ —
// not "Task received" / "Preparing the execution path".
func suppressCodingWorkbenchStatusProgress(onProgress func(string), codingWorkbench bool) func(string) {
	if onProgress == nil {
		return nil
	}
	if !codingWorkbench {
		return onProgress
	}
	return func(text string) {
		if strings.HasPrefix(strings.TrimSpace(text), "[Status]") {
			return
		}
		onProgress(text)
	}
}

// wrapCodingAgentReasoningToken sends live model prose to the collapsed
// thinking pane. Finish text must use the raw onToken callback.
func wrapCodingAgentReasoningToken(onToken func(string)) func(string) {
	if onToken == nil {
		return nil
	}
	var fence codingThoughtFenceStream
	return func(delta string) {
		if delta == "" {
			return
		}
		body := delta
		prefixed := strings.HasPrefix(body, "\x01")
		if prefixed {
			body = strings.TrimPrefix(body, "\x01")
		}
		if body == "" {
			return
		}
		// A finished rule inside an open fence is code, not a step banner.
		// Replace any other finished banner with one newline so the next chunk
		// does not glue onto the previous line.
		if !fence.inside() && codingThoughtSeparatorDelta(body) {
			fence.feed("\n")
			onToken("\x01\n")
			return
		}
		if !prefixed {
			if strings.HasPrefix(body, "Browser:") || strings.HasPrefix(body, "Browser：") {
				body = strings.TrimPrefix(strings.TrimPrefix(body, "Browser:"), "Browser：")
				body = strings.TrimLeft(body, " ")
				if body == "" {
					return
				}
			}
		}
		fence.feed(body)
		if prefixed {
			onToken(delta)
			return
		}
		onToken("\x01" + body)
	}
}

// codingThoughtFenceStream remembers an unclosed ``` or ~~~ across deltas.
type codingThoughtFenceStream struct {
	open    bool
	pending string
}

func (s *codingThoughtFenceStream) inside() bool {
	if s.open {
		return true
	}
	return codingThoughtFenceLine(strings.TrimSpace(s.pending))
}

func (s *codingThoughtFenceStream) feed(delta string) {
	delta = strings.ReplaceAll(delta, "\r\n", "\n")
	for {
		nl := strings.IndexByte(delta, '\n')
		if nl < 0 {
			s.pending += delta
			return
		}
		line := s.pending + delta[:nl]
		s.pending = ""
		if codingThoughtFenceLine(strings.TrimSpace(line)) {
			s.open = !s.open
		}
		delta = delta[nl+1:]
	}
}

func codingThoughtFenceLine(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

// codingThoughtSeparatorDelta is a finished step banner or markdown rule with
// no prose. The chunk must end on a newline: a bare "-" can still grow into
// "-3" or "- item". TaskRunner sends "\n\n---\n### T3: title\n\n". A newline
// on its own is a paragraph break and is not a banner.
func codingThoughtSeparatorDelta(delta string) bool {
	if strings.HasPrefix(delta, "\x01") {
		delta = strings.TrimPrefix(delta, "\x01")
	}
	delta = strings.ReplaceAll(delta, "\r\n", "\n")
	if delta == "" || (!strings.HasSuffix(delta, "\n") && !strings.HasSuffix(delta, "\r")) {
		return false
	}
	sawChrome := false
	for _, line := range strings.Split(delta, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "-", "--", "---", "*", "**", "***", "_", "__", "___", "•", "·":
			sawChrome = true
			continue
		}
		if codingStepHeadingLine(line) {
			sawChrome = true
			continue
		}
		return false
	}
	return sawChrome
}

func codingStepHeadingLine(line string) bool {
	if !strings.HasPrefix(line, "###") {
		return false
	}
	rest := strings.TrimLeft(line[len("###"):], " \t")
	if !strings.HasPrefix(rest, "T") {
		return false
	}
	rest = rest[1:]
	if rest == "" || rest[0] < '0' || rest[0] > '9' {
		return false
	}
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	tail := rest[i:]
	if tail == "" {
		return true
	}
	return strings.HasPrefix(tail, ":") || strings.HasPrefix(tail, "：") || strings.HasPrefix(tail, " ") || strings.HasPrefix(tail, "\t")
}

func emitCodingAgentEvent(onProgress func(string), event CodingAgentEvent) {
	if onProgress == nil {
		return
	}
	message := formatCodingAgentEvent(event)
	if message == "" {
		return
	}
	onProgress(message)
}

// codingWriterAdmissionWaitDetail is the turn's account of a writer admission
// wait that is still real: an unknown scope, an unknown write set, a remote
// shared tree, or an isolated merge. Shared local tasks with concrete claims
// do not take this wait. The sentence is the phase detail, not a board banner.
const codingWriterAdmissionWaitDetail = "同一目录上另一个任务还在写入，本任务等它结束后再开始"

// codingWriterAdmissionWaitEvent is the coding-turn phase for that wait.
// assistant_note is activity the process list already renders. task_status
// alone leaves the list empty, and a bare sentence is dropped by the trail filter.
func codingWriterAdmissionWaitEvent(task *TaskItem) CodingAgentEvent {
	event := newCodingAgentTaskEvent(codingAgentEventPhaseWaitingWriter, task, "", "")
	event.Event = codingAgentEventKindAssistantNote.String()
	event.Detail = codingWriterAdmissionWaitDetail
	return event
}

func emitCodingWriterAdmissionWait(onProgress func(string), task *TaskItem) {
	emitCodingAgentEvent(onProgress, codingWriterAdmissionWaitEvent(task))
}
