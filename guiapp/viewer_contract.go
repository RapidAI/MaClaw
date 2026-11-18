package guiapp

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// Viewer contract shared by the local panel, a controlling desktop, and mobilebot.
// The task-owning process publishes one event shape. Local subscribers see it
// before any Hub copy. Proto 1 on the wire is the same event with the new
// fields omitted.

var remoteStreamProtoMax = 2

func currentRemoteStreamProtoMax() int {
	switch strings.TrimSpace(os.Getenv("MACLAW_REMOTE_STREAM_PROTO")) {
	case "1":
		return 1
	case "2":
		return 2
	}
	if remoteStreamProtoMax < 1 {
		return 1
	}
	return remoteStreamProtoMax
}

func negotiateStreamProto(requested int) int {
	if requested <= 0 {
		requested = 1
	}
	max := currentRemoteStreamProtoMax()
	if requested > max {
		return max
	}
	return requested
}

// viewerReducerEnabled is the panel switch. Unset or 1 emits viewer-event.
// 0 keeps the raw Wails events and does not emit viewer-event.
func viewerReducerEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_VIEWER_REDUCER"))) {
	case "0", "false", "off":
		return false
	default:
		return true
	}
}

func (a *App) ViewerReducerEnabled() bool { return viewerReducerEnabled() }

var viewerCursors sync.Map // session key -> *atomic.Uint64

// viewerSessionGates serializes publish for one session from cursor allocation
// through wire admission. Two goroutines otherwise allocate cursor 5 then 6,
// and 6 can be queued or dropped before 5 is admitted; the resync snapshot
// then moves the reducer past 5 and 5 is discarded.
var viewerSessionGates sync.Map // session key -> *sync.Mutex

func viewerSessionLock(sessionKey string) *sync.Mutex {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = "_"
	}
	v, _ := viewerSessionGates.LoadOrStore(sessionKey, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func nextViewerCursor(sessionKey string) uint64 {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = "_"
	}
	v, _ := viewerCursors.LoadOrStore(sessionKey, &atomic.Uint64{})
	return v.(*atomic.Uint64).Add(1)
}

func viewerCursorNow(sessionKey string) uint64 {
	v, ok := viewerCursors.Load(strings.TrimSpace(sessionKey))
	if !ok {
		return 0
	}
	return v.(*atomic.Uint64).Load()
}

type viewerEvent struct {
	GrantID      string                   `json:"grant_id,omitempty"`
	SessionKey   string                   `json:"session_key,omitempty"`
	ProjectPath  string                   `json:"project_path,omitempty"`
	RequestID    string                   `json:"request_id,omitempty"`
	Event        string                   `json:"event"`
	Seq          uint64                   `json:"seq,omitempty"`
	Proto        int                      `json:"proto,omitempty"`
	Cursor       uint64                   `json:"cursor,omitempty"`
	CursorFrom   uint64                   `json:"cursor_from,omitempty"`
	Lane         string                   `json:"lane,omitempty"`
	Text         string                   `json:"text,omitempty"`
	Pieces       []string                 `json:"pieces,omitempty"`
	State        string                   `json:"state,omitempty"`
	DisplayText  string                   `json:"display_text,omitempty"`
	Coding       *codingAgentEventPayload `json:"coding,omitempty"`
	Terminal     *viewerTerminal          `json:"terminal,omitempty"`
	Kind         string                   `json:"kind,omitempty"`
	Confirmation json.RawMessage          `json:"confirmation,omitempty"`
	Actions      json.RawMessage          `json:"actions,omitempty"`
	AsError      bool                     `json:"as_error,omitempty"`
	Replace      bool                     `json:"replace,omitempty"`
	Truncated    bool                     `json:"truncated,omitempty"`
	Workbench    json.RawMessage          `json:"workbench,omitempty"`
	Response     json.RawMessage          `json:"response,omitempty"`
	Resync       bool                     `json:"resync,omitempty"`
	Live         *remoteLiveSnapshot      `json:"live,omitempty"`
}

