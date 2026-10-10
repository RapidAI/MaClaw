package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	desktopHubURLEnv   = "MACLAW_HUB_URL"
	desktopHubTokenEnv = "MACLAW_DESKTOP_API_TOKEN"
)

// desktopRemoteSession is set by tests. Production uses Hub.
var desktopRemoteSession func(ctx context.Context, tenantID, userID string) (desktopEndpoint, error)

// desktopRemoteApp is set by tests. Production sends the command through Hub
// to the Docker service, which may be on another machine.
var desktopRemoteApp func(ctx context.Context, tenantID, userID, display string, args []string) (string, error)

// desktopRemoteOpen is set by tests. Production asks Hub to start one GUI
// program on the user's display. The program is argv, not a shell command.
var desktopRemoteOpen func(ctx context.Context, tenantID, userID, display, program string, args []string) (string, error)

// desktopRemoteInstall is set by tests. Production asks Hub to apt-get install
// inside the user's desktop container.
var desktopRemoteInstall func(ctx context.Context, tenantID, userID string, packages []string) (string, error)

// desktopRemoteScreenshot is set by tests. Production asks Hub, which asks the
// Docker service that owns the user's desktop.
var desktopRemoteScreenshot func(ctx context.Context, tenantID, userID, display string) ([]byte, error)

// desktopRemoteSavedPath is set by tests that write a screenshot file.
// Production learns the path from Hub. A value other than DesktopShotPath is ignored.
var desktopRemoteSavedPath func(name string) string

type desktopHubClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func desktopHubConfigured() bool {
	return desktopHubClientFromEnv() != nil
}

func desktopHubClientFromEnv() *desktopHubClient {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(desktopHubURLEnv)), "/")
	token := strings.TrimSpace(os.Getenv(desktopHubTokenEnv))
	if base == "" || token == "" {
		return nil
	}
	return &desktopHubClient{baseURL: base, token: token, http: &http.Client{Timeout: 2 * time.Minute}}
}

func (c *desktopHubClient) Session(ctx context.Context, tenantID, userID string) (desktopEndpoint, error) {
	var out struct {
		CDP     string `json:"cdp_url"`
		Display string `json:"display"`
	}
	if err := c.post(ctx, "/api/v1/desktop-services/session", map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
	}, &out); err != nil {
		return desktopEndpoint{}, err
	}
	return desktopEndpoint{CDP: out.CDP, Display: out.Display}, nil
}

// errDesktopKept means Hub left the desktop running. The person may be
// logging in, so the logged-in page must stay locked to this browser.
var errDesktopKept = errors.New("desktop kept")

type desktopStopInstanceKey struct{}

func (c *desktopHubClient) Stop(ctx context.Context, tenantID, userID string) error {
	var out struct {
		Status string `json:"status"`
	}
	body := map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
	}
	if instanceID, _ := ctx.Value(desktopStopInstanceKey{}).(string); strings.TrimSpace(instanceID) != "" {
		body["instance_id"] = strings.TrimSpace(instanceID)
	}
	if err := c.post(ctx, "/api/v1/desktop-services/stop", body, &out); err != nil {
		return err
	}
	if out.Status == "kept" {
		return errDesktopKept
	}
	return nil
}

func (c *desktopHubClient) Hold(ctx context.Context, tenantID, userID, instanceID string) error {
	return c.post(ctx, "/api/v1/desktop-services/hold", map[string]string{
		"tenant_id":   tenantID,
		"user_id":     userID,
		"instance_id": instanceID,
	}, nil)
}

