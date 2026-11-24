package botmgmt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

// nodeLink is one node's MaClawSrv: its own address, admin secret, connection
// credential and principals. The public connection on record stays untouched.
type nodeLink struct {
	NodeID         string                `json:"node_id"`
	BaseURL        string                `json:"base_url"`
	AdminSecret    string                `json:"admin_secret,omitempty"`
	MaClawTenantID string                `json:"maclaw_tenant_id,omitempty"`
	Connection     *connectionCredential `json:"connection,omitempty"`
	Principals     []ownerPrincipal      `json:"principals,omitempty"`
}

// ErrNodeOffline is a brief tunnel drop. The message stays queued and is
// tried again; it is not a permanent failure.
var ErrNodeOffline = errors.New("这台内网电脑暂时连不上")

const (
	desktopQueueWaitNode = "node"
	nodeOfflineAck       = "这台内网电脑暂时连不上，这条消息先排着。"
	nodeOfflineOutcome   = "这台内网电脑还没连回来，这条消息先停在这里。"
)

// nodeOfflineQueueMax is how long a message waits for the tunnel to return.
var nodeOfflineQueueMax = 2 * time.Minute

// nodeLinkMissing is the dialer for a node call that has no tunnel client.
// It must not be the public client: that client would dial
// srv.<id>.node.invalid on the open network.
type nodeLinkMissing struct{}

func (nodeLinkMissing) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("edge node link is unavailable")
}

func nodeLinkMissingError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "edge node link is unavailable")
}

func nodeLinkOffline(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNodeOffline) || errors.Is(err, edgenode.ErrOffline) {
		return true
	}
	// desktoppool says "node is offline"; the tunnel says "edge node is offline".
	return strings.Contains(err.Error(), "node is offline")
}

// nodeSessionMissing reports a desktop read that never found a tunnel.
// A stream that fails after the session is up wraps edgenode.ErrOffline.
// That failure is this one call, and a message for srv still goes out.
func nodeSessionMissing(err error) bool {
	if err == nil || errors.Is(err, edgenode.ErrOffline) || errors.Is(err, ErrNodeOffline) {
		return false
	}
	return strings.Contains(err.Error(), "node is offline")
}

// explainDesktopErr turns a dead tunnel into the sentence the person sees.
// Any other desktop failure stays a service error.
func explainDesktopErr(err error) error {
	if err == nil {
		return nil
	}
	if nodeLinkOffline(err) {
		return ErrNodeOffline
	}
	return fmt.Errorf("%w: %s", ErrSrv, err.Error())
}

func ownerNodeID(rec record, owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" || rec.OwnerNode == nil {
		return ""
	}
	return strings.TrimSpace(rec.OwnerNode[owner])
}

// callTarget is the srv a command should use. An empty nodeID keeps the
// public connection. A node with a srv does not need that public connection.
func callTarget(rec record, nodeID string) (record, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		if err := configured(rec); err != nil {
			return record{}, err
		}
		return rec, nil
	}
	view, ok := nodeView(rec, nodeID)
	if !ok {
		return record{}, fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	if err := configured(view); err != nil {
		return record{}, err
	}
	return view, nil
}

func nodeSrvBase(nodeID string) string {
	return "http://srv." + strings.TrimSpace(nodeID) + ".node.invalid"
}

func findNodeLink(links []nodeLink, nodeID string) *nodeLink {
	nodeID = strings.TrimSpace(nodeID)
	for i := range links {
		if links[i].NodeID == nodeID {
			return &links[i]
		}
	}
	return nil
}

// nodeView is a record whose call fields point at one node srv.
func nodeView(rec record, nodeID string) (record, bool) {
	link := findNodeLink(rec.NodeLinks, nodeID)
	if link == nil || strings.TrimSpace(link.BaseURL) == "" {
		return record{}, false
	}
	view := rec
	view.BaseURL = link.BaseURL
	view.AdminSecret = link.AdminSecret
	view.MaClawTenantID = link.MaClawTenantID
	view.Connection = link.Connection
	view.Principals = append([]ownerPrincipal(nil), link.Principals...)
	view.AccessToken = ""
	view.DialNode = link.NodeID
	return view, true
}

