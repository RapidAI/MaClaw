package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/botlog"
)

const desktopBotDisabledMessage = "服务器没有开通bot功能"

// desktopBotRelayResult is one Hub reply for a bot message.
// A desktop address is not a handoff unless the person has the keyboard
// or Hub named an attention reason.
// DesktopBotImage is one cloud-desktop screenshot for the bot bubble.
// Data is standard base64 with no data: prefix.
type DesktopBotImage struct {
	MIME string `json:"mime"`
	Data string `json:"data"`
}

// DesktopBotFile is one document attached to the bot reply.
// Data is standard base64 with no data: prefix.
type DesktopBotFile struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

type desktopBotRelayResult struct {
	Text               string
	NovncURL           string
	UserControl        bool
	AttentionReason    string
	AskUserInputType   string
	AskUserSecretName  string
	AskUserQuestion    string
	AskUserOptionsJSON string
	Images             []DesktopBotImage
	Files              []DesktopBotFile
}

// desktopBotRelayOverride is set by tests. Production posts to Hub.
var desktopBotRelayOverride func(ctx context.Context, botID, text, phase string) (desktopBotRelayResult, error)

// desktopBotExchangeOverride is set by tests of the admit-and-poll loop.
// Production talks to Hub.
var desktopBotExchangeOverride func(ctx context.Context, method, path string, body any, dest any, timeout time.Duration, headers map[string]string) (int, error)

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
// CreatedAt is the Hub bot's created_at (RFC3339), not the time the list was loaded.
type DesktopBotInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	InstanceID  string `json:"instance_id"`
	CreatedAt   string `json:"created_at"`
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
func (a *App) WatchDesktopBot(botID string, epoch int) (*DesktopBotWatch, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("bot id is required")
	}
	var out struct {
		NovncURL        string `json:"novnc_url"`
		UserControl     bool   `json:"user_control"`
		AttentionReason string `json:"attention_reason"`
	}
	// The generation travels with every poll of this open panel. Hub ignores
	// a release from the panel that was just closed when a newer one is up.
	var body any
	if epoch > 0 {
		body = map[string]int64{"epoch": int64(epoch)}
	}
	// 60s matches the Hub watch-open budget. The poll used to give up at 15s
	// and the panel showed a disconnect while the container was still starting.
	err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/desktop", body, &out, 60*time.Second)
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
func (a *App) ReleaseDesktopBotWatch(botID string, epoch int) error {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return fmt.Errorf("bot id is required")
	}
	payload := map[string]any{"release": true}
	if epoch > 0 {
		payload["epoch"] = epoch
	}
	return a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/desktop", payload, nil, 15*time.Second)
}

type hubBot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	InstanceID  string `json:"instance_id"`
	CreatedAt   string `json:"created_at"`
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
	started := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	botlog.Write(botID, "gui.relay_begin", nil, "request_id", requestID, "phase", phase, "text_len", strconv.Itoa(len(text)))
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
		// The screenshot lives on this machine. The event carries the path, and
		// the bytes stay out of the chat transcript. Bytes remain only when the
		// file could not be written.
		shots := cleanDesktopBotImages(result.Images)
		resp.DesktopShotPaths = storeDesktopBotShots(botID, shots)
		if len(resp.DesktopShotPaths) == 0 {
			resp.DesktopImages = shots
		}
		// The document lives on this machine. The event carries the path, and
		// the bytes stay out of the chat transcript.
		saved, left := storeDesktopBotFiles(botID, cleanDesktopBotFiles(result.Files))
		resp.LocalFilePaths = saved
		resp.DesktopFiles = left
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
	botlog.Write(botID, "gui.relay_end", err,
		"request_id", requestID,
		"phase", phase,
		"text_len", strconv.Itoa(len(resp.Text)),
		"images", strconv.Itoa(len(resp.DesktopImages)),
		"shots", strconv.Itoa(len(resp.DesktopShotPaths)),
		"files", strconv.Itoa(len(resp.DesktopFiles)),
		"local_files", strconv.Itoa(len(resp.LocalFilePaths)),
		"handoff", strconv.FormatBool(resp.DesktopHandoffURL != ""),
		"user_control", strconv.FormatBool(resp.DesktopUserControl),
		"attention", resp.DesktopAttentionReason,
		"ask", strconv.FormatBool(resp.AskUserQuestion != "" || resp.AskUserSecretName != ""),
		"dur_ms", strconv.FormatInt(time.Since(started).Milliseconds(), 10),
	)
}