func (c *desktopHubClient) App(ctx context.Context, tenantID, userID, display string, args []string) (string, error) {
	var out struct {
		Output string `json:"output"`
	}
	if err := c.post(ctx, "/api/v1/desktop-services/app", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"display":   display,
		"args":      args,
	}, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

func (c *desktopHubClient) Open(ctx context.Context, tenantID, userID, display, program string, args []string) (string, error) {
	var out struct {
		Output string `json:"output"`
	}
	if err := c.post(ctx, "/api/v1/desktop-services/open", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"display":   display,
		"program":   program,
		"args":      args,
	}, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

func (c *desktopHubClient) Install(ctx context.Context, tenantID, userID string, packages []string) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"packages":  packages,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/desktop-services/install", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: desktop.InstallClientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("desktop service is unreachable: %s", err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var parsed struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &parsed)
		message := strings.TrimSpace(parsed.Message)
		if message == "" {
			message = "desktop service rejected the install"
		}
		return "", fmt.Errorf("%s", message)
	}
	var out struct {
		Output string `json:"output"`
	}
	if len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, &out); err != nil {
			return "", fmt.Errorf("desktop service returned invalid status")
		}
	}
	return out.Output, nil
}

func (c *desktopHubClient) File(ctx context.Context, tenantID, userID, action, filePath, content, oldString, newString string) (string, error) {
	var out struct {
		Output string `json:"output"`
	}
	if err := c.postLimit(ctx, "/api/v1/desktop-services/file", map[string]any{
		"tenant_id":  tenantID,
		"user_id":    userID,
		"action":     action,
		"path":       filePath,
		"content":    content,
		"old_string": oldString,
		"new_string": newString,
	}, &out, 1<<20); err != nil {
		return "", err
	}
	return out.Output, nil
}

func (c *desktopHubClient) Bash(ctx context.Context, tenantID, userID, command string) (string, error) {
	var out struct {
		Output string `json:"output"`
	}
	if err := c.post(ctx, "/api/v1/desktop-services/bash", map[string]any{
		"tenant_id": tenantID,
		"user_id":   userID,
		"command":   command,
	}, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

func (c *desktopHubClient) HTTP(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	var out struct {
		Body   string `json:"body"`
		Status int    `json:"status"`
		Kind   string `json:"content_type"`
	}
	if err := c.postLimit(ctx, "/api/v1/desktop-services/http", map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
		"url":       rawURL,
	}, &out, 1<<20); err != nil {
		return "", 0, "", err
	}
	return out.Body, out.Status, out.Kind, nil
}

// Screenshot returns a PNG of the user's desktop.
func (c *desktopHubClient) Screenshot(ctx context.Context, tenantID, userID, display string) ([]byte, error) {
	data, _, err := c.screenshot(ctx, tenantID, userID, display, "")
	return data, err
}

// screenshot returns the PNG and the desktop path when this capture wrote a file.
// An empty path means the image was captured and no file was written.
func (c *desktopHubClient) screenshot(ctx context.Context, tenantID, userID, display, name string) ([]byte, string, error) {
	var out struct {
		MIME  string `json:"mime"`
		Image string `json:"image_base64"`
		Saved string `json:"saved_path"`
	}
	body := map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
		"display":   display,
	}
	if strings.TrimSpace(name) != "" {
		body["name"] = strings.TrimSpace(name)
	}
	if err := c.postLimit(ctx, "/api/v1/desktop-services/screenshot", body, &out, desktop.MaxScreenshotBytes*2); err != nil {
		return nil, "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.Image))
	if err != nil || !desktop.IsPNG(data) || len(data) > desktop.MaxScreenshotBytes {
		return nil, "", fmt.Errorf("desktop service returned an invalid screenshot")
	}
	saved := strings.TrimSpace(out.Saved)
	if strings.TrimSpace(name) == "" || saved != desktop.DesktopShotPath(name) {
		saved = ""
	}
	return data, saved, nil
}

func desktopRejectedMessage(payload []byte) string {
	var parsed struct {
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	if json.Unmarshal(payload, &parsed) == nil {
		if message := strings.TrimSpace(parsed.Message); message != "" {
			return message
		}
		if text, ok := parsed.Error.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return "desktop service rejected the request"
}

func (c *desktopHubClient) post(ctx context.Context, path string, body any, dest any) error {
	return c.postLimit(ctx, path, body, dest, 1<<20)
}

func (c *desktopHubClient) postLimit(ctx context.Context, path string, body any, dest any, limit int64) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("desktop service is unreachable: %s", err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s", desktopRejectedMessage(payload))
	}
	if dest != nil && len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, dest); err != nil {
			return fmt.Errorf("desktop service returned invalid status")
		}
	}
	return nil
}

