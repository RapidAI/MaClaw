package desktoppool

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// NodeHostSuffix is the virtual host of a desktop service reached through a
// Bot node tunnel: desktopd.<nodeID>.node.invalid. It never resolves.
const NodeHostSuffix = ".node.invalid"

// virtualNodeHost reports a hostname an administrator must not type. The
// tunnel uses it internally. Letter case and a trailing dot do not make it
// a public address.
func virtualNodeHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "node.invalid" || strings.HasSuffix(host, ".node.invalid")
}

// NodeBaseURL is the virtual BaseURL recorded for a node's desktopd.
func NodeBaseURL(nodeID string) string {
	return "http://desktopd." + strings.TrimSpace(nodeID) + NodeHostSuffix
}

const tunnelMarker = "/v1/desktops/tunnel/"

// nodeEndpoint moves a CDP or noVNC URL a node's desktopd returned onto the
// node's virtual origin. desktopd builds it from its own listener or public
// name, which Hub and MaClawSrv cannot reach. Gate userinfo, the tunnel path
// and the query are kept. A URL that is not a desktopd tunnel is unchanged.
func nodeEndpoint(server Server, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.TrimSpace(server.NodeID) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	path := parsed.EscapedPath()
	at := strings.Index(path, tunnelMarker)
	if at < 0 {
		return raw
	}
	base, err := url.Parse(NodeBaseURL(server.NodeID))
	if err != nil {
		return raw
	}
	parsed.Scheme = base.Scheme
	parsed.Host = base.Host
	tail := path[at:]
	if unescaped, err := url.PathUnescape(tail); err == nil {
		parsed.Path = unescaped
		parsed.RawPath = tail
	}
	return parsed.String()
}

// ResolvedServerID is the service the user's desktop calls go to now, or ""
// when none resolves.
func (p *Pool) ResolvedServerID(ctx context.Context, tenantID, userID string) string {
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return ""
	}
	return server.ID
}

// StopUserOn stops the user's desktop on one service regardless of the
// current assignment. Best effort: a retired or offline service is ignored.
func (p *Pool) StopUserOn(ctx context.Context, tenantID, serverID, userID string) {
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return
	}
	server, err := serverByID(rec.Servers, serverID)
	if err != nil {
		return
	}
	server, _ = p.stamp(tenantID, server, nil)
	_, _ = p.stopOnServer(ctx, server, tenantID, userID)
}

// OwnedServers lists the desktop services a user registered for themselves,
// oldest first, and the one their desktop uses now ("" when none of them).
func (p *Pool) OwnedServers(ctx context.Context, tenantID, userID string) ([]Server, string, error) {
	userID = strings.TrimSpace(userID)
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	var out []Server
	for _, server := range rec.Servers {
		if server.OwnerUserID != "" && server.OwnerUserID == userID {
			out = append(out, server)
		}
	}
	active := ""
	if id := matchAssignment(rec.Assignments, ScopeUser, userID); id != "" {
		for _, server := range out {
			if server.ID == id {
				active = id
			}
		}
	}
	return out, active, nil
}

// AttachOwnedServer records one more service only userID's desktop uses and
// makes it the active one. Earlier owned services stay registered. The
// user's previous non-owned user assignment is kept on the record so the
// last detach can restore it. Nobody else's resolve changes.
func (p *Pool) AttachOwnedServer(ctx context.Context, tenantID, userID string, in Server) (ServerView, error) {
	userID = strings.TrimSpace(userID)
	if !desktop.ValidUserID(userID) {
		return ServerView{}, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.NodeID) == "" {
		return ServerView{}, fmt.Errorf("%w: node is required", ErrInvalidInput)
	}
	in.OwnerUserID = userID
	// A desktopd installed from the GUI is on the user's own network.
	in.Intranet = true
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
	owned := map[string]bool{}
	prev := ""
	for _, item := range rec.Servers {
		if item.OwnerUserID == userID {
			owned[item.ID] = true
			if prev == "" {
				prev = item.PrevServerID
			}
		}
	}
	if prev == "" {
		if id := matchAssignment(rec.Assignments, ScopeUser, userID); id != "" && !owned[id] {
			prev = id
		}
	}
	server.ID = newID("dsrv")
	server.PrevServerID = prev
	rec.Servers = append(rec.Servers, server)
	rec.Assignments = pointUserAt(rec.Assignments, userID, server.ID)
	if err := p.save(ctx, tenantID, rec); err != nil {
		return ServerView{}, err
	}
	return serverView(server), nil
}

// ActivateOwnedServer points the user's desktop at one of their own services.
func (p *Pool) ActivateOwnedServer(ctx context.Context, tenantID, userID, serverID string) error {
	userID = strings.TrimSpace(userID)
	serverID = strings.TrimSpace(serverID)
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index := serverIndex(rec.Servers, serverID)
	if index < 0 || rec.Servers[index].OwnerUserID == "" || rec.Servers[index].OwnerUserID != userID {
		return ErrNotFound
	}
	rec.Assignments = pointUserAt(rec.Assignments, userID, serverID)
	return p.save(ctx, tenantID, rec)
}