func (a *App) relayDesktopBot(ctx context.Context, botID, text, phase string) (desktopBotRelayResult, error) {
	if desktopBotRelayOverride != nil {
		return desktopBotRelayOverride(ctx, botID, text, phase)
	}
	var out struct {
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
		Accepted           bool              `json:"accepted"`
		RunID              string            `json:"run_id"`
		Status             string            `json:"status"`
	}
	body := map[string]string{"content": text}
	if phase = strings.TrimSpace(phase); phase != "" {
		body["phase"] = phase
	}
	// Prefer returns as soon as the run exists. An older Hub ignores it and
	// still writes the reply on this same call, so the wait stays 30 minutes
	// for that case only. A 202 is not the result.
	status, err := a.desktopBotExchange(ctx, http.MethodPost, "/api/v1/bots/"+url.PathEscape(strings.TrimSpace(botID))+"/messages", body, &out, 30*time.Minute, map[string]string{"Prefer": "respond-async"})
	if err != nil {
		return desktopBotRelayResult{}, err
	}
	if status == http.StatusAccepted && strings.TrimSpace(out.RunID) != "" && strings.TrimSpace(out.Text) == "" {
		botlog.Write(botID, "gui.admit", nil,
			"run", strings.TrimSpace(out.RunID),
			"http", strconv.Itoa(status),
			"phase", phase,
		)
		return a.waitDesktopBotRun(ctx, botID, out.RunID)
	}
	botlog.Write(botID, "gui.admit", nil,
		"mode", "sync",
		"http", strconv.Itoa(status),
		"phase", phase,
		"text_len", strconv.Itoa(len(strings.TrimSpace(out.Text))),
	)
	return a.desktopRelayFromWire(out.Text, out.NovncURL, out.AttentionReason, out.AskUserInputType, out.AskUserSecretName, out.AskUserQuestion, out.AskUserOptionsJSON, out.Handoff, out.Images, out.Files)
}

// desktopBotPollInterval is the gap between short reads of an admitted run.
// One slow read is tried again. The run itself is not limited by this gap.
var desktopBotPollInterval = time.Second

func (a *App) desktopRelayFromWire(text, novnc, attention, askType, askName, askQuestion, askOptions string, handoff bool, images []DesktopBotImage, files []DesktopBotFile) (desktopBotRelayResult, error) {
	if strings.TrimSpace(text) == "" {
		return desktopBotRelayResult{}, fmt.Errorf("MaClawSrv 没有返回结果")
	}
	return desktopBotRelayResult{
		Text:               text,
		NovncURL:           a.absoluteHubPath(novnc),
		UserControl:        handoff,
		AttentionReason:    strings.TrimSpace(attention),
		AskUserInputType:   strings.TrimSpace(askType),
		AskUserSecretName:  strings.TrimSpace(askName),
		AskUserQuestion:    strings.TrimSpace(askQuestion),
		AskUserOptionsJSON: strings.TrimSpace(askOptions),
		Images:             cleanDesktopBotImages(images),
		Files:              cleanDesktopBotFiles(files),
	}, nil
}

