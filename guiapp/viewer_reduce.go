package guiapp

import (
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// reduceViewerEvent is the desktop fold of one viewer-contract frame. The
// frontend reducer and the Dart phone reducer follow the same JSON vectors.
type viewerThinking struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type viewerTimelineRow struct {
	Phase  string `json:"phase"`
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
	RunID  string `json:"runId"`
	Text   string `json:"text,omitempty"`
}

type viewerCard struct {
	RequestID      string `json:"requestId"`
	Kind           string `json:"kind"`
	Expired        bool   `json:"expired"`
	ConfirmationID string `json:"confirmationId"`
	Text           string `json:"text"`
	AsError        bool   `json:"asError"`
}

type viewerNotice struct {
	Text    string `json:"text"`
	AsError bool   `json:"asError"`
}

type viewerReduceState struct {
	Last          uint64              `json:"last"`
	RequestID     string              `json:"requestId"`
	Body          string              `json:"body"`
	Thinking      []viewerThinking    `json:"thinking"`
	Timeline      []viewerTimelineRow `json:"timeline"`
	Cards         []viewerCard        `json:"cards"`
	Notices       []viewerNotice      `json:"notices"`
	Visible       string              `json:"visible"`
	TrialStatus   string              `json:"trialStatus"`
	TrialFailures int                 `json:"trialFailures"`
	Gap           bool                `json:"gap"`
	Responded     bool                `json:"responded"`
	DisplayText   string              `json:"displayText"`
}

func emptyViewerReduceState() viewerReduceState {
	return viewerReduceState{
		Thinking: []viewerThinking{},
		Timeline: []viewerTimelineRow{},
		Cards:    []viewerCard{},
		Notices:  []viewerNotice{},
	}
}

func viewerEventName(event map[string]any) string {
	name, _ := event["event"].(string)
	return name
}

func viewerText(v any) string {
	s, _ := v.(string)
	return s
}

func viewerNum(v any) uint64 {
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return 0
		}
		return uint64(n)
	case int:
		if n <= 0 {
			return 0
		}
		return uint64(n)
	case int64:
		if n <= 0 {
			return 0
		}
		return uint64(n)
	case uint64:
		return n
	default:
		return 0
	}
}

func viewerKnown(name string) bool {
	switch name {
	case "snapshot", "round_started", "token", "thinking", "progress", "heartbeat", "workbench_steps", "new_round", "stream_done", "card", "response":
		return true
	default:
		return false
	}
}

var codingLegacyLine = regexp.MustCompile(`(?i)^Coding Agent:\s*([a-z]+)(?:\s+(T\d+))?(?:\s+-\s*(.*))?$`)
var credentialCardRequest = regexp.MustCompile(`^credential-card-.+`)

func viewerPieces(event map[string]any) []string {
	if raw, ok := event["pieces"].([]any); ok && len(raw) > 0 {
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			out = append(out, viewerText(item))
		}
		return out
	}
	if text := viewerText(event["text"]); text != "" {
		return []string{text}
	}
	return nil
}

func applyViewerPieces(body string, pieces []string) string {
	next := body
	for _, piece := range pieces {
		if piece == "" {
			continue
		}
		// A leading U+0002 replaces the draft. The suffix is the new body,
		// so a sentinel glued inside it must not survive the join.
		if strings.HasPrefix(piece, rejectedRoundContentToken) {
			next = textutil.SanitizeVisibleChatText(strings.TrimPrefix(piece, rejectedRoundContentToken))
			continue
		}
		visible := textutil.SanitizeVisibleChatText(piece)
		if visible != "" {
			next += visible
		}
	}
	return next
}

func sealViewerThinking(state *viewerReduceState) {
	for i := range state.Thinking {
		state.Thinking[i].Done = true
	}
}

func appendViewerThinking(state *viewerReduceState, delta string) {
	body := textutil.SanitizeVisibleChatText(delta)
	if body == "" {
		return
	}
	for i := len(state.Thinking) - 1; i >= 0; i-- {
		if !state.Thinking[i].Done {
			state.Thinking[i].Text += body
			return
		}
	}
	state.Thinking = append(state.Thinking, viewerThinking{Text: body})
}

func viewerMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func rowFromCodingMap(coding map[string]any) viewerTimelineRow {
	taskID := viewerText(coding["task_id"])
	if taskID == "" {
		taskID = viewerText(coding["taskId"])
	}
	runID := viewerText(coding["run_id"])
	if runID == "" {
		runID = viewerText(coding["runId"])
	}
	row := viewerTimelineRow{
		Phase:  viewerText(coding["phase"]),
		TaskID: taskID,
		Title:  viewerText(coding["title"]),
		RunID:  runID,
	}
	row.Text = shortCodingLine(row)
	return row
}