// ReleaseOwnedDesktop points the user back at the desktop they had before
// registering their own. The owned desktop stays registered, so they can
// use it again. The returned id is the owned service they just left (""
// when the current desktop is not one of theirs) and is the one to stop.
func (p *Pool) ReleaseOwnedDesktop(ctx context.Context, tenantID, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return "", err
	}
	activeID := matchAssignment(rec.Assignments, ScopeUser, userID)
	index := serverIndex(rec.Servers, activeID)
	if index < 0 || rec.Servers[index].OwnerUserID == "" || rec.Servers[index].OwnerUserID != userID {
		return "", nil
	}
	left := rec.Servers[index]
	kept := make([]Assignment, 0, len(rec.Assignments))
	for _, item := range rec.Assignments {
		if item.Scope == ScopeUser && item.TargetID == userID {
			continue
		}
		kept = append(kept, item)
	}
	prev := strings.TrimSpace(left.PrevServerID)
	if prev != "" && serverIndex(rec.Servers, prev) >= 0 {
		kept = pointUserAt(kept, userID, prev)
	}
	rec.Assignments = kept
	if err := p.save(ctx, tenantID, rec); err != nil {
		return "", err
	}
	return left.ID, nil
}

// DetachOwnedServer removes one of the user's own services. When it was the
// active one, the newest remaining owned service takes over; with none left
// the assignment the user had before registering comes back, if that server
// still exists. It returns the node id that served the removed record.
func (p *Pool) DetachOwnedServer(ctx context.Context, tenantID, userID, serverID string) (string, error) {
	userID = strings.TrimSpace(userID)
	serverID = strings.TrimSpace(serverID)
	if userID == "" {
		return "", fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return "", err
	}
	index := serverIndex(rec.Servers, serverID)
	if index < 0 || rec.Servers[index].OwnerUserID == "" || rec.Servers[index].OwnerUserID != userID {
		return "", ErrNotFound
	}
	removed := rec.Servers[index]
	rec.Servers = append(rec.Servers[:index:index], rec.Servers[index+1:]...)
	wasActive := matchAssignment(rec.Assignments, ScopeUser, userID) == removed.ID
	kept := make([]Assignment, 0, len(rec.Assignments))
	for _, item := range rec.Assignments {
		if item.ServerID != removed.ID {
			kept = append(kept, item)
		}
	}
	rec.Assignments = kept
	if wasActive {
		next := ""
		prev := removed.PrevServerID
		for _, item := range rec.Servers {
			if item.OwnerUserID == userID {
				next = item.ID
				if prev == "" {
					prev = item.PrevServerID
				}
			}
		}
		if next == "" && prev != "" && serverIndex(rec.Servers, prev) >= 0 {
			next = prev
		}
		if next != "" {
			rec.Assignments = pointUserAt(rec.Assignments, userID, next)
		}
	}
	if err := p.save(ctx, tenantID, rec); err != nil {
		return "", err
	}
	return removed.NodeID, nil
}

// pointUserAt replaces the user's user-scope assignment with one to serverID.
func pointUserAt(items []Assignment, userID, serverID string) []Assignment {
	kept := make([]Assignment, 0, len(items)+1)
	for _, item := range items {
		if item.Scope == ScopeUser && item.TargetID == userID {
			continue
		}
		kept = append(kept, item)
	}
	return append(kept, Assignment{ID: newID("dasg"), Scope: ScopeUser, TargetID: userID, ServerID: serverID})
}

// SetNodeServersOwner makes this node's desktop services exclusive to owner,
// or shared when owner is empty. The services keep their tunnel and tokens.
// An exclusive change also drops assignments anyone else could no longer
// resolve (a dead binding would fail at wake time), points the new owner at
// the services matching what a GUI self-registration sets up, and stops the
// desktops displaced users may still hold open on the node.
func (p *Pool) SetNodeServersOwner(ctx context.Context, tenantID, nodeID, owner string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	owner = strings.TrimSpace(owner)
	if owner != "" && !desktop.ValidUserID(owner) {
		return fmt.Errorf("%w: owner is invalid", ErrInvalidInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return err
	}
	changed := false
	serverIDs := map[string]bool{}
	var ordered []string
	displaced := map[string]bool{}
	for i := range rec.Servers {
		if rec.Servers[i].NodeID != nodeID {
			continue
		}
		serverIDs[rec.Servers[i].ID] = true
		ordered = append(ordered, rec.Servers[i].ID)
		if prev := strings.TrimSpace(rec.Servers[i].OwnerUserID); prev != owner {
			if owner != "" && prev != "" {
				displaced[prev] = true
			}
			rec.Servers[i].OwnerUserID = owner
			changed = true
		}
	}
	if len(ordered) == 0 {
		return nil
	}
	if owner != "" {
		var kept []Assignment
		for _, item := range rec.Assignments {
			if serverIDs[item.ServerID] && (item.Scope != ScopeUser || item.TargetID != owner) {
				if item.Scope == ScopeUser && strings.TrimSpace(item.TargetID) != "" {
					displaced[item.TargetID] = true
				}
				changed = true
				continue
			}
			kept = append(kept, item)
		}
		rec.Assignments = kept
		// The first service record is the computer: refreshRotatedDesktop
		// writes each rotated desktopd key onto it, so it is the record
		// that stays live; a later admin-added record goes stale.
		first := ordered[0]
		if matchAssignment(rec.Assignments, ScopeUser, owner) != first {
			rec.Assignments = pointUserAt(rec.Assignments, owner, first)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := p.save(ctx, tenantID, rec); err != nil {
		return err
	}
	for _, serverID := range ordered {
		for user := range displaced {
			p.StopUserOnLater(tenantID, serverID, user)
		}
	}
	return nil
}

// ownedStopTimeout bounds the background stop of a desktop left behind on
// the service a user just moved away from.
const ownedStopTimeout = 30 * time.Second

// StopUserOnLater stops the user's desktop on serverID in the background.
func (p *Pool) StopUserOnLater(tenantID, serverID, userID string) {
	if strings.TrimSpace(serverID) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ownedStopTimeout)
		defer cancel()
		p.StopUserOn(ctx, tenantID, serverID, userID)
	}()
}