type viewerTerminal struct {
	Text                  string                        `json:"text,omitempty"`
	Reasoning             string                        `json:"reasoning,omitempty"`
	Error                 string                        `json:"error,omitempty"`
	Fields                []IMResponseField             `json:"fields,omitempty"`
	Actions               []IMResponseAction            `json:"actions,omitempty"`
	Confirmation          *IMResponseConfirmation       `json:"confirmation,omitempty"`
	UnfinishedTask        *IMResponseUnfinishedTask     `json:"unfinished_task,omitempty"`
	UnfinishedSlot        *IMResponseUnfinishedTask     `json:"unfinished_slot,omitempty"`
	RecoverableSession    *IMResponseRecoverableSession `json:"recoverable_session,omitempty"`
	CodingRuntimeRecovery *CodingRuntimeRecoveryReview  `json:"coding_runtime_recovery,omitempty"`
	AskUserQuestion       string                        `json:"ask_user_question,omitempty"`
	AskUserOptionsJSON    string                        `json:"ask_user_options_json,omitempty"`
	AskUserInputType      string                        `json:"ask_user_input_type,omitempty"`
	AskUserSecretName     string                        `json:"ask_user_secret_name,omitempty"`
	ResponseSource        string                        `json:"response_source,omitempty"`
	ClearUI               bool                          `json:"clear_ui,omitempty"`
	KeepPanel             bool                          `json:"keep_panel,omitempty"`
	ConfirmedResume       bool                          `json:"confirmed_resume,omitempty"`
	LocalFilePath         string                        `json:"local_file_path,omitempty"`
	LocalFilePaths        []string                      `json:"local_file_paths,omitempty"`
	ImageKey              string                        `json:"image_key,omitempty"`
	DesktopShotPaths      []string                      `json:"desktop_shot_paths,omitempty"`
	DesktopAttachmentIDs  []string                      `json:"desktop_attachment_ids,omitempty"`
	FileName              string                        `json:"file_name,omitempty"`
	FileMimeType          string                        `json:"file_mime_type,omitempty"`
	ChatTaskID            string                        `json:"chat_task_id,omitempty"`
	ChatTaskKind          string                        `json:"chat_task_kind,omitempty"`
	TraceStatus           string                        `json:"trace_status,omitempty"`
	TraceSummary          string                        `json:"trace_summary,omitempty"`
	TraceEventCount       int                           `json:"trace_event_count,omitempty"`
	EvidenceCount         int                           `json:"evidence_count,omitempty"`
	JobID                 string                        `json:"job_id,omitempty"`
	RunID                 string                        `json:"run_id,omitempty"`
	InputTokens           int                           `json:"input_tokens,omitempty"`
	OutputTokens          int                           `json:"output_tokens,omitempty"`
	TotalTokens           int                           `json:"total_tokens,omitempty"`
	CacheReadTokens       int                           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens      int                           `json:"cache_write_tokens,omitempty"`
	EstCostRMB            float64                       `json:"est_cost_rmb,omitempty"`
	CreditsDeducted       *float64                      `json:"credits_deducted,omitempty"`
	RouteTask             string                        `json:"route_task,omitempty"`
	RouteSource           string                        `json:"route_source,omitempty"`
	RouteModel            string                        `json:"route_model,omitempty"`
	RouteReason           string                        `json:"route_reason,omitempty"`
	RouteEscalated        bool                          `json:"route_escalated,omitempty"`
	PromptProfile         string                        `json:"prompt_profile,omitempty"`
	TrialReflectSummary   string                        `json:"trial_reflect_summary,omitempty"`
	TrialReflectStatus    string                        `json:"trial_reflect_status,omitempty"`
	TrialReflectFailures  int                           `json:"trial_reflect_failures,omitempty"`
}