func shortCodingLine(row viewerTimelineRow) string {
	phase := row.Phase
	if phase == "" {
		phase = "running"
	}
	line := "Coding Agent: " + phase
	if row.TaskID != "" {
		line += " " + row.TaskID
	}
	if row.Title != "" {
		line += " - " + row.Title
	}
	return strings.TrimSpace(line)
}

func rowFromProgressText(raw string) (viewerTimelineRow, bool) {
	match := codingLegacyLine.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return viewerTimelineRow{}, false
	}
	phase := strings.ToLower(match[1])
	if phase == "unknown" {
		phase = ""
	}
	return viewerTimelineRow{Phase: phase, TaskID: match[2], Title: strings.TrimSpace(match[3]), Text: raw}, true
}

func applyViewerPayload(state *viewerReduceState, event map[string]any) {
	name := viewerEventName(event)
	requestID := viewerText(event["request_id"])
	if requestID != "" && name == "round_started" && requestID != state.RequestID {
		state.RequestID = requestID
		state.Body = ""
		state.Thinking = []viewerThinking{}
		state.Responded = false
		state.Gap = false
		state.DisplayText = viewerText(event["display_text"])
	} else if requestID != "" && state.RequestID == "" {
		state.RequestID = requestID
	}
	switch name {
	case "round_started":
		if display := viewerText(event["display_text"]); display != "" {
			state.DisplayText = display
		}
	case "token":
		sealViewerThinking(state)
		pieces := viewerPieces(event)
		body := state.Body
		if event["replace"] == true {
			body = ""
		}
		state.Body = applyViewerPieces(body, pieces)
	case "thinking":
		mode := viewerText(event["state"])
		if mode == "" {
			mode = "delta"
		}
		switch mode {
		case "started":
			if n := len(state.Thinking); n == 0 || state.Thinking[n-1].Done {
				state.Thinking = append(state.Thinking, viewerThinking{})
			}
		case "done":
			doneText := textutil.SanitizeVisibleChatText(viewerText(event["text"]))
			found := false
			for i := len(state.Thinking) - 1; i >= 0; i-- {
				if !state.Thinking[i].Done {
					if doneText != "" {
						state.Thinking[i].Text = doneText
					}
					state.Thinking[i].Done = true
					found = true
					break
				}
			}
			if !found {
				state.Thinking = append(state.Thinking, viewerThinking{Text: doneText, Done: true})
			}
		default:
			deltas := viewerPieces(event)
			if len(deltas) == 0 {
				if text := viewerText(event["text"]); text != "" {
					deltas = []string{text}
				}
			}
			for _, delta := range deltas {
				if delta != "" {
					appendViewerThinking(state, delta)
				}
			}
		}
	case "progress":
		raw := viewerText(event["text"])
		if coding := viewerMap(event["coding"]); coding != nil {
			state.Timeline = append(state.Timeline, rowFromCodingMap(coding))
			return
		}
		if raw == "" {
			return
		}
		if row, ok := rowFromProgressText(raw); ok {
			state.Timeline = append(state.Timeline, row)
		}
	case "heartbeat":
	case "new_round", "stream_done":
		if name != "heartbeat" {
			sealViewerThinking(state)
		}
		if name == "stream_done" {
			state.Responded = false
		}
	case "workbench_steps", "snapshot":
	case "card":
		kind := viewerText(event["kind"])
		if kind == "" {
			kind = "notice"
		}
		if kind == "notice" && credentialCardRequest.MatchString(requestID) {
			for i := range state.Cards {
				if state.Cards[i].RequestID == requestID {
					state.Cards[i].Expired = true
				}
			}
			if note := viewerText(event["text"]); note != "" {
				state.Notices = append(state.Notices, viewerNotice{Text: note, AsError: event["as_error"] == true})
			}
			return
		}
		if kind == "confirmation" {
			confirmation := viewerMap(event["confirmation"])
			state.Cards = append(state.Cards, viewerCard{
				RequestID:      requestID,
				Kind:           kind,
				ConfirmationID: viewerText(confirmation["id"]),
				Text:           viewerText(event["text"]),
			})
			return
		}
		if note := viewerText(event["text"]); note != "" {
			state.Notices = append(state.Notices, viewerNotice{Text: note, AsError: event["as_error"] == true})
		}
	case "response":
		if requestID != "" && state.RequestID != "" && requestID != state.RequestID && !state.Responded {
			return
		}
		sealViewerThinking(state)
		state.Responded = true
		terminal := viewerMap(event["terminal"])
		if terminal == nil {
			terminal = viewerMap(event["response"])
		}
		if terminal == nil {
			terminal = map[string]any{}
		}
		body := viewerText(terminal["text"])
		summary := viewerText(terminal["trial_reflect_summary"])
		if summary == "" {
			summary = viewerText(terminal["trace_summary"])
		}
		if body != "" {
			state.Visible = body
		} else {
			state.Visible = summary
		}
		state.TrialStatus = viewerText(terminal["trial_reflect_status"])
		state.TrialFailures = int(viewerNum(terminal["trial_reflect_failures"]))
		if state.Body == "" && body != "" {
			state.Body = body
		}
		if confirmation := viewerMap(terminal["confirmation"]); confirmation != nil && viewerText(confirmation["id"]) != "" {
			state.Cards = append(state.Cards, viewerCard{
				RequestID:      requestID,
				Kind:           "confirmation",
				ConfirmationID: viewerText(confirmation["id"]),
			})
		}
	}
}

