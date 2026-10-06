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
	"github.com/RapidAI/CodeClaw/hub/internal/store"
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
}

// ServerView hides the access token.
type ServerView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	TokenSet bool   `json:"token_set"`
	Image    string `json:"image"`
	Memory   string `json:"memory"`
	CPUs     string `json:"cpus"`
	ShmSize  string `json:"shm_size"`
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

// View is the admin payload.
type View struct {
	Servers     []ServerView `json:"servers"`
	Assignments []Assignment `json:"assignments"`
}

type record struct {
	Servers     []Server     `json:"servers"`
	Assignments []Assignment `json:"assignments"`
}

// Pool stores Docker services and calls the one assigned to a user.
type Pool struct {
	System    store.SystemSettingsRepository
	Directory Directory
	HTTP      *http.Client
	Now       func() time.Time

	mu sync.Mutex
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
	server, err := normalizeServer(in, current.AccessToken)
	if err != nil {
		return ServerView{}, err
	}
	server.ID = current.ID
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
func (p *Pool) StopDesktop(ctx context.Context, tenantID, userID string) (Desktop, error) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return Desktop{}, err
	}
	return p.stopOnServer(ctx, server, tenantID, userID)
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
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/stop", body, &out); err != nil {
		return Desktop{}, err
	}
	return Desktop{UserID: userID, ServerID: server.ID, ServerName: server.Name, Container: out.Container, Status: out.Status}, nil
}

// DesktopSession is the remote desktop MaClawSrv and the GUI can reach.
type DesktopSession struct {
	CDP     string
	Display string
	Novnc   string
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
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return DesktopSession{}, err
	}
	var out struct {
		CDP     string `json:"cdp_url"`
		Display string `json:"display"`
		Novnc   string `json:"novnc_url"`
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   strings.TrimSpace(userID),
		"image":     server.Image,
		"memory":    server.Memory,
		"cpus":      server.CPUs,
		"shm_size":  server.ShmSize,
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/session", body, &out); err != nil {
		return DesktopSession{}, err
	}
	if !strings.HasPrefix(out.CDP, "http://") && !strings.HasPrefix(out.CDP, "https://") {
		return DesktopSession{}, fmt.Errorf("%w: cdp url is invalid", ErrService)
	}
	return DesktopSession{CDP: out.CDP, Display: out.Display, Novnc: strings.TrimSpace(out.Novnc)}, nil
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

func (p *Pool) resolve(ctx context.Context, tenantID, userID string) (Server, error) {
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
		return serverByID(rec.Servers, serverID)
	}
	for _, groupID := range p.departmentChain(ctx, userID) {
		if serverID := matchAssignment(rec.Assignments, ScopeDepartment, groupID); serverID != "" {
			return serverByID(rec.Servers, serverID)
		}
	}
	if serverID := matchAssignment(rec.Assignments, ScopeGlobal, ""); serverID != "" {
		return serverByID(rec.Servers, serverID)
	}
	return Server{}, ErrNotAssigned
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
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrService, err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrService, resp.StatusCode)
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
	if serverIndex(rec.Servers, in.ServerID) < 0 {
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