func (a *App) addViewerSubscriber(fn func(viewerEvent)) {
	if a == nil || fn == nil {
		return
	}
	a.viewerSubMu.Lock()
	a.viewerSubs = append(a.viewerSubs, fn)
	a.viewerSubMu.Unlock()
}

func (a *App) publishInProcess(ev viewerEvent) {
	if a == nil {
		return
	}
	a.viewerSubMu.Lock()
	subs := append([]func(viewerEvent){}, a.viewerSubs...)
	a.viewerSubMu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}

func (a *App) publishViewerEvent(ev viewerEvent) {
	if a == nil || ev.Event == "" {
		return
	}
	// Hold the session gate until the wire copy is admitted. Local delivery
	// and the queue then observe this session in cursor order. The gate is
	// not r.mu: the writer never takes it, and relayCopy takes r.mu only
	// after this function has already delivered the local subscriber.
	gate := viewerSessionLock(ev.SessionKey)
	gate.Lock()
	defer gate.Unlock()
	if ev.Cursor == 0 {
		ev.Cursor = nextViewerCursor(ev.SessionKey)
	}
	if ev.CursorFrom == 0 {
		ev.CursorFrom = ev.Cursor
	}
	a.publishInProcess(ev)
	if viewerReducerEnabled() {
		if raw, err := json.Marshal(ev); err == nil {
			a.emitEvent("viewer-event", string(raw))
		}
	}
	if reg := a.remoteWatchRegistryForPublish(); reg != nil {
		reg.relayCopy(ev)
	}
}

// remoteWatchRegistryForPublish returns the registry, creating it on the
// first local publish. Events that ran before any watch_task still have to
// fold, or the first controller seeds at 0 and gaps on the next cursor.
// remoteInfraMu is not reentrant: TryLock skips the create when this
// goroutine already holds the write lock during init.
func (a *App) remoteWatchRegistryForPublish() *remoteWatchRegistry {
	if a == nil {
		return nil
	}
	if reg := a.remoteWatchRegistryTry(); reg != nil {
		return reg
	}
	if !a.remoteInfraMu.TryLock() {
		return a.remoteWatches
	}
	defer a.remoteInfraMu.Unlock()
	if a.remoteWatches == nil {
		a.remoteWatches = newRemoteWatchRegistry(a)
	}
	return a.remoteWatches
}

func (a *App) remoteWatchRegistryTry() *remoteWatchRegistry {
	if a == nil {
		return nil
	}
	if a.remoteInfraMu.TryRLock() {
		reg := a.remoteWatches
		a.remoteInfraMu.RUnlock()
		return reg
	}
	return a.remoteWatches
}

func viewerStreamAllowed(name string) bool {
	switch name {
	case "ai-assistant-foreground-round-started",
		"ai-assistant-token",
		"ai-assistant-progress",
		"ai-assistant-new-round",
		"ai-assistant-stream-done":
		return true
	default:
		return false
	}
}

var (
	viewerThinkOpen sync.Map // session -> bool
	viewerThinkText sync.Map // session -> string
)

