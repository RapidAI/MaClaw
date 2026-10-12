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
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Text            string
	NovncURL        string
	UserControl     bool
	AttentionReason string
	// WaitingForUser means the run reached its terminal state because the worker
	// asked this person something, rather than because the work finished.
	WaitingForUser     bool
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

// DesktopBotLLM is the model setting every bot of this user runs on. Mode is
// "custom" when they picked a provider, and "default" when their bots follow
// whatever the service resolves: the Hub route billed to this user, or the
// provider an administrator selected.
//
// The provider key stays on the server. This struct carries enough to describe
// the selection to the person who made it, and nothing that would put a
// credential into the frontend bundle or the chat transcript.
type DesktopBotLLM struct {
	Mode    string `json:"mode"`
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	Model   string `json:"model,omitempty"`
	KeySet  bool   `json:"key_set"`
	Warning string `json:"warning,omitempty"`
}

// DesktopBotLLMInput is one save of that setting. A blank Mode, or "default",
// drops the stored provider and restores the configured default. A blank Key
// keeps the key already stored for this provider.
type DesktopBotLLMInput struct {
	Mode     string `json:"mode"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	WireAPI  string `json:"wire_api"`
}

// GetDesktopBotLLM reads the model setting this user's bots run on.
func (a *App) GetDesktopBotLLM() (*DesktopBotLLM, error) {
	var out DesktopBotLLM
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/llm", nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Mode) == "" {
		out.Mode = "default"
	}
	return &out, nil
}

// SaveDesktopBotLLM applies a provider to every bot of this user and returns
// the setting as stored. A non-empty Warning means the choice was kept but not
// written to all of them yet; the bots pick it up on their next run.
func (a *App) SaveDesktopBotLLM(in DesktopBotLLMInput) (*DesktopBotLLM, error) {
	var out DesktopBotLLM
	if err := a.desktopBotCall(context.Background(), http.MethodPut, "/api/v1/bots/llm", in, &out, 30*time.Second); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Mode) == "" {
		out.Mode = "default"
	}
	return &out, nil
}

// ActiveDesktopRun is one cloud-desktop turn this client has in flight.
//
// Identity reaches the run, not the bot. A bot can have several runs live at
// once: neither the service ingress nor this client serialises turns, so a
// table keyed only by bot id would let a later registration overwrite an
// earlier one and let that earlier turn's unregister clear the wrong entry.
// Every lookup and every removal therefore names bot and run together.
type ActiveDesktopRun struct {
	BotID      string    `json:"bot_id"`
	RunID      string    `json:"run_id"`
	RequestID  string    `json:"request_id"`
	SessionKey string    `json:"session_key"`
	TaskText   string    `json:"task_text"`
	Phase      string    `json:"phase"`
	StartedAt  time.Time `json:"started_at"`
	// WaitingForUser means the run reached its terminal state because the
	// worker asked this person something. Outwardly it looks like a paused
	// task, but the run is over: the answer arrives as the next turn and is
	// rebuilt from session state. Input aimed at it must not be treated as a
	// supplement to something still running.
	WaitingForUser bool `json:"waiting_for_user"`
}

// desktopRunSeparator joins bot and run into one map key. It cannot occur in
// either identifier, so the parts stay recoverable and no run can impersonate
// another by containing the delimiter.
const desktopRunSeparator = "\x00"

var (
	activeDesktopRunsMu sync.Mutex
	activeDesktopRuns   = make(map[string]*ActiveDesktopRun)
)

func desktopRunKey(botID, runID string) string {
	return strings.TrimSpace(botID) + desktopRunSeparator + strings.TrimSpace(runID)
}

// registerActiveDesktopRun records a turn that has been sent and accepted.
func (a *App) registerActiveDesktopRun(botID, requestID, sessionKey, text, phase string) *ActiveDesktopRun {
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	// Before Hub answers with a run id the request id is the only handle we
	// have, so it stands in until setActiveDesktopRunID replaces it. It is
	// unique per send, which keeps concurrent turns apart in the meantime.
	run := &ActiveDesktopRun{
		BotID:      botID,
		RunID:      "",
		RequestID:  requestID,
		SessionKey: sessionKey,
		TaskText:   text,
		Phase:      phase,
		StartedAt:  time.Now(),
	}
	activeDesktopRuns[desktopRunKey(botID, requestID)] = run
	return run
}

// setActiveDesktopRunID swaps the request-id placeholder for the real run id
// once the admit response names it. Until then the entry stays reachable by
// request id and invisible to run-keyed lookups.
func (a *App) setActiveDesktopRunID(botID, requestID, runID string) {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if botID == "" || runID == "" {
		return
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	placeholder := desktopRunKey(botID, requestID)
	run, exists := activeDesktopRuns[placeholder]
	if !exists || run == nil {
		return
	}
	delete(activeDesktopRuns, placeholder)
	run.RunID = runID
	activeDesktopRuns[desktopRunKey(botID, runID)] = run
}

func unregisterActiveDesktopRun(botID, requestID, runID string) {
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	keys := []string{desktopRunKey(botID, runID)}
	if strings.TrimSpace(requestID) != "" {
		keys = append(keys, desktopRunKey(botID, requestID))
	}
	for _, key := range keys {
		if key == desktopRunSeparator {
			continue
		}
		delete(activeDesktopRuns, key)
	}
}

// GetActiveDesktopRuns lists every cloud-desktop turn still believed to be
// running, newest first.
//
// Entries whose run id has not arrived yet are left out: they carry no
// delivery address, and offering one as a supplement target would either fail
// opaquely server-side or invite guessing, both of which the fail-safe rule
// (§3.1f 保守) forbids. They become visible the moment setActiveDesktopRunID
// re-keys them.
//
// More than one is normal. Callers that cannot tell which run a message is
// about must ask the person rather than pick one: routing their words into the
// wrong run cannot be undone once they are part of that run's context.
func (a *App) GetActiveDesktopRuns(botID string) []*ActiveDesktopRun {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	out := make([]*ActiveDesktopRun, 0, len(activeDesktopRuns))
	for key, run := range activeDesktopRuns {
		prefix := botID + desktopRunSeparator
		if !strings.HasPrefix(key, prefix) || run == nil {
			continue
		}
		if strings.TrimSpace(run.RunID) == "" {
			continue
		}
		copied := *run
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// DesktopBotSupplementReceipt tells the caller what became of one message it
// offered as supplementary input. Stage never claims more than happened: until
// the loop reports consumption we say queued, and when no run could take it we
// say so plainly.
type DesktopBotSupplementReceipt struct {
	Accepted     bool     `json:"accepted"`
	RunID        string   `json:"run_id,omitempty"`
	Stage        string   `json:"stage"` // queued | no_active_run | waiting_for_user | ambiguous
	Message      string   `json:"message"`
	SupplementID string   `json:"supplement_id,omitempty"`
	Candidates   []string `json:"candidates,omitempty"`
}

const (
	desktopBotSupplementQueued         = "queued"
	desktopBotSupplementNoActiveRun    = "no_active_run"
	desktopBotSupplementWaitingForUser = "waiting_for_user"
	desktopBotSupplementAmbiguous      = "ambiguous"
)

// AppendDesktopBotSupplement offers one message to a cloud-desktop turn that is
// still running, naming the run explicitly.
//
// Routing is never guessed from the bot alone. When the message names no run
// and more than one is live, the caller gets candidates back and must ask the
// person which turn they meant: delivering words into the wrong run is not
// recoverable once they are in its context.
func (a *App) AppendDesktopBotSupplement(botID, runID, supplement string) (*DesktopBotSupplementReceipt, error) {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	supplement = strings.TrimSpace(supplement)
	if botID == "" || supplement == "" {
		return nil, fmt.Errorf("bot id and supplement are required")
	}
	// Defense in depth for the §3.1f kill switch: the classifier already
	// short-circuits, so a direct call here means someone bypassed it. Saying
	// the channel is closed keeps the caller from learning a run id from this
	// path while the feature is off.
	if desktopSupplementDisabled() {
		botlog.Write(botID, "gui.supplement_classified", nil, "stage", desktopBotSupplementNoActiveRun, "disabled", "true")
		return &DesktopBotSupplementReceipt{Stage: desktopBotSupplementNoActiveRun, Message: "补充通道已关闭，这条会作为新任务发送。"}, nil
	}
	runs := a.GetActiveDesktopRuns(botID)
	if len(runs) == 0 {
		botlog.Write(botID, "gui.supplement_classified", nil, "stage", desktopBotSupplementNoActiveRun)
		return &DesktopBotSupplementReceipt{Stage: desktopBotSupplementNoActiveRun, Message: "当前没有正在执行的任务，这条会作为新任务发送。"}, nil
	}
	var target *ActiveDesktopRun
	if runID != "" {
		for _, run := range runs {
			if run.RunID == runID {
				target = run
				break
			}
		}
	} else if len(runs) == 1 {
		target = runs[0]
	} else {
		candidates := make([]string, 0, len(runs))
		for _, run := range runs {
			candidates = append(candidates, run.RunID)
		}
		botlog.Write(botID, "gui.supplement_classified", nil, "stage", desktopBotSupplementAmbiguous, "candidates", strconv.Itoa(len(candidates)))
		return &DesktopBotSupplementReceipt{Stage: desktopBotSupplementAmbiguous, Candidates: candidates, Message: "你有多个任务正在执行，请说明这条是补充给哪一个。"}, nil
	}
	if target == nil {
		return &DesktopBotSupplementReceipt{Stage: desktopBotSupplementNoActiveRun, Message: "那个任务已经结束了，这条会作为新任务发送。"}, nil
	}
	// A run that stopped to ask a question is finished, not paused. Its answer
	// must travel as the next ordinary turn, which already knows how to rebuild
	// the question from session state; routing it here would drop it.
	if target.WaitingForUser {
		botlog.Write(botID, "gui.supplement_classified", nil, "stage", desktopBotSupplementWaitingForUser, "run", target.RunID)
		return &DesktopBotSupplementReceipt{Stage: desktopBotSupplementWaitingForUser, RunID: target.RunID, Message: "这条会作为对正在等待的回答发送。"}, nil
	}
	sent, err := a.postDesktopBotSupplement(botID, target.RunID, supplement)
	if err != nil {
		return nil, err
	}
	botlog.Write(botID, "gui.supplement_dispatched", nil, "run", target.RunID, "supplement", sent.SupplementID)
	return &DesktopBotSupplementReceipt{Accepted: true, Stage: desktopBotSupplementQueued, RunID: target.RunID, SupplementID: sent.SupplementID, Message: "已收到，会在当前任务的下一步生效。"}, nil
}

// PersistDesktopBotPreference writes a preference to the user's instance on the
// server.
//
// The copy matters: desktop work is carried out over there, so that is where a
// preference has to live for later tasks to honour it. The local store still
// holds preferences too, because the foreground reading of a message reads
// them here, but it cannot reach the worker.
//
// A single hyphen withdraws. Withdrawal has to travel to the server as well:
// clearing only the local copy would leave the worker honouring a preference
// the person has just taken back, with nothing on screen to explain why.
func (a *App) PersistDesktopBotPreference(botID, content string) error {
	botID = strings.TrimSpace(botID)
	content = strings.TrimSpace(content)
	if botID == "" || content == "" {
		return fmt.Errorf("bot id and preference are required")
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/preference"
	method := http.MethodPost
	if content == "-" {
		method = http.MethodDelete
	}
	if err := a.desktopBotCall(context.Background(), method, path, map[string]string{"content": content}, nil, 20*time.Second); err != nil {
		botlog.Write(botID, "gui.preference_failed", err, "stage", "withdraw", "text_len", strconv.Itoa(len(content)))
		return err
	}
	botlog.Write(botID, "gui.preference_saved", nil, "text_len", strconv.Itoa(len(content)))
	return nil
}

// markDesktopRunWaitingForUser records that the run finished because the worker
// asked a question. The entry stays listed until the turn returns so the UI can
// still describe it, but it no longer accepts supplementary input.
func (a *App) markDesktopRunWaitingForUser(botID, requestID string) {
	botID = strings.TrimSpace(botID)
	requestID = strings.TrimSpace(requestID)
	if botID == "" {
		return
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	for key, run := range activeDesktopRuns {
		if run == nil {
			continue
		}
		if strings.HasPrefix(key, botID+desktopRunSeparator) && run.RequestID == requestID {
			run.WaitingForUser = true
			return
		}
	}
}

// postDesktopBotSupplement forwards one supplement to Hub for the named run.
func (a *App) postDesktopBotSupplement(botID, runID, supplement string) (*desktopRunSupplementAck, error) {
	var out desktopRunSupplementAck
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/supplement"
	if err := a.desktopBotCall(context.Background(), http.MethodPost, path, map[string]string{"content": supplement}, &out, 20*time.Second); err != nil {
		return nil, err
	}
	return &out, nil
}

type desktopRunSupplementAck struct {
	SupplementID string `json:"supplement_id"`
	Accepted     bool   `json:"accepted"`
	State        string `json:"state"`
	Detail       string `json:"detail"`
}

// DesktopSupplementState is one supplement's terminal state as the server
// recorded it. Content carries the original words only so the UI can offer
// them again when the run ended before they were used; it must never reach a
// log or a durable store from here.
type DesktopSupplementState struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Content string `json:"content,omitempty"`
}

// GetDesktopBotSupplements reports the final state of the supplements recorded
// for one run. This is the pull half of invariant I4: the push arrives on the
// run's terminal event, but a UI that loads afterwards has no event to catch,
// so it asks instead. A run whose supplements never existed answers an empty
// list, not an error.
func (a *App) GetDesktopBotSupplements(botID, runID string) ([]DesktopSupplementState, error) {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if botID == "" || runID == "" {
		return nil, fmt.Errorf("bot id and run id are required")
	}
	var out struct {
		Items []DesktopSupplementState `json:"items"`
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/" + url.PathEscape(runID) + "/supplements"
	if err := a.desktopBotCall(context.Background(), http.MethodGet, path, nil, &out, 20*time.Second); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// reportDesktopSupplementState pushes one finished run's supplement outcomes
// to the UI. Best effort: if the report fails the receipts stay at their last
// known state rather than inventing one, and the pull binding can still be
// asked later. Only items with a final state are sent — a queued item in a
// finished run is already impossible server-side.
func (a *App) reportDesktopSupplementState(botID, sessionKey, runID string) {
	items, err := a.GetDesktopBotSupplements(botID, runID)
	if err != nil {
		botlog.Write(botID, "gui.supplement_state_failed", err, "run", runID)
		return
	}
	final := make([]DesktopSupplementState, 0, len(items))
	for _, item := range items {
		if item.State != "queued" {
			final = append(final, item)
		}
	}
	if len(final) == 0 {
		return
	}
	a.emitEvent("desktop-bot-supplement-state", map[string]any{
		"bot_id":      botID,
		"session_key": sessionKey,
		"run_id":      runID,
		"items":       final,
	})
}

func (a *App) finishDesktopBotTask(requestID, sessionKey, botID, text, phase string) {
	run := a.registerActiveDesktopRun(botID, requestID, sessionKey, text, phase)
	// The run id has to be read inside the closure. A bare defer would freeze
	// the value it had when armed, which is the empty placeholder, because Hub
	// only names the run later; the entry would then be re-keyed under the real
	// id and never removed, and every finished run would go on being offered as
	// a supplement target.
	defer func() { unregisterActiveDesktopRun(botID, requestID, run.RunID) }()
	started := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	botlog.Write(botID, "gui.relay_begin", nil, "request_id", requestID, "phase", phase, "text_len", strconv.Itoa(len(text)))
	if desktopBotRelayOverride == nil {
		go a.publishDesktopBotView(ctx, requestID, sessionKey, botID)
	}
	result, err := a.relayDesktopBot(ctx, botID, text, phase, requestID)
	resp := &IMAgentResponse{RequestID: requestID, SessionKey: sessionKey, EventScopeID: sessionKey}
	waiting := false
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
		if result.WaitingForUser || resp.AskUserQuestion != "" || resp.AskUserSecretName != "" {
			// The run ended to ask this person something. Mark it so a reply is
			// routed back as the next turn instead of being offered to a run
			// that can no longer consume it.
			a.markDesktopRunWaitingForUser(botID, requestID)
			waiting = true
		}
		if result.UserControl || resp.DesktopAttentionReason != "" {
			resp.DesktopHandoffURL = result.NovncURL
		}
	}
	if desktopBotResultSink != nil {
		desktopBotResultSink(resp)
	}
	a.emitAIAssistantResponse(requestID, resp)
	// The run is terminal now, so the server has already reaped its supplements
	// and their states are final. A turn that ended to ask a question is not:
	// its answer travels as the next ordinary turn, and the queued items may
	// still be consumed by it. Everything else reports here so a receipt the
	// person saw as queued can still turn into applied or missed (invariant
	// I4) without anyone polling.
	if run.RunID != "" && !waiting {
		a.reportDesktopSupplementState(botID, sessionKey, run.RunID)
	}
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

func (a *App) relayDesktopBot(ctx context.Context, botID, text, phase, requestID string) (desktopBotRelayResult, error) {
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
		// ClientMessageID is Hub's echo of the per-turn key. It arrives on
		// success and on failure; either one proves this Hub forwards the
		// key, which is the only condition under which a lost admission
		// answer may be sent again.
		ClientMessageID string `json:"client_message_id"`
	}
	body := map[string]any{"content": text, "client_message_id": requestID}
	if phase = strings.TrimSpace(phase); phase != "" {
		body["phase"] = phase
	}
	path := "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/messages"
	// Prefer returns as soon as the run exists. An older Hub ignores it and
	// still writes the reply on this same call, so the wait stays 30 minutes
	// for that case only. A 202 is not the result.
	//
	// admitBudget caps every attempt together. Each attempt may itself wait
	// 30 minutes — the long allowance exists because an older Hub holds the
	// request for the whole run — so without a ceiling three tries could keep
	// this bot busy for an hour and a half with nothing to show. Losing the
	// whole budget is answered with the last failure.
	admitCtx, cancelAdmit := context.WithTimeout(ctx, desktopBotAdmitBudget)
	defer cancelAdmit()
	var status int
	var err error
	for attempt := 1; attempt <= desktopBotAdmitAttempts; attempt++ {
		out.ClientMessageID = ""
		status, err = a.desktopBotExchange(admitCtx, http.MethodPost, path, body, &out, 30*time.Minute, map[string]string{"Prefer": "respond-async"})
		// Whether this answer proved the key round-tripped. It belongs to
		// this attempt only, never to the session: the Hub answering now is
		// the one whose behavior matters, and a later Hub can be an older
		// build that drops the key. Remembering "some Hub once echoed" would
		// license a resend against a Hub that replays nothing, which is how
		// a retry becomes a second run.
		//
		// An empty reply can never prove anything. A caller with no key of
		// its own — a scheduled occurrence — must not be treated as echoed:
		// Hub mints a fresh one per call, so every resend would be a new
		// run rather than a replay of the first.
		echoed := strings.TrimSpace(requestID) != "" &&
			strings.TrimSpace(out.ClientMessageID) == strings.TrimSpace(requestID)
		if err == nil {
			break
		}
		if attempt == desktopBotAdmitAttempts || !echoed || !desktopBotAdmitAgain(status, err) {
			return desktopBotRelayResult{}, err
		}
		botlog.Write(botID, "gui.admit_retry", err,
			"http", strconv.Itoa(status),
			"attempt", strconv.Itoa(attempt),
		)
		wait := time.NewTimer(desktopBotAdmitRetryWait(attempt))
		select {
		case <-ctx.Done():
			wait.Stop()
			return desktopBotRelayResult{}, ctx.Err()
		case <-wait.C:
		}
	}
	if err != nil {
		return desktopBotRelayResult{}, err
	}
	if status == http.StatusAccepted && strings.TrimSpace(out.RunID) != "" && strings.TrimSpace(out.Text) == "" {
		botlog.Write(botID, "gui.admit", nil,
			"run", strings.TrimSpace(out.RunID),
			"http", strconv.Itoa(status),
			"phase", phase,
		)
		// The turn only becomes addressable once Hub names its run. Before this
		// line nothing can route supplementary input to it.
		a.setActiveDesktopRunID(botID, requestID, out.RunID)
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

// desktopBotAdmitAttempts is how many times one admission is tried. The
// first answer that is lost — a 502 from the proxy in front of Hub, a
// deadline — costs one more send of the same key, never a second run.
const desktopBotAdmitAttempts = 3

// desktopBotAdmitBudget bounds all admission attempts together. One attempt
// carries a 30 minute read because an older Hub may answer only when the run
// ends, so the attempts are not simply additive and still need a ceiling.
const desktopBotAdmitBudget = 45 * time.Minute

// desktopBotAdmitRetryWait is the gap before the next admission try. The
// person is waiting on a bubble, so the first wait is short; the second is
// long enough for a restarting Hub to come back.
func desktopBotAdmitRetryWait(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Second
	}
	return 3 * time.Second
}

// desktopBotAdmitAgain reports whether a failed admission is worth another
// send of the same key. The same rule as a run read: nothing the server
// decided ends the turn, and a proxy that never answered leaves the turn
// unstarted. A 4xx is an answer — bad input, a disabled bot — and resending
// it would only repeat the rejection.
func desktopBotAdmitAgain(status int, err error) bool {
	if err == nil {
		return false
	}
	switch status {
	case 0, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
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
// short. A read that never got an answer — a dropped connection, a deadline,
// or a 5xx from the proxy in front of Hub — is tried again and does not
// cancel the run. Only an answer the server actually made ends this turn.
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
	// readsFailed counts reads that never got an answer. The gap stretches
	// with it and resets on any answered read, so a Hub outage costs a
	// bounded read rate instead of a turn.
	readsFailed := 0
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
		// Derived from ctx, not Background: this read must be interruptible
		// when the turn is abandoned. A Background parent leaves each poll
		// holding a full 20s that no cancel can cut short, so abandoning a
		// bot that is talking to a slow Hub keeps reading it anyway.
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
				if sleepErr := sleepDesktopBotPoll(ctx, desktopBotPollStretch(readsFailed)); sleepErr != nil {
					botlog.Write(botID, "gui.run_poll", sleepErr, "run", runID, "polls", strconv.Itoa(polls))
					return desktopBotRelayResult{}, sleepErr
				}
				readsFailed++
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
		readsFailed = 0
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

// desktopBotPollAgain reports whether a failed read of an admitted run is
// worth another try. The run itself lives on MaClawSrv and a read that never
// got an answer cannot have changed it, so only a rejection the server
// actually made ends the turn:
//
//   - status 0 is a dropped connection or a deadline: nothing was decided.
//   - 502/503/504 are the proxy in front of Hub, or Hub itself, failing to
//     answer. The run keeps driving the desktop either way. A single nginx
//     hiccup used to end a turn that had already been running for forty
//     minutes, which is how a run read 2347 times and still lost its reply.
//   - anything else — a 4xx, or a 2xx carrying a finished run — is an
//     answer, and answering ends the turn.
func desktopBotPollAgain(status int, err error) bool {
	if err == nil {
		return false
	}
	switch status {
	case 0:
		// No status at all: the read never reached a server that could
		// reject it. DeadlineExceeded counts because the attempt spent its
		// own allowance, not the run's.
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		text := err.Error()
		return strings.Contains(text, "不可达") || strings.Contains(text, "没有在时限内")
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// desktopBotPollStretchMax caps how long one failing read waits before the
// next one. A Hub outage must not be answered with one read per second
// forever, and the cap matches the hub-side watcher's ceiling so the two
// pollers of the same run stay in the same register.
const desktopBotPollStretchMax = 8 * time.Second

// desktopBotPollStretch doubles the gap after each failed read, from the
// base interval up to the cap. A read that succeeds resets the count, so a
// run that is merely slow to finish never pays for it.
func desktopBotPollStretch(failures int) time.Duration {
	wait := desktopBotPollInterval
	if wait <= 0 {
		wait = time.Second
	}
	for i := 0; i < failures && wait < desktopBotPollStretchMax; i++ {
		wait *= 2
	}
	if wait > desktopBotPollStretchMax {
		wait = desktopBotPollStretchMax
	}
	return wait
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
		// The failure body is still decoded into dest when it carries the
		// per-turn key echo. Hub names the key it received even on a 502,
		// and that echo is the only proof this Hub forwards the key to
		// MaClawSrv — the one condition under which the caller may send the
		// same turn again. Decoding an error body into a read destination is
		// harmless: the fields do not overlap.
		if dest != nil {
			_ = json.Unmarshal(payload, dest)
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
