// Package desktoppool is Hub's registry of Docker desktop services.
// Hub chooses a service for a user and asks that service to create or stop
// the desktop. Hub does not talk to the Docker daemon.
package desktoppool

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

const SettingsKey = "desktop_service"

const (
	ScopeGlobal     = "global"
	ScopeDepartment = "department"
	ScopeUser       = "user"
)

var (
	ErrInvalidInput        = errors.New("invalid desktop service settings")
	ErrSettingsUnavailable = errors.New("desktop service store is unavailable")
	ErrNotFound            = errors.New("docker service not found")
	ErrNotAssigned         = errors.New("no docker service is assigned")
	ErrService             = errors.New("docker service request failed")
)

// WarmStartBudget is how long Hub waits for a stopped desktop to accept
// docker start. The two-minute client timeout on bash stays the hard cap
// for the command itself.
const WarmStartBudget = 25 * time.Second

// Wake operations. A memory write, a send_to_user, a progress read of the
// three sections already on the run record, a chat message, and a schedule
// do not start a stopped container.
const (
	WakeSession    = "session"
	WakeFile       = "file"
	WakeBash       = "bash"
	WakeScreenshot = "screenshot"
	WakeScreen     = "screen"
	WakeMemory     = "memory"
	WakeSendToUser = "send_to_user"
	WakeProgress   = "progress"
	WakeMessage    = "message"
	WakeSchedule   = "schedule"

	// Wake reasons for desktopd. Empty is refused by desktopd when starting.
	WakeReasonBot   = "bot-desktop"
	WakeReasonHuman = "human-wake"
)

// WakeError is the retryable tool error for a warm start that passed 25
// seconds. UserBubble is false: it is not a send_to_user sentence.
type WakeError struct{}

func (WakeError) Error() string    { return "桌面正在唤醒，再试一次。" }
func (WakeError) Retryable() bool  { return true }
func (WakeError) UserBubble() bool { return false }

type wakeReasonKey struct{}

// WithWakeReason labels OpenDesktop/Ensure as bot-desktop or human-wake.
func WithWakeReason(ctx context.Context, reason string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, wakeReasonKey{}, reason)
}

type computerNodeKey struct{}

type computerNode struct {
	id string
	ok bool
}

// WithComputer pins one call to a computer. An empty nodeID is the cloud
// computer, including when the user is currently assigned to a local node.
func WithComputer(ctx context.Context, nodeID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, computerNodeKey{}, computerNode{id: strings.TrimSpace(nodeID), ok: true})
}

func computerNodeOf(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	choice, ok := ctx.Value(computerNodeKey{}).(computerNode)
	if !ok || !choice.ok {
		return "", false
	}
	return choice.id, true
}

// PinnedComputer is the computer WithComputer selected for this call.
// ok is false when the call follows the user's current assignment.
func PinnedComputer(ctx context.Context) (nodeID string, ok bool) {
	return computerNodeOf(ctx)
}

func wakeReasonOf(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	reason, _ := ctx.Value(wakeReasonKey{}).(string)
	return strings.TrimSpace(reason)
}

// IsDesktopWake reports a warm-start retry. Callers leave send_to_user empty.
func IsDesktopWake(err error) bool {
	var wake WakeError
	return errors.As(err, &wake)
}

// TurnWakes reports whether this operation starts a stopped desktop.
// progressCopied means the three PROGRESS sections are already on the run
// record, so reading them does not wake a machine.
func TurnWakes(op string, progressCopied bool) bool {
	switch op {
	case WakeMemory, WakeSendToUser, WakeMessage, WakeSchedule:
		return false
	case WakeProgress:
		if progressCopied {
			return false
		}
		return false
	case WakeFile, WakeBash:
		// desktopd runs bash and file calls in the running desktop, or in
		// the user's headless workspace while the desktop sleeps, and wakes
		// a machine runtime itself. Hub must not start the GUI for them.
		return false
	case WakeSession, WakeScreenshot, WakeScreen:
		return true
	default:
		return false
	}
}

// ServiceFailureMessage turns a docker desktop service failure into a message
// an admin can act on, instead of the generic "rejected the desktop request".
func ServiceFailureMessage(err error) string {
	base := "docker service rejected the desktop request"
	if err == nil || (errors.Is(err, ErrService) && err.Error() == ErrService.Error()) {
		return base
	}
	return upstream.Message(err, base)
}

// Directory resolves a user's department chain. Nil skips department matching.
type Directory interface {
	Email(ctx context.Context, userID string) (string, error)
	GroupID(ctx context.Context, email string) (string, error)
	ParentID(ctx context.Context, groupID string) (string, error)
}

// Server is one Docker desktop service Hub can call.
type Server struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	AccessToken string `json:"access_token,omitempty"`
	Image       string `json:"image"`
	Memory      string `json:"memory"`
	CPUs        string `json:"cpus"`
	ShmSize     string `json:"shm_size"`
	// NodeID is set when this service is reached through a Bot node tunnel.
	// Empty means Hub dials BaseURL directly, which is every server created
	// before nodes existed.
	NodeID string `json:"node_id,omitempty"`
	// OwnerUserID marks a service one user registered for their own desktop
	// from the GUI. Only that user's assignment points at it.
	OwnerUserID string `json:"owner_user_id,omitempty"`
	// Intranet is a desktopd the user installed from the desktop bot
	// settings. It lives on their network: Hub reaches it through the node
	// tunnel, and the desktop's own traffic leaves from that machine.
	Intranet bool `json:"intranet,omitempty"`
	// ResourcesAuto means memory, CPU, and shared memory were chosen from
	// the machine at install. A later report from that machine may replace
	// them. ResourcesPinned means an operator set the size; reports leave it.
	ResourcesAuto   bool `json:"resources_auto,omitempty"`
	ResourcesPinned bool `json:"resources_pinned,omitempty"`
	// PrevServerID is the server that user was assigned to before
	// registering; unregistering puts that assignment back.
	PrevServerID string `json:"prev_server_id,omitempty"`
	// TenantID is filled by resolve and is not stored. The tunnel dial
	// refuses a node that belongs to a different tenant.
	TenantID string `json:"-"`
}

// ServerView hides the access token.
type ServerView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	BaseURL         string `json:"base_url"`
	TokenSet        bool   `json:"token_set"`
	Image           string `json:"image"`
	Memory          string `json:"memory"`
	CPUs            string `json:"cpus"`
	ShmSize         string `json:"shm_size"`
	NodeID          string `json:"node_id,omitempty"`
	OwnerID         string `json:"owner_user_id,omitempty"`
	Intranet        bool   `json:"intranet,omitempty"`
	ResourcesAuto   bool   `json:"resources_auto,omitempty"`
	ResourcesPinned bool   `json:"resources_pinned,omitempty"`
}

// Assignment binds global users, one department, or one user to a server.
type Assignment struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	TargetID string `json:"target_id,omitempty"`
	ServerID string `json:"server_id"`
}

// Desktop is the service response after create or stop.
type Desktop struct {
	UserID     string `json:"user_id"`
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Container  string `json:"container,omitempty"`
	Image      string `json:"image,omitempty"`
	Memory     string `json:"memory,omitempty"`
	CPUs       string `json:"cpus,omitempty"`
	ShmSize    string `json:"shm_size,omitempty"`
	Status     string `json:"status"`
}