// desktopRemoteStop is set by tests. Production asks Hub to stop the desktop.
var desktopRemoteStop func(ctx context.Context, tenantID, userID string) error

// desktopReleasePlan is the stop-or-keep choice of an owning run whose
// count could not hit zero because another occupy was still open.
type desktopReleasePlan struct {
	holdsAtStart int
	instanceID   string
	tenantID     string
	userID       string
	callerLeft   bool
}

// desktopRetiredLogin is a finished login set aside so the open that took
// the desktop can stop. The count and the person come back if that stop
// is not accepted.
type desktopRetiredLogin struct {
	holds      int
	prevSpent  int
	person     string
	personHeld bool
}

var (
	desktopRunsMu sync.Mutex
	desktopRuns   = map[string]int{}
	desktopHolds  = map[string]int{}
	// desktopSpentHolds is the part of desktopHolds that a finished
	// login already accounted for while a newer open still had the
	// desktop. That open's own handoff is the count above this.
	// An accepted stop clears these maps. A stop that does not land
	// puts the set-aside login back.
	desktopSpentHolds    = map[string]int{}
	desktopRetiredLogins = map[string]desktopRetiredLogin{}
	desktopReleasePlans  = map[string]desktopReleasePlan{}
	desktopHandoff       sync.Map
	desktopAttention     sync.Map
	// desktopUserGates keep one user's stop and the next start apart.
	// Stopping writes the website login. Starting during that write would
	// open another browser and leave the login behind.
	desktopUserGates sync.Map
)

func desktopRunKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

// desktopAccount is the Hub account whose cloud desktop an instance drives.
// The MaClawSrv token's own tenant is not that account.
type desktopAccount struct {
	TenantID string
	UserID   string
}

// desktopUserByInstance maps an agent instance to the Hub account that owns
// its cloud desktop. hub_user_id and hub_tenant_id are that person. The
// MaClawSrv connection token may belong to a different service tenant.
var desktopUserByInstance sync.Map

func rememberDesktopOwner(instanceID string, metadata map[string]string, fallbackUser, fallbackTenant string) (string, string) {
	instanceID = strings.TrimSpace(instanceID)
	account := desktopAccount{
		TenantID: strings.TrimSpace(fallbackTenant),
		UserID:   strings.TrimSpace(fallbackUser),
	}
	bound := false
	if metadata != nil {
		if user := strings.TrimSpace(metadata["hub_user_id"]); desktop.ValidUserID(user) {
			account.UserID = user
			bound = true
		}
		if tenant := strings.TrimSpace(metadata["hub_tenant_id"]); validDesktopTenant(tenant) {
			account.TenantID = tenant
		}
	}
	// A missed read has no hub user. Keep the account already bound to this
	// instance. Storing the MaClaw service account instead would open a
	// different desktop and leave the website login behind.
	if !bound && instanceID != "" {
		if value, ok := desktopUserByInstance.Load(instanceID); ok {
			if cached, _ := value.(desktopAccount); cached.UserID != "" {
				return cached.UserID, cached.TenantID
			}
		}
		if metadata == nil {
			return account.UserID, account.TenantID
		}
		// This instance is a cloud-desktop bot and the person is not on it.
		// The MaClaw service account is a different desktop from the one noVNC opens.
		if strings.TrimSpace(metadata["hub_bot"]) == "1" {
			return "", ""
		}
	}
	if instanceID != "" && account.UserID != "" {
		desktopUserByInstance.Store(instanceID, account)
	}
	return account.UserID, account.TenantID
}

func validDesktopTenant(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw != "" && len(raw) <= 200 && !strings.ContainsAny(raw, "\r\n\x00")
}

func desktopOwner(scope agentruntime.Scope) agentruntime.Scope {
	value, ok := desktopUserByInstance.Load(strings.TrimSpace(scope.InstanceID))
	if !ok {
		return scope
	}
	account, _ := value.(desktopAccount)
	if account.UserID != "" {
		scope.UserID = account.UserID
	}
	if account.TenantID != "" {
		scope.TenantID = account.TenantID
	}
	return scope
}

