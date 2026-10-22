package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botlog"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// QueryDesktopBotTaskStatus asks Hub how this bot's recent runs are doing and
// returns the sentence to show. hint is the person's words about which task
// they mean. The desktop worker is not called.
func (a *App) QueryDesktopBotTaskStatus(botID, hint, lang string) (string, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return "", fmt.Errorf("bot id is required")
	}
	q := url.Values{}
	q.Set("q", strings.TrimSpace(hint))
	q.Set("lang", strings.TrimSpace(lang))
	var out struct {
		Text string `json:"text"`
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/tasks?" + q.Encode()
	if err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, 20*time.Second); err != nil {
		return "", err
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		return "", fmt.Errorf("没有查到任务状态")
	}
	return text, nil
}

// DesktopBotTaskItem is one row from Hub's task list. An empty run id is not
// a row the person can open; a posting reserve still arrives as status running.
type DesktopBotTaskItem struct {
	RunID  string `json:"run_id"`
	BotID  string `json:"bot_id"`
	Status string `json:"status"`
	Task   string `json:"task"`
}

// ListDesktopBotTasks reads this bot's recent runs. The window uses the list
// to see when this bot's desktop is free. The desktop worker is not called.
func (a *App) ListDesktopBotTasks(botID string) ([]DesktopBotTaskItem, error) {
	return a.listDesktopBotTasks(context.Background(), botID)
}