// AuthorizedUser is one platform user a desktop assignment covers. The
// check-desktop list shows only these users: anyone else would fail with
// ErrNotAssigned because no assignment resolves for them.
// Email is the address already loaded with the user. It is not part of the
// admin payload; the bot-scope filter uses it so a department grant does not
// read that user a second time.
type AuthorizedUser struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Email string `json:"-"`
}

// View is the admin payload.
type View struct {
	Servers     []ServerView `json:"servers"`
	Assignments []Assignment `json:"assignments"`
	// Users lists the platform users covered by an assignment. The pool has
	// no user list, so the admin handler fills it in via AuthorizedUsers.
	Users []AuthorizedUser `json:"users,omitempty"`
}

type record struct {
	Servers     []Server     `json:"servers"`
	Assignments []Assignment `json:"assignments"`
	// ProxyModeDefault is the tenant's desktop egress mode for users who did
	// not pick one ("" means smart). ProxyModeLocked forces it on everyone.
	ProxyModeDefault string `json:"proxy_mode_default,omitempty"`
	ProxyModeLocked  bool   `json:"proxy_mode_locked,omitempty"`
	// ComputerResetDisabled stops users of this tenant from resetting
	// (recreating) their own desktop. Restart stays allowed.
	ComputerResetDisabled bool `json:"computer_reset_disabled,omitempty"`
	// ProxyModes is each user's own desktop_proxy_mode, by user id.
	ProxyModes map[string]string `json:"proxy_modes,omitempty"`
}

// Pool stores Docker services and calls the one assigned to a user.
type Pool struct {
	System    store.SystemSettingsRepository
	Directory Directory
	HTTP      *http.Client
	Now       func() time.Time
	// Nodes dials a Bot node. Nil, or a server with an empty NodeID, keeps
	// the client() path.
	Nodes func(ctx context.Context, tenantID, nodeID, target string) *http.Client
	// Home is the computer one bot uses. An empty nodeID with ok true is the
	// cloud computer. ok false leaves the user's current assignment. Desktop
	// users that do not name a bot never call Home.
	Home func(ctx context.Context, tenantID, ownerID, botID string) (nodeID string, ok bool)

	mu sync.Mutex

	// useMu guards botUse, the in-memory record of bot desktop-screen steps
	// (see bot_use.go). It is separate from mu so a slow settings load does
	// not hold up a 电脑-tab poll.
	useMu  sync.Mutex
	botUse map[string]botGUIUse
}

func New(system store.SystemSettingsRepository, directory Directory) *Pool {
	return &Pool{System: system, Directory: directory, Now: time.Now}
}

func (p *Pool) View(ctx context.Context, tenantID string) (View, error) {
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return View{}, err
	}
	return viewOf(rec), nil
}

// AuthorizedUsers filters candidates down to the users an assignment covers:
// a user-scope assignment names them, a department-scope assignment covers
// their department chain, and a global assignment covers everyone. It is
// resolve()'s matching in reverse, so every listed user's desktop calls
// succeed; unlisted users would fail with ErrNotAssigned.
func (p *Pool) AuthorizedUsers(ctx context.Context, tenantID string, candidates []AuthorizedUser) []AuthorizedUser {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return nil
	}
	return p.filterAuthorizedUsers(directoryContext(ctx, tenantID), candidates, rec.Assignments)
}

// ViewAuthorized is the admin view with Users filled in, from one settings
// read: the panel needs both and two reads could disagree after a write.
func (p *Pool) ViewAuthorized(ctx context.Context, tenantID string, candidates []AuthorizedUser) (View, error) {
	if p == nil {
		return View{}, ErrSettingsUnavailable
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return View{}, err
	}
	view := viewOf(rec)
	view.Users = p.filterAuthorizedUsers(directoryContext(ctx, tenantID), candidates, rec.Assignments)
	return view, nil
}

// filterAuthorizedUsers applies resolve()'s matching in reverse. A global
// assignment short-circuits everything: nobody needs a department chain, so
// the directory is left alone for large tenants. With department assignments
// and no global, each candidate's chain resolves at most to a first match.
func (p *Pool) filterAuthorizedUsers(ctx context.Context, candidates []AuthorizedUser, assignments []Assignment) []AuthorizedUser {
	var (
		global bool
		users  = map[string]bool{}
		depts  = map[string]bool{}
	)
	for _, item := range assignments {
		switch item.Scope {
		case ScopeGlobal:
			global = true
		case ScopeUser:
			users[item.TargetID] = true
		case ScopeDepartment:
			depts[item.TargetID] = true
		}
	}
	if !global && len(users) == 0 && len(depts) == 0 {
		return nil
	}
	out := []AuthorizedUser{}
	add := func(id, label string) {
		out = append(out, AuthorizedUser{ID: id, Label: label})
	}
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.ID)
		if id == "" {
			continue
		}
		switch {
		case global || users[id]:
			add(id, candidate.Label)
		case len(depts) > 0:
			for _, groupID := range p.departmentChain(ctx, id) {
				if depts[groupID] {
					add(id, candidate.Label)
					break
				}
			}
		}
	}
	return out
}

func (p *Pool) CreateServer(ctx context.Context, tenantID string, in Server) (ServerView, error) {
	server, err := normalizeServer(in, "")
	if err != nil {
		return ServerView{}, err
	}
	if server.AccessToken == "" {
		return ServerView{}, fmt.Errorf("%w: access token is required", ErrInvalidInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return ServerView{}, err
	}
	server.ID = newID("dsrv")
	rec.Servers = append(rec.Servers, server)
	if err := p.save(ctx, tenantID, rec); err != nil {
		return ServerView{}, err
	}
	return serverView(server), nil
}

func (p *Pool) UpdateServer(ctx context.Context, tenantID, serverID string, in Server, tokenProvided bool) (ServerView, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return ServerView{}, err
	}
	index := serverIndex(rec.Servers, serverID)
	if index < 0 {
		return ServerView{}, ErrNotFound
	}
	current := rec.Servers[index]
	if !tokenProvided {
		in.AccessToken = current.AccessToken
	}
	// The admin patch does not send node_id. Dropping it would reject the
	// virtual host, or dial that host on the public network.
	if strings.TrimSpace(in.NodeID) == "" {
		in.NodeID = current.NodeID
	}
	in.OwnerUserID = current.OwnerUserID
	in.PrevServerID = current.PrevServerID
	in.Intranet = current.Intranet || strings.TrimSpace(current.OwnerUserID) != ""
	server, err := normalizeServer(in, current.AccessToken)
	if err != nil {
		return ServerView{}, err
	}
	server.ID = current.ID
	// A token refresh copies the size and must not forget whether it was
	// measured or typed. Compare the normalized sizes, so "4G" and "4g" are
	// the same size. A size the operator actually changed is kept as typed.
	sameSize := server.Memory == current.Memory && server.CPUs == current.CPUs && server.ShmSize == current.ShmSize
	if currentSize, normErr := desktop.NormalizeResources("", current.Memory, current.CPUs, current.ShmSize); normErr == nil {
		sameSize = server.Memory == currentSize.Memory && server.CPUs == currentSize.CPUs && server.ShmSize == currentSize.ShmSize
	}
	if sameSize {
		server.ResourcesAuto = current.ResourcesAuto
		server.ResourcesPinned = current.ResourcesPinned
	} else {
		server.ResourcesAuto = false
		server.ResourcesPinned = true
	}
	if strings.TrimSpace(server.AccessToken) == "" {
		return ServerView{}, fmt.Errorf("%w: access token is required", ErrInvalidInput)
	}
	rec.Servers[index] = server
	if err := p.save(ctx, tenantID, rec); err != nil {
		return ServerView{}, err
	}
	return serverView(server), nil
}

