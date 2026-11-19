package desktoppool

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Desktop egress proxy modes (desktop_proxy_mode). desktopd applies them per
// user container in its bridge forward proxy; see desktopd/proxymode.go.
const (
	ProxyModeProxy   = "proxy"
	ProxyModeDirect  = "direct"
	ProxyModeSmart   = "smart"
	DefaultProxyMode = ProxyModeSmart
)

// ErrProxyModeLocked is returned when a user changes a mode the tenant locked.
var ErrProxyModeLocked = errors.New("desktop proxy mode is locked by the tenant administrator")

// ErrIntranetProxyDirect is returned when a user changes the mode of a
// desktopd they installed. That desktop connects from their own machine.
var ErrIntranetProxyDirect = errors.New("intranet desktop connects directly from that machine")

// NormalizeProxyMode accepts "", proxy (alias always), direct and smart.
func NormalizeProxyMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inherit", "default":
		return "", nil
	case ProxyModeProxy, "always":
		return ProxyModeProxy, nil
	case ProxyModeDirect:
		return ProxyModeDirect, nil
	case ProxyModeSmart:
		return ProxyModeSmart, nil
	}
	return "", fmt.Errorf("%w: proxy mode must be proxy, direct or smart", ErrInvalidInput)
}

// ProxyModeView is one user's desktop proxy setting as the UI shows it.
type ProxyModeView struct {
	// Mode is the mode in effect for this user's desktop.
	Mode string `json:"mode"`
	// UserMode is the user's own choice ("" = follow the tenant default).
	UserMode string `json:"user_mode"`
	// TenantDefault is the tenant default ("" = smart).
	TenantDefault string `json:"tenant_default"`
	// Locked means the tenant forces TenantDefault and users cannot change it.
	// An intranet desktop also locks the mode: it is always direct.
	Locked bool `json:"locked"`
	// Intranet means this user's desktop is a desktopd they installed from
	// the GUI. Its traffic leaves from that machine, not the cloud proxy.
	Intranet bool `json:"intranet,omitempty"`
	// Applied is false when the choice was stored but the desktop host could
	// not be told yet (it picks the mode up on the next desktop open).
	Applied bool   `json:"applied"`
	Warning string `json:"warning,omitempty"`
}

// ProxyModePolicy is the tenant-level part of the setting.
type ProxyModePolicy struct {
	Default string `json:"default"`
	Locked  bool   `json:"locked"`
}

func proxyModeUser(userID string) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	return strings.TrimSpace(owner)
}

func resolveProxyMode(rec record, userID string) ProxyModeView {
	view := ProxyModeView{TenantDefault: rec.ProxyModeDefault, Locked: rec.ProxyModeLocked, Applied: true}
	if !rec.ProxyModeLocked {
		view.UserMode = rec.ProxyModes[proxyModeUser(userID)]
	}
	switch {
	case view.UserMode != "":
		view.Mode = view.UserMode
	case rec.ProxyModeDefault != "":
		view.Mode = rec.ProxyModeDefault
	default:
		view.Mode = DefaultProxyMode
	}
	return view
}

// effectiveProxyMode is what the create/session calls carry so a desktopd
// restart or a fresh host still applies the user's mode. "" on any read error.
func (p *Pool) effectiveProxyMode(ctx context.Context, tenantID, userID string) string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return ""
	}
	return resolveProxyMode(rec, userID).Mode
}

// desktopEgressMode is the mode sent to the desktopd we actually resolved.
// An intranet machine always dials from itself, even when a department
// assignment landed on it. A cloud machine keeps the user's mode.
func (p *Pool) desktopEgressMode(ctx context.Context, tenantID, userID string, server Server) string {
	if serverIntranet(server) {
		return ProxyModeDirect
	}
	return p.effectiveProxyMode(ctx, tenantID, userID)
}

// serverIntranet reports a desktopd the user installed from the GUI.
// OwnerUserID is the older mark; Intranet is set on attach now.
func serverIntranet(server Server) bool {
	return server.Intranet || strings.TrimSpace(server.OwnerUserID) != ""
}

// intranetDesktop reports whether the user's own assignment is a desktopd
// they installed. Department and global assignments are the tenant's
// desktops and stay on the cloud proxy policy.
func intranetDesktop(rec record, userID string) bool {
	id := matchAssignment(rec.Assignments, ScopeUser, proxyModeUser(userID))
	if id == "" {
		return false
	}
	for _, server := range rec.Servers {
		if server.ID == id && serverIntranet(server) {
			return true
		}
	}
	return false
}

