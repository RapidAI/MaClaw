// Package edgenode is Hub's registry of tenant Bot nodes and the tunnel those
// nodes keep open. A node with an empty id is not a node: callers keep using
// the HTTP client they already had.
package edgenode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/hashicorp/yamux"
)

const settingsKey = "edge_nodes"

// indexKey maps a node id to its tenant. Tenant files are not listable, so
// without this a Hub restart cannot find a node or its hub-token hash.
const indexKey = "edge_node_index"

// Enabled is the HUB_EDGE_NODES gate. Unset, the routes and the admin control
// stay unregistered and every existing call keeps its old client.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HUB_EDGE_NODES"))) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

var (
	ErrDisabled = errors.New("edge nodes are disabled")
	ErrNotFound = errors.New("edge node not found")
	ErrCode     = errors.New("enrollment code is invalid")
	ErrOffline  = errors.New("edge node is offline")
	ErrTenant   = errors.New("edge node belongs to another tenant")
)

// Node is one registered Bot node. Secrets are stored as hashes.
type Node struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"` // direct or tunnel
	HasDesktopd  bool   `json:"has_desktopd"`
	HasSrv       bool   `json:"has_srv"`
	Version      string `json:"version,omitempty"`
	SecretHash   string `json:"secret_hash,omitempty"`
	HubTokenHash string `json:"hub_token_hash,omitempty"`
	DesktopdKey  string `json:"desktopd_key,omitempty"`
	SrvAdmin     string `json:"srv_admin_secret,omitempty"`
	AdvertiseURL string `json:"advertise_url,omitempty"`
	// DirectBase is the intranet address a GUI on that network can probe.
	// It is not a public name and is never used as the CDP origin.
	DirectBase string `json:"direct_base,omitempty"`
	// OwnerUserID is set on a node one user registered from the GUI for
	// their own desktop. Such a node serves only that user.
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
	Online      bool      `json:"online,omitempty"`
}

// Code is a one-time enrollment code.
type Code struct {
	Code        string
	TenantID    string
	NodeID      string
	Name        string
	OwnerUserID string
	Until       time.Time
	// Rotate replaces the secret of NodeID. It does not create a node.
	Rotate bool
}

// Registry stores nodes per tenant and answers tunnel auth.
type Registry struct {
	System store.SystemSettingsRepository
	Now    func() time.Time

	mu       sync.Mutex
	codes    map[string]Code
	tokens   map[string]string // hub token hash -> tenantID
	byID     map[string]Node
	hydrated bool
	epoch    uint64 // bumps on every disk write so a stale hydrate cannot resurrect a node
}

func NewRegistry(system store.SystemSettingsRepository) *Registry {
	return &Registry{
		System: system,
		Now:    time.Now,
		codes:  map[string]Code{},
		tokens: map[string]string{},
		byID:   map[string]Node{},
	}
}

func (r *Registry) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func tenantKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return settingsKey
	}
	return "tenant:" + tenantID + ":" + settingsKey
}

type file struct {
	Nodes []Node `json:"nodes"`
}

func (r *Registry) load(ctx context.Context, tenantID string) (file, error) {
	out := file{}
	if r == nil || r.System == nil {
		return out, errors.New("edge node store is unavailable")
	}
	raw, err := r.System.Get(ctx, tenantKey(tenantID))
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	return out, nil
}

func (r *Registry) save(ctx context.Context, tenantID string, f file) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return r.System.Set(ctx, tenantKey(tenantID), string(raw))
}

func (r *Registry) readIndex(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if r == nil || r.System == nil {
		return out, errors.New("edge node store is unavailable")
	}
	raw, err := r.System.Get(ctx, indexKey)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	var decoded struct {
		Nodes map[string]string `json:"nodes"`
	}
	if json.Unmarshal([]byte(raw), &decoded) != nil || decoded.Nodes == nil {
		return out, nil
	}
	return decoded.Nodes, nil
}

