package guiapp

// desktop_bot_local.go: local sub-bots as Hub mailbox participants. A bot
// hosted by this GUI (local files, local network) registers on Hub, gets a
// Hub mailbox like any remote bot, receives work orders (topic "task") and
// messages there, and answers through Hub. Hub never runs it; this GUI
// reads its mailbox and drives it.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DesktopLocalBot mirrors Hub's LocalBot.
type DesktopLocalBot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Device       string   `json:"device,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	SeenAt       string   `json:"seen_at,omitempty"`
}

// RegisterDesktopLocalBot registers (or heartbeats) a local bot on Hub.
func (a *App) RegisterDesktopLocalBot(name string, capabilities []string) (*DesktopLocalBot, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	device, _ := os.Hostname()
	var out DesktopLocalBot
	body := map[string]any{"name": strings.TrimSpace(name), "device": device, "capabilities": capabilities}
	if err := a.desktopBotCall(context.Background(), http.MethodPut, "/api/v1/local-bots", body, &out, 20*time.Second); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDesktopLocalBots lists this account's local bots on all machines.
func (a *App) ListDesktopLocalBots() ([]DesktopLocalBot, error) {
	if a == nil {
		return nil, errDesktopBotUnavailable
	}
	var out struct {
		Items []DesktopLocalBot `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/local-bots", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// RemoveDesktopLocalBot unregisters a local bot.
func (a *App) RemoveDesktopLocalBot(id string) error {
	if a == nil {
		return errDesktopBotUnavailable
	}
	return a.desktopBotCall(context.Background(), http.MethodDelete, "/api/v1/local-bots/"+url.PathEscape(strings.TrimSpace(id)), nil, nil, 20*time.Second)
}

// DesktopLocalBotMailbox acks ack and returns the local bot's unread mail.
func (a *App) DesktopLocalBotMailbox(id string, ack []string) ([]DesktopBotCrossNote, error) {
	return a.desktopBotMailbox(strings.TrimSpace(id), ack)
}

// SendFromDesktopLocalBot posts a message from a local bot through Hub
// (target "main", a bot id or a bot name).
func (a *App) SendFromDesktopLocalBot(id, target, topic, text, replyTo string) (DesktopBotCrossNote, error) {
	return a.postDesktopBotReport(strings.TrimSpace(id), target, topic, text, replyTo, nil)
}