// foldNodeView writes a node call-record back onto the stored public record.
func foldNodeView(stored, view record) record {
	nodeID := strings.TrimSpace(view.DialNode)
	links := append([]nodeLink(nil), stored.NodeLinks...)
	placed := false
	for i := range links {
		if links[i].NodeID != nodeID {
			continue
		}
		links[i].Principals = view.Principals
		links[i].Connection = view.Connection
		links[i].MaClawTenantID = view.MaClawTenantID
		if strings.TrimSpace(view.BaseURL) != "" {
			links[i].BaseURL = view.BaseURL
		}
		if strings.TrimSpace(view.AdminSecret) != "" {
			links[i].AdminSecret = view.AdminSecret
		}
		placed = true
	}
	if !placed && nodeID != "" {
		links = append(links, nodeLink{
			NodeID: nodeID, BaseURL: view.BaseURL, AdminSecret: view.AdminSecret,
			MaClawTenantID: view.MaClawTenantID, Connection: view.Connection, Principals: view.Principals,
		})
	}
	view.BaseURL = stored.BaseURL
	view.AccessToken = stored.AccessToken
	view.AdminSecret = stored.AdminSecret
	view.MaClawTenantID = stored.MaClawTenantID
	view.Connection = stored.Connection
	view.Principals = stored.Principals
	view.NodeLinks = links
	if view.OwnerNode == nil {
		view.OwnerNode = stored.OwnerNode
	}
	view.DialNode = ""
	return view
}

func putNodePrincipal(links []nodeLink, nodeID string, principals []ownerPrincipal, maclawTenant string, conn *connectionCredential) []nodeLink {
	for i := range links {
		if links[i].NodeID != nodeID {
			continue
		}
		links[i].Principals = principals
		links[i].MaClawTenantID = maclawTenant
		links[i].Connection = conn
		return links
	}
	return links
}

