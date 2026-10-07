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

// desktopBotRelayOverride is set by tests. Production posts to Hub.
var desktopBotRelayOverride func(ctx context.Context, botID, text string) (reply, novncURL string, userControl bool, err error)

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

func (a *App) finishDesktopBotTask(requestID, sessionKey, botID, text string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if desktopBotRelayOverride == nil {
		go a.publishDesktopBotView(ctx, requestID, sessionKey, botID)
	}
	reply, novnc, userControl, err := a.relayDesktopBot(ctx, botID, text)
	resp := &IMAgentResponse{RequestID: requestID, SessionKey: sessionKey, EventScopeID: sessionKey}
	if err != nil {
		resp.Error = err.Error()
		if novnc, userControl, ok := a.desktopAfterTimeout(botID, err); ok {
			resp.DesktopHandoffURL = novnc
			resp.DesktopUserControl = userControl
			if userControl {
				resp.Text = "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。"
				resp.Error = ""
			}
		} else if novnc, ok := a.desktopFailureKeepsLogin(botID, err); ok {
			resp.DesktopHandoffURL = novnc
			resp.DesktopUserControl = true
		}
	} else {
		resp.Text = reply
		resp.DesktopHandoffURL = novnc
		resp.DesktopUserControl = userControl
	}
	a.emitAIAssistantResponse(requestID, resp)
}

func (a *App) relayDesktopBot(ctx context.Context, botID, text string) (string, string, bool, error) {
	if desktopBotRelayOverride != nil {
		return desktopBotRelayOverride(ctx, botID, text)
	}
	var out struct {
		Text     string `json:"text"`
		NovncURL string `json:"novnc_url"`
		Handoff  bool   `json:"handoff"`
	}
	err := a.desktopBotCall(ctx, http.MethodPost, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/messages", map[string]string{
		"content": text,
	}, &out, 8*time.Minute)
	if err != nil {
		return "", "", false, err
	}
	if strings.TrimSpace(out.Text) == "" {
		return "", "", false, fmt.Errorf("MaClawSrv 没有返回结果")
	}
	return out.Text, a.absoluteHubPath(out.NovncURL), out.Handoff, nil
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

// publishDesktopBotView polls Hub until this user's desktop is up and tells
// the chat to show it while the instance is still working.
func (a *App) publishDesktopBotView(ctx context.Context, requestID, sessionKey, botID string) {
	var lastURL string
	var lastControl bool
	poll := func() {
		if ctx.Err() != nil {
			return
		}
		var out struct {
			NovncURL    string `json:"novnc_url"`
			UserControl bool   `json:"user_control"`
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := a.desktopBotCall(callCtx, http.MethodGet, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/desktop", nil, &out, 5*time.Second)
		cancel()
		if err != nil || ctx.Err() != nil {
			return
		}
		novnc := a.absoluteHubPath(out.NovncURL)
		if novnc == "" || (novnc == lastURL && out.UserControl == lastControl) {
			return
		}
		lastURL = novnc
		lastControl = out.UserControl
		raw, err := json.Marshal(map[string]any{
			"request_id":   requestID,
			"session_key":  sessionKey,
			"novnc_url":    novnc,
			"user_control": out.UserControl,
		})
		if err != nil {
			return
		}
		a.emitEvent("desktop-bot-view", string(raw))
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
	if desktopBotRelayOverride != nil || !desktopCommandTimedOut(err) {
		return "", false, false
	}
	novnc, userControl, watchErr := a.watchDesktopBot(context.Background(), botID)
	if watchErr != nil || strings.TrimSpace(novnc) == "" {
		return "", false, false
	}
	return novnc, userControl, true
}

func (a *App) desktopFailureKeepsLogin(botID string, err error) (string, bool) {
	if desktopBotRelayOverride != nil || !keepLoginDesktopAfterFailure(err, "pending", true) {
		return "", false
	}
	novnc, userControl, watchErr := a.watchDesktopBot(context.Background(), botID)
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

func (a *App) watchDesktopBot(ctx context.Context, botID string) (string, bool, error) {
	var out struct {
		NovncURL    string `json:"novnc_url"`
		UserControl bool   `json:"user_control"`
	}
	err := a.desktopBotCall(ctx, http.MethodGet, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/desktop", nil, &out, 5*time.Second)
	if err != nil {
		return "", false, err
	}
	return a.absoluteHubPath(out.NovncURL), out.UserControl, nil
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