func desktopHandoffKey(tenantID, userID, instanceID string) string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return desktopRunKey(tenantID, userID)
	}
	return desktopRunKey(tenantID, userID) + "\x00" + instanceID
}

// desktopRemoteHold is set by tests. Production tells Hub immediately, before
// the message reply returns, so another bot cannot stop the login browser.
var desktopRemoteHold func(ctx context.Context, tenantID, userID, instanceID string) error

func noteDesktopAttention(tenantID, userID, instanceID, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	desktopAttention.Store(desktopHandoffKey(tenantID, userID, instanceID), reason)
}

func takeDesktopTurn(tenantID, userID, instanceID string) (bool, string) {
	return takeDesktopHandoff(tenantID, userID, instanceID), takeDesktopAttention(tenantID, userID, instanceID)
}

func takeDesktopAttention(tenantID, userID, instanceID string) string {
	value, ok := desktopAttention.LoadAndDelete(desktopHandoffKey(tenantID, userID, instanceID))
	reason, _ := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(reason)
}

func noteDesktopHandoff(tenantID, userID, instanceID string) {
	desktopHandoff.Store(desktopHandoffKey(tenantID, userID, instanceID), true)
	key := desktopRunKey(tenantID, userID)
	desktopRunsMu.Lock()
	desktopHolds[key]++
	// This open is handing off again. A finished login set aside for it
	// no longer needs to be put back.
	delete(desktopRetiredLogins, key)
	// The reply has not been sent yet. Another bot of this user can still
	// reach the browser until the run ends. Mark the login now so that bot
	// cannot switch the page the person is about to use.
	if id := strings.TrimSpace(instanceID); id != "" && !desktopUnattended(id) {
		desktopPersonInstance.Store(key, id)
	}
	desktopRunsMu.Unlock()
	notifyDesktopHeld(tenantID, userID, instanceID)
}

func notifyDesktopHeld(tenantID, userID, instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || desktopUnattended(instanceID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if desktopRemoteHold != nil {
		_ = desktopRemoteHold(ctx, tenantID, userID, instanceID)
		return
	}
	client := desktopHubClientFromEnv()
	if client == nil {
		return
	}
	_ = client.Hold(ctx, tenantID, userID, instanceID)
}

// desktopResumeFocus remembers which bot handed the desktop to the person.
// Another bot of the same user must not switch the page while they are logging in.
var desktopResumeFocus sync.Map

// desktopLoginDocument stays set from the handoff until the continuation
// run ends. A reconnected browser must keep the same page. The one-shot
// resume flag only chooses that page the first time.
var desktopLoginDocument sync.Map

// desktopResumeUser is set until the handing-off bot attaches again.
// The handoff run ends before that, and must not drop the page lock.
var desktopResumeUser sync.Map

func noteDesktopResume(scope agentruntime.Scope) {
	desktopResumeFocus.Store(desktopResumeKey(scope), true)
	userKey := desktopRunKey(scope.TenantID, scope.UserID)
	desktopResumeUser.Store(userKey, true)
	desktopLoginDocument.Store(userKey, true)
}

func desktopLoginDocumentHeld(tenantID, userID string) bool {
	_, ok := desktopLoginDocument.Load(desktopRunKey(tenantID, userID))
	return ok
}

func desktopResumeKey(scope agentruntime.Scope) string {
	if id := strings.TrimSpace(scope.InstanceID); id != "" {
		return "instance\x00" + id
	}
	return desktopRunKey(scope.TenantID, scope.UserID)
}

func consumeDesktopResume(scope agentruntime.Scope) bool {
	_, ok := desktopResumeFocus.LoadAndDelete(desktopResumeKey(scope))
	if ok {
		desktopResumeUser.Delete(desktopRunKey(scope.TenantID, scope.UserID))
	}
	return ok
}

func takeDesktopHandoff(tenantID, userID, instanceID string) bool {
	value, ok := desktopHandoff.LoadAndDelete(desktopHandoffKey(tenantID, userID, instanceID))
	flag, _ := value.(bool)
	return ok && flag
}

// desktopPersonInstance is the bot whose user is logging in on the shared desktop.
// Other work for that user must not stop or drive the desktop until this bot continues.
var desktopPersonInstance sync.Map

// desktopUnattendedInstances are scheduled runs. Nobody is at the desktop,
// so a login wall must not hold the desktop or block the user's bots.
var desktopUnattendedInstances sync.Map

func markDesktopUnattended(instanceID string) func() {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return func() {}
	}
	desktopUnattendedInstances.Store(instanceID, true)
	return func() { desktopUnattendedInstances.Delete(instanceID) }
}