// waitDesktopBotRun reads the admitted run until it finishes. Each read is
// short. A dropped read is tried again and does not cancel the run. An HTTP
// status ends this turn.
func (a *App) waitDesktopBotRun(ctx context.Context, botID, runID string) (desktopBotRelayResult, error) {
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/runs/" + url.PathEscape(strings.TrimSpace(runID))
	polls := 0
	// WriteOnce keys only the stage and the error text. A later state would
	// stay hidden for the repeat window, so a changed state is written now.
	lastState := ""
	writePoll := func(state string, err error, kv ...string) {
		if state != lastState {
			botlog.Write(botID, "gui.run_poll", err, kv...)
			lastState = state
			return
		}
		botlog.WriteOnce(botID, "gui.run_poll", err, kv...)
	}
	for {
		if err := ctx.Err(); err != nil {
			botlog.Write(botID, "gui.run_poll", err, "run", runID, "polls", strconv.Itoa(polls))
			return desktopBotRelayResult{}, err
		}
		var out struct {
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
			Accepted           bool              `json:"accepted"`
			Status             string            `json:"status"`
		}
		callCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		status, err := a.desktopBotExchange(callCtx, http.MethodGet, path, nil, &out, 20*time.Second, nil)
		cancel()
		polls++
		if err != nil {
			if desktopBotPollAgain(status, err) {
				writePoll("retry", err,
					"run", runID,
					"http", strconv.Itoa(status),
					"retry", "yes",
					"polls", strconv.Itoa(polls),
				)
				if sleepErr := sleepDesktopBotPoll(ctx, desktopBotPollInterval); sleepErr != nil {
					botlog.Write(botID, "gui.run_poll", sleepErr, "run", runID, "polls", strconv.Itoa(polls))
					return desktopBotRelayResult{}, sleepErr
				}
				continue
			}
			botlog.Write(botID, "gui.run_poll", err,
				"run", runID,
				"http", strconv.Itoa(status),
				"retry", "no",
				"polls", strconv.Itoa(polls),
			)
			return desktopBotRelayResult{}, err
		}
		if status == http.StatusAccepted || out.Status == "running" || (out.Accepted && strings.TrimSpace(out.Text) == "") {
			state := strings.TrimSpace(out.Status)
			if state == "" {
				state = "accepted"
			}
			writePoll("open:"+state, nil,
				"run", runID,
				"http", strconv.Itoa(status),
				"status", out.Status,
				"polls", strconv.Itoa(polls),
			)
			if sleepErr := sleepDesktopBotPoll(ctx, desktopBotPollInterval); sleepErr != nil {
				botlog.Write(botID, "gui.run_poll", sleepErr, "run", runID, "polls", strconv.Itoa(polls))
				return desktopBotRelayResult{}, sleepErr
			}
			continue
		}
		botlog.Write(botID, "gui.run_poll", nil,
			"run", runID,
			"http", strconv.Itoa(status),
			"status", out.Status,
			"text_len", strconv.Itoa(len(strings.TrimSpace(out.Text))),
			"images", strconv.Itoa(len(out.Images)),
			"files", strconv.Itoa(len(out.Files)),
			"handoff", strconv.FormatBool(out.Handoff),
			"attention", strings.TrimSpace(out.AttentionReason),
			"polls", strconv.Itoa(polls),
		)
		return a.desktopRelayFromWire(out.Text, out.NovncURL, out.AttentionReason, out.AskUserInputType, out.AskUserSecretName, out.AskUserQuestion, out.AskUserOptionsJSON, out.Handoff, out.Images, out.Files)
	}
}

// desktopBotPollAgain retries when the read produced no HTTP status.
// Status 0 is a dropped connection or a deadline. Any status the server
// sent, including an empty 502 that becomes "不可达", ends the turn.
func desktopBotPollAgain(status int, err error) bool {
	if err == nil || status != 0 {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "不可达") || strings.Contains(text, "没有在时限内")
}

func sleepDesktopBotPoll(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		wait = time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// cleanDesktopBotImages keeps one PNG or JPEG screenshot. The bytes never
// go into a log line; callers only record how many survived.
func desktopBotImageData(data string) bool {
	const maxShot = 1_200_000
	if data == "" || len(data) > maxShot || len(data)%4 != 0 {
		return false
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == '=' {
			if i < len(data)-2 {
				return false
			}
			continue
		}
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return false
		}
	}
	return true
}

func cleanDesktopBotFiles(files []DesktopBotFile) []DesktopBotFile {
	const maxFile = 200_000
	for i := len(files) - 1; i >= 0; i-- {
		name := strings.TrimSpace(files[i].Name)
		if !desktopBotDeliverName(name) {
			continue
		}
		mime := desktopBotDeliverMIME(files[i].MIME)
		if mime == "" {
			continue
		}
		data := strings.TrimSpace(files[i].Data)
		if data == "" || len(data) > maxFile || !desktopBotImageDataSized(data, maxFile) {
			continue
		}
		return []DesktopBotFile{{Name: name, MIME: mime, Data: data}}
	}
	return nil
}

func desktopBotDeliverName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || strings.HasPrefix(name, ".") || len([]rune(name)) > 80 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func desktopBotDeliverMIME(raw string) string {
	mime := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == "" {
		return "application/octet-stream"
	}
	parts := strings.Split(mime, "/")
	if len(parts) != 2 {
		return ""
	}
	for _, part := range parts {
		if part == "" || len(part) > 127 {
			return ""
		}
		for j := 0; j < len(part); j++ {
			c := part[j]
			letter := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
			if j == 0 {
				if !letter {
					return ""
				}
				continue
			}
			if !letter && c != '.' && c != '+' && c != '_' && c != '-' {
				return ""
			}
		}
	}
	return mime
}

