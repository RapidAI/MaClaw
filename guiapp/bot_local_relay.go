package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The local relay lets the person's cloud bots reach this computer: files in
// folders allowed in Bot 管理 → 设置 → 本地访问, and hosts on this computer's
// network. This file keeps the outbound link to Hub, stores the policy on
// this computer, and shows the questions. corelib/localrelay does the rest.

const (
	localRelayPromptEvent   = "bot-local-access-prompt"
	localRelayPromptDone    = "bot-local-access-prompt-done"
	localRelayTransferEvent = "desktop-bot-local-transfer"
	localRelayLinkEvent     = "bot-local-access-link"
)

// BotLocalAccessState is what the settings tab shows.
type BotLocalAccessState struct {
	Settings  localrelay.Settings `json:"settings"`
	Connected bool                `json:"connected"`
	Home      string              `json:"home"`
	LinkError string              `json:"link_error,omitempty"`
}

type localRelayRuntime struct {
	mu        sync.Mutex
	started   bool
	connected bool
	linkError string
	agent     *localrelay.Agent
	pending   map[string]localRelayPending
	cancel    context.CancelFunc
}

type localRelayPending struct {
	prompt localrelay.Prompt
	answer chan localrelay.Answer
}

var localRelayState localRelayRuntime

// localRelaySettingsMu guards the settings file.
var localRelaySettingsMu sync.Mutex

func localRelaySettingsPath() string {
	return filepath.Join(maclawpath.DataDir(), "bot_local_access.json")
}

func localRelayHome() string {
	home, _ := os.UserHomeDir()
	return home
}

// loadLocalRelaySettings reads the policy; a missing file is the default.
func loadLocalRelaySettings() localrelay.Settings {
	localRelaySettingsMu.Lock()
	defer localRelaySettingsMu.Unlock()
	return loadLocalRelaySettingsLocked()
}

func loadLocalRelaySettingsLocked() localrelay.Settings {
	raw, err := os.ReadFile(localRelaySettingsPath())
	if err != nil {
		return localrelay.DefaultSettings(localRelayHome()).Normalize()
	}
	var s localrelay.Settings
	if json.Unmarshal(raw, &s) != nil {
		// A damaged file falls back to the safe default, never to "all on".
		return localrelay.DefaultSettings(localRelayHome()).Normalize()
	}
	return s.Normalize()
}