func desktopUnattended(instanceID string) bool {
	_, ok := desktopUnattendedInstances.Load(strings.TrimSpace(instanceID))
	return ok
}

// occupyUserDesktop starts this user's cloud desktop for one run and stops it
// when that user has no run left. A login or captcha handoff keeps the desktop
// up until the same bot finishes the follow-up. Another bot finishing in
// between must not shut the desktop down while the person is still logging in.
// A missing Hub configuration does not start or stop a desktop. The run
// still clears the logged-in document hold when it ends.
func occupyUserDesktop(ctx context.Context, tenantID, userID, instanceID string) func() {
	release, _ := occupyUserDesktopRun(ctx, tenantID, userID, instanceID)
	return release
}

// occupyUserDesktopRun is occupyUserDesktop with a flag the async finish
// sets. A repeated admission occupies before it knows the run already
// exists. That release only drops the extra count. When it is the release
// that reaches zero while the owner's choice is still stored, it applies
// that choice. After the owner has already finished, the same release
// still must not treat itself as the follow-up that clears the login.
func occupyUserDesktopRun(ctx context.Context, tenantID, userID, instanceID string) (func(), *bool) {
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" {
		return func() {}, nil
	}
	if desktopRemoteSession == nil && desktopRemoteStop == nil && !desktopHubConfigured() {
		return func() { releaseLoggedInDocument(tenantID, userID) }, nil
	}
	key := desktopRunKey(tenantID, userID)
	// A failed open still counts. Hub may already have started the desktop,
	// and this run can open it again while the command is executing. Stopping
	// here would leave that later desktop with no run to shut it down.
	// Count this run before opening, and hold the user gate across the open,
	// so a stop that is still writing the website login cannot clear it.
	gate := desktopUserGate(key)
	var holdsAtStart int
	owned := true
	func() {
		gate.Lock()
		defer gate.Unlock()
		desktopRunsMu.Lock()
		desktopRuns[key]++
		holdsAtStart = desktopHolds[key]
		desktopRunsMu.Unlock()
		_, _ = desktopSession(ctx, agentruntime.Scope{TenantID: tenantID, UserID: userID}, "")
	}()
	return func() {
		desktopRunsMu.Lock()
		desktopRuns[key]--
		left := desktopRuns[key]
		if left < 0 {
			desktopRuns[key] = 0
			left = 0
		}
		stop := false
		stopTenant, stopUser, stopInstance := tenantID, userID, instanceID
		callerLeft := ctx.Err() != nil && !desktopUnattended(instanceID)
		if left == 0 {
			plan, planned := desktopReleasePlans[key]
			delete(desktopReleasePlans, key)
			if !owned && planned {
				stop = decideDesktopRelease(key, plan.instanceID, plan.holdsAtStart)
				stopTenant, stopUser, stopInstance = plan.tenantID, plan.userID, plan.instanceID
				callerLeft = plan.callerLeft
			} else if !owned {
				stop = decideUnownedDesktopRelease(key)
			} else {
				stop = decideDesktopRelease(key, instanceID, holdsAtStart)
			}
		} else if owned {
			desktopReleasePlans[key] = desktopReleasePlan{
				holdsAtStart: holdsAtStart,
				instanceID:   instanceID,
				tenantID:     tenantID,
				userID:       userID,
				callerLeft:   callerLeft,
			}
		}
		// Snapshot the login this decision saw. A newer open can change
		// the count or the person before the stop runs, and that newer
		// handoff has to stay.
		var stopHolds int
		var stopPerson string
		var stopPersonHeld bool
		if stop {
			stopHolds = desktopHolds[key]
			if person, held := desktopPersonInstance.Load(key); held {
				stopPerson, _ = person.(string)
				stopPersonHeld = true
			}
		}
		desktopRunsMu.Unlock()
		// The caller went away before this run could answer. The person may
		// already be on this browser. Stopping, or clearing the page lock,
		// would drop the website login. A scheduled run has nobody waiting,
		// so it still stops.
		// A handoff leaves the desktop up. The page lock stays while the
		// person is still signing in, and drops only after that continuation
		// has finished and nobody is waiting.
		if left == 0 && !callerLeft && !stop {
			releaseLoggedInDocument(stopTenant, stopUser)
		}
		if !stop || callerLeft {
			// The continuation already set the finished login aside. This
			// open is disconnected before it can stop, so that login has
			// to come back. A handoff recorded above leaves it unset.
			if stop && callerLeft {
				desktopRunsMu.Lock()
				restoreRetiredDesktopLogin(key)
				desktopRunsMu.Unlock()
			}
			return
		}
		// A newer run may already be opening this same desktop. Stopping
		// anyway would discard the website login that run is about to use.
		// The page lock stays until this stop is accepted, so that run keeps
		// the same logged-in page.
		gate.Lock()
		defer gate.Unlock()
		desktopRunsMu.Lock()
		busy := desktopRuns[key] > 0
		if busy {
			// The continuation already chose to stop. Leaving the old
			// login markers would make this newer open look unfinished,
			// so its release would keep the desktop. Set that login
			// aside. A handoff this open already recorded changes the
			// count or the person and stays. If the stop is not
			// accepted, or this open's caller has disconnected, the
			// login is put back. The page lock stays with this open.
			retireFinishedDesktopLogin(key, stopHolds, stopPersonHeld, stopPerson)
		}
		desktopRunsMu.Unlock()
		if busy {
			return
		}
		if err := releaseUserDesktop(context.Background(), stopTenant, stopUser, stopInstance); err != nil {
			// Hub kept the desktop, or the stop did not land. A login
			// set aside for this open is still the one a later release
			// must not shut down.
			desktopRunsMu.Lock()
			restoreRetiredDesktopLogin(key)
			desktopRunsMu.Unlock()
			return
		}
		// Drop the handoff only after the stop is accepted. Hub can keep
		// the desktop, and a disconnect skips the stop. Those paths
		// put a set-aside login back, so the count and the person stay.
		// Clearing earlier lets the next repeated admission shut a
		// desktop that is still up.
		desktopRunsMu.Lock()
		desktopHolds[key] = 0
		delete(desktopSpentHolds, key)
		delete(desktopRetiredLogins, key)
		desktopPersonInstance.Delete(key)
		desktopRunsMu.Unlock()
		releaseLoggedInDocument(stopTenant, stopUser)
	}, &owned
}

