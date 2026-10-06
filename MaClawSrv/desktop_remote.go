package main

import (
	"bytes"
	"context"
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

func (c *desktopHubClient) post(ctx context.Context, path string, body any, dest any) error {
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
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("desktop service rejected the request")
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

var (
	desktopRunsMu  sync.Mutex
	desktopRuns    = map[string]int{}
	desktopHolds   = map[string]int{}
	desktopHandoff sync.Map
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

func noteDesktopHandoff(tenantID, userID, instanceID string) {
	desktopHandoff.Store(desktopHandoffKey(tenantID, userID, instanceID), true)
	key := desktopRunKey(tenantID, userID)
	desktopRunsMu.Lock()
	desktopHolds[key]++
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
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" {
		return func() {}
	}
	if desktopRemoteSession == nil && desktopRemoteStop == nil && !desktopHubConfigured() {
		return func() { releaseLoggedInDocument(tenantID, userID) }
	}
	key := desktopRunKey(tenantID, userID)
	// A failed open still counts. Hub may already have started the desktop,
	// and this run can open it again while the command is executing. Stopping
	// here would leave that later desktop with no run to shut it down.
	// Count this run before opening, and hold the user gate across the open,
	// so a stop that is still writing the website login cannot clear it.
	gate := desktopUserGate(key)
	var holdsAtStart int
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
		if left == 0 {
			handedOff := desktopHolds[key] > holdsAtStart
			person, _ := desktopPersonInstance.Load(key)
			personID, _ := person.(string)
			switch {
			case handedOff:
				if instanceID != "" && !desktopUnattended(instanceID) {
					desktopPersonInstance.Store(key, instanceID)
				}
			case personID != "" && personID == instanceID && !desktopUnattended(instanceID):
				desktopHolds[key] = 0
				desktopPersonInstance.Delete(key)
				stop = true
			case personID != "":
			default:
				stop = true
			}
		}
		desktopRunsMu.Unlock()
		// The caller went away before this run could answer. The person may
		// already be on this browser. Stopping, or clearing the page lock,
		// would drop the website login. A scheduled run has nobody waiting,
		// so it still stops.
		callerLeft := ctx.Err() != nil && !desktopUnattended(instanceID)
		// A handoff leaves the desktop up. The page lock stays while the
		// person is still signing in, and drops only after that continuation
		// has finished and nobody is waiting.
		if left == 0 && !callerLeft && !stop {
			releaseLoggedInDocument(tenantID, userID)
		}
		if !stop || callerLeft {
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
		desktopRunsMu.Unlock()
		if busy {
			return
		}
		if err := releaseUserDesktop(context.Background(), tenantID, userID, instanceID); err != nil {
			return
		}
		releaseLoggedInDocument(tenantID, userID)
	}
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
	desktopPersonInstance.Range(func(key, value any) bool {
		id, _ := value.(string)
		if id != instanceID {
			return true
		}
		desktopPersonInstance.Delete(key)
		if runKey, ok := key.(string); ok {
			desktopRunsMu.Lock()
			delete(desktopHolds, runKey)
			left := desktopRuns[runKey]
			desktopRunsMu.Unlock()
			desktopLoginDocument.Delete(runKey)
			desktopResumeUser.Delete(runKey)
			tenantID, userID, _ := strings.Cut(runKey, "\x00")
			// Another bot of this user may still be in the browser. Stopping
			// here would drop the website login that run is using.
			if left <= 0 {
				idle = append(idle, idleDesktop{tenantID: tenantID, userID: userID, runKey: runKey, instanceID: instanceID})
			}
			releaseLoggedInDocument(tenantID, userID)
		}
		desktopResumeFocus.Delete("instance\x00" + instanceID)
		return true
	})
	for _, item := range idle {
		gate := desktopUserGate(item.runKey)
		gate.Lock()
		desktopRunsMu.Lock()
		busy := desktopRuns[item.runKey] > 0
		desktopRunsMu.Unlock()
		if !busy {
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