func (r *Registry) writeIndex(ctx context.Context, nodes map[string]string) error {
	if nodes == nil {
		nodes = map[string]string{}
	}
	raw, err := json.Marshal(struct {
		Nodes map[string]string `json:"nodes"`
	}{Nodes: nodes})
	if err != nil {
		return err
	}
	return r.System.Set(ctx, indexKey, string(raw))
}

// IssueCode starts an enrollment. The code is valid for 10 minutes.
func (r *Registry) IssueCode(ctx context.Context, tenantID, name string) (Code, error) {
	return r.issue(ctx, tenantID, name, "")
}

// IssueOwnedCode starts an enrollment for one user's own desktop-only node.
func (r *Registry) IssueOwnedCode(ctx context.Context, tenantID, userID, name string) (Code, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Code{}, errors.New("user is required")
	}
	return r.issue(ctx, tenantID, name, userID)
}

func (r *Registry) issue(ctx context.Context, tenantID, name, owner string) (Code, error) {
	if !Enabled() {
		return Code{}, ErrDisabled
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Code{}, errors.New("node name is required")
	}
	if len([]rune(name)) > 80 {
		return Code{}, errors.New("node name is too long")
	}
	if err := r.admitNewNode(ctx, tenantID); err != nil {
		return Code{}, err
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return Code{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Code{}, err
	}
	code := Code{
		Code:     strings.ToUpper(hex.EncodeToString(buf[:])),
		TenantID: store.NormalizeTenantID(tenantID),
		NodeID:   hex.EncodeToString(id[:]),
		Name:     name,
		Until:    r.now().Add(10 * time.Minute),
	}
	code.OwnerUserID = owner
	r.mu.Lock()
	now := r.now()
	for key, item := range r.codes {
		if !now.Before(item.Until) {
			delete(r.codes, key)
		}
	}
	r.codes[code.Code] = code
	r.mu.Unlock()
	return code, nil
}

// IssueRotateCode is a one-time code for a node that is already registered.
// Spending it replaces that node's secret and hub token. The old secret stops
// working. The node id, tenant, and owner stay, and the node cap is not used.
func (r *Registry) IssueRotateCode(ctx context.Context, tenantID, nodeID string) (Code, error) {
	if !Enabled() {
		return Code{}, ErrDisabled
	}
	node, err := r.find(ctx, nodeID)
	if err != nil {
		return Code{}, err
	}
	if store.NormalizeTenantID(tenantID) != store.NormalizeTenantID(node.TenantID) {
		return Code{}, ErrTenant
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return Code{}, err
	}
	code := Code{
		Code:        strings.ToUpper(hex.EncodeToString(buf[:])),
		TenantID:    node.TenantID,
		NodeID:      node.ID,
		Name:        node.Name,
		OwnerUserID: node.OwnerUserID,
		Until:       r.now().Add(10 * time.Minute),
		Rotate:      true,
	}
	r.mu.Lock()
	if r.codes == nil {
		r.codes = map[string]Code{}
	}
	r.codes[code.Code] = code
	r.mu.Unlock()
	return code, nil
}

// EnrollResult is what the node keeps. Secrets are returned once.
type EnrollResult struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
	HubToken   string `json:"hub_token"`
	TenantID   string `json:"tenant_id,omitempty"`
	Rotated    bool   `json:"-"`
	Previous   Node   `json:"-"`
}