func applyViewerLiveParts(state *viewerReduceState, raw []any) {
	var body strings.Builder
	for _, item := range raw {
		part := viewerMap(item)
		if part == nil {
			continue
		}
		switch viewerText(part["kind"]) {
		case "text":
			body.WriteString(textutil.SanitizeVisibleChatText(viewerText(part["text"])))
		case "thinking":
			state.Thinking = append(state.Thinking, viewerThinking{
				Text: textutil.SanitizeVisibleChatText(viewerText(part["text"])),
				Done: part["done"] == true,
			})
		case "progress":
			if coding := viewerMap(part["coding"]); coding != nil {
				state.Timeline = append(state.Timeline, rowFromCodingMap(coding))
				continue
			}
			if row, ok := rowFromProgressText(viewerText(part["text"])); ok {
				state.Timeline = append(state.Timeline, row)
			}
		}
	}
	state.Body = body.String()
}

func replaceViewerFromLive(event map[string]any, cursor uint64) viewerReduceState {
	next := emptyViewerReduceState()
	next.Last = cursor
	live := viewerMap(event["live"])
	if live == nil {
		next.Gap = true
		return next
	}
	next.RequestID = viewerText(live["request_id"])
	next.DisplayText = viewerText(live["display_text"])
	next.Responded = live["responded"] == true
	// The wire snapshot is remoteLiveSnapshot: parts + cursor. live.text is
	// only the older golden shape, used when parts is absent.
	if raw, ok := live["parts"].([]any); ok {
		applyViewerLiveParts(&next, raw)
		return next
	}
	next.Body = textutil.SanitizeVisibleChatText(viewerText(live["text"]))
	if raw, ok := live["thinking"].([]any); ok {
		for _, item := range raw {
			block := viewerMap(item)
			next.Thinking = append(next.Thinking, viewerThinking{
				Text: textutil.SanitizeVisibleChatText(viewerText(block["text"])),
				Done: block["done"] == true,
			})
		}
	}
	return next
}

func reduceViewerEvent(state viewerReduceState, event map[string]any) viewerReduceState {
	name := viewerEventName(event)
	if !viewerKnown(name) {
		return state
	}
	cursor := viewerNum(event["cursor"])
	_, hasFrom := event["cursor_from"]
	from := cursor
	if hasFrom {
		from = viewerNum(event["cursor_from"])
	}
	if cursor == 0 && !hasFrom {
		next := state
		applyViewerPayload(&next, event)
		return next
	}
	if name == "snapshot" && event["resync"] == true && cursor > state.Last {
		if viewerMap(event["live"]) == nil {
			next := state
			next.Gap = true
			return next
		}
		return replaceViewerFromLive(event, cursor)
	}
	if cursor <= state.Last {
		return state
	}
	if from == state.Last+1 {
		next := state
		applyViewerPayload(&next, event)
		next.Last = cursor
		next.Gap = false
		return next
	}
	if from > state.Last+1 && name == "round_started" && viewerText(event["request_id"]) != "" && viewerText(event["request_id"]) != state.RequestID {
		next := state
		applyViewerPayload(&next, event)
		next.Last = cursor
		next.Gap = false
		return next
	}
	next := state
	next.Gap = true
	return next
}
