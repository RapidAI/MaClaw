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
// to see when the shared desktop is free. The desktop worker is not called.
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
	Reply  struct {
		Text               string            `json:"text"`
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
	text := strings.TrimSpace(item.Reply.Text)
	if text == "" {
		text = strings.TrimSpace(item.Error)
	}
	failed := item.Status == "failed" || item.Status == "cancelled"
	// A screenshot with no caption would be stored as a failure and then
	// dropped, because an empty reply is treated as "nothing came back".
	if text == "" && !failed && (len(item.Reply.Images) > 0 || len(item.Reply.Files) > 0) {
		text = desktopDeliveryLabel(item.Task)
	}
	if label := desktopDeliveryLabel(item.Task); label != "" && text != "" && !strings.Contains(text, label) {
		text = label + "\n" + text
	}
	shots := storeDesktopBotShots(botID, cleanDesktopBotImages(item.Reply.Images))
	var images []DesktopBotImage
	if len(shots) == 0 {
		images = cleanDesktopBotImages(item.Reply.Images)
	}
	saved, left := storeDesktopBotFiles(botID, cleanDesktopBotFiles(item.Reply.Files))
	payload := map[string]any{
		"request_id":      "desktop-bot-deliver-" + strings.TrimSpace(item.RunID),
		"session_key":     desktopBotLocalSessionKey(owner, botID),
		"bot_id":          botID,
		"owner_id":        owner,
		"run_id":          strings.TrimSpace(item.RunID),
		"delivery_report": true,
	}
	// A failed run with reply text must not look like a success. The chat
	// treats any non-empty text as succeeded, so the failure goes in error.
	if failed {
		if text == "" {
			text = strings.TrimSpace(item.Error)
		}
		if text != "" {
			payload["error"] = text
		}
	} else if text != "" {
		payload["text"] = text
	}
	if len(shots) > 0 {
		payload["desktop_shot_paths"] = shots
	}
	if len(images) > 0 {
		payload["desktop_images"] = images
	}
	if len(saved) > 0 {
		payload["local_file_paths"] = saved
	}
	if len(left) > 0 {
		payload["desktop_files"] = left
	}
	if item.Reply.Handoff || strings.TrimSpace(item.Reply.AttentionReason) != "" {
		payload["desktop_handoff_url"] = a.absoluteHubPath(item.Reply.NovncURL)
		payload["desktop_user_control"] = item.Reply.Handoff
		payload["attention_reason"] = strings.TrimSpace(item.Reply.AttentionReason)
	}
	if question := strings.TrimSpace(item.Reply.AskUserQuestion); question != "" || strings.TrimSpace(item.Reply.AskUserSecretName) != "" {
		payload["ask_user_input_type"] = strings.TrimSpace(item.Reply.AskUserInputType)
		payload["ask_user_secret_name"] = strings.TrimSpace(item.Reply.AskUserSecretName)
		payload["ask_user_question"] = question
		payload["ask_user_options_json"] = strings.TrimSpace(item.Reply.AskUserOptionsJSON)
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

func desktopDeliveryLabel(task string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return ""
	}
	if i := strings.IndexAny(task, "\r\n"); i >= 0 {
		task = strings.TrimSpace(task[:i])
	}
	if i := strings.IndexAny(task, "，。,.;；"); i > 0 {
		task = strings.TrimSpace(task[:i])
	}
	runes := []rune(task)
	if len(runes) > 32 {
		task = string(runes[:32])
	}
	return task
}