// decideUnownedDesktopRelease is the last release of an occupy that did not
// admit the run, after the owning run already applied its own choice and
// dropped the saved plan. A live hold or a person still on the desktop
// means that choice was to keep it. A hold a finished login already
// accounted for does not count. An accepted stop clears the hold and the
// person together. When Hub keeps the desktop, both stay.
// This release must not clear the login itself. With neither signal, the
// extra open is the only thing left, so it stops.
func decideUnownedDesktopRelease(key string) bool {
	if desktopActiveHolds(key) > 0 {
		return false
	}
	if _, held := desktopPersonInstance.Load(key); held {
		return false
	}
	return true
}

// decideDesktopRelease reports whether the desktop stops once its run count
// hits zero. The hold baseline belongs to the run that owns the turn.
func decideDesktopRelease(key, instanceID string, holdsAtStart int) bool {
	handedOff := desktopActiveHolds(key) > desktopActiveBaseline(key, holdsAtStart)
	person, _ := desktopPersonInstance.Load(key)
	personID, _ := person.(string)
	switch {
	case handedOff:
		if instanceID != "" && !desktopUnattended(instanceID) {
			desktopPersonInstance.Store(key, instanceID)
		}
		return false
	case personID != "" && personID == instanceID && !desktopUnattended(instanceID):
		// Same bot finished the login. The count and the person stay until
		// the stop is accepted; see the release that calls this.
		return true
	case personID != "":
		return false
	default:
		return true
	}
}