func viewerEventsFromStream(name string, event AIAssistantStreamEvent) []viewerEvent {
	session := strings.TrimSpace(event.SessionKey)
	if session == "" || !viewerStreamAllowed(name) {
		return nil
	}
	path := projectPathFromSessionOwnerID(session)
	one := func(evName, lane string) viewerEvent {
		return viewerEvent{
			SessionKey:  session,
			ProjectPath: path,
			RequestID:   event.RequestID,
			Event:       evName,
			Seq:         event.Sequence,
			Lane:        lane,
		}
	}
	switch name {
	case "ai-assistant-foreground-round-started":
		ev := one("round_started", "control")
		ev.DisplayText = event.DisplayText
		if ev.DisplayText == "" {
			ev.DisplayText = event.Text
		}
		viewerThinkOpen.Delete(session)
		viewerThinkText.Delete(session)
		return []viewerEvent{ev}
	case "ai-assistant-new-round":
		ev := one("new_round", "control")
		return append(closeThinkingEvents(session, event.RequestID, event.Sequence, path), ev)
	case "ai-assistant-stream-done":
		ev := one("stream_done", "control")
		return append(closeThinkingEvents(session, event.RequestID, event.Sequence, path), ev)
	case "ai-assistant-progress":
		if strings.TrimSpace(event.Text) == imHeartbeatMsg {
			ev := one("heartbeat", "control")
			return []viewerEvent{ev}
		}
		ev := one("progress", "progress")
		ev.Text = progressTextForWire(event.Text)
		if parsed, ok := parseCodingAgentEventText(event.Text); ok {
			wire := truncateCodingWire(codingAgentEventWire(parsed))
			ev.Coding = &wire
			ev.Text = progressCompatText(event.Text, parsed)
		}
		return []viewerEvent{ev}
	case "ai-assistant-token":
		return tokenViewerEvents(event, path)
	default:
		return nil
	}
}

func closeThinkingEvents(session, requestID string, seq uint64, path string) []viewerEvent {
	open, _ := viewerThinkOpen.Load(session)
	if open != true {
		return nil
	}
	text, _ := viewerThinkText.Load(session)
	viewerThinkOpen.Delete(session)
	viewerThinkText.Delete(session)
	body, _ := text.(string)
	return []viewerEvent{{
		SessionKey:  session,
		ProjectPath: path,
		RequestID:   requestID,
		Event:       "thinking",
		Seq:         seq,
		Lane:        "reasoning",
		State:       "done",
		Text:        body,
	}}
}

// visibleRejectToken keeps the rejected-round marker and strips tofu from
// the replacement suffix. An exact "\x02" stays "\x02". "\x02重写" stays
// "\x02重写". "\x02ke\x01pt" becomes "\x02kept".
func visibleRejectToken(delta string) string {
	body := textutil.SanitizeVisibleChatText(strings.TrimPrefix(delta, rejectedRoundContentToken))
	if body == "" {
		return rejectedRoundContentToken
	}
	return rejectedRoundContentToken + body
}

func tokenViewerEvents(event AIAssistantStreamEvent, path string) []viewerEvent {
	session := strings.TrimSpace(event.SessionKey)
	deltas := event.Deltas
	if len(deltas) == 0 && event.Text != "" {
		deltas = []string{event.Text}
	}
	var out []viewerEvent
	for _, delta := range deltas {
		switch {
		case strings.HasPrefix(delta, rejectedRoundContentToken):
			// The suffix replaces the draft. Interior sentinels in it are not
			// text; the marker itself must stay so viewers still reset.
			text := visibleRejectToken(delta)
			out = append(out, viewerEvent{
				SessionKey: session, ProjectPath: path, RequestID: event.RequestID,
				Event: "token", Seq: event.Sequence, Lane: "content",
				Text: text, Pieces: []string{text},
			})
		case strings.HasPrefix(delta, "\x01"):
			_, body := textutil.SplitVisibleStreamDelta(delta)
			if body == "" {
				continue
			}
			if open, _ := viewerThinkOpen.Load(session); open != true {
				viewerThinkOpen.Store(session, true)
				viewerThinkText.Store(session, "")
				out = append(out, viewerEvent{
					SessionKey: session, ProjectPath: path, RequestID: event.RequestID,
					Event: "thinking", Seq: event.Sequence, Lane: "reasoning",
					State: "started",
				})
			}
			prev, _ := viewerThinkText.Load(session)
			prevText, _ := prev.(string)
			viewerThinkText.Store(session, prevText+body)
			ev := viewerEvent{
				SessionKey: session, ProjectPath: path, RequestID: event.RequestID,
				Event: "thinking", Seq: event.Sequence, Lane: "reasoning",
				State: "delta", Text: body,
			}
			if body != "" {
				ev.Pieces = []string{body}
			}
			out = append(out, ev)
		default:
			body := textutil.SanitizeVisibleChatText(delta)
			if body == "" {
				continue
			}
			if open, _ := viewerThinkOpen.Load(session); open == true {
				out = append(out, closeThinkingEvents(session, event.RequestID, event.Sequence, path)...)
			}
			ev := viewerEvent{
				SessionKey: session, ProjectPath: path, RequestID: event.RequestID,
				Event: "token", Seq: event.Sequence, Lane: "content",
				Text: body,
			}
			ev.Pieces = []string{body}
			out = append(out, ev)
		}
	}
	return out
}