// storeDesktopBotShots writes each screenshot next to that bot's documents.
// The returned paths are absolute. A shot that cannot be written is omitted
// so the caller can still offer the bytes for this turn.
func storeDesktopBotShots(botID string, images []DesktopBotImage) []string {
	id := safeDesktopBotDir(botID)
	if id == "" || len(images) == 0 {
		return nil
	}
	dir := filepath.Join(corelib.MaclawDataDir(), "bot-files", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	var paths []string
	for _, image := range images {
		raw, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil || len(raw) == 0 {
			continue
		}
		name := "screenshot.png"
		if image.MIME == "image/jpeg" {
			name = "screenshot.jpg"
		}
		dest := uniqueBotFile(dir, name)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			continue
		}
		abs, err := filepath.Abs(dest)
		if err != nil {
			abs = dest
		}
		paths = append(paths, abs)
	}
	return paths
}

// ReadDesktopBotShot returns one screenshot saved for a bot bubble.
// The path has to stay under this machine's bot-files directory.
func (a *App) ReadDesktopBotShot(path string) (*DesktopBotImage, error) {
	abs, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return nil, fmt.Errorf("screenshot is not on this machine")
	}
	root, err := filepath.Abs(filepath.Join(corelib.MaclawDataDir(), "bot-files"))
	if err != nil || !desktopBotPathInside(root, abs) {
		return nil, fmt.Errorf("screenshot is not on this machine")
	}
	mime := ""
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	default:
		return nil, fmt.Errorf("screenshot is not on this machine")
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > 1_000_000 {
		return nil, fmt.Errorf("screenshot is not on this machine")
	}
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("screenshot is not on this machine")
	}
	return &DesktopBotImage{MIME: mime, Data: base64.StdEncoding.EncodeToString(raw)}, nil
}

func desktopBotPathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// storeDesktopBotFiles writes each document under the maclaw data directory.
// A file that cannot be written stays in the returned slice so the bubble can
// still offer those bytes. Saved paths are absolute and contain no file bytes.
func storeDesktopBotFiles(botID string, files []DesktopBotFile) (paths []string, left []DesktopBotFile) {
	id := safeDesktopBotDir(botID)
	if id == "" || len(files) == 0 {
		return nil, files
	}
	dir := filepath.Join(corelib.MaclawDataDir(), "bot-files", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, files
	}
	for _, file := range files {
		raw, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil || len(raw) == 0 {
			left = append(left, file)
			continue
		}
		dest := uniqueBotFile(dir, file.Name)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			left = append(left, file)
			continue
		}
		abs, err := filepath.Abs(dest)
		if err != nil {
			abs = dest
		}
		paths = append(paths, abs)
	}
	return paths, left
}

func safeDesktopBotDir(botID string) string {
	id := strings.TrimSpace(botID)
	if id == "" || len(id) > 80 || strings.Contains(id, "..") {
		return ""
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return ""
		}
	}
	return id
}

