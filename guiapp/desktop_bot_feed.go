package guiapp

// desktop_bot_feed.go: what the person sees of their bots' server-side
// work, kept apart from what the bots' agents consume. Hub's feed returns
// every message between the bots (delivered to an agent or not) until a
// GUI marks it shown, so a wake that ran while the GUI was closed is still
// shown when it opens, with the output of the run Hub started.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PullDesktopBotFeed marks shown as displayed and returns the notes no GUI
// has shown yet (all: the kept history too), oldest first.
func (a *App) PullDesktopBotFeed(shown []string, all bool) ([]DesktopBotCrossNote, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	if shown == nil {
		shown = []string{}
	}
	var out struct {
		Items []DesktopBotCrossNote `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/feed", map[string]any{"shown": shown, "all": all}, &out, 20*time.Second); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// DesktopBotRunOutput is one read of a run Hub started (a delegate or a
// server-side wake), for showing it in that bot's chat.
type DesktopBotRunOutput struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	Error  string `json:"error,omitempty"`
	Done   bool   `json:"done"`
}

// GetDesktopBotRunOutput reads a run once; it never waits, cancels or acks.
func (a *App) GetDesktopBotRunOutput(botID, runID string) (DesktopBotRunOutput, error) {
	if a == nil {
		return DesktopBotRunOutput{}, errDesktopBotUnavailable
	}
	botID, runID = strings.TrimSpace(botID), strings.TrimSpace(runID)
	var out struct {
		Text       string `json:"text"`
		SendToUser string `json:"send_to_user"`
		Status     string `json:"status"`
		Error      string `json:"error"`
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/" + url.PathEscape(runID)
	if err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, 20*time.Second); err != nil {
		return DesktopBotRunOutput{}, err
	}
	return desktopBotRunOutputOf(out.Status, out.Text, out.SendToUser, out.Error), nil
}

func desktopBotRunOutputOf(status, text, sendToUser, errText string) DesktopBotRunOutput {
	status = strings.ToLower(strings.TrimSpace(status))
	o := DesktopBotRunOutput{Status: status, Text: strings.TrimSpace(firstNonEmptyMB(sendToUser, text)), Error: strings.TrimSpace(errText)}
	switch status {
	case "running", "queued", "pending", "accepted", "":
		o.Done = false
	default:
		o.Done = true
	}
	return o
}