// UserDesktopIntranet reports whether this user's own desktop assignment is
// a desktopd they installed. Switching that assignment back to the cloud
// desktop reports false. Opening a desktop uses the same rule on the
// resolved machine.
func (p *Pool) UserDesktopIntranet(ctx context.Context, tenantID, userID string) bool {
	if p == nil || proxyModeUser(userID) == "" {
		return false
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return false
	}
	return intranetDesktop(rec, userID)
}

func applyIntranetProxy(rec record, userID string, view ProxyModeView) ProxyModeView {
	if !intranetDesktop(rec, userID) {
		return view
	}
	view.Intranet = true
	view.Mode = ProxyModeDirect
	view.Locked = true
	return view
}

// ProxyMode reports one user's desktop proxy setting.
func (p *Pool) ProxyMode(ctx context.Context, tenantID, userID string) (ProxyModeView, error) {
	if proxyModeUser(userID) == "" {
		return ProxyModeView{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return ProxyModeView{}, err
	}
	return applyIntranetProxy(rec, userID, resolveProxyMode(rec, userID)), nil
}

// SetUserProxyMode stores the user's desktop_proxy_mode ("" = follow the
// tenant default) and pushes the effective mode to the user's desktopd. A
// failed push keeps the choice and reports Applied=false with a warning.
func (p *Pool) SetUserProxyMode(ctx context.Context, tenantID, userID, mode string) (ProxyModeView, error) {
	user := proxyModeUser(userID)
	if user == "" {
		return ProxyModeView{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	mode, err := NormalizeProxyMode(mode)
	if err != nil {
		return ProxyModeView{}, err
	}
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		p.mu.Unlock()
		return ProxyModeView{}, err
	}
	if intranetDesktop(rec, user) {
		view := applyIntranetProxy(rec, user, resolveProxyMode(rec, user))
		p.mu.Unlock()
		return view, ErrIntranetProxyDirect
	}
	if rec.ProxyModeLocked {
		p.mu.Unlock()
		return resolveProxyMode(rec, user), ErrProxyModeLocked
	}
	if rec.ProxyModes == nil {
		rec.ProxyModes = map[string]string{}
	}
	if mode == "" {
		delete(rec.ProxyModes, user)
	} else {
		rec.ProxyModes[user] = mode
	}
	err = p.save(ctx, tenantID, rec)
	p.mu.Unlock()
	if err != nil {
		return ProxyModeView{}, err
	}
	view := resolveProxyMode(rec, user)
	p.pushProxyMode(ctx, tenantID, user, &view)
	return view, nil
}

// TenantProxyModePolicy reads the tenant default and lock.
func (p *Pool) TenantProxyModePolicy(ctx context.Context, tenantID string) (ProxyModePolicy, error) {
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return ProxyModePolicy{}, err
	}
	return ProxyModePolicy{Default: rec.ProxyModeDefault, Locked: rec.ProxyModeLocked}, nil
}

// SetTenantProxyModePolicy stores the tenant default and lock. Running
// desktops pick it up on their next open (or the user's next change); it is
// not pushed to every host here.
func (p *Pool) SetTenantProxyModePolicy(ctx context.Context, tenantID string, in ProxyModePolicy) (ProxyModePolicy, error) {
	mode, err := NormalizeProxyMode(in.Default)
	if err != nil {
		return ProxyModePolicy{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return ProxyModePolicy{}, err
	}
	rec.ProxyModeDefault = mode
	rec.ProxyModeLocked = in.Locked
	if err := p.save(ctx, tenantID, rec); err != nil {
		return ProxyModePolicy{}, err
	}
	return ProxyModePolicy{Default: rec.ProxyModeDefault, Locked: rec.ProxyModeLocked}, nil
}

// pushProxyMode tells the user's desktopd (POST /v1/desktops/proxy-mode).
func (p *Pool) pushProxyMode(ctx context.Context, tenantID, userID string, view *ProxyModeView) {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, ErrNotAssigned) {
			// No desktop host yet: the mode rides the first create call.
			return
		}
		view.Applied = false
		view.Warning = "saved; applies the next time the desktop opens"
		return
	}
	body := map[string]string{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   userID,
		"mode":      view.Mode,
	}
	if err := p.call(ctx, server, http.MethodPost, "/v1/desktops/proxy-mode", body, nil); err != nil {
		view.Applied = false
		view.Warning = "saved; applies the next time the desktop opens"
	}
}