// AttachNodeSrv records the srv that enrolled with a node. The public
// connection is left as it is. An empty admin secret is kept when the link
// already has one.
func (s *Service) AttachNodeSrv(ctx context.Context, tenantID, nodeID, baseURL, adminSecret string) error {
	nodeID = strings.TrimSpace(nodeID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if s == nil || nodeID == "" || baseURL == "" {
		return fmt.Errorf("%w: node srv is incomplete", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	adminSecret = strings.TrimSpace(adminSecret)
	if link := findNodeLink(rec.NodeLinks, nodeID); link != nil {
		link.BaseURL = baseURL
		if adminSecret != "" {
			link.AdminSecret = adminSecret
		}
		return s.save(ctx, tenantID, rec)
	}
	rec.NodeLinks = append(rec.NodeLinks, nodeLink{NodeID: nodeID, BaseURL: baseURL, AdminSecret: adminSecret})
	return s.save(ctx, tenantID, rec)
}

// PinOwnerNode sends this user's new bots, and calls that only know the
// user, to the node srv. An empty nodeID clears the pin. A node that has
// no srv link is refused and the current pin stays unchanged.
func (s *Service) PinOwnerNode(ctx context.Context, tenantID, userID, nodeID string) error {
	userID = strings.TrimSpace(userID)
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || userID == "" {
		return fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if rec.OwnerNode == nil {
		rec.OwnerNode = map[string]string{}
	}
	if nodeID == "" {
		delete(rec.OwnerNode, userID)
		return s.save(ctx, tenantID, rec)
	}
	if findNodeLink(rec.NodeLinks, nodeID) == nil {
		return fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	rec.OwnerNode[userID] = nodeID
	return s.save(ctx, tenantID, rec)
}

// PinOwnerCloudNode sends this user's cloud agents to the node srv. An empty
// nodeID clears the pin, putting the user back on the public connection. A
// node that has no srv link is refused and the current pin stays unchanged.
func (s *Service) PinOwnerCloudNode(ctx context.Context, tenantID, userID, nodeID string) error {
	userID = strings.TrimSpace(userID)
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || userID == "" {
		return fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if rec.OwnerCloudNode == nil {
		rec.OwnerCloudNode = map[string]string{}
	}
	if nodeID == "" {
		delete(rec.OwnerCloudNode, userID)
		return s.save(ctx, tenantID, rec)
	}
	if findNodeLink(rec.NodeLinks, nodeID) == nil {
		return fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	rec.OwnerCloudNode[userID] = nodeID
	return s.save(ctx, tenantID, rec)
}

// OwnerCloudNodeID reports the node srv this user's cloud agents use.
// Empty means the public connection.
func (s *Service) OwnerCloudNodeID(ctx context.Context, tenantID, userID string) string {
	userID = strings.TrimSpace(userID)
	if s == nil || userID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rec.OwnerCloudNode[userID])
}

// CloudNodeBindings lists every user pinned to a node srv for cloud agents.
func (s *Service) CloudNodeBindings(ctx context.Context, tenantID string) map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return out
	}
	for user, node := range rec.OwnerCloudNode {
		if strings.TrimSpace(node) != "" {
			out[user] = node
		}
	}
	return out
}

// UnpinNodeExcept drops every pin that names this node except keepUserID's.
// A node turning exclusive strands every other user's pin: their next bots
// would land on the srv but could never wake the desktop.
func (s *Service) UnpinNodeExcept(ctx context.Context, tenantID, nodeID, keepUserID string) error {
	nodeID = strings.TrimSpace(nodeID)
	keepUserID = strings.TrimSpace(keepUserID)
	if s == nil || nodeID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	changed := false
	for user, id := range rec.OwnerNode {
		if id == nodeID && user != keepUserID {
			delete(rec.OwnerNode, user)
			changed = true
		}
	}
	for user, id := range rec.OwnerCloudNode {
		if id == nodeID && user != keepUserID {
			delete(rec.OwnerCloudNode, user)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save(ctx, tenantID, rec)
}

// ForgetNodeSrv drops the node connection and any user pin that named it.
// Bots keep the NodeID they were created with.
func (s *Service) ForgetNodeSrv(ctx context.Context, tenantID, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || nodeID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	kept := rec.NodeLinks[:0]
	for _, link := range rec.NodeLinks {
		if link.NodeID != nodeID {
			kept = append(kept, link)
		}
	}
	rec.NodeLinks = kept
	for user, id := range rec.OwnerNode {
		if id == nodeID {
			delete(rec.OwnerNode, user)
		}
	}
	for user, id := range rec.OwnerCloudNode {
		if id == nodeID {
			delete(rec.OwnerCloudNode, user)
		}
	}
	return s.save(ctx, tenantID, rec)
}

const nodeConnectionEmail = "hub-connection@bots.maclaw.local"

func connectionReady(conn *connectionCredential) bool {
	return conn != nil && strings.TrimSpace(conn.APIKey) != "" && strings.TrimSpace(conn.APISecret) != ""
}

// ProvisionNodeConnection issues the node's shared MaClawSrv credential.
// The public connection is left as it is. A credential already on the link
// is kept. Callers must not hold s.mu.
func (s *Service) ProvisionNodeConnection(ctx context.Context, tenantID, nodeID string) error {
	return s.provisionNodeConnection(ctx, tenantID, nodeID, true)
}

func (s *Service) provisionNodeConnection(ctx context.Context, tenantID, nodeID string, lock bool) error {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || nodeID == "" {
		return fmt.Errorf("%w: node srv is incomplete", ErrInvalidInput)
	}
	load := func() (record, error) {
		if lock {
			s.mu.Lock()
			defer s.mu.Unlock()
		}
		return s.load(ctx, tenantID)
	}
	rec, err := load()
	if err != nil {
		return err
	}
	view, ok := nodeView(rec, nodeID)
	if !ok {
		return fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	if connectionReady(view.Connection) {
		return nil
	}
	if strings.TrimSpace(view.AdminSecret) == "" {
		return fmt.Errorf("%w: %s", ErrAdminSecretMissing, AdminSecretHint)
	}
	maclawTenant := strings.TrimSpace(view.MaClawTenantID)
	if maclawTenant == "" {
		var listed struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := s.doAdmin(ctx, view, http.MethodGet, "/api/v1/admin/tenants", nil, &listed); err != nil {
			return err
		}
		if len(listed.Items) > 0 {
			maclawTenant = strings.TrimSpace(listed.Items[0].ID)
		}
		if maclawTenant == "" {
			var created struct {
				ID string `json:"id"`
			}
			if err := s.doAdmin(ctx, view, http.MethodPost, "/api/v1/admin/tenants", map[string]string{"name": "hub"}, &created); err != nil {
				return err
			}
			maclawTenant = strings.TrimSpace(created.ID)
		}
		if maclawTenant == "" {
			return fmt.Errorf("%w: maclawsrv tenant missing", ErrSrv)
		}
		view.MaClawTenantID = maclawTenant
	}
	var user struct {
		ID string `json:"id"`
	}
	userPath := "/api/v1/admin/tenants/" + url.PathEscape(maclawTenant) + "/users"
	if err := s.doAdmin(ctx, view, http.MethodPost, userPath, map[string]string{
		"name":  "hub-connection",
		"email": nodeConnectionEmail,
	}, &user); err != nil {
		return err
	}
	if strings.TrimSpace(user.ID) == "" {
		return fmt.Errorf("%w: maclawsrv user id missing", ErrSrv)
	}
	var issued struct {
		ID        string `json:"id"`
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	credPath := userPath + "/" + url.PathEscape(user.ID) + "/credentials"
	if err := s.doAdmin(ctx, view, http.MethodPost, credPath, map[string]string{"name": "hub-connection"}, &issued); err != nil {
		return err
	}
	if strings.TrimSpace(issued.APIKey) == "" || strings.TrimSpace(issued.APISecret) == "" {
		return fmt.Errorf("%w: maclawsrv credential missing", ErrSrv)
	}
	view.Connection = &connectionCredential{
		ID:        strings.TrimSpace(issued.ID),
		TenantID:  maclawTenant,
		UserID:    strings.TrimSpace(user.ID),
		APIKey:    strings.TrimSpace(issued.APIKey),
		APISecret: strings.TrimSpace(issued.APISecret),
	}
	if lock {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	current, ok := nodeView(fresh, nodeID)
	if !ok {
		return fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	if connectionReady(current.Connection) {
		return nil
	}
	current.MaClawTenantID = view.MaClawTenantID
	current.Connection = view.Connection
	return s.save(ctx, tenantID, current)
}

// ownerTokenOn is the bearer for one existing bot's srv. An empty nodeID is
// the public connection. A set nodeID is that node's srv. The owner's pin is
// not consulted: it only chooses where a new bot is created when the caller
// does not name a server.
func (s *Service) ownerTokenOn(ctx context.Context, tenantID, owner, nodeID string) (record, string, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return s.ownerTokenPublic(ctx, tenantID, owner)
	}
	return s.finishNodeOwnerToken(ctx, tenantID, owner, nodeID)
}

// tokenForBot resolves the srv recorded on the bot, then ownerTokenOn.
// A missing bot uses the public connection. Callers that already checked
// ownership still get the srv that instance was created on.
func (s *Service) tokenForBot(ctx context.Context, tenantID, owner, botID string) (record, string, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, "", err
	}
	nodeID := ""
	if bot, ok := ownedBot(rec, owner, botID); ok {
		nodeID = bot.NodeID
	}
	return s.ownerTokenOn(ctx, tenantID, owner, nodeID)
}

// ownedBot is this owner's bot, named by bot id or by the MaClawSrv instance
// id. ok is false when the id is missing or belongs to someone else.
func ownedBot(rec record, owner, botID string) (Bot, bool) {
	owner = strings.TrimSpace(owner)
	botID = strings.TrimSpace(botID)
	if botID == "" || owner == "" {
		return Bot{}, false
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		for i, bot := range rec.Bots {
			if bot.InstanceID == botID {
				index = i
				break
			}
		}
	}
	if index < 0 || rec.Bots[index].OwnerUserID != owner {
		return Bot{}, false
	}
	return rec.Bots[index], true
}

func (s *Service) ensureNodeOwnerToken(ctx context.Context, tenantID string, rec *record, owner, nodeID string) (string, error) {
	view, ok := nodeView(*rec, nodeID)
	if !ok {
		return "", fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	if strings.TrimSpace(view.AdminSecret) == "" {
		return "", fmt.Errorf("%w: %s", ErrAdminSecretMissing, AdminSecretHint)
	}
	// A principal already on this node can mint its own bearer. Provisioning
	// the shared connection is only needed before the first user is created.
	if principal := findPrincipal(view.Principals, owner); principal != nil {
		return s.exchangeOwnerToken(ctx, view, *principal)
	}
	if !connectionReady(view.Connection) {
		if err := s.provisionNodeConnection(ctx, tenantID, nodeID, false); err != nil {
			return "", err
		}
		fresh, err := s.load(ctx, tenantID)
		if err != nil {
			return "", err
		}
		*rec = fresh
		view, ok = nodeView(*rec, nodeID)
		if !ok {
			return "", fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
		}
	}
	if principal := findPrincipal(view.Principals, owner); principal != nil {
		return s.exchangeOwnerToken(ctx, view, *principal)
	}
	created, err := s.provisionOwner(ctx, &view, owner)
	if err != nil {
		return "", err
	}
	view.Principals = append(view.Principals, created)
	rec.NodeLinks = putNodePrincipal(rec.NodeLinks, nodeID, view.Principals, view.MaClawTenantID, view.Connection)
	if err := s.save(ctx, tenantID, *rec); err != nil {
		return "", err
	}
	return s.exchangeOwnerToken(ctx, view, created)
}

func (s *Service) finishNodeOwnerToken(ctx context.Context, tenantID, owner, nodeID string) (record, string, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, "", err
	}
	view, ok := nodeView(rec, nodeID)
	if !ok {
		return record{}, "", fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	if strings.TrimSpace(view.AdminSecret) == "" {
		return record{}, "", fmt.Errorf("%w: %s", ErrAdminSecretMissing, AdminSecretHint)
	}
	if principal := findPrincipal(view.Principals, owner); principal != nil {
		token, err := s.exchangeOwnerToken(ctx, view, *principal)
		return view, token, err
	}
	if !connectionReady(view.Connection) {
		if err := s.ProvisionNodeConnection(ctx, tenantID, nodeID); err != nil {
			return record{}, "", err
		}
		rec, err = s.load(ctx, tenantID)
		if err != nil {
			return record{}, "", err
		}
		view, ok = nodeView(rec, nodeID)
		if !ok {
			return record{}, "", fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
		}
	}
	if principal := findPrincipal(view.Principals, owner); principal != nil {
		token, err := s.exchangeOwnerToken(ctx, view, *principal)
		return view, token, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, "", err
	}
	view, ok = nodeView(fresh, nodeID)
	if !ok {
		return record{}, "", fmt.Errorf("%w: 这个节点还没有 srv", ErrSrv)
	}
	principal := findPrincipal(view.Principals, owner)
	if principal == nil {
		created, err := s.provisionOwner(ctx, &view, owner)
		if err != nil {
			return record{}, "", err
		}
		view.Principals = append(view.Principals, created)
		if err := s.save(ctx, tenantID, view); err != nil {
			return record{}, "", err
		}
		principal = &view.Principals[len(view.Principals)-1]
	}
	token, err := s.exchangeOwnerToken(ctx, view, *principal)
	return view, token, err
}