func (a *App) listDesktopBotTasks(ctx context.Context, botID string) ([]DesktopBotTaskItem, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	var out struct {
		Items []DesktopBotTaskItem `json:"items"`
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/tasks"
	if err := a.desktopBotCall(ctx, http.MethodGet, path, nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return []DesktopBotTaskItem{}, nil
	}
	return out.Items, nil
}

// CancelDesktopBotRun stops one admitted desktop run. The person said stop.
// The chat reading is not given a tool for this; the GUI calls Hub's cancel
// route with the run id it already has.
func (a *App) CancelDesktopBotRun(botID, runID string) error {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if botID == "" || runID == "" {
		return fmt.Errorf("bot id and run id are required")
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/" + url.PathEscape(runID) + "/cancel"
	return a.desktopBotCall(context.Background(), http.MethodPost, path, map[string]string{}, nil, 20*time.Second)
}

// PullDesktopBotDeliveries pushes finished runs the GUI missed while it was
// offline. A run this process is still waiting on is left for that wait, which
// acks it when the reply is shown. Each pushed run is acked so the next pull
// does not show it again.
func (a *App) PullDesktopBotDeliveries() error {
	if a == nil {
		return fmt.Errorf("AI assistant backend is unavailable")
	}
	var out struct {
		Items []desktopBotDelivery `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/pending", nil, &out, 20*time.Second); err != nil {
		return err
	}
	owner := a.desktopBotAccountUserID()
	for _, item := range out.Items {
		botID := strings.TrimSpace(item.BotID)
		runID := strings.TrimSpace(item.RunID)
		if botID == "" || runID == "" {
			continue
		}
		if !claimDesktopRunShow(botID, runID) {
			if desktopRunAwaitingAck(botID, runID) && a.ackDesktopBotDelivery(botID, runID) {
				noteDesktopRunAcked(botID, runID)
			}
			continue
		}
		if !a.emitDesktopBotDelivery(owner, item) {
			forgetDesktopRunShown(botID, runID)
			continue
		}
		if a.ackDesktopBotDelivery(botID, runID) {
			noteDesktopRunAcked(botID, runID)
		}
	}
	return nil
}

// claimDesktopRunShow reserves one run so two pulls cannot both append it.
// A run this process is still waiting on is left for that wait.
func claimDesktopRunShow(botID, runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	key := desktopRunKey(botID, runID)
	if _, ok := activeDesktopRuns[key]; ok {
		return false
	}
	if _, shown := shownDesktopRuns[key]; shown {
		return false
	}
	shownDesktopRuns[key] = struct{}{}
	return true
}

func forgetDesktopRunShown(botID, runID string) {
	activeDesktopRunsMu.Lock()
	key := desktopRunKey(botID, runID)
	delete(shownDesktopRuns, key)
	delete(ackedDesktopRuns, key)
	activeDesktopRunsMu.Unlock()
}

func noteDesktopRunAcked(botID, runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	activeDesktopRunsMu.Lock()
	ackedDesktopRuns[desktopRunKey(botID, runID)] = struct{}{}
	activeDesktopRunsMu.Unlock()
}

// desktopRunAwaitingAck is a result this process already showed whose ack
// did not land. A later pull retries the ack and does not append it again.
func desktopRunAwaitingAck(botID, runID string) bool {
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	key := desktopRunKey(botID, runID)
	if _, ok := activeDesktopRuns[key]; ok {
		return false
	}
	if _, ok := shownDesktopRuns[key]; !ok {
		return false
	}
	_, acked := ackedDesktopRuns[key]
	return !acked
}

func noteDesktopRunShown(botID, runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	activeDesktopRunsMu.Lock()
	shownDesktopRuns[desktopRunKey(botID, runID)] = struct{}{}
	activeDesktopRunsMu.Unlock()
}

func (a *App) ackDesktopBotDelivery(botID, runID string) bool {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if a == nil || botID == "" || runID == "" {
		return false
	}
	// A test that replaces the whole relay never opened a Hub run to ack.
	// Skipping here keeps that test off the network. A test of the pull loop
	// sets the exchange override and still records the ack.
	if desktopBotRelayOverride != nil && desktopBotExchangeOverride == nil {
		return false
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/" + url.PathEscape(runID) + "/ack"
	if err := a.desktopBotCall(context.Background(), http.MethodPost, path, nil, nil, 15*time.Second); err != nil {
		botlog.Write(botID, "gui.delivery_ack_failed", err, "run", runID)
		return false
	}
	return true
}

type desktopBotDelivery struct {
	BotID  string `json:"bot_id"`
	RunID  string `json:"run_id"`
	Task   string `json:"task"`
	Status string `json:"status"`
	Phase  string `json:"phase"`
	Reply  struct {
		Text               string            `json:"text"`
		SendToUser         string            `json:"send_to_user"`
		NovncURL           string            `json:"novnc_url"`
		Handoff            bool              `json:"handoff"`
		AttentionReason    string            `json:"attention_reason"`
		AskUserInputType   string            `json:"ask_user_input_type"`
		AskUserSecretName  string            `json:"ask_user_secret_name"`
		AskUserQuestion    string            `json:"ask_user_question"`
		AskUserOptionsJSON string            `json:"ask_user_options_json"`
		Images             []DesktopBotImage `json:"images"`
		Files              []DesktopBotFile  `json:"files"`
	} `json:"reply"`
	Error string `json:"error"`
}

func (a *App) emitDesktopBotDelivery(owner string, item desktopBotDelivery) bool {
	if a == nil {
		return false
	}
	botID := strings.TrimSpace(item.BotID)
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "local"
	}
	failed := item.Status == "failed" || item.Status == "cancelled"
	visible := desktopbot.Visible(desktopbot.Turn{
		Prose:      item.Reply.Text,
		SendToUser: item.Reply.SendToUser,
		Error:      item.Error,
	})
	ids := desktopAttachmentIDs(item.Reply.Images, item.Reply.Files)
	payload := map[string]any{
		"request_id":      "desktop-bot-deliver-" + strings.TrimSpace(item.RunID),
		"session_key":     desktopBotLocalSessionKey(owner, botID),
		"bot_id":          botID,
		"owner_id":        owner,
		"run_id":          strings.TrimSpace(item.RunID),
		"delivery_report": true,
	}
	// send_to_user is the only sentence. Assistant prose stays off the event.
	// A real transport error stays on error.
	if visible.Bubble && !visible.Failed {
		payload["send_to_user"] = visible.Sentence
	}
	if failed {
		errText := strings.TrimSpace(item.Error)
		if errText != "" && !desktopbot.Filler(errText) {
			payload["error"] = errText
		}
	}
	if len(ids) > 0 {
		payload["desktop_attachment_ids"] = ids
	}
	switch strings.TrimSpace(item.Phase) {
	case "plan", "execute":
		payload["phase"] = strings.TrimSpace(item.Phase)
	}
	if item.Reply.Handoff || strings.TrimSpace(item.Reply.AttentionReason) != "" {
		payload["desktop_handoff_url"] = a.absoluteHubPath(item.Reply.NovncURL)
		payload["desktop_user_control"] = item.Reply.Handoff
		payload["attention_reason"] = strings.TrimSpace(item.Reply.AttentionReason)
	}
	question := strings.TrimSpace(item.Reply.AskUserQuestion)
	options := strings.TrimSpace(item.Reply.AskUserOptionsJSON)
	if question != "" || strings.TrimSpace(item.Reply.AskUserSecretName) != "" || desktopAskHasOptions(options) {
		payload["ask_user_input_type"] = strings.TrimSpace(item.Reply.AskUserInputType)
		payload["ask_user_secret_name"] = strings.TrimSpace(item.Reply.AskUserSecretName)
		payload["ask_user_question"] = question
		payload["ask_user_options_json"] = options
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	if desktopBotReportSink != nil {
		desktopBotReportSink(string(raw))
	}
	a.emitEvent("desktop-bot-report", string(raw))
	return true
}

// DesktopBotActiveRun is one ongoing run for the 任务 tab. Source "server" is
// a run MaClawSrv still marks running that Hub does not follow; Stale marks
// one whose process is probably gone. Status "queued" has no run id yet.
type DesktopBotActiveRun struct {
	RunID          string `json:"run_id"`
	BotID          string `json:"bot_id"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	Task           string `json:"task"`
	Step           string `json:"step"`
	NextStep       string `json:"next_step"`
	Phase          string `json:"phase"`
	WaitingForUser bool   `json:"waiting_for_user"`
	Stale          bool   `json:"stale"`
	StartedAt      string `json:"started_at"`
}

// ListDesktopBotActiveRuns reads this bot's ongoing runs from Hub.
func (a *App) ListDesktopBotActiveRuns(botID string) ([]DesktopBotActiveRun, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	var out struct {
		Items []DesktopBotActiveRun `json:"items"`
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/active"
	if err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return []DesktopBotActiveRun{}, nil
	}
	return out.Items, nil
}
