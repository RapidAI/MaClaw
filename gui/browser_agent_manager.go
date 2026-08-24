package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

type BrowserSessionView struct {
	ID             string                         `json:"id"`
	Tool           string                         `json:"tool"`
	Title          string                         `json:"title"`
	Status         SessionStatus                  `json:"status"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	Summary        SessionSummary                 `json:"summary"`
	Preview        SessionPreview                 `json:"preview"`
	Events         []ImportantEvent               `json:"events"`
	RawOutputLines []string                       `json:"raw_output_lines"`
	OutputImages   []SessionOutputImage           `json:"output_images,omitempty"`
	RunID          string                         `json:"run_id,omitempty"`
	LastSnapshotID string                         `json:"last_snapshot_id,omitempty"`
	CurrentURL     string                         `json:"current_url,omitempty"`
	CurrentTitle   string                         `json:"current_title,omitempty"`
	ReadyState     string                         `json:"ready_state,omitempty"`
	LatestRefs     []browser.BrowserElementRef    `json:"latest_refs,omitempty"`
	BrowserTabs    []browser.BrowserTabSnapshot   `json:"browser_tabs,omitempty"`
	BrowserFrames  []browser.BrowserFrameSnapshot `json:"browser_frames,omitempty"`
	ActiveTabID    string                         `json:"browser_active_tab_id,omitempty"`
	ActiveFrameID  string                         `json:"browser_active_frame_id,omitempty"`
}

type BrowserAgentManager struct {
	app      *App
	mu       sync.RWMutex
	mapViews map[string]*BrowserSessionView
}

func NewBrowserAgentManager(app *App) *BrowserAgentManager {
	return &BrowserAgentManager{app: app, mapViews: map[string]*BrowserSessionView{}}
}

func (m *BrowserAgentManager) traceService() *AITraceService {
	if m == nil || m.app == nil {
		return nil
	}
	m.app.ensureAITrace()
	return m.app.aiTrace
}

func (m *BrowserAgentManager) syncFromCore() {
	if m == nil {
		return
	}
	traceSvc := m.traceService()
	m.mu.Lock()
	defer m.mu.Unlock()
	coreSessions := browser.ListAgentSessions()
	alive := map[string]bool{}
	for _, sess := range coreSessions {
		if sess == nil {
			continue
		}
		state := sess.State()
		alive[state.ID] = true
		view, ok := m.mapViews[state.ID]
		if !ok || view == nil {
			view = &BrowserSessionView{ID: state.ID, Tool: "browser", Status: SessionRunning, CreatedAt: state.CreatedAt}
			m.mapViews[state.ID] = view
			if traceSvc != nil {
				_, run := traceSvc.StartJobRun(TraceJobKindBrowserSession, browserTraceStoredText("session", state.ID), "browser", "", "")
				view.RunID = run.RunID
				traceSvc.SetRunSessionID(run.RunID, state.ID)
			}
		}
		m.applyStateLocked(view, state)
		if traceSvc != nil && view.RunID != "" {
			traceSvc.UpdateRun(view.RunID, traceStatusFromSessionStatus(view.Status), browserTraceStoredText("progress", view.Summary.ProgressSummary), "")
			events := make([]TraceEvent, 0, len(view.Events))
			for _, evt := range view.Events {
				events = append(events, TraceEvent{
					Kind:      evt.Type,
					Severity:  evt.Severity,
					Title:     firstNonEmptyBrowserText(evt.Title, evt.Type),
					Summary:   browserTraceStoredText(evt.Type, evt.Summary),
					CreatedAt: evt.CreatedAt,
				})
			}
			evidence := make([]EvidenceRecord, 0, len(view.RawOutputLines))
			for _, line := range tailStrings(view.RawOutputLines, 6) {
				safeLine := browserTraceStoredText("output", line)
				evidence = append(evidence, EvidenceRecord{
					SourceKind:     "browser_output",
					Category:       "browser",
					Summary:        safeLine,
					ContentSnippet: safeLine,
					CreatedAt:      traceNowMillis(),
				})
			}
			traceSvc.ReplaceRun(view.RunID, events, evidence)
		}
	}
	for id, view := range m.mapViews {
		if !alive[id] && view != nil {
			view.Status = SessionExited
			view.Summary.Status = SessionExited.String()
			view.Summary.ProgressSummary = "Browser session closed"
			view.Summary.LastResult = "Browser session closed"
			view.UpdatedAt = time.Now()
		}
	}
}

func (m *BrowserAgentManager) applyStateLocked(view *BrowserSessionView, state browser.BrowserAgentState) {
	view.Tool = "browser"
	view.Title = firstNonEmptyBrowserText(state.CurrentTitle, state.ID)
	view.CreatedAt = state.CreatedAt
	view.UpdatedAt = state.UpdatedAt
	view.LastSnapshotID = state.LastSnapshotID
	view.CurrentURL = state.CurrentURL
	view.CurrentTitle = state.CurrentTitle
	view.ReadyState = state.ReadyState
	view.LatestRefs = append([]browser.BrowserElementRef(nil), state.LatestRefs...)
	view.BrowserTabs = append([]browser.BrowserTabSnapshot(nil), state.Tabs...)
	view.BrowserFrames = append([]browser.BrowserFrameSnapshot(nil), state.Frames...)
	view.ActiveTabID = state.ActiveTabID
	view.ActiveFrameID = state.ActiveFrameID
	if state.Alive {
		view.Status = SessionRunning
	} else {
		view.Status = SessionExited
	}
	statusText := string(view.Status)
	previewLines := append([]string{}, state.ActivityLog...)
	if len(previewLines) == 0 && state.CurrentURL != "" {
		previewLines = []string{state.CurrentTitle, state.CurrentURL}
	}
	if len(state.Snapshots) > 0 {
		latest := state.Snapshots[len(state.Snapshots)-1]
		previewLines = append(previewLines, "--- refs ---")
		for i, ref := range latest.Refs {
			if i >= 6 {
				break
			}
			label := firstNonEmptyBrowserText(ref.Name, ref.Text, ref.Selector)
			previewLines = append(previewLines, fmt.Sprintf("%s %s", ref.Ref, label))
		}
	}
	view.Summary = SessionSummary{
		SessionID:       state.ID,
		Tool:            "browser",
		Title:           view.Title,
		Source:          "browser",
		Status:          statusText,
		Severity:        severityFromBrowserState(state),
		CurrentTask:     browserCurrentTask(state),
		ProgressSummary: fmt.Sprintf("%s · refs=%d · ready=%s", browserProgressSummary(state), len(state.LatestRefs), firstNonEmptyBrowserText(state.ReadyState, "unknown")),
		LastResult:      browserLastResult(state),
		SuggestedAction: browserSuggestedAction(state),
		UpdatedAt:       time.Now().Unix(),
	}
	view.Preview = SessionPreview{SessionID: state.ID, OutputSeq: int64(len(previewLines)), PreviewLines: previewLines, UpdatedAt: time.Now().Unix()}
	view.RawOutputLines = append([]string{}, state.ActivityLog...)
	view.Events = mapBrowserEvents(state)
	view.OutputImages = mapBrowserImages(state)
}

func (m *BrowserAgentManager) List() []BrowserSessionView {
	m.syncFromCore()
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BrowserSessionView, 0, len(m.mapViews))
	for _, view := range m.mapViews {
		if view == nil {
			continue
		}
		cp := *view
		cp.Events = append([]ImportantEvent(nil), view.Events...)
		cp.RawOutputLines = append([]string(nil), view.RawOutputLines...)
		cp.OutputImages = append([]SessionOutputImage(nil), view.OutputImages...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *BrowserAgentManager) Get(sessionID string) (BrowserSessionView, bool) {
	m.syncFromCore()
	m.mu.RLock()
	defer m.mu.RUnlock()
	view, ok := m.mapViews[sessionID]
	if !ok || view == nil {
		return BrowserSessionView{}, false
	}
	cp := *view
	cp.Events = append([]ImportantEvent(nil), view.Events...)
	cp.RawOutputLines = append([]string(nil), view.RawOutputLines...)
	cp.OutputImages = append([]SessionOutputImage(nil), view.OutputImages...)
	return cp, true
}

func (m *BrowserAgentManager) Start(args map[string]interface{}) (BrowserSessionView, error) {
	policy := browser.BrowserPolicy{
		AllowedDomains:             stringSliceValue(args["allowed_domains"]),
		BlockedDomains:             stringSliceValue(args["blocked_domains"]),
		AllowCrossOriginNavigation: boolValue(args["allow_cross_origin_navigation"], true),
		AllowPopup:                 boolValue(args["allow_popup"], false),
		AllowDownload:              boolValue(args["allow_download"], false),
		AllowUpload:                boolValue(args["allow_upload"], false),
		ContentBoundary:            boolValue(args["content_boundary"], true),
	}
	mode := stableBrowserManagerSessionMode(args)
	ownerID, explicitRuntimeOwner := runtimePolicyOwnerIDFromToolArgsWithPresence(args)
	if explicitRuntimeOwner && ownerID == "" {
		return BrowserSessionView{}, fmt.Errorf("runtime owner is missing; isolated runtime will not fall back to desktop owner")
	}
	sess, err := browser.StartAgentSessionForOwner(ownerID, stringValue(args["addr"]), policy, boolValue(args["reuse_existing"], true), mode)
	if err != nil {
		return BrowserSessionView{}, err
	}
	if startURL := strings.TrimSpace(stringValue(args["start_url"])); startURL != "" {
		if err := sess.OpenURL(startURL); err != nil {
			return BrowserSessionView{}, err
		}
	}
	m.syncFromCore()
	view, ok := m.Get(sess.ID)
	if !ok {
		return BrowserSessionView{}, fmt.Errorf("browser session not visible after start: %s", sess.ID)
	}
	if m.app != nil {
		m.app.emitRemoteStateChanged()
	}
	return view, nil
}

func (m *BrowserAgentManager) Stop(sessionID string, closeBrowser bool) error {
	if err := browser.StopAgentSession(sessionID, closeBrowser); err != nil {
		return err
	}
	m.syncFromCore()
	if m.app != nil {
		m.app.emitRemoteStateChanged()
	}
	return nil
}

func (m *BrowserAgentManager) Trace(runID string) (*AIAssistantTraceView, error) {
	traceSvc := m.traceService()
	if traceSvc == nil {
		return nil, fmt.Errorf("AI trace service not initialized")
	}
	view, ok := traceSvc.GetTrace(runID)
	if !ok {
		return nil, fmt.Errorf("trace not found: %s", runID)
	}
	return &view, nil
}

func mapBrowserEvents(state browser.BrowserAgentState) []ImportantEvent {
	out := make([]ImportantEvent, 0, len(state.Trace))
	for idx, evt := range state.Trace {
		out = append(out, ImportantEvent{
			EventID:   fmt.Sprintf("browser-event-%s-%d", state.ID, idx),
			SessionID: state.ID,
			Type:      "browser." + evt.Kind,
			Severity:  severityForBrowserTrace(evt.Kind),
			Title:     browserEventTitle(evt.Kind),
			Summary:   evt.Summary,
			CreatedAt: evt.CreatedAt,
		})
	}
	return out
}

func mapBrowserImages(state browser.BrowserAgentState) []SessionOutputImage {
	if len(state.Snapshots) == 0 {
		return nil
	}
	latest := state.Snapshots[len(state.Snapshots)-1]
	if strings.TrimSpace(latest.Screenshot) == "" {
		return nil
	}
	afterIdx := len(state.ActivityLog) - 1
	if afterIdx < 0 {
		afterIdx = 0
	}
	return []SessionOutputImage{{ImageID: latest.SnapshotID, MediaType: "image/png", Data: latest.Screenshot, AfterLineIdx: afterIdx}}
}

func browserCurrentTask(state browser.BrowserAgentState) string {
	if len(state.ActivityLog) == 0 {
		return "Observing browser session"
	}
	return state.ActivityLog[len(state.ActivityLog)-1]
}

func browserProgressSummary(state browser.BrowserAgentState) string {
	if state.CurrentURL != "" {
		return fmt.Sprintf("%s · %s", firstNonEmptyBrowserText(state.CurrentTitle, state.CurrentURL), state.CurrentURL)
	}
	return "Browser session active"
}

func stableBrowserManagerSessionMode(args map[string]interface{}) browser.SessionMode {
	mode := strings.ToLower(strings.TrimSpace(stringValue(args["mode"])))
	switch browser.SessionMode(mode) {
	case "", browser.SessionModeAuto:
		return browser.SessionModePersistent
	case browser.SessionModePersistent, browser.SessionModeIsolated, browser.SessionModeConnectUser:
		return browser.SessionMode(mode)
	default:
		return browser.SessionModePersistent
	}
}

func browserLastResult(state browser.BrowserAgentState) string {
	if len(state.Trace) > 0 {
		return state.Trace[len(state.Trace)-1].Summary
	}
	if state.CurrentURL != "" {
		return state.CurrentURL
	}
	return "Browser session ready"
}

func browserSuggestedAction(state browser.BrowserAgentState) string {
	if state.LastSnapshotID == "" {
		return "Run browser(action=\"observe\") to capture refs and page state"
	}
	return "Use browser(action=\"click\"/\"type\") with refs from the latest snapshot"
}

func severityFromBrowserState(state browser.BrowserAgentState) string {
	if len(state.ErrorLines) > 0 {
		return string(summarySeverityWarn)
	}
	return string(summarySeverityInfo)
}

func severityForBrowserTrace(kind string) string {
	if normalizeBrowserEventKind(kind) == browserEventKindError {
		return string(summarySeverityWarn)
	}
	return string(summarySeverityInfo)
}

func browserEventTitle(kind string) string {
	return normalizeBrowserEventKind(kind).Title(kind)
}

func browserTraceStoredText(kind, text string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "browser"
	}
	return fmt.Sprintf("%s text_len=%d", kind, len([]rune(text)))
}

func firstNonEmptyBrowserText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "browser"
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	if b, ok := value.(bool); ok {
		return b
	}
	return fallback
}

func stringSliceValue(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		if direct, ok := value.([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func tailStrings(lines []string, max int) []string {
	if len(lines) <= max {
		return append([]string(nil), lines...)
	}
	return append([]string(nil), lines[len(lines)-max:]...)
}