func viewerEventsFromContinuation(event AIContinuationEvent) []viewerEvent {
	session := strings.TrimSpace(event.SessionKey)
	path := projectPathFromSessionOwnerID(session)
	base := viewerEvent{
		SessionKey: session, ProjectPath: path, RequestID: event.RequestID,
		Lane: "control",
	}
	switch event.Kind {
	case "token":
		base.Event = "token"
		base.Lane = "content"
		body := textutil.SanitizeVisibleChatText(event.Text)
		base.Text = body
		if body != "" {
			base.Pieces = []string{body}
		}
		return []viewerEvent{base}
	case "final":
		base.Event = "token"
		base.Lane = "content"
		body := textutil.SanitizeVisibleChatText(event.Text)
		base.Text = body
		base.Replace = true
		if body != "" {
			base.Pieces = []string{body}
		}
		return []viewerEvent{base}
	case "notice", "confirmation":
		base.Event = "card"
		base.Kind = event.Kind
		base.Text = event.Text
		base.AsError = event.AsError
		if event.Confirmation != nil {
			if raw, err := json.Marshal(event.Confirmation); err == nil {
				base.Confirmation = raw
			}
		}
		if len(event.Actions) > 0 {
			if raw, err := json.Marshal(event.Actions); err == nil {
				base.Actions = raw
			}
		}
		return []viewerEvent{base}
	default:
		return nil
	}
}

func stripRemoteResponseMedia(resp *IMAgentResponse) IMAgentResponse {
	trimmed := *resp
	trimmed.FileData = ""
	trimmed.VoiceData = ""
	trimmed.VoiceParts = nil
	trimmed.ThumbnailBase64 = ""
	trimmed.DesktopImages = nil
	trimmed.DesktopFiles = nil
	trimmed.FileCompanionFigures = nil
	trimmed.FileCompanionFiles = nil
	return trimmed
}

func terminalFromResponse(resp IMAgentResponse) viewerTerminal {
	return viewerTerminal{
		Text: resp.Text, Reasoning: resp.Reasoning, Error: resp.Error,
		Fields: resp.Fields, Actions: resp.Actions, Confirmation: resp.Confirmation,
		UnfinishedTask: resp.UnfinishedTask, UnfinishedSlot: resp.UnfinishedSlot,
		RecoverableSession: resp.RecoverableSession, CodingRuntimeRecovery: resp.CodingRuntimeRecovery,
		AskUserQuestion: resp.AskUserQuestion, AskUserOptionsJSON: resp.AskUserOptionsJSON,
		AskUserInputType: resp.AskUserInputType, AskUserSecretName: resp.AskUserSecretName,
		ResponseSource: resp.ResponseSource, ClearUI: resp.ClearUI, KeepPanel: resp.KeepPanel,
		ConfirmedResume: resp.ConfirmedResume, LocalFilePath: resp.LocalFilePath,
		LocalFilePaths: resp.LocalFilePaths, ImageKey: resp.ImageKey,
		DesktopShotPaths: resp.DesktopShotPaths, DesktopAttachmentIDs: resp.DesktopAttachmentIDs,
		FileName: resp.FileName, FileMimeType: resp.FileMimeType,
		ChatTaskID: resp.ChatTaskID, ChatTaskKind: resp.ChatTaskKind,
		TraceStatus: resp.TraceStatus, TraceSummary: resp.TraceSummary, TraceEventCount: resp.TraceEventCount,
		EvidenceCount: resp.EvidenceCount, JobID: resp.JobID, RunID: resp.RunID,
		InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, TotalTokens: resp.TotalTokens,
		CacheReadTokens: resp.CacheReadTokens, CacheWriteTokens: resp.CacheWriteTokens,
		EstCostRMB: resp.EstCostRMB, CreditsDeducted: resp.CreditsDeducted,
		RouteTask: resp.RouteTask, RouteSource: resp.RouteSource, RouteModel: resp.RouteModel,
		RouteReason: resp.RouteReason, RouteEscalated: resp.RouteEscalated, PromptProfile: resp.PromptProfile,
		TrialReflectSummary: resp.TrialReflectSummary, TrialReflectStatus: resp.TrialReflectStatus,
		TrialReflectFailures: resp.TrialReflectFailures,
	}
}