func uniqueBotFile(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err != nil {
		return dest
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	stamp := time.Now().Format("150405")
	for n := 0; n < 100; n++ {
		suffix := stamp
		if n > 0 {
			suffix = fmt.Sprintf("%s-%d", stamp, n)
		}
		candidate := filepath.Join(dir, base+"-"+suffix+ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return dest
}

func desktopBotImageDataSized(data string, max int) bool {
	if data == "" || len(data) > max || len(data)%4 != 0 {
		return false
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == '=' {
			if i < len(data)-2 {
				return false
			}
			continue
		}
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return false
		}
	}
	return true
}

func cleanDesktopBotImages(images []DesktopBotImage) []DesktopBotImage {
	for i := len(images) - 1; i >= 0; i-- {
		mime := strings.ToLower(strings.TrimSpace(images[i].MIME))
		if mime != "image/png" && mime != "image/jpeg" {
			continue
		}
		data := strings.TrimSpace(images[i].Data)
		if !desktopBotImageData(data) {
			continue
		}
		return []DesktopBotImage{{MIME: mime, Data: data}}
	}
	return nil
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
// A rising handoff is. A falling keyboard handoff on a URL we already
// announced is, so the viewer can release it. The first look at a turn can
// already find the keyboard back: that has to be announced too, or a handoff
// the person collapsed stays latched and the next login never opens. A later
// quiet address change is not a handoff and stays silent.
func desktopViewShouldEmit(prev, next desktopViewSnap) bool {
	prev = normalizeDesktopView(prev)
	next = normalizeDesktopView(next)
	if prev == next {
		return false
	}
	if desktopViewHasAttention(prev) || desktopViewHasAttention(next) {
		return true
	}
	var unseen desktopViewSnap
	return prev == unseen && next.url != ""
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
			if err != nil && ctx.Err() == nil {
				botlog.WriteOnce(botID, "gui.desktop_poll", err, "request_id", requestID)
			}
			return
		}
		prev := last
		last = a.announceDesktopView(requestID, sessionKey, last, desktopViewSnap{
			url: novnc, control: control, reason: reason,
		})
		if desktopViewShouldEmit(prev, last) {
			botlog.Write(botID, "gui.desktop_view", nil,
				"request_id", requestID,
				"url_set", strconv.FormatBool(last.url != ""),
				"user_control", strconv.FormatBool(last.control),
				"attention", last.reason,
			)
		}
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

// desktopBotResponseLimit matches Hub. A message reply carries one desktop
// screenshot of at most 1_200_000 base64 characters, plus the text. A shorter
// read cuts the JSON in half and the bubble loses both.
func desktopBotResponseLimit(path string) int64 {
	// An admitted run is read from /runs and can carry the same screenshot.
	if strings.Contains(path, "/messages") || strings.Contains(path, "/runs/") {
		return 1<<20 + 1_200_000
	}
	return 1 << 20
}

func (a *App) desktopBotCall(ctx context.Context, method, path string, body any, dest any, timeout time.Duration) error {
	_, err := a.desktopBotExchange(ctx, method, path, body, dest, timeout, nil)
	return err
}

func (a *App) desktopBotExchange(ctx context.Context, method, path string, body any, dest any, timeout time.Duration, headers map[string]string) (int, error) {
	if desktopBotExchangeOverride != nil {
		return desktopBotExchangeOverride(ctx, method, path, body, dest, timeout, headers)
	}
	if a == nil {
		return 0, fmt.Errorf("AI assistant backend is unavailable")
	}
	started := time.Now()
	botID := botIDFromHubPath(path)
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		a.logDesktopBotCall(botID, method, path, "", 0, time.Since(started), err)
		return 0, fmt.Errorf("MaClawSrv 不可达")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), 0, time.Since(started), err)
			return 0, err
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
		a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), 0, time.Since(started), err)
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if cfg, loadErr := a.LoadConfig(); loadErr == nil {
		if machineID := strings.TrimSpace(groupDiscussionAgentID(cfg)); machineID != "" {
			req.Header.Set("X-Machine-ID", machineID)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), 0, time.Since(started), err)
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, fmt.Errorf("MaClawSrv 没有在时限内返回结果")
		}
		return 0, fmt.Errorf("MaClawSrv 不可达")
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, desktopBotResponseLimit(path)))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &failure)
		callErr := fmt.Errorf("MaClawSrv 不可达")
		if strings.TrimSpace(failure.Message) != "" {
			callErr = fmt.Errorf("%s", strings.TrimSpace(failure.Message))
		}
		a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), resp.StatusCode, time.Since(started), fmt.Errorf("%w body=%s", callErr, string(payload)))
		return resp.StatusCode, callErr
	}
	a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), resp.StatusCode, time.Since(started), nil)
	if dest == nil || len(bytes.TrimSpace(payload)) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		a.logDesktopBotCall(botID, method, path, botlog.Host(hubURL), resp.StatusCode, time.Since(started), err)
		return resp.StatusCode, fmt.Errorf("MaClawSrv 没有返回结果")
	}
	return resp.StatusCode, nil
}

// logDesktopBotCall records the Hub call the chat would otherwise collapse
// into "不可达". Desktop polls stay quiet unless they fail.
func (a *App) logDesktopBotCall(botID, method, path, hub string, status int, dur time.Duration, err error) {
	if botID == "" {
		return
	}
	// Run polls have their own gui.run_poll lines. A successful read every
	// second would hide the admit and the finished reply.
	if strings.Contains(path, "/runs/") {
		return
	}
	quiet := strings.HasSuffix(path, "/desktop")
	if err == nil && quiet {
		return
	}
	fields := []string{
		"method", method,
		"path", path,
		"hub", hub,
		"status", strconv.Itoa(status),
		"dur_ms", strconv.FormatInt(dur.Milliseconds(), 10),
	}
	if quiet {
		botlog.WriteOnce(botID, "gui.hub", err, fields...)
		return
	}
	botlog.Write(botID, "gui.hub", err, fields...)
}

func botIDFromHubPath(path string) string {
	const prefix = "/api/v1/bots/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	id, _, _ := strings.Cut(rest, "/")
	id, err := url.PathUnescape(strings.TrimSpace(id))
	if err != nil {
		return ""
	}
	if id == "" || id == "access" {
		return ""
	}
	return id
}

func desktopBotInfo(bot hubBot) DesktopBotInfo {
	return DesktopBotInfo{
		ID:          bot.ID,
		Title:       bot.Name,
		Description: bot.Description,
		InstanceID:  bot.InstanceID,
		CreatedAt:   strings.TrimSpace(bot.CreatedAt),
	}
}