// desktopActiveHolds is the handoff count that still means someone is
// logging in. desktopRunsMu is held. A finished login can leave its count
// in place while a newer open is using the desktop; that part does not
// keep the desktop.
func desktopActiveHolds(key string) int {
	n := desktopHolds[key] - desktopSpentHolds[key]
	if n < 0 {
		return 0
	}
	return n
}

func desktopActiveBaseline(key string, holdsAtStart int) int {
	n := holdsAtStart - desktopSpentHolds[key]
	if n < 0 {
		return 0
	}
	return n
}

// retireFinishedDesktopLogin drops a login the continuation already
// finished, when a newer open took the desktop before the stop could run.
// desktopRunsMu is held. A handoff recorded after this decision changes
// the count or the person and is left in place. The raw count stays so
// that newer handoff still sits above the baseline its open captured.
func retireFinishedDesktopLogin(key string, holds int, personHeld bool, personID string) {
	current, held := desktopPersonInstance.Load(key)
	currentID, _ := current.(string)
	if desktopHolds[key] != holds || held != personHeld || currentID != personID {
		return
	}
	// This login was already set aside. A later open can reach the same
	// decision while the person is gone. Replacing the saved login would
	// forget who was signing in, and a stop Hub does not accept could
	// not put that person back.
	if prev, ok := desktopRetiredLogins[key]; ok && prev.holds == holds && !personHeld {
		return
	}
	prevSpent := desktopSpentHolds[key]
	desktopRetiredLogins[key] = desktopRetiredLogin{
		holds:      holds,
		prevSpent:  prevSpent,
		person:     personID,
		personHeld: personHeld,
	}
	if holds > 0 {
		desktopSpentHolds[key] = holds
	} else {
		delete(desktopSpentHolds, key)
	}
	desktopPersonInstance.Delete(key)
}

// restoreRetiredDesktopLogin puts a set-aside login back. desktopRunsMu
// is held. A newer handoff changes the count or stores a person, and
// that login is left as it is.
func restoreRetiredDesktopLogin(key string) {
	retired, ok := desktopRetiredLogins[key]
	if !ok {
		return
	}
	_, held := desktopPersonInstance.Load(key)
	if held || desktopHolds[key] != retired.holds || desktopSpentHolds[key] != retired.holds {
		delete(desktopRetiredLogins, key)
		return
	}
	if retired.prevSpent > 0 {
		desktopSpentHolds[key] = retired.prevSpent
	} else {
		delete(desktopSpentHolds, key)
	}
	if retired.personHeld {
		desktopPersonInstance.Store(key, retired.person)
	}
	delete(desktopRetiredLogins, key)
}

func desktopUserGate(key string) *sync.Mutex {
	gate, _ := desktopUserGates.LoadOrStore(key, &sync.Mutex{})
	return gate.(*sync.Mutex)
}

func releaseLoggedInDocument(tenantID, userID string) {
	// The handoff run ends while the person is still logging in.
	// Clearing here would let the next connection open a new document.
	if _, waiting := desktopResumeUser.Load(desktopRunKey(tenantID, userID)); waiting {
		return
	}
	desktopLoginDocument.Delete(desktopRunKey(tenantID, userID))
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return
	}
	binding := desktopBindingFor(key)
	binding.mu.Lock()
	session := binding.session
	binding.mu.Unlock()
	if session == nil {
		return
	}
	desktopKeepLoggedInDocument(session, false)
}