func viewerEventFromResponse(requestID string, resp *IMAgentResponse) (viewerEvent, IMAgentResponse) {
	trimmed := stripRemoteResponseMedia(resp)
	session := strings.TrimSpace(trimmed.SessionKey)
	term := terminalFromResponse(trimmed)
	raw, _ := json.Marshal(&trimmed)
	ev := viewerEvent{
		SessionKey: session, ProjectPath: projectPathFromSessionOwnerID(session),
		RequestID: requestID, Event: "response", Lane: "control",
		Response: raw, Terminal: &term,
	}
	if open, _ := viewerThinkOpen.Load(session); open == true {
		// The done event is separate so the wire can order it before the response.
	}
	return ev, trimmed
}

func thinkingDoneBeforeResponse(resp *IMAgentResponse) []viewerEvent {
	if resp == nil {
		return nil
	}
	session := strings.TrimSpace(resp.SessionKey)
	return closeThinkingEvents(session, resp.RequestID, 0, projectPathFromSessionOwnerID(session))
}

func progressTextForWire(text string) string {
	// Rune cap, never a mid-byte cut of a JSON object.
	const max = 2048
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return trimRunes(text, max)
}

func progressCompatText(full string, parsed CodingAgentEvent) string {
	if utf8.RuneCountInString(full) <= 2048 && strings.HasPrefix(strings.TrimSpace(full), codingAgentEventPrefix) {
		return full
	}
	title := trimRunes(parsed.Title, 80)
	phase := trimRunes(parsed.Phase, 40)
	taskID := trimRunes(parsed.TaskID, 40)
	line := strings.TrimSpace("Coding Agent: " + phase + " " + taskID + " - " + title)
	return line
}

func truncateCodingWire(in codingAgentEventPayload) codingAgentEventPayload {
	in.Title = trimRunes(in.Title, 80)
	in.Phase = trimRunes(in.Phase, 40)
	in.TaskID = trimRunes(in.TaskID, 40)
	in.TurnID = trimRunes(in.TurnID, 40)
	in.Outcome = trimRunes(in.Outcome, 40)
	in.Severity = trimRunes(in.Severity, 40)
	in.RunID = trimRunes(in.RunID, 40)
	in.Ts = trimRunes(in.Ts, 40)
	in.Detail = trimRunes(in.Detail, 160)
	in.Summary = trimRunes(in.Summary, 160)
	in.Command = trimRunes(in.Command, 160)
	in.Target = trimRunes(in.Target, 160)
	if len(in.Files) > 4 {
		in.Files = in.Files[:4]
	}
	for i := range in.Files {
		in.Files[i] = trimRunes(in.Files[i], 80)
	}
	if len(in.FileChanges) > 6 {
		in.FileChanges = in.FileChanges[:6]
	}
	for i := range in.FileChanges {
		in.FileChanges[i].Path = trimRunes(in.FileChanges[i].Path, 80)
	}
	return in
}

func trimRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