func (p *Pool) DeleteServer(ctx context.Context, tenantID, serverID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index := serverIndex(rec.Servers, serverID)
	if index < 0 {
		return ErrNotFound
	}
	for _, item := range rec.Assignments {
		if item.ServerID == serverID {
			return fmt.Errorf("%w: server is still assigned", ErrInvalidInput)
		}
	}
	rec.Servers = append(rec.Servers[:index], rec.Servers[index+1:]...)
	return p.save(ctx, tenantID, rec)
}

func (p *Pool) CreateAssignment(ctx context.Context, tenantID string, in Assignment) (Assignment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return Assignment{}, err
	}
	item, err := normalizeAssignment(in, rec)
	if err != nil {
		return Assignment{}, err
	}
	item.ID = newID("dasg")
	rec.Assignments = append(rec.Assignments, item)
	if err := p.save(ctx, tenantID, rec); err != nil {
		return Assignment{}, err
	}
	return item, nil
}

func (p *Pool) DeleteAssignment(ctx context.Context, tenantID, assignmentID string) error {
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	index := -1
	for i := range rec.Assignments {
		if rec.Assignments[i].ID == assignmentID {
			index = i
			break
		}
	}
	if index < 0 {
		p.mu.Unlock()
		return ErrNotFound
	}
	removed := rec.Assignments[index]
	p.mu.Unlock()

	// A user-scope removal moves that user to another server on the next
	// resolve. Stop the desktop the old server still holds first, or its
	// container keeps running there with no Hub handle left to stop it.
	// Best effort: a dead server must not block removing the assignment.
	if removed.Scope == ScopeUser {
		if server, err := serverByID(rec.Servers, removed.ServerID); err == nil {
			server, _ = p.stamp(tenantID, server, nil)
			_, _ = p.stopOnServer(ctx, server, tenantID, removed.TargetID)
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err = p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index = -1
	for i := range rec.Assignments {
		if rec.Assignments[i].ID == assignmentID {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNotFound
	}
	rec.Assignments = append(rec.Assignments[:index], rec.Assignments[index+1:]...)
	return p.save(ctx, tenantID, rec)
}

// CreateDesktop asks the Docker service assigned to this user to start a desktop.
// The service applies that server's memory, CPU, and shared-memory settings.
func (p *Pool) CreateDesktop(ctx context.Context, tenantID, userID string) (Desktop, error) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return Desktop{}, err
	}
	var out struct {
		Container string `json:"container"`
		Status    string `json:"status"`
		Image     string `json:"image"`
		Memory    string `json:"memory"`
		CPUs      string `json:"cpus"`
		ShmSize   string `json:"shm_size"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"image":     server.Image,
		"memory":    server.Memory,
		"cpus":      server.CPUs,
		"shm_size":  server.ShmSize,
	}
	if mode := p.desktopEgressMode(ctx, tenantID, userID, server); mode != "" {
		body["proxy_mode"] = mode
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops", body, &out); err != nil {
		return Desktop{}, err
	}
	return Desktop{
		UserID: userID, ServerID: server.ID, ServerName: server.Name,
		Container: out.Container, Image: out.Image, Memory: out.Memory,
		CPUs: out.CPUs, ShmSize: out.ShmSize, Status: out.Status,
	}, nil
}

// StopDesktop asks the assigned Docker service to stop that user's desktop.
// An isolated bot display is left running: desktopd returns idle and the
// supervisor stops that display later. Forgetting its screen-use record
// here draws the sleep picture on a display that is still up. A stop that
// does not land leaves the record too.
func (p *Pool) StopDesktop(ctx context.Context, tenantID, userID string) (Desktop, error) {
	if _, bot := desktop.SplitDesktopUser(userID); bot != "" {
		// desktopd answers this with "idle" and does not touch the display.
		// Calling it hairpins Hub out to the node and back for that answer,
		// and the screen-use record has to stay so the 电脑 tab does not
		// draw sleep on a display that is still up.
		return Desktop{UserID: userID, Status: "idle"}, nil
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return Desktop{}, err
	}
	out, err := p.stopOnServer(ctx, server, tenantID, userID)
	if err != nil {
		return Desktop{}, err
	}
	p.ForgetBotGUIUse(tenantID, userID)
	return out, nil
}

// ReleaseBotDesktop gives a deleted bot's display number back on the owner's
// Docker service (desktopd /v1/desktops/bot-release). A bot still in use is
// refused there and keeps its number.
func (p *Pool) ReleaseBotDesktop(ctx context.Context, tenantID, ownerUserID, botID string) error {
	server, err := p.resolve(ctx, tenantID, ownerUserID)
	if err != nil {
		return err
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(ownerUserID),
		"bot_id":    strings.TrimSpace(botID),
	}
	return p.call(ctx, server, http.MethodPost, "/v1/desktops/bot-release", body, nil)
}

// PruneBotDesktops drops the display entries of the owner's bots that no
// longer exist (desktopd /v1/desktops/bot-prune). keep is every bot the
// owner still has; running or busy screens are never touched there.
func (p *Pool) PruneBotDesktops(ctx context.Context, tenantID, ownerUserID string, keep []string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	servers, err := p.computersOf(ctx, tenantID, ownerUserID)
	if err != nil {
		return err
	}
	if keep == nil {
		keep = []string{}
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   ownerUserID,
		"keep_bots": keep,
	}
	assigned, _ := p.resolveAssigned(ctx, tenantID, ownerUserID)
	var assignedErr error
	seenAssigned := false
	var otherErr error
	for _, server := range servers {
		callErr := p.call(ctx, server, http.MethodPost, "/v1/desktops/bot-prune", body, nil)
		if assigned.ID != "" && server.ID == assigned.ID {
			assignedErr = callErr
			seenAssigned = true
			continue
		}
		if callErr != nil && otherErr == nil {
			otherErr = callErr
		}
	}
	if seenAssigned {
		return assignedErr
	}
	return otherErr
}

// stopOnServer stops the desktop on one specific Docker service, independent
// of the current assignment.
func (p *Pool) stopOnServer(ctx context.Context, server Server, tenantID, userID string) (Desktop, error) {
	var out struct {
		Container string `json:"container"`
		Status    string `json:"status"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
	}
	if err := p.callIdempotent(ctx, server, http.MethodPost, "/v1/desktops/stop", body, &out); err != nil {
		return Desktop{}, err
	}
	return Desktop{UserID: userID, ServerID: server.ID, ServerName: server.Name, Container: out.Container, Status: out.Status}, nil
}

// DesktopSession is the remote desktop MaClawSrv and the GUI can reach.
type DesktopSession struct {
	CDP     string
	Display string
	Novnc   string
	// NodeID is the Bot node that owns this desktop. Empty is a public
	// Docker service Hub dials directly.
	NodeID string
	// LocalCDP is the address desktopd published before Hub moved it onto
	// the node virtual host. A srv on that same node dials this directly.
	LocalCDP string
}

// OpenSession asks the assigned Docker service for a CDP URL MaClawSrv can
// reach even when that service is on another machine.
func (p *Pool) OpenSession(ctx context.Context, tenantID, userID string) (string, string, error) {
	session, err := p.OpenDesktop(ctx, tenantID, userID)
	if err != nil {
		return "", "", err
	}
	return session.CDP, session.Display, nil
}

// OpenDesktop is OpenSession plus the noVNC page for a human login handoff.
func (p *Pool) OpenDesktop(ctx context.Context, tenantID, userID string) (DesktopSession, error) {
	if err := p.ensureOp(ctx, tenantID, userID, WakeSession); err != nil {
		return DesktopSession{}, err
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return DesktopSession{}, err
	}
	var out struct {
		CDP     string `json:"cdp_url"`
		Display string `json:"display"`
		Novnc   string `json:"novnc_url"`
	}
	reason := wakeReasonOf(ctx)
	if reason == "" {
		reason = WakeReasonBot
	}
	body := map[string]string{
		"tenant_id":   store.NormalizeTenantID(tenantID),
		"user_id":     strings.TrimSpace(userID),
		"image":       server.Image,
		"memory":      server.Memory,
		"cpus":        server.CPUs,
		"shm_size":    server.ShmSize,
		"wake_reason": reason,
	}
	if mode := p.desktopEgressMode(ctx, tenantID, userID, server); mode != "" {
		body["proxy_mode"] = mode
	}
	// The session call is create-or-get for this (tenant, user): a service
	// that never answered, or answered 5xx, is tried again rather than
	// failing a command that has not opened a desktop yet.
	if err := p.callIdempotent(ctx, server, http.MethodPost, "/v1/desktops/session", body, &out); err != nil {
		return DesktopSession{}, err
	}
	if !strings.HasPrefix(out.CDP, "http://") && !strings.HasPrefix(out.CDP, "https://") {
		return DesktopSession{}, fmt.Errorf("%w: cdp url is invalid", ErrService)
	}
	return DesktopSession{
		CDP:      nodeEndpoint(server, out.CDP),
		Display:  out.Display,
		Novnc:    nodeEndpoint(server, out.Novnc),
		NodeID:   strings.TrimSpace(server.NodeID),
		LocalCDP: out.CDP,
	}, nil
}

// ContainerMemory reads the desktop container's memory from desktopd.
// used and limit are bytes.
func (p *Pool) ContainerMemory(ctx context.Context, tenantID, userID string) (int64, int64, error) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return 0, 0, err
	}
	var out struct {
		Used  int64 `json:"used_bytes"`
		Limit int64 `json:"limit_bytes"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/memory", body, &out); err != nil {
		return 0, 0, err
	}
	return out.Used, out.Limit, nil
}

// PeekDesktopView returns the noVNC page for a desktop user whose screen is
// already running (the owner's display, or one isolated bot's display in a
// shared container). It never calls Ensure or OpenDesktop. An empty string
// means the screen is not up, the service is older than this route, or the
// look-up failed — viewing must not start or fail a tool that already
// succeeded.
func (p *Pool) PeekDesktopView(ctx context.Context, tenantID, userID string) (string, error) {
	if p == nil {
		return "", nil
	}
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", nil
	}
	var out struct {
		Novnc string `json:"novnc_url"`
		State string `json:"desktop_state"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/view", body, &out); err != nil {
		// An older desktopd has no /view. That is "not running", not a
		// failure: viewing must not start the desktop or fail the caller.
		var status *upstream.StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	switch strings.TrimSpace(out.State) {
	case "unavailable":
		// HTTP 200 with an empty page is also how a stopped screen answers.
		// unavailable is a failed read, so it must not look like "down".
		return "", ErrDesktopViewUnavailable
	case "asleep", "display_stopped":
		return "", nil
	default:
		return nodeEndpoint(server, out.Novnc), nil
	}
}