// Enroll spends a code and stores the node.
func (r *Registry) Enroll(ctx context.Context, code, desktopdKey, srvAdmin, version string, hasDesktopd, hasSrv bool, advertise string) (EnrollResult, error) {
	if !Enabled() {
		return EnrollResult{}, ErrDisabled
	}
	if !hasDesktopd && !hasSrv {
		return EnrollResult{}, errors.New("node must include srv or desktopd")
	}
	if len(strings.TrimSpace(desktopdKey)) > 4096 || len(strings.TrimSpace(srvAdmin)) > 4096 || len(strings.TrimSpace(advertise)) > 2048 || len(version) > 64 {
		return EnrollResult{}, errors.New("enrollment field is too long")
	}
	r.mu.Lock()
	item, ok := r.codes[strings.ToUpper(strings.TrimSpace(code))]
	if ok && !r.now().Before(item.Until) {
		delete(r.codes, item.Code)
		ok = false
	}
	if ok {
		delete(r.codes, item.Code)
	}
	r.mu.Unlock()
	if !ok {
		return EnrollResult{}, ErrCode
	}
	secret, err := randomHex(32)
	if err != nil {
		r.restoreCode(item)
		return EnrollResult{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		r.restoreCode(item)
		return EnrollResult{}, err
	}
	node := Node{
		ID:           item.NodeID,
		TenantID:     item.TenantID,
		Name:         item.Name,
		Kind:         "tunnel",
		HasDesktopd:  hasDesktopd,
		HasSrv:       hasSrv,
		Version:      strings.TrimSpace(version),
		SecretHash:   hashSecret(secret),
		HubTokenHash: hashSecret(token),
		DesktopdKey:  strings.TrimSpace(desktopdKey),
		SrvAdmin:     strings.TrimSpace(srvAdmin),
		AdvertiseURL: strings.TrimSpace(advertise),
		OwnerUserID:  item.OwnerUserID,
		CreatedAt:    r.now(),
	}
	if node.OwnerUserID != "" && !hasDesktopd {
		r.restoreCode(item)
		return EnrollResult{}, errors.New("a personal desktop code registers desktopd")
	}
	if hasSrv && node.OwnerUserID != "" && strings.TrimSpace(srvAdmin) == "" {
		r.restoreCode(item)
		return EnrollResult{}, errors.New("srv admin secret is required")
	}
	if node.OwnerUserID != "" && !hasSrv {
		// Desktop only. A hub token would let this machine call the desktop
		// API for every user of the tenant. A same-machine srv keeps the
		// token, because that process is what calls Hub.
		node.HubTokenHash = ""
		token = ""
	}
	var previous Node
	if item.Rotate {
		previous, err = r.find(ctx, item.NodeID)
		if err != nil {
			r.restoreCode(item)
			return EnrollResult{}, err
		}
		if store.NormalizeTenantID(previous.TenantID) != store.NormalizeTenantID(item.TenantID) {
			r.restoreCode(item)
			return EnrollResult{}, ErrTenant
		}
		node.TenantID = previous.TenantID
		node.Name = previous.Name
		node.OwnerUserID = previous.OwnerUserID
		node.CreatedAt = previous.CreatedAt
		node.DirectBase = previous.DirectBase
	} else if err := r.admitNewNode(ctx, node.TenantID); err != nil {
		r.restoreCode(item)
		return EnrollResult{}, err
	}
	if err := r.commit(ctx, node); err != nil {
		r.restoreCode(item)
		return EnrollResult{}, err
	}
	r.remember(node)
	return EnrollResult{NodeID: node.ID, NodeSecret: secret, HubToken: token, TenantID: node.TenantID, Rotated: item.Rotate, Previous: previous}, nil
}

// Restore puts a node back after a rotation whose follow-up write failed.
// The secret that rotation just issued stops working again.
func (r *Registry) Restore(ctx context.Context, node Node) error {
	if strings.TrimSpace(node.ID) == "" {
		return ErrNotFound
	}
	if err := r.commit(ctx, node); err != nil {
		return err
	}
	r.remember(node)
	return nil
}

// ParseDirectBase checks an intranet address a GUI can probe. Empty is
// allowed and means "no direct picture". A virtual node host is refused.
func ParseDirectBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("direct base must be an http url")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".node.invalid") || strings.Contains(parsed.Path, "..") {
		return "", errors.New("direct base must be an http url")
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// SetDirectBase records the intranet address a GUI can probe for this node.
// An empty base clears it.
func (r *Registry) SetDirectBase(ctx context.Context, nodeID, base string) error {
	parsed, err := ParseDirectBase(base)
	if err != nil {
		return err
	}
	node, err := r.find(ctx, nodeID)
	if err != nil {
		return err
	}
	node.DirectBase = parsed
	return r.commit(ctx, node)
}

// commit writes the tenant file and the global index together. The caller
// restores the enrollment code when this returns an error.
func (r *Registry) commit(ctx context.Context, node Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	f, err := r.load(ctx, node.TenantID)
	if err != nil {
		return err
	}
	nodes, prev, replaced := putNode(f.Nodes, node)
	f.Nodes = nodes
	if err := r.save(ctx, node.TenantID, f); err != nil {
		return err
	}
	idx, err := r.readIndex(ctx)
	if err != nil {
		f.Nodes = undoPutNode(f.Nodes, node.ID, prev, replaced)
		_ = r.save(ctx, node.TenantID, f)
		return err
	}
	idx[node.ID] = node.TenantID
	if err := r.writeIndex(ctx, idx); err != nil {
		f.Nodes = undoPutNode(f.Nodes, node.ID, prev, replaced)
		_ = r.save(ctx, node.TenantID, f)
		return err
	}
	if replaced && prev.HubTokenHash != "" && prev.HubTokenHash != node.HubTokenHash {
		delete(r.tokens, prev.HubTokenHash)
	}
	// Publish the new secret before releasing the lock. A caller that
	// remembered the node after commit left a window where the old secret
	// still authenticated.
	r.rememberLocked(node)
	return nil
}

// putNode inserts node and drops every earlier record with the same id.
// A retried enroll must not leave two records, or a restart can load the
// old secret and reject the one just issued. prev is the first record
// replaced, so a failed index write can put that one back.
func putNode(nodes []Node, node Node) ([]Node, Node, bool) {
	var prev Node
	replaced := false
	kept := make([]Node, 0, len(nodes)+1)
	for _, item := range nodes {
		if item.ID != node.ID {
			kept = append(kept, item)
			continue
		}
		if !replaced {
			prev = item
			replaced = true
		}
	}
	return append(kept, node), prev, replaced
}

func undoPutNode(nodes []Node, id string, prev Node, replaced bool) []Node {
	kept := make([]Node, 0, len(nodes)+1)
	for _, item := range nodes {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	if replaced {
		kept = append(kept, prev)
	}
	return kept
}

func (r *Registry) restoreCode(item Code) {
	if r == nil || !r.now().Before(item.Until) {
		return
	}
	r.mu.Lock()
	r.codes[item.Code] = item
	r.mu.Unlock()
}

// Remove drops a node that enrolled but could not be wired (for example the
// desktop service record failed). The enrollment code is already spent.
func (r *Registry) Remove(ctx context.Context, nodeID string) error {
	node, err := r.find(ctx, nodeID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.epoch++
	f, err := r.load(ctx, node.TenantID)
	if err != nil {
		return err
	}
	kept := make([]Node, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		if n.ID != node.ID {
			kept = append(kept, n)
		}
	}
	f.Nodes = kept
	if err := r.save(ctx, node.TenantID, f); err != nil {
		return err
	}
	delete(r.byID, node.ID)
	if node.HubTokenHash != "" {
		delete(r.tokens, node.HubTokenHash)
	}
	idx, err := r.readIndex(ctx)
	if err != nil {
		return err
	}
	delete(idx, node.ID)
	return r.writeIndex(ctx, idx)
}

// TenantForHubToken locks a node hub token to its tenant. Empty means the
// token is not a node token. The global desktop API token is compared by
// the caller first and is not handled here.
func (r *Registry) TenantForHubToken(token string) string {
	if r == nil || strings.TrimSpace(token) == "" {
		return ""
	}
	r.hydrate(context.Background())
	sum := hashSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tokens[sum]
}

// NodeIDForHubToken is the node that presented this hub token. Empty means
// the token is not a node token. The secret itself is not returned.
func (r *Registry) NodeIDForHubToken(token string) string {
	if r == nil || strings.TrimSpace(token) == "" {
		return ""
	}
	r.hydrate(context.Background())
	sum := hashSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens[sum] == "" {
		return ""
	}
	for id, node := range r.byID {
		if node.HubTokenHash == sum {
			return id
		}
	}
	return ""
}

// Authenticate checks a node secret and returns the node. It does not mark
// the node online; Attach does that.
func (r *Registry) Authenticate(ctx context.Context, nodeID, secret string) (Node, error) {
	node, err := r.find(ctx, nodeID)
	if err != nil {
		return Node{}, err
	}
	got := hashSecret(secret)
	if subtle.ConstantTimeCompare([]byte(got), []byte(node.SecretHash)) != 1 {
		return Node{}, ErrCode
	}
	return node, nil
}

// Lookup returns a registered node without its secrets checked.
func (r *Registry) Lookup(ctx context.Context, nodeID string) (Node, error) {
	return r.find(ctx, nodeID)
}

// List returns this tenant's nodes with secrets removed. Online is left
// false; the caller fills it from the live broker.
func (r *Registry) List(ctx context.Context, tenantID string) ([]Node, error) {
	if r == nil || !Enabled() {
		return nil, ErrDisabled
	}
	f, err := r.load(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(f.Nodes))
	for _, node := range f.Nodes {
		node.SecretHash = ""
		node.HubTokenHash = ""
		node.DesktopdKey = ""
		node.SrvAdmin = ""
		node.Online = false
		out = append(out, node)
	}
	return out, nil
}

func (r *Registry) find(ctx context.Context, nodeID string) (Node, error) {
	nodeID = strings.TrimSpace(nodeID)
	if r == nil || r.System == nil || nodeID == "" {
		return Node{}, ErrNotFound
	}
	r.hydrate(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.byID[nodeID]
	if !ok {
		return Node{}, ErrNotFound
	}
	return node, nil
}

// hydrate loads every indexed node into memory once per process. Hub tokens
// are stored only as hashes, so this is what lets a node call the desktop
// API after a restart.
func (r *Registry) hydrate(ctx context.Context) {
	if r == nil || r.System == nil {
		return
	}
	r.mu.Lock()
	if r.hydrated {
		r.mu.Unlock()
		return
	}
	seen := r.epoch
	r.mu.Unlock()
	idx, err := r.readIndex(ctx)
	if err != nil {
		return
	}
	var loaded []Node
	incomplete := false
	for id, tenant := range idx {
		f, loadErr := r.load(ctx, tenant)
		if loadErr != nil {
			incomplete = true
			continue
		}
		var found *Node
		for i := range f.Nodes {
			if f.Nodes[i].ID == id {
				n := f.Nodes[i]
				found = &n
			}
		}
		if found != nil {
			loaded = append(loaded, *found)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.epoch != seen {
		return
	}
	if r.byID == nil {
		r.byID = map[string]Node{}
	}
	if r.tokens == nil {
		r.tokens = map[string]string{}
	}
	for _, n := range loaded {
		if _, exists := r.byID[n.ID]; exists {
			continue
		}
		n.Online = false
		r.byID[n.ID] = n
		if n.HubTokenHash != "" && r.tokens[n.HubTokenHash] == "" {
			r.tokens[n.HubTokenHash] = n.TenantID
		}
	}
	if !incomplete {
		r.hydrated = true
	}
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Broker holds the live yamux session of each node.
type Broker struct {
	Registry *Registry

	mu       sync.Mutex
	sessions map[string]*yamux.Session
	nodes    map[string]Node
	clients  map[string]*http.Client
	streams  map[string]int
}

func NewBroker(reg *Registry) *Broker {
	return &Broker{
		Registry: reg,
		sessions: map[string]*yamux.Session{},
		nodes:    map[string]Node{},
		clients:  map[string]*http.Client{},
		streams:  map[string]int{},
	}
}

// Attach runs the Hub side of one node connection until it ends. Hub is the
// yamux client: it opens streams. The node accepts them.
func (b *Broker) Attach(ctx context.Context, node Node, conn net.Conn) error {
	sess, err := yamux.Client(conn, localrelay.YamuxConfig())
	if err != nil {
		return err
	}
	b.mu.Lock()
	if prev := b.sessions[node.ID]; prev != nil {
		_ = prev.Close()
	}
	b.sessions[node.ID] = sess
	node.Online = true
	node.LastSeen = time.Now()
	b.nodes[node.ID] = node
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		current := b.sessions[node.ID] == sess
		if current {
			delete(b.sessions, node.ID)
			node.Online = false
			b.nodes[node.ID] = node
		}
		b.mu.Unlock()
		if current {
			b.closeIdle(node.ID)
		}
		_ = sess.Close()
	}()
	b.closeIdle(node.ID)
	if b.Registry != nil {
		b.Registry.remember(node)
	}
	go b.probe(node)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sess.CloseChan():
		return io.EOF
	}
}

// Dial opens one stream to a target on the node. tenantID must own the node.
func (b *Broker) Dial(ctx context.Context, tenantID, nodeID, target string) (net.Conn, error) {
	if !edgelink.AllowedTarget(target) {
		return nil, edgelink.ErrTarget
	}
	b.mu.Lock()
	node, ok := b.nodes[strings.TrimSpace(nodeID)]
	sess := b.sessions[strings.TrimSpace(nodeID)]
	b.mu.Unlock()
	if !ok || sess == nil {
		return nil, ErrOffline
	}
	if store.NormalizeTenantID(tenantID) != store.NormalizeTenantID(node.TenantID) {
		return nil, ErrTenant
	}
	if ctx == nil {
		ctx = context.Background()
	}
	maxStreams := 0
	if b.Registry != nil {
		if p, err := b.Registry.Policy(ctx, node.TenantID); err == nil {
			maxStreams = p.MaxStreams
		}
	}
	held := b.acquireStream(node.TenantID, maxStreams)
	if !held && maxStreams > 0 {
		// A finished health check keeps its tunnel in the idle pool. That
		// pool must not use up the last slot while nothing is in flight.
		// The client that is dialing is left alone: closing it from inside
		// its own Dial would wait on the lock that dial already holds.
		b.closeIdleTenant(node.TenantID, dialingClient(ctx))
		held = b.acquireStream(node.TenantID, maxStreams)
	}
	if !held && maxStreams > 0 {
		return nil, ErrStreamLimit
	}
	release := func() {}
	if held && maxStreams > 0 {
		release = func() { b.releaseStream(node.TenantID) }
	}
	type opened struct {
		conn net.Conn
		err  error
	}
	ch := make(chan opened, 1)
	go func() {
		conn, openErr := sess.OpenStream()
		ch <- opened{conn, openErr}
	}()
	var stream net.Conn
	select {
	case <-ctx.Done():
		go func() {
			result := <-ch
			if result.conn != nil {
				_ = result.conn.Close()
			}
		}()
		release()
		return nil, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			release()
			return nil, fmt.Errorf("%w: %s", ErrOffline, result.err.Error())
		}
		stream = result.conn
	}
	if err := edgelink.WriteTarget(stream, target); err != nil {
		_ = stream.Close()
		release()
		return nil, err
	}
	if maxStreams > 0 {
		stream = &streamLease{Conn: stream, release: release}
	}
	return stream, nil
}

func (b *Broker) acquireStream(tenantID string, max int) bool {
	if b == nil || max <= 0 {
		return false
	}
	tenantID = store.NormalizeTenantID(tenantID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.streams == nil {
		b.streams = map[string]int{}
	}
	if b.streams[tenantID] >= max {
		return false
	}
	b.streams[tenantID]++
	return true
}

type dialingClientKey struct{}

func dialingClient(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(dialingClientKey{}).(string)
	return key
}

// closeIdleTenant drops keep-alive tunnels for one tenant so a new call
// can take a slot that only an idle probe was holding. skip is the client
// already inside Dial, which must not be closed from there.
func (b *Broker) closeIdleTenant(tenantID, skip string) {
	if b == nil {
		return
	}
	tenantID = store.NormalizeTenantID(tenantID)
	b.mu.Lock()
	prefix := tenantID + "\x00"
	var clients []*http.Client
	for key, client := range b.clients {
		if client != nil && strings.HasPrefix(key, prefix) && key != skip {
			clients = append(clients, client)
		}
	}
	b.mu.Unlock()
	for _, client := range clients {
		client.CloseIdleConnections()
	}
}

func (b *Broker) releaseStream(tenantID string) {
	if b == nil {
		return
	}
	tenantID = store.NormalizeTenantID(tenantID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.streams[tenantID] > 0 {
		b.streams[tenantID]--
	}
}

type streamLease struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *streamLease) Close() error {
	c.once.Do(func() {
		if c.release != nil {
			c.release()
		}
	})
	if c.Conn == nil {
		return nil
	}
	return c.Conn.Close()
}

// closeIdle drops pooled streams after the yamux session is replaced.
// Those streams belong to the session that just closed.
func (b *Broker) closeIdle(nodeID string) {
	if b == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	b.mu.Lock()
	var idle []*http.Client
	for key, client := range b.clients {
		parts := strings.Split(key, "\x00")
		if len(parts) >= 2 && parts[1] == nodeID && client != nil {
			idle = append(idle, client)
		}
	}
	b.mu.Unlock()
	for _, client := range idle {
		client.CloseIdleConnections()
	}
}

// probe asks the node, through the tunnel, whether desktopd is healthy and
// which srv version is running. The result is what the node list shows as
// 在线 / 能力 / 版本. The desktopd key is sent as a bearer and is not logged.
func (b *Broker) probe(node Node) {
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var parts []string
	if node.HasDesktopd && b.desktopdHealthy(ctx, node) {
		parts = append(parts, "desktopd")
	}
	if node.HasSrv {
		if ver := b.srvVersion(ctx, node); ver != "" {
			parts = append(parts, "srv "+ver)
		}
	}
	if len(parts) == 0 {
		log.Printf("[edge] probe empty node=%s desktopd=%v srv=%v", node.ID, node.HasDesktopd, node.HasSrv)
		return
	}
	shown := strings.Join(parts, " / ")
	log.Printf("[edge] probe node=%s result=%s", node.ID, shown)
	if len(shown) > 80 {
		shown = shown[:80]
	}
	b.mu.Lock()
	current, ok := b.nodes[node.ID]
	sess := b.sessions[node.ID]
	if ok && sess != nil && !sess.IsClosed() {
		current.Version = shown
		b.nodes[node.ID] = current
	}
	b.mu.Unlock()
}

func (b *Broker) desktopdHealthy(ctx context.Context, node Node) bool {
	client := b.Client(node.TenantID, node.ID, edgelink.TargetDesktopd)
	if client == nil {
		log.Printf("[edge] probe desktopd node=%s no tunnel", node.ID)
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://desktopd.node.invalid/v1/health", nil)
	if err != nil {
		return false
	}
	if key := strings.TrimSpace(node.DesktopdKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[edge] probe desktopd node=%s err=%v", node.ID, err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		log.Printf("[edge] probe desktopd node=%s status=%d", node.ID, resp.StatusCode)
		return false
	}
	var parsed struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(body, &parsed) != nil || !parsed.OK {
		log.Printf("[edge] probe desktopd node=%s health not ok", node.ID)
		return false
	}
	return true
}

func (b *Broker) srvVersion(ctx context.Context, node Node) string {
	client := b.Client(node.TenantID, node.ID, edgelink.TargetSrv)
	if client == nil {
		log.Printf("[edge] probe srv node=%s no tunnel", node.ID)
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://srv.node.invalid/version", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[edge] probe srv node=%s err=%v", node.ID, err)
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		log.Printf("[edge] probe srv node=%s status=%d", node.ID, resp.StatusCode)
		return ""
	}
	var parsed struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		log.Printf("[edge] probe srv node=%s version unreadable", node.ID)
		return ""
	}
	ver := strings.TrimSpace(parsed.Version)
	if ver == "" || strings.ContainsAny(ver, "\r\n") || len(ver) > 32 {
		log.Printf("[edge] probe srv node=%s version rejected", node.ID)
		return ""
	}
	return ver
}

// ApplyLive fills online and the probed version from the open tunnel.
// Secrets on the broker's copy are not copied back.
func (b *Broker) ApplyLive(node *Node) {
	if b == nil || node == nil {
		return
	}
	node.Online = b.Online(node.ID)
	b.mu.Lock()
	live, ok := b.nodes[node.ID]
	b.mu.Unlock()
	if ok && strings.TrimSpace(live.Version) != "" {
		node.Version = live.Version
	}
}

// Online reports whether the node has a live link now.
func (b *Broker) Online(nodeID string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	sess := b.sessions[strings.TrimSpace(nodeID)]
	return sess != nil && !sess.IsClosed()
}

// Drop closes the node's live link and forgets its pooled clients. A removed
// node must not keep serving through a session it opened earlier.
func (b *Broker) Drop(nodeID string) {
	if b == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	b.mu.Lock()
	sess := b.sessions[nodeID]
	delete(b.sessions, nodeID)
	delete(b.nodes, nodeID)
	var idle []*http.Client
	for key, client := range b.clients {
		parts := strings.Split(key, "\x00")
		if len(parts) >= 2 && parts[1] == nodeID {
			idle = append(idle, client)
			delete(b.clients, key)
		}
	}
	b.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
	for _, client := range idle {
		client.CloseIdleConnections()
	}
}

// TransportForHost carries requests for desktopd.<id>.node.invalid through
// that node, with the node's own tenant. Any other host returns nil.
func (b *Broker) TransportForHost(host string) http.RoundTripper {
	if b == nil {
		return nil
	}
	nodeID := NodeIDFromHost(host)
	if nodeID == "" {
		return nil
	}
	b.mu.Lock()
	node, ok := b.nodes[nodeID]
	b.mu.Unlock()
	if !ok {
		if b.Registry == nil {
			return nil
		}
		found, err := b.Registry.Lookup(context.Background(), nodeID)
		if err != nil {
			return nil
		}
		node = found
	}
	if !node.HasDesktopd {
		return nil
	}
	client := b.Client(node.TenantID, node.ID, edgelink.TargetDesktopd)
	if client == nil {
		return nil
	}
	return client.Transport
}

// NodeIDFromHost returns the node id of desktopd.<id>.node.invalid, or "".
func NodeIDFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	const prefix, suffix = "desktopd.", ".node.invalid"
	if !strings.HasPrefix(host, prefix) || !strings.HasSuffix(host, suffix) {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(host, prefix), suffix)
	if id == "" || strings.Contains(id, ".") {
		return ""
	}
	return id
}

func (b *Broker) Client(tenantID, nodeID, target string) *http.Client {
	tenantID = store.NormalizeTenantID(tenantID)
	nodeID = strings.TrimSpace(nodeID)
	target = strings.TrimSpace(target)
	key := tenantID + "\x00" + nodeID + "\x00" + target
	b.mu.Lock()
	if b.clients == nil {
		b.clients = map[string]*http.Client{}
	}
	if client := b.clients[key]; client != nil {
		b.mu.Unlock()
		return client
	}
	b.mu.Unlock()
	client := &http.Client{
		Timeout: 2 * time.Minute,
		Transport: &http.Transport{
			// DialContext ignores the address. A process HTTP proxy would
			// still rewrite the request, so the tunnel must not use one.
			Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				if ctx == nil {
					ctx = context.Background()
				}
				ctx = context.WithValue(ctx, dialingClientKey{}, key)
				return b.Dial(ctx, tenantID, nodeID, target)
			},
			MaxIdleConns:    4,
			IdleConnTimeout: 90 * time.Second,
		},
	}
	b.mu.Lock()
	if existing := b.clients[key]; existing != nil {
		b.mu.Unlock()
		return existing
	}
	b.clients[key] = client
	b.mu.Unlock()
	return client
}

func (r *Registry) remember(node Node) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rememberLocked(node)
}

func (r *Registry) rememberLocked(node Node) {
	if r.byID == nil {
		r.byID = map[string]Node{}
	}
	if r.tokens == nil {
		r.tokens = map[string]string{}
	}
	// Online is the broker's session flag. Keeping it here made a node look
	// connected after the tunnel had already closed.
	node.Online = false
	if prev, ok := r.byID[node.ID]; ok && prev.HubTokenHash != "" && prev.HubTokenHash != node.HubTokenHash {
		delete(r.tokens, prev.HubTokenHash)
	}
	r.byID[node.ID] = node
	if node.HubTokenHash != "" {
		r.tokens[node.HubTokenHash] = node.TenantID
	}
}
