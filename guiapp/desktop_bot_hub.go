package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const desktopBotDisabledMessage = "服务器没有开通bot功能"

// desktopBotRelayResult is one Hub reply for a bot message.
// A desktop address is not a handoff unless the person has the keyboard
// or Hub named an attention reason.
type desktopBotRelayResult struct {
	Text               string
	NovncURL           string
	UserControl        bool
	AttentionReason    string
	AskUserInputType   string
	AskUserSecretName  string
	AskUserQuestion    string
	AskUserOptionsJSON string
}

// desktopBotRelayOverride is set by tests. Production posts to Hub.
var desktopBotRelayOverride func(ctx context.Context, botID, text, phase string) (desktopBotRelayResult, error)

// desktopBotWatchOverride is set by tests that drive a timeout without Hub.
var desktopBotWatchOverride func(ctx context.Context, botID string) (novncURL string, userControl bool, attentionReason string, err error)

// desktopBotViewSink and desktopBotResultSink record what the relay would
// show. Production leaves them nil; emitEvent still runs.
var desktopBotViewSink func(payload string)
var desktopBotResultSink func(*IMAgentResponse)

// DesktopBotAccess is the gate for the left-hand bot entry.
type DesktopBotAccess struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message,omitempty"`
}

// DesktopBotInfo is one bot owned by the logged-in user.
type DesktopBotInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	InstanceID  string `json:"instance_id"`
}

// DesktopBotWatch is one watch poll result: the live noVNC page for this
// user's desktop and whether a bot handed that keyboard to the person.
type DesktopBotWatch struct {
	NovncURL        string `json:"novnc_url"`
	UserControl     bool   `json:"user_control"`
	AttentionReason string `json:"attention_reason,omitempty"`
}

// WatchDesktopBot polls the live desktop for the Bot page. The GUI calls it
// every few seconds while a desktop panel is open; each call refreshes the
// Hub-side view hold, so the desktop stays up while a human is watching or
// taking over instead of going black the moment the last command finishes.
func (a *App) WatchDesktopBot(botID string) (*DesktopBotWatch, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	var out struct {
		NovncURL        string `json:"novnc_url"`
		UserControl     bool   `json:"user_control"`
		AttentionReason string `json:"attention_reason"`
	}
	err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/desktop", nil, &out, 15*time.Second)
	if err != nil {
		return nil, err
	}
	return &DesktopBotWatch{
		NovncURL:        a.absoluteHubPath(out.NovncURL),
		UserControl:     out.UserControl,
		AttentionReason: strings.TrimSpace(out.AttentionReason),
	}, nil
}

// ReleaseDesktopBotWatch drops the watch hold for the Bot page that just hid.
func (a *App) ReleaseDesktopBotWatch(botID string) error {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return fmt.Errorf("bot id is required")
	}
	return a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/desktop", map[string]bool{
		"release": true,
	}, nil, 15*time.Second)
}

type hubBot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	InstanceID  string `json:"instance_id"`
}