// ErrDesktopViewUnavailable is a view read that failed. It is not an asleep
// or stopped screen, and it must not start the desktop.
var ErrDesktopViewUnavailable = errors.New("desktop view is unavailable")

// NudgeDesktopView asks the desktop service to turn a running screen's
// picture back on. It never starts a stopped screen. An older desktopd
// without this route is a no-op.
func (p *Pool) NudgeDesktopView(ctx context.Context, tenantID, userID string) error {
	if p == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return nil
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/nudge", body, nil); err != nil {
		var status *upstream.StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return nil
		}
		return err
	}
	return nil
}

// DesktopViewState is the typed picture of a screen that is already up.
// It never starts, wakes, or pins the desktop. state is running, asleep,
// or display_stopped. A 409 DESKTOP_ASLEEP and an empty page are asleep.
// A transport failure is display_stopped: a viewer must not keep a frame
// it can no longer confirm.
func (p *Pool) DesktopViewState(ctx context.Context, tenantID, userID string) (string, string, error) {
	if p == nil {
		return "asleep", "", nil
	}
	if ctx != nil && ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "asleep", "", nil
	}
	var out struct {
		Novnc string `json:"novnc_url"`
		State string `json:"desktop_state"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/view", body, &out); err != nil {
		var statusErr *upstream.StatusError
		if errors.As(err, &statusErr) && statusErr.Status == http.StatusConflict {
			return "asleep", "", nil
		}
		if desktopbot.IsDesktopAsleep(err) {
			return "asleep", "", nil
		}
		// A transport or inspect failure is not a stopped display.
		return "", "", err
	}
	state, novnc := reportedViewState(out.State, out.Novnc)
	return state, novnc, nil
}

// reportedViewState keeps a typed state from desktopd. An older desktopd
// omits the field. The view route's contract is that a page means the
// screen is up and no page means it is not; Hub turns that into the typed
// state the GUI reads. A non-running state never keeps the page.
func reportedViewState(state, novnc string) (string, string) {
	state = strings.TrimSpace(state)
	novnc = strings.TrimSpace(novnc)
	switch state {
	case "running":
		return "running", novnc
	case "asleep", "display_stopped", "unavailable":
		return state, ""
	default:
		if novnc == "" {
			return "asleep", ""
		}
		return "running", novnc
	}
}

// RunApp forwards one xdotool command to the Docker service that owns the user.
func (p *Pool) RunApp(ctx context.Context, tenantID, userID, display string, args []string) (string, error) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"display":   display,
		"args":      args,
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/app", body, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

// RunOpen asks the user's Docker service to start one GUI program on the
// desktop display. The program is argv, not a shell command.
func (p *Pool) RunOpen(ctx context.Context, tenantID, userID, display, program string, args []string) (string, error) {
	if _, err := desktop.OpenArgv(program, args); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"display":   display,
		"program":   program,
		"args":      args,
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/open", body, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

// RunInstall asks the user's Docker service to apt-get install. The client
// waits one minute past the apt budget so the stop sentence can be read.
func (p *Pool) RunInstall(ctx context.Context, tenantID, userID string, packages []string) (string, error) {
	names, err := desktop.PackageNames(packages)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"packages":  names,
	}
	client, err := p.withNodeTransport(ctx, server, &http.Client{Timeout: desktop.InstallClientTimeout})
	if err != nil {
		return "", err
	}
	if err := p.callClient(ctx, client, server, http.MethodPost, "/v1/desktops/install", body, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

// RunFile forwards a read, write, edit, list, or bytes fetch to the user's desktop container.
// bytes is the base64 of the file. A PDF stays intact across the JSON string.
func (p *Pool) RunFile(ctx context.Context, tenantID, userID, action, filePath, content, oldString, newString string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "read", "write", "edit", "list", "bytes", "link":
	default:
		return "", fmt.Errorf("%w: file action is invalid", ErrInvalidInput)
	}
	if action == "list" && strings.TrimSpace(filePath) == "" {
		filePath = desktop.ContainerHome
	}
	if action != "list" || strings.TrimSpace(filePath) != "" {
		resolved, err := desktop.ContainerPath(filePath)
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
		}
		filePath = resolved
	}
	if action == "write" && len(content) > desktop.FileContentMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalidInput)
	}
	if err := p.ensureOp(ctx, tenantID, userID, WakeFile); err != nil {
		return "", err
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	body := map[string]any{
		"tenant_id":  store.NormalizeTenantID(tenantID),
		"user_id":    strings.TrimSpace(userID),
		"action":     action,
		"path":       filePath,
		"content":    content,
		"old_string": oldString,
		"new_string": newString,
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/file", body, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

// UploadFile streams one material file of size bytes to the user's desktop
// container. filePath must already be a desktop.MaterialPath.
func (p *Pool) UploadFile(ctx context.Context, tenantID, userID, filePath string, body io.Reader, size int64) (string, error) {
	resolved, err := desktop.ContainerPath(filePath)
	if err != nil || !strings.HasPrefix(resolved, desktop.MaterialsRoot+"/") {
		return "", fmt.Errorf("%w: upload path is invalid", ErrInvalidInput)
	}
	if size < 0 || size > desktop.UploadMax {
		return "", fmt.Errorf("%w: file is too large", ErrInvalidInput)
	}
	if err := p.ensureOp(ctx, tenantID, userID, WakeFile); err != nil {
		return "", err
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("tenant_id", store.NormalizeTenantID(tenantID))
	q.Set("user_id", strings.TrimSpace(userID))
	q.Set("path", resolved)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server.BaseURL, "/")+"/v1/desktops/upload?"+q.Encode(), body)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+server.AccessToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 10 * time.Minute}
	if strings.TrimSpace(server.NodeID) != "" {
		// The tunnel client times out at two minutes. Keep the upload
		// deadline and reuse only its dialer.
		client, err = p.withNodeTransport(ctx, server, client)
		if err != nil {
			return "", err
		}
	} else if p.HTTP != nil && p.HTTP.Transport != nil {
		client.Transport = p.HTTP.Transport
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", upstream.NewStatusError(ErrService, resp.StatusCode, payload)
	}
	var out struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal(payload, &out)
	return out.Output, nil
}

// RunBash runs one command in the user's desktop container.
func (p *Pool) RunBash(ctx context.Context, tenantID, userID, command string) (string, error) {
	return p.RunBashScreen(ctx, tenantID, userID, command, false)
}

func (p *Pool) RunBashScreen(ctx context.Context, tenantID, userID, command string, needsScreen bool) (string, error) {
	if _, err := desktop.BashArgs("container", command); err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	if err := p.ensureOp(ctx, tenantID, userID, WakeBash); err != nil {
		return "", err
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
		Code   string `json:"code"`
	}
	body := map[string]any{
		"tenant_id":    store.NormalizeTenantID(tenantID),
		"user_id":      strings.TrimSpace(userID),
		"command":      strings.TrimSpace(command),
		"needs_screen": needsScreen,
	}
	client, err := p.withNodeTransport(ctx, server, &http.Client{Timeout: 2 * time.Minute})
	if err != nil {
		return "", err
	}
	if err := p.callClient(ctx, client, server, http.MethodPost, "/v1/desktops/bash", body, &out); err != nil {
		return "", err
	}
	if out.Code == desktopbot.CodeScreenRequired {
		return out.Output, &desktopbot.ScreenRequiredError{Output: out.Output}
	}
	return out.Output, nil
}

// RunHTTP fetches one URL through the desktop service. It does not Ensure:
// curl runs in the running desktop or in the workspace that stays up while
// the GUI sleeps. A 503 DESKTOP_WAKING stays a WakeError so the handler
// returns 503 and the caller can wait.
func (p *Pool) RunHTTP(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	if _, err := desktop.PublicURL(rawURL); err != nil {
		return "", 0, "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	body, status, kind, err := p.runHTTPOnce(ctx, tenantID, userID, rawURL)
	if err != nil {
		return "", 0, "", classifyDesktopOp(err)
	}
	return body, status, kind, nil
}

// classifyDesktopOp keeps a warm-start and a stopped-container refusal typed.
// Other failures stay as the service returned them.
func classifyDesktopOp(err error) error {
	if err == nil || IsDesktopWake(err) {
		return err
	}
	var statusErr *upstream.StatusError
	if !errors.As(err, &statusErr) {
		if desktopbot.IsDesktopAsleep(err) {
			return desktopbot.AsleepError{}
		}
		return err
	}
	text := statusErr.Error()
	if statusErr.Status == http.StatusServiceUnavailable && strings.Contains(text, desktopbot.CodeDesktopWaking) {
		return WakeError{}
	}
	if statusErr.Status == http.StatusConflict && strings.Contains(text, desktopbot.CodeDesktopAsleep) {
		return desktopbot.AsleepError{}
	}
	if desktopbot.IsDesktopAsleep(err) {
		return desktopbot.AsleepError{}
	}
	return err
}

func (p *Pool) runHTTPOnce(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return "", 0, "", err
	}
	var out struct {
		Body   string `json:"body"`
		Status int    `json:"status"`
		Kind   string `json:"content_type"`
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"url":       strings.TrimSpace(rawURL),
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/http", body, &out); err != nil {
		return "", 0, "", err
	}
	return out.Body, out.Status, out.Kind, nil
}

// maxScreenshotBytes bounds one PNG read from a Docker service.
const maxScreenshotBytes = 16 << 20

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// AuthorizeLocal checks the assigned desktop and returns the server. op is
// the same wake class as the public call: a screenshot may start the GUI,
// a file or a shell must not. A same-node srv then reads the bytes locally.
func (p *Pool) AuthorizeLocal(ctx context.Context, tenantID, userID, op string) (Server, error) {
	if err := p.ensureOp(ctx, tenantID, userID, op); err != nil {
		return Server{}, err
	}
	return p.resolve(ctx, tenantID, userID)
}

// Screenshot returns a PNG of the user's desktop from the Docker service that
// owns the user. An empty display means the service's default (:20).
func (p *Pool) Screenshot(ctx context.Context, tenantID, userID, display, name string) ([]byte, string, error) {
	shot, saved, err := p.ScreenshotWith(ctx, tenantID, userID, display, name, desktop.ShotOptions{})
	return shot.Data, saved, err
}

// ScreenshotWith is Screenshot with scaling, a zoom region, another format,
// the pointer, or accessibility marks. The Shot carries the screen size and
// scale the service reported; an older service without those headers can
// only answer plain options, whose PNG is the full screen.
func (p *Pool) ScreenshotWith(ctx context.Context, tenantID, userID, display, name string, opts desktop.ShotOptions) (desktop.Shot, string, error) {
	opts, err := opts.Normalize()
	if err != nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	if err := p.ensureOp(ctx, tenantID, userID, WakeScreenshot); err != nil {
		return desktop.Shot{}, "", err
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return desktop.Shot{}, "", err
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"display":   strings.TrimSpace(display),
	}
	name = strings.TrimSpace(name)
	if name != "" {
		body["name"] = name
	}
	if !opts.Plain() {
		body["max_width"], body["max_height"], body["format"] = opts.MaxWidth, opts.MaxHeight, opts.Format
		body["quality"], body["cursor"], body["marks"] = opts.Quality, opts.Cursor, opts.Marks
		if !opts.Region.Empty() {
			body["region"] = opts.Region
		}
		if opts.Window != "" {
			body["window"] = opts.Window
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return desktop.Shot{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server.BaseURL, "/")+"/v1/desktops/screenshot", bytes.NewReader(raw))
	if err != nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+server.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	client := p.clientFor(ctx, server)
	if client == nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: node is offline", ErrService)
	}
	resp, err := client.Do(req)
	if err != nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxScreenshotBytes+1))
	if err != nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(payload) > 1<<20 {
			payload = payload[:1<<20]
		}
		return desktop.Shot{}, "", upstream.NewStatusError(ErrService, resp.StatusCode, payload)
	}
	if len(payload) > maxScreenshotBytes || desktop.SniffImageMIME(payload) != opts.MIME() {
		return desktop.Shot{}, "", fmt.Errorf("%w: screenshot is not a %s image", ErrService, opts.MIME())
	}
	if opts.Plain() && !bytes.HasPrefix(payload, pngSignature) {
		return desktop.Shot{}, "", fmt.Errorf("%w: screenshot is not a PNG image", ErrService)
	}
	shot, err := desktop.ParseShotHeaders(resp.Header.Get, payload)
	if err != nil {
		return desktop.Shot{}, "", fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	if !opts.Plain() && resp.Header.Get(desktop.HeaderShotScreen) == "" {
		return desktop.Shot{}, "", fmt.Errorf("%w: this desktop service does not support screenshot options yet", ErrService)
	}
	saved := strings.TrimSpace(resp.Header.Get("X-Desktop-Saved-Path"))
	if name == "" || saved != desktop.DesktopShotPath(name) {
		saved = ""
	}
	return shot, saved, nil
}

// Ctl runs one desktop control request (clipboard, windows, screen, wait,
// status, accessibility) on the Docker service that owns the user.
func (p *Pool) Ctl(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
	op, _ := request["op"].(string)
	if !desktop.ValidCtlOp(strings.TrimSpace(op)) {
		return nil, fmt.Errorf("%w: desktop control op must be one of %s", ErrInvalidInput, strings.Join(desktop.CtlOps, ", "))
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"display":   strings.TrimSpace(display),
		"request":   request,
	}
	var out map[string]any
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/ctl", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Pool) ensureOp(ctx context.Context, tenantID, userID, op string) error {
	if !TurnWakes(op, false) {
		return nil
	}
	reason := wakeReasonOf(ctx)
	if reason == "" {
		reason = WakeReasonBot
	}
	return p.EnsureReason(ctx, tenantID, userID, reason)
}

// Ensure asks the assigned Docker service to start a stopped desktop before
// a session, file, bash, screenshot, or screen view. A warm start that is
// still unfinished after 25 seconds returns WakeError. That error is not a
// user bubble. A chat message and a schedule do not call this.
func (p *Pool) Ensure(ctx context.Context, tenantID, userID string) error {
	return p.EnsureReason(ctx, tenantID, userID, WakeReasonBot)
}

// EnsureReason is Ensure with an explicit desktopd wake_reason.
func (p *Pool) EnsureReason(ctx context.Context, tenantID, userID, reason string) error {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	started := p.now()
	client, err := p.withNodeTransport(ctx, server, &http.Client{Timeout: WarmStartBudget})
	if err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		reason = WakeReasonBot
	}
	body := map[string]string{
		"tenant_id":   store.NormalizeTenantID(tenantID),
		"user_id":     strings.TrimSpace(userID),
		"image":       server.Image,
		"memory":      server.Memory,
		"cpus":        server.CPUs,
		"shm_size":    server.ShmSize,
		"wake_reason": reason,
	}
	var out struct {
		Status string `json:"status"`
	}
	err = p.callClient(ctx, client, server, http.MethodPost, "/v1/desktops/ensure", body, &out)
	if p.now().Sub(started) > WarmStartBudget || wakeTimedOut(err) {
		return WakeError{}
	}
	if err != nil {
		if strings.Contains(err.Error(), "再试一次") {
			return WakeError{}
		}
		return err
	}
	return nil
}

func (p *Pool) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func wakeTimedOut(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout")
}

func (p *Pool) resolve(ctx context.Context, tenantID, userID string) (Server, error) {
	owner, botID := desktop.SplitDesktopUser(userID)
	if nodeID, ok := computerNodeOf(ctx); ok {
		return p.resolveComputer(ctx, tenantID, owner, nodeID)
	}
	if botID != "" && p != nil && p.Home != nil {
		if nodeID, ok := p.Home(ctx, tenantID, owner, botID); ok {
			return p.resolveComputer(ctx, tenantID, owner, nodeID)
		}
	}
	return p.resolveAssigned(ctx, tenantID, owner)
}

// resolveAssigned is the user's current computer: their assignment, then
// their department, then the tenant default.
func (p *Pool) resolveAssigned(ctx context.Context, tenantID, userID string) (Server, error) {
	ctx = directoryContext(ctx, tenantID)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Server{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return Server{}, err
	}
	if serverID := matchAssignment(rec.Assignments, ScopeUser, userID); serverID != "" {
		server, err := serverByID(rec.Servers, serverID)
		return p.stamp(tenantID, server, err)
	}
	for _, groupID := range p.departmentChain(ctx, userID) {
		if serverID := matchAssignment(rec.Assignments, ScopeDepartment, groupID); serverID != "" {
			server, err := serverByID(rec.Servers, serverID)
			return p.stamp(tenantID, server, err)
		}
	}
	if serverID := matchAssignment(rec.Assignments, ScopeGlobal, ""); serverID != "" {
		server, err := serverByID(rec.Servers, serverID)
		return p.stamp(tenantID, server, err)
	}
	return Server{}, ErrNotAssigned
}

// resolveComputer is one bot's computer. An empty nodeID is the cloud
// computer: a user assignment that points at a local node is not used
// (its previous cloud server is, when that is what the user had), then
// department and global, skipping node servers. A set nodeID is that
// node's desktop and does not fall back to the cloud.
func (p *Pool) resolveComputer(ctx context.Context, tenantID, userID, nodeID string) (Server, error) {
	ctx = directoryContext(ctx, tenantID)
	userID = strings.TrimSpace(userID)
	nodeID = strings.TrimSpace(nodeID)
	if userID == "" {
		return Server{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return Server{}, err
	}
	if nodeID != "" {
		var owned Server
		ownedOK := false
		var current Server
		currentOK := false
		assignedID := matchAssignment(rec.Assignments, ScopeUser, userID)
		var shared Server
		sharedOK := false
		for _, server := range rec.Servers {
			if strings.TrimSpace(server.NodeID) != nodeID {
				continue
			}
			owner := strings.TrimSpace(server.OwnerUserID)
			if owner == userID {
				if !ownedOK {
					owned = server
					ownedOK = true
				}
				// Reinstalling the same machine appends another server.
				// The one the user is on now is the computer, not the oldest.
				if server.ID == assignedID {
					current = server
					currentOK = true
				}
				continue
			}
			// A node desktop with no owner is the shared one for that node.
			// Another user's own desktop is not this bot's computer.
			if owner == "" && !sharedOK {
				shared = server
				sharedOK = true
			}
		}
		if currentOK {
			return p.stamp(tenantID, current, nil)
		}
		if ownedOK {
			return p.stamp(tenantID, owned, nil)
		}
		if sharedOK {
			return p.stamp(tenantID, shared, nil)
		}
		return Server{}, ErrNotAssigned
	}
	if server, ok := cloudServerForAssigned(rec, userID, p.departmentChain(ctx, userID)); ok {
		return p.stamp(tenantID, server, nil)
	}
	return Server{}, ErrNotAssigned
}

// CloudServerFor is the cloud computer this user's bots would land on: a
// user, department, or global assignment naming a non-node server. A nil
// departments slice resolves the chain through the directory; callers that
// already walked it pass it in so the lookup does not repeat a directory
// round trip under another service's lock.
func (p *Pool) CloudServerFor(ctx context.Context, tenantID, userID string, departments []string) (Server, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Server{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	ctx = directoryContext(ctx, tenantID)
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return Server{}, err
	}
	if departments == nil {
		departments = p.departmentChain(ctx, userID)
	}
	if server, ok := cloudServerForAssigned(rec, userID, departments); ok {
		return p.stamp(tenantID, server, nil)
	}
	return Server{}, ErrNotAssigned
}

// cloudServerForAssigned walks user -> departments -> global assignments and
// returns the first that resolves to a cloud desktop.
func cloudServerForAssigned(rec record, userID string, departments []string) (Server, bool) {
	if server, ok := cloudServer(rec, matchAssignment(rec.Assignments, ScopeUser, userID)); ok {
		return server, true
	}
	for _, groupID := range departments {
		if server, ok := cloudServer(rec, matchAssignment(rec.Assignments, ScopeDepartment, groupID)); ok {
			return server, true
		}
	}
	if server, ok := cloudServer(rec, matchAssignment(rec.Assignments, ScopeGlobal, "")); ok {
		return server, true
	}
	return Server{}, false
}

// cloudServer is the assignment's server when that server is a cloud desktop.
// A local-node assignment yields the cloud server the user had before the
// node was attached.
func cloudServer(rec record, serverID string) (Server, bool) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return Server{}, false
	}
	server, err := serverByID(rec.Servers, serverID)
	if err != nil {
		return Server{}, false
	}
	if strings.TrimSpace(server.NodeID) == "" {
		return server, true
	}
	prev := strings.TrimSpace(server.PrevServerID)
	if prev == "" {
		return Server{}, false
	}
	prevServer, err := serverByID(rec.Servers, prev)
	if err != nil || strings.TrimSpace(prevServer.NodeID) != "" {
		return Server{}, false
	}
	return prevServer, true
}

// computersOf is every desktop service this user may have a display on:
// the current assignment, the cloud computer, and each node they own.
func (p *Pool) computersOf(ctx context.Context, tenantID, userID string) ([]Server, error) {
	userID = strings.TrimSpace(userID)
	seen := map[string]struct{}{}
	var out []Server
	add := func(server Server, err error) {
		if err != nil || strings.TrimSpace(server.ID) == "" {
			return
		}
		if _, ok := seen[server.ID]; ok {
			return
		}
		seen[server.ID] = struct{}{}
		out = append(out, server)
	}
	assigned, assignedErr := p.resolveAssigned(ctx, tenantID, userID)
	add(assigned, assignedErr)
	add(p.resolveComputer(ctx, tenantID, userID, ""))
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, server := range rec.Servers {
		if strings.TrimSpace(server.OwnerUserID) == userID && strings.TrimSpace(server.NodeID) != "" {
			stamped, stampErr := p.stamp(tenantID, server, nil)
			add(stamped, stampErr)
		}
	}
	if len(out) == 0 {
		if assignedErr != nil {
			return nil, assignedErr
		}
		return nil, ErrNotAssigned
	}
	return out, nil
}

func (p *Pool) stamp(tenantID string, server Server, err error) (Server, error) {
	if err != nil {
		return Server{}, err
	}
	server.TenantID = store.NormalizeTenantID(tenantID)
	return server, nil
}

// directoryContext carries the assignment's tenant into department lookups.
// Membership is stored per tenant. Without this value the lookup reads the
// default tenant, and a department assignment for any other tenant misses.
func directoryContext(ctx context.Context, tenantID string) context.Context {
	return security.WithTenant(ctx, tenantID)
}

func (p *Pool) departmentChain(ctx context.Context, userID string) []string {
	if p == nil || p.Directory == nil {
		return nil
	}
	email, err := p.Directory.Email(ctx, userID)
	if err != nil || strings.TrimSpace(email) == "" {
		return nil
	}
	groupID, err := p.Directory.GroupID(ctx, email)
	if err != nil {
		return nil
	}
	var chain []string
	seen := map[string]bool{}
	for groupID != "" && len(chain) < 32 && !seen[groupID] {
		seen[groupID] = true
		chain = append(chain, groupID)
		parent, err := p.Directory.ParentID(ctx, groupID)
		if err != nil {
			break
		}
		groupID = strings.TrimSpace(parent)
	}
	return chain
}

func (p *Pool) call(ctx context.Context, server Server, method, path string, body any, dest any) error {
	client := p.clientFor(ctx, server)
	if client == nil {
		return fmt.Errorf("%w: node is offline", ErrService)
	}
	return p.callClient(ctx, client, server, method, path, body, dest)
}

// clientFor returns the process-wide client when the server is dialed
// directly. A node server gets a tunnel client, or nil when the tunnel is
// not there. The two are never mixed: an empty NodeID does not call Nodes,
// and a node server does not fall back to the public dialer.
func (p *Pool) clientFor(ctx context.Context, server Server) *http.Client {
	if strings.TrimSpace(server.NodeID) != "" {
		if p == nil || p.Nodes == nil {
			return nil
		}
		return p.Nodes(ctx, server.TenantID, server.NodeID, "desktopd")
	}
	return p.client()
}

// withNodeTransport keeps client as-is for a direct server. A node server
// gets a client with the same timeout and the tunnel dialer. A missing
// tunnel is an error, so the call does not try the placeholder host.
func (p *Pool) withNodeTransport(ctx context.Context, server Server, client *http.Client) (*http.Client, error) {
	if strings.TrimSpace(server.NodeID) == "" {
		return client, nil
	}
	nodeClient := p.clientFor(ctx, server)
	if nodeClient == nil || nodeClient.Transport == nil {
		return nil, fmt.Errorf("%w: node is offline", ErrService)
	}
	if client == nil {
		client = nodeClient
	}
	return &http.Client{Timeout: client.Timeout, Transport: nodeClient.Transport}, nil
}

// desktopIdempotentAttempts is how many times one create-or-read call to the
// Docker service is tried. The session call is create-or-get for one
// (tenant, user) and the stop call is idempotent, so answering an overloaded
// service with one more try cannot double a desktop. Commands that act on
// the desktop (app, open) deliberately do not come through here.
const desktopIdempotentAttempts = 3

// desktopIdempotentRetryWait is the gap before the next try of an
// create-or-read call. Short, because the caller is a person waiting for a
// desktop and the failure being retried is a proxy or service blink.
const desktopIdempotentRetryWait = 250 * time.Millisecond

// callIdempotent sends one create-or-read call and tries it again when the
// service never answered or answered 5xx. A status the service actually
// chose — 4xx — is returned at once: retrying a rejection is how a
// misconfigured server turns into a slow one.
func (p *Pool) callIdempotent(ctx context.Context, server Server, method, path string, body any, dest any) error {
	var err error
	for attempt := 1; attempt <= desktopIdempotentAttempts; attempt++ {
		err = p.call(ctx, server, method, path, body, dest)
		if err == nil || !desktopPoolCallWorthRetry(err) {
			return err
		}
		if attempt == desktopIdempotentAttempts {
			return err
		}
		wait := time.NewTimer(desktopIdempotentRetryWait * time.Duration(attempt))
		select {
		case <-ctx.Done():
			wait.Stop()
			return err
		case <-wait.C:
		}
	}
	return err
}

// desktopPoolCallWorthRetry reports whether one Docker-service call failed
// without the service deciding anything: a transport error, or a 5xx from a
// proxy in front of it. Both leave the requested state unchanged, which is
// what makes another try safe for the create-or-read calls.
func desktopPoolCallWorthRetry(err error) bool {
	if err == nil || strings.Contains(err.Error(), "node is offline") {
		return false
	}
	var statusErr *upstream.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status >= 500
	}
	return true
}

func (p *Pool) callClient(ctx context.Context, client *http.Client, server Server, method, path string, body any, dest any) error {
	if client == nil {
		client = p.client()
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(server.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+server.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstream.NewStatusError(ErrService, resp.StatusCode, payload)
	}
	if dest != nil && len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, dest); err != nil {
			return fmt.Errorf("%w: %s", ErrService, err.Error())
		}
	}
	return nil
}

func (p *Pool) client() *http.Client {
	if p != nil && p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (p *Pool) load(ctx context.Context, tenantID string) (record, error) {
	out := record{Servers: []Server{}, Assignments: []Assignment{}}
	if p == nil || p.System == nil {
		return out, ErrSettingsUnavailable
	}
	raw, err := p.System.Get(ctx, storageKey(tenantID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return record{Servers: []Server{}, Assignments: []Assignment{}}, nil
	}
	if out.Servers == nil {
		out.Servers = []Server{}
	}
	if out.Assignments == nil {
		out.Assignments = []Assignment{}
	}
	return out, nil
}

func (p *Pool) save(ctx context.Context, tenantID string, rec record) error {
	if p == nil || p.System == nil {
		return ErrSettingsUnavailable
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return p.System.Set(ctx, storageKey(tenantID), string(raw))
}

func normalizeServer(in Server, keepToken string) (Server, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.AccessToken = strings.TrimSpace(in.AccessToken)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return Server{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	parsed, err := url.Parse(in.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Server{}, fmt.Errorf("%w: base_url is invalid", ErrInvalidInput)
	}
	if strings.TrimSpace(in.NodeID) == "" && virtualNodeHost(parsed.Hostname()) {
		return Server{}, fmt.Errorf("%w: base_url is invalid", ErrInvalidInput)
	}
	if in.AccessToken == "" {
		in.AccessToken = keepToken
	}
	resources, err := desktop.NormalizeResources(in.Image, in.Memory, in.CPUs, in.ShmSize)
	if err != nil {
		return Server{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	in.Image, in.Memory, in.CPUs, in.ShmSize = resources.Image, resources.Memory, resources.CPUs, resources.ShmSize
	return in, nil
}

func normalizeAssignment(in Assignment, rec record) (Assignment, error) {
	in.Scope = strings.TrimSpace(in.Scope)
	in.TargetID = strings.TrimSpace(in.TargetID)
	in.ServerID = strings.TrimSpace(in.ServerID)
	server, err := serverByID(rec.Servers, in.ServerID)
	if err != nil {
		return Assignment{}, ErrNotFound
	}
	switch in.Scope {
	case ScopeGlobal:
		in.TargetID = ""
		if matchAssignment(rec.Assignments, ScopeGlobal, "") != "" {
			return Assignment{}, fmt.Errorf("%w: global assignment already exists", ErrInvalidInput)
		}
	case ScopeDepartment, ScopeUser:
		if !desktop.ValidUserID(in.TargetID) {
			return Assignment{}, fmt.Errorf("%w: target is required", ErrInvalidInput)
		}
		if matchAssignment(rec.Assignments, in.Scope, in.TargetID) != "" {
			return Assignment{}, fmt.Errorf("%w: assignment already exists", ErrInvalidInput)
		}
	default:
		return Assignment{}, fmt.Errorf("%w: scope must be global, department, or user", ErrInvalidInput)
	}
	// An exclusive desktop resolves only for its owner. Anything broader
	// stores a binding that can never wake the computer.
	if owner := strings.TrimSpace(server.OwnerUserID); owner != "" && (in.Scope != ScopeUser || in.TargetID != owner) {
		return Assignment{}, fmt.Errorf("%w: this computer is exclusive to %s", ErrInvalidInput, owner)
	}
	return in, nil
}

func matchAssignment(items []Assignment, scope, target string) string {
	for _, item := range items {
		if item.Scope == scope && item.TargetID == target {
			return item.ServerID
		}
	}
	return ""
}

func serverByID(servers []Server, id string) (Server, error) {
	index := serverIndex(servers, id)
	if index < 0 {
		return Server{}, ErrNotFound
	}
	return servers[index], nil
}

func serverIndex(servers []Server, id string) int {
	id = strings.TrimSpace(id)
	for i := range servers {
		if servers[i].ID == id {
			return i
		}
	}
	return -1
}

func viewOf(rec record) View {
	servers := make([]ServerView, 0, len(rec.Servers))
	for _, server := range rec.Servers {
		servers = append(servers, serverView(server))
	}
	assignments := rec.Assignments
	if assignments == nil {
		assignments = []Assignment{}
	}
	return View{Servers: servers, Assignments: assignments}
}

func serverView(server Server) ServerView {
	return ServerView{
		ID: server.ID, Name: server.Name, BaseURL: server.BaseURL,
		TokenSet: strings.TrimSpace(server.AccessToken) != "",
		Image:    server.Image, Memory: server.Memory, CPUs: server.CPUs, ShmSize: server.ShmSize,
		NodeID: server.NodeID, OwnerID: server.OwnerUserID, Intranet: serverIntranet(server),
		ResourcesAuto: server.ResourcesAuto, ResourcesPinned: server.ResourcesPinned,
	}
}

func storageKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return SettingsKey
	}
	return "tenant:" + tenantID + ":" + SettingsKey
}

func newID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		return prefix + "_" + hex.EncodeToString(sum[:4])
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}