func releaseDesktopPerson(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	type idleDesktop struct {
		tenantID   string
		userID     string
		runKey     string
		instanceID string
	}
	var idle []idleDesktop
	var unlockPage []idleDesktop
	// Hold the run lock across both scans. A set-aside login is not in the
	// live map, and putting it back takes this same lock. Looking first and
	// deleting later lets that put-back land in between.
	desktopRunsMu.Lock()
	desktopPersonInstance.Range(func(key, value any) bool {
		id, _ := value.(string)
		if id != instanceID {
			return true
		}
		runKey, ok := key.(string)
		if !ok {
			desktopPersonInstance.Delete(key)
			return true
		}
		desktopPersonInstance.Delete(key)
		delete(desktopHolds, runKey)
		delete(desktopSpentHolds, runKey)
		delete(desktopRetiredLogins, runKey)
		tenantID, userID, _ := strings.Cut(runKey, "\x00")
		item := idleDesktop{tenantID: tenantID, userID: userID, runKey: runKey, instanceID: instanceID}
		unlockPage = append(unlockPage, item)
		// Another bot of this user may still be in the browser. Stopping
		// here would drop the website login that run is using.
		if desktopRuns[runKey] <= 0 {
			idle = append(idle, item)
		}
		return true
	})
	for runKey, retired := range desktopRetiredLogins {
		if !retired.personHeld || retired.person != instanceID {
			continue
		}
		current, held := desktopPersonInstance.Load(runKey)
		currentID, _ := current.(string)
		if held && currentID != instanceID {
			// A newer login is on this desktop. Drop only the token that
			// would put the deleted bot back over them.
			delete(desktopRetiredLogins, runKey)
			continue
		}
		delete(desktopRetiredLogins, runKey)
		desktopPersonInstance.Delete(runKey)
		if held {
			delete(desktopHolds, runKey)
			delete(desktopSpentHolds, runKey)
			tenantID, userID, _ := strings.Cut(runKey, "\x00")
			item := idleDesktop{tenantID: tenantID, userID: userID, runKey: runKey, instanceID: instanceID}
			unlockPage = append(unlockPage, item)
			if desktopRuns[runKey] <= 0 {
				idle = append(idle, item)
			}
			continue
		}
		// The hold count stays spent. The next release still decides to
		// stop. Clearing only that spent count would look like a live
		// handoff and leave the desktop up with nobody logged in.
		if desktopRuns[runKey] > 0 {
			continue
		}
		tenantID, userID, _ := strings.Cut(runKey, "\x00")
		item := idleDesktop{tenantID: tenantID, userID: userID, runKey: runKey, instanceID: instanceID}
		unlockPage = append(unlockPage, item)
		idle = append(idle, item)
	}
	desktopRunsMu.Unlock()
	desktopResumeFocus.Delete("instance\x00" + instanceID)
	for _, item := range unlockPage {
		desktopLoginDocument.Delete(item.runKey)
		desktopResumeUser.Delete(item.runKey)
		releaseLoggedInDocument(item.tenantID, item.userID)
	}
	for _, item := range idle {
		gate := desktopUserGate(item.runKey)
		gate.Lock()
		desktopRunsMu.Lock()
		busy := desktopRuns[item.runKey] > 0
		current, held := desktopPersonInstance.Load(item.runKey)
		currentID, _ := current.(string)
		someoneElse := held && currentID != "" && currentID != item.instanceID
		desktopRunsMu.Unlock()
		if !busy && !someoneElse {
			_ = releaseUserDesktop(context.Background(), item.tenantID, item.userID, item.instanceID)
		}
		gate.Unlock()
	}
}

func desktopPersonInstanceID(scope agentruntime.Scope) (string, bool) {
	value, ok := desktopPersonInstance.Load(desktopRunKey(scope.TenantID, scope.UserID))
	id, _ := value.(string)
	id = strings.TrimSpace(id)
	return id, ok && id != ""
}

func releaseUserDesktop(ctx context.Context, tenantID, userID, instanceID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, desktopStopInstanceKey{}, strings.TrimSpace(instanceID))
	if desktopRemoteStop != nil {
		return desktopRemoteStop(ctx, tenantID, userID)
	}
	client := desktopHubClientFromEnv()
	if client == nil {
		return nil
	}
	return client.Stop(ctx, tenantID, userID)
}