func (a *App) DesktopBotAccess() (*DesktopBotAccess, error) {
	var out struct {
		Enabled bool   `json:"enabled"`
		Message string `json:"message"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/access", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	access := &DesktopBotAccess{Enabled: out.Enabled, Message: out.Message}
	if !access.Enabled && access.Message == "" {
		access.Message = desktopBotDisabledMessage
	}
	return access, nil
}

func (a *App) ListDesktopBots() ([]DesktopBotInfo, error) {
	var out struct {
		Items []hubBot `json:"items"`
	}
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	items := make([]DesktopBotInfo, 0, len(out.Items))
	for _, bot := range out.Items {
		items = append(items, desktopBotInfo(bot))
	}
	return items, nil
}

func (a *App) CreateDesktopBot(name, description string) (*DesktopBotInfo, error) {
	var bot hubBot
	err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots", map[string]string{
		"name":        name,
		"description": description,
	}, &bot, 30*time.Second)
	if err != nil {
		return nil, err
	}
	info := desktopBotInfo(bot)
	return &info, nil
}

func (a *App) RenameDesktopBot(botID, name, description string) error {
	return a.desktopBotCall(context.Background(), http.MethodPatch, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID)), map[string]string{
		"name":        name,
		"description": description,
	}, nil, 30*time.Second)
}

func (a *App) DeleteDesktopBot(botID string) error {
	return a.desktopBotCall(context.Background(), http.MethodDelete, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID)), nil, nil, 30*time.Second)
}

func (a *App) finishDesktopBotTask(requestID, sessionKey, botID, text, phase string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if desktopBotRelayOverride == nil {
		go a.publishDesktopBotView(ctx, requestID, sessionKey, botID)
	}
	result, err := a.relayDesktopBot(ctx, botID, text, phase)
	resp := &IMAgentResponse{RequestID: requestID, SessionKey: sessionKey, EventScopeID: sessionKey}
	if err != nil {
		resp.Error = err.Error()
		if novnc, ok := a.desktopTimeoutHandoff(botID, err); ok {
			resp.DesktopHandoffURL = novnc
			resp.DesktopUserControl = true
			resp.Text = "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。"
			resp.Error = ""
		} else if novnc, ok := a.desktopFailureKeepsLogin(botID, err); ok {
			resp.DesktopHandoffURL = novnc
			resp.DesktopUserControl = true
		} else {
			a.emitDesktopBotCleared(requestID, sessionKey)
		}
	} else {
		resp.Text = result.Text
		resp.DesktopUserControl = result.UserControl
		resp.DesktopAttentionReason = strings.TrimSpace(result.AttentionReason)
		resp.AskUserInputType = strings.TrimSpace(result.AskUserInputType)
		resp.AskUserSecretName = strings.TrimSpace(result.AskUserSecretName)
		resp.AskUserQuestion = strings.TrimSpace(result.AskUserQuestion)
		resp.AskUserOptionsJSON = strings.TrimSpace(result.AskUserOptionsJSON)
		if result.UserControl || resp.DesktopAttentionReason != "" {
			resp.DesktopHandoffURL = result.NovncURL
		}
	}
	if desktopBotResultSink != nil {
		desktopBotResultSink(resp)
	}
	a.emitAIAssistantResponse(requestID, resp)
}

func (a *App) relayDesktopBot(ctx context.Context, botID, text, phase string) (desktopBotRelayResult, error) {
	if desktopBotRelayOverride != nil {
		return desktopBotRelayOverride(ctx, botID, text, phase)
	}
	var out struct {
		Text               string `json:"text"`
		NovncURL           string `json:"novnc_url"`
		Handoff            bool   `json:"handoff"`
		AttentionReason    string `json:"attention_reason"`
		AskUserInputType   string `json:"ask_user_input_type"`
		AskUserSecretName  string `json:"ask_user_secret_name"`
		AskUserQuestion    string `json:"ask_user_question"`
		AskUserOptionsJSON string `json:"ask_user_options_json"`
	}
	body := map[string]string{"content": text}
	if phase = strings.TrimSpace(phase); phase != "" {
		body["phase"] = phase
	}
	err := a.desktopBotCall(ctx, http.MethodPost, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/messages", body, &out, 8*time.Minute)
	if err != nil {
		return desktopBotRelayResult{}, err
	}
	if strings.TrimSpace(out.Text) == "" {
		return desktopBotRelayResult{}, fmt.Errorf("MaClawSrv 没有返回结果")
	}
	return desktopBotRelayResult{
		Text:               out.Text,
		NovncURL:           a.absoluteHubPath(out.NovncURL),
		UserControl:        out.Handoff,
		AttentionReason:    strings.TrimSpace(out.AttentionReason),
		AskUserInputType:   strings.TrimSpace(out.AskUserInputType),
		AskUserSecretName:  strings.TrimSpace(out.AskUserSecretName),
		AskUserQuestion:    strings.TrimSpace(out.AskUserQuestion),
		AskUserOptionsJSON: strings.TrimSpace(out.AskUserOptionsJSON),
	}, nil
}

func (a *App) absoluteHubPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/") {
		if hubURL, _, err := a.getHubCredentials(); err == nil && strings.TrimSpace(hubURL) != "" {
			return strings.TrimRight(strings.TrimSpace(hubURL), "/") + raw
		}
	}
	return raw
}

// desktopViewSnap is one poll of the live desktop.
type desktopViewSnap struct {
	url     string
	control bool
	reason  string
}

func normalizeDesktopView(snap desktopViewSnap) desktopViewSnap {
	snap.url = strings.TrimSpace(snap.url)
	snap.reason = strings.TrimSpace(snap.reason)
	return snap
}

func desktopViewHasAttention(snap desktopViewSnap) bool {
	return snap.control || snap.reason != ""
}

// desktopViewShouldEmit reports whether this poll is a screen announcement.
// A desktop address by itself is not. A rising handoff is. A falling
// keyboard handoff on a URL we already announced is, so the viewer can close.
func desktopViewShouldEmit(prev, next desktopViewSnap) bool {
	prev = normalizeDesktopView(prev)
	next = normalizeDesktopView(next)
	if prev == next {
		return false
	}
	return desktopViewHasAttention(prev) || desktopViewHasAttention(next)
}

// publishDesktopBotView polls Hub while a bot message is in flight.
// It announces the desktop only when the poll is a real handoff.
func (a *App) publishDesktopBotView(ctx context.Context, requestID, sessionKey, botID string) {
	var last desktopViewSnap
	poll := func() {
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		novnc, control, reason, err := a.watchDesktopBot(callCtx, botID)
		cancel()
		if err != nil || ctx.Err() != nil {
			return
		}
		last = a.announceDesktopView(requestID, sessionKey, last, desktopViewSnap{
			url: novnc, control: control, reason: reason,
		})
	}
	poll()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

// announceDesktopView stores the latest poll even when it stays quiet.
func (a *App) announceDesktopView(requestID, sessionKey string, prev, next desktopViewSnap) desktopViewSnap {
	next = normalizeDesktopView(next)
	if !desktopViewShouldEmit(prev, next) {
		return next
	}
	raw, err := json.Marshal(map[string]any{
		"request_id":       requestID,
		"session_key":      sessionKey,
		"novnc_url":        next.url,
		"user_control":     next.control,
		"attention_reason": next.reason,
	})
	if err != nil {
		return next
	}
	a.emitDesktopBotView(string(raw))
	return next
}

func (a *App) emitDesktopBotView(payload string) {
	if desktopBotViewSink != nil {
		desktopBotViewSink(payload)
	}
	a.emitEvent("desktop-bot-view", payload)
}

func (a *App) emitDesktopBotCleared(requestID, sessionKey string) {
	raw, err := json.Marshal(map[string]any{
		"request_id":  requestID,
		"session_key": sessionKey,
		"cleared":     true,
	})
	if err != nil {
		return
	}
	a.emitDesktopBotView(string(raw))
}

func (a *App) desktopTimeoutHandoff(botID string, err error) (string, bool) {
	novnc, userControl, ok := a.desktopAfterTimeout(botID, err)
	if !ok || !desktopTimeoutHandsOff(err, novnc, userControl) {
		return "", false
	}
	return novnc, true
}

// desktopAfterTimeout keeps the open browser on screen when the reply never
// arrives. The keyboard stays with the person only if this bot already handed
// the desktop over for login.
func (a *App) desktopAfterTimeout(botID string, err error) (string, bool, bool) {
	if !desktopCommandTimedOut(err) {
		return "", false, false
	}
	// A test that replaced the relay and did not stub the watch must not dial Hub.
	if desktopBotWatchOverride == nil && desktopBotRelayOverride != nil {
		return "", false, false
	}
	novnc, userControl, _, watchErr := a.watchDesktopBot(context.Background(), botID)
	if watchErr != nil || strings.TrimSpace(novnc) == "" {
		return "", false, false
	}
	return novnc, userControl, true
}

func (a *App) desktopFailureKeepsLogin(botID string, err error) (string, bool) {
	if !keepLoginDesktopAfterFailure(err, "pending", true) {
		return "", false
	}
	if desktopBotWatchOverride == nil && desktopBotRelayOverride != nil {
		return "", false
	}
	novnc, userControl, _, watchErr := a.watchDesktopBot(context.Background(), botID)
	if watchErr != nil || !keepLoginDesktopAfterFailure(err, novnc, userControl) {
		return "", false
	}
	return novnc, true
}

// keepLoginDesktopAfterFailure leaves the logged-in browser on screen when
// the continuation never started. An unreachable service stays a failure.
func keepLoginDesktopAfterFailure(err error, novnc string, userControl bool) bool {
	if err == nil || desktopCommandTimedOut(err) || strings.Contains(err.Error(), "不可达") {
		return false
	}
	return strings.TrimSpace(novnc) != "" && userControl
}

// desktopTimeoutHandsOff is a login handoff only when this bot already gave
// the desktop to the person. A slow task that is still driving the browser
// keeps the keyboard, so it cannot wipe the page the person is logging into.
func desktopTimeoutHandsOff(err error, novnc string, userControl bool) bool {
	return desktopCommandTimedOut(err) && strings.TrimSpace(novnc) != "" && userControl
}

func desktopCommandTimedOut(err error) bool {
	return err != nil && strings.Contains(err.Error(), "没有在时限内")
}

func (a *App) watchDesktopBot(ctx context.Context, botID string) (string, bool, string, error) {
	if desktopBotWatchOverride != nil {
		return desktopBotWatchOverride(ctx, botID)
	}
	var out struct {
		NovncURL        string `json:"novnc_url"`
		UserControl     bool   `json:"user_control"`
		AttentionReason string `json:"attention_reason"`
	}
	err := a.desktopBotCall(ctx, http.MethodGet, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/desktop", nil, &out, 5*time.Second)
	if err != nil {
		return "", false, "", err
	}
	return a.absoluteHubPath(out.NovncURL), out.UserControl, strings.TrimSpace(out.AttentionReason), nil
}

func (a *App) desktopBotCall(ctx context.Context, method, path string, body any, dest any, timeout time.Duration) error {
	if a == nil {
		return fmt.Errorf("AI assistant backend is unavailable")
	}
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return fmt.Errorf("MaClawSrv 不可达")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, method, hubURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg, loadErr := a.LoadConfig(); loadErr == nil {
		if machineID := strings.TrimSpace(groupDiscussionAgentID(cfg)); machineID != "" {
			req.Header.Set("X-Machine-ID", machineID)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("MaClawSrv 没有在时限内返回结果")
		}
		return fmt.Errorf("MaClawSrv 不可达")
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &failure)
		if strings.TrimSpace(failure.Message) != "" {
			return fmt.Errorf("%s", strings.TrimSpace(failure.Message))
		}
		return fmt.Errorf("MaClawSrv 不可达")
	}
	if dest == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("MaClawSrv 没有返回结果")
	}
	return nil
}

func desktopBotInfo(bot hubBot) DesktopBotInfo {
	return DesktopBotInfo{
		ID:          bot.ID,
		Title:       bot.Name,
		Description: bot.Description,
		InstanceID:  bot.InstanceID,
	}
}