func saveLocalRelaySettingsLocked(s localrelay.Settings) error {
	s = s.Normalize()
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	p := localRelaySettingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func rememberLocalRelayApproval(key string) error {
	localRelaySettingsMu.Lock()
	defer localRelaySettingsMu.Unlock()
	s := loadLocalRelaySettingsLocked()
	s.Approved = append(s.Approved, key)
	return saveLocalRelaySettingsLocked(s)
}

// startLocalRelay keeps the link to Hub up for the life of the app.
func (a *App) startLocalRelay() {
	localRelayState.mu.Lock()
	if localRelayState.started {
		localRelayState.mu.Unlock()
		return
	}
	localRelayState.started = true
	ctx, cancel := context.WithCancel(context.Background())
	localRelayState.cancel = cancel
	localRelayState.agent = &localrelay.Agent{
		Home:     localRelayHome(),
		Load:     loadLocalRelaySettings,
		Remember: rememberLocalRelayApproval,
		Ask:      a.askLocalRelay,
		Notify:   func(ev localrelay.TransferEvent) { a.emitEvent(localRelayTransferEvent, ev) },
	}
	agent := localRelayState.agent
	localRelayState.mu.Unlock()
	go a.localRelayLoop(ctx, agent)
}

func (a *App) stopLocalRelay() {
	localRelayState.mu.Lock()
	cancel := localRelayState.cancel
	localRelayState.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func setLocalRelayLink(a *App, connected bool, errText string) {
	localRelayState.mu.Lock()
	changed := localRelayState.connected != connected || localRelayState.linkError != errText
	localRelayState.connected, localRelayState.linkError = connected, errText
	localRelayState.mu.Unlock()
	if changed {
		a.emitEvent(localRelayLinkEvent, map[string]any{"connected": connected, "error": errText})
	}
}

type localRelayDialError struct {
	status int
	err    error
}

func (e *localRelayDialError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("Hub answered HTTP %d", e.status)
	}
	return e.err.Error()
}

func (a *App) localRelayLoop(ctx context.Context, agent *localrelay.Agent) {
	failures := 0
	for ctx.Err() == nil {
		s := loadLocalRelaySettings()
		if !s.FilesEnabled && !s.NetworkEnabled {
			setLocalRelayLink(a, false, "")
			if sleepCtx(ctx, 30*time.Second) != nil {
				return
			}
			continue
		}
		started := time.Now()
		err := a.localRelayOnce(ctx, agent)
		setLocalRelayLink(a, false, errText(err))
		if ctx.Err() != nil {
			return
		}
		var de *localRelayDialError
		wait := 2 * time.Second
		switch {
		case errors.As(err, &de) && (de.status == http.StatusNotFound || de.status == http.StatusMethodNotAllowed):
			// An older Hub without the relay: look again later.
			wait = 10 * time.Minute
		case errors.As(err, &de) && (de.status == http.StatusUnauthorized || de.status == http.StatusForbidden):
			wait = 2 * time.Minute
		default:
			if time.Since(started) > time.Minute {
				failures = 0
			}
			failures++
			wait = time.Duration(1<<min(failures, 5)) * time.Second
		}
		if sleepCtx(ctx, wait) != nil {
			return
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (a *App) localRelayOnce(ctx context.Context, agent *localrelay.Agent) error {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return &localRelayDialError{err: err}
	}
	wsURL := hubURL
	switch {
	case strings.HasPrefix(wsURL, "https://"):
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	case strings.HasPrefix(wsURL, "http://"):
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	if cfg, loadErr := a.LoadConfig(); loadErr == nil {
		if machineID := strings.TrimSpace(groupDiscussionAgentID(cfg)); machineID != "" {
			header.Set("X-Machine-ID", machineID)
		}
	}
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 20 * time.Second, ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	ws, resp, err := dialer.DialContext(dctx, wsURL+localrelay.LinkPath, header)
	cancel()
	if err != nil {
		if resp != nil {
			return &localRelayDialError{status: resp.StatusCode, err: err}
		}
		return &localRelayDialError{err: err}
	}
	setLocalRelayLink(a, true, "")
	log.Printf("[local-relay] linked to %s", strings.TrimSpace(hubURL))
	err = agent.ServeGUI(ctx, localrelay.NewWSConn(ws))
	log.Printf("[local-relay] link ended: %v", err)
	return err
}

// askLocalRelay shows one question and waits for AnswerBotLocalAccessPrompt.
func (a *App) askLocalRelay(ctx context.Context, p localrelay.Prompt) localrelay.Answer {
	ch := make(chan localrelay.Answer, 1)
	localRelayState.mu.Lock()
	if localRelayState.pending == nil {
		localRelayState.pending = map[string]localRelayPending{}
	}
	localRelayState.pending[p.ID] = localRelayPending{prompt: p, answer: ch}
	localRelayState.mu.Unlock()
	defer func() {
		localRelayState.mu.Lock()
		delete(localRelayState.pending, p.ID)
		localRelayState.mu.Unlock()
		a.emitEvent(localRelayPromptDone, p.ID)
	}()
	a.emitEvent(localRelayPromptEvent, p)
	if FlashAndBeep != nil {
		FlashAndBeep()
	}
	if ShowNotification != nil {
		who := strings.TrimSpace(p.BotName)
		if who == "" {
			who = "Bot"
		}
		ShowNotification("MaClaw："+who+" 请求访问这台电脑", trayBalloonBody(localRelayPromptLine(p)), 2)
	}
	select {
	case ans := <-ch:
		return ans
	case <-ctx.Done():
		return localrelay.AnswerDeny
	}
}

func localRelayPromptLine(p localrelay.Prompt) string {
	switch p.Kind {
	case "network":
		return "连接 " + p.Target + "，请在 MaClaw 窗口里允许或拒绝。"
	case "write":
		return "保存文件到 " + p.Target + "，请在 MaClaw 窗口里允许或拒绝。"
	case "list":
		return "查看文件夹 " + p.Target + "，请在 MaClaw 窗口里允许或拒绝。"
	}
	return "读取 " + p.Target + "，请在 MaClaw 窗口里允许或拒绝。"
}

// AnswerBotLocalAccessPrompt answers one question: deny, once, or always.
func (a *App) AnswerBotLocalAccessPrompt(id, answer string) error {
	localRelayState.mu.Lock()
	p, ok := localRelayState.pending[strings.TrimSpace(id)]
	localRelayState.mu.Unlock()
	if !ok {
		return fmt.Errorf("这个请求已经结束（超时或已回答）")
	}
	ans := localrelay.Answer(strings.TrimSpace(answer))
	switch ans {
	case localrelay.AnswerOnce, localrelay.AnswerAlways, localrelay.AnswerDeny:
	default:
		ans = localrelay.AnswerDeny
	}
	select {
	case p.answer <- ans:
	default:
	}
	return nil
}

// GetPendingBotLocalAccessPrompts lists questions still waiting, for a view
// that mounted after they were raised.
func (a *App) GetPendingBotLocalAccessPrompts() []localrelay.Prompt {
	localRelayState.mu.Lock()
	defer localRelayState.mu.Unlock()
	out := make([]localrelay.Prompt, 0, len(localRelayState.pending))
	for _, p := range localRelayState.pending {
		out = append(out, p.prompt)
	}
	return out
}

func localRelaySnapshot(s localrelay.Settings) *BotLocalAccessState {
	localRelayState.mu.Lock()
	defer localRelayState.mu.Unlock()
	return &BotLocalAccessState{Settings: s, Connected: localRelayState.connected, Home: localRelayHome(), LinkError: localRelayState.linkError}
}

// GetBotLocalAccess returns the policy and the link state for the settings tab.
func (a *App) GetBotLocalAccess() (*BotLocalAccessState, error) {
	return localRelaySnapshot(loadLocalRelaySettings()), nil
}

// SaveBotLocalAccess stores the policy on this computer. Relative folders
// are refused so a root cannot silently mean "wherever the app started".
func (a *App) SaveBotLocalAccess(in localrelay.Settings) (*BotLocalAccessState, error) {
	for _, r := range in.Roots {
		p := localrelay.ExpandHome(r.Path, localRelayHome())
		if strings.TrimSpace(p) != "" && !filepath.IsAbs(p) {
			return nil, fmt.Errorf("文件夹要写完整路径：%s", r.Path)
		}
	}
	for i := range in.Roots {
		in.Roots[i].Path = localrelay.ExpandHome(in.Roots[i].Path, localRelayHome())
	}
	localRelaySettingsMu.Lock()
	err := saveLocalRelaySettingsLocked(in)
	s := loadLocalRelaySettingsLocked()
	localRelaySettingsMu.Unlock()
	if err != nil {
		return nil, err
	}
	a.startLocalRelay()
	return localRelaySnapshot(s), nil
}

// PickBotLocalAccessFolder opens the folder chooser for a new allowed root.
func (a *App) PickBotLocalAccessFolder() (string, error) {
	if a == nil || a.ctx == nil {
		return "", fmt.Errorf("window is not ready")
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择允许 Bot 访问的文件夹", DefaultDirectory: localRelayHome()})
}
