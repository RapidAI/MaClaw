package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
)

// A reading group lives on the hub that created it. An invitee on another hub
// never polls that store, so the invitation is relayed to their hub and the
// documents they may open are listed back through it. The invite secret is
// the credential: their hub presents it, and this hub checks it before
// accepting the person or handing over a group document.
//
// A registered hub only accepts a relay whose origin center knows. An
// unregistered hub (tests, a machine that has not joined center) still checks
// the secret against the origin. The secret never appears in a client response.

const companionRemoteSnapTTL = 4 * time.Second

type companionHomeHub struct {
	HubID   string
	BaseURL string
	Tenant  string
}

type companionRemoteInvite struct {
	GroupID        string    `json:"group_id"`
	Name           string    `json:"name"`
	Owner          string    `json:"owner"`
	Contact        string    `json:"contact"`
	OriginHub      string    `json:"origin_hub"`
	Secret         string    `json:"secret"`
	Status         string    `json:"status"`
	Muted          bool      `json:"muted,omitempty"`
	UserID         string    `json:"user_id,omitempty"`
	NotificationID string    `json:"notification_id,omitempty"`
	InvitedAt      time.Time `json:"invited_at"`
}

var companionRemoteInvites = struct {
	sync.Mutex
	loaded bool
	items  map[string]companionRemoteInvite
}{items: map[string]companionRemoteInvite{}}

var companionFederation = struct {
	sync.RWMutex
	identity *auth.IdentityService
	center   *center.Service
}{}

// companionHomeHubLookup, companionLocalOriginFn, and companionOriginTrusted
// are test seams. Production leaves them nil and asks center.
var (
	companionHomeHubLookup func(context.Context, string) (companionHomeHub, bool)
	companionLocalOriginFn func() string
	companionOriginTrusted func(context.Context, string) bool
)

var companionFederationClient = &http.Client{
	Timeout: 4 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type companionRemoteSnapshot struct {
	gen    uint64
	at     time.Time
	shares []map[string]any
	stamp  uint64
}

var companionRemoteSnapCache = struct {
	sync.Mutex
	gen        uint64
	items      map[string]companionRemoteSnapshot
	refreshing map[string]bool
}{items: map[string]companionRemoteSnapshot{}, refreshing: map[string]bool{}}

// ConfigureCompanionFederation wires center so an invite can find the
// invitee's hub and a receiving hub can check that the origin is real.
func ConfigureCompanionFederation(identity *auth.IdentityService, centerSvc *center.Service) {
	companionFederation.Lock()
	companionFederation.identity = identity
	companionFederation.center = centerSvc
	companionFederation.Unlock()
}

func companionFederationClear() {
	companionFederation.Lock()
	companionFederation.identity = nil
	companionFederation.center = nil
	companionFederation.Unlock()
	companionHomeHubLookup = nil
	companionLocalOriginFn = nil
	companionOriginTrusted = nil
	companionRemoteSnapForgetAll()
}

func companionFederationCenter() *center.Service {
	companionFederation.RLock()
	defer companionFederation.RUnlock()
	return companionFederation.center
}

func companionFederationIdentity() *auth.IdentityService {
	companionFederation.RLock()
	defer companionFederation.RUnlock()
	return companionFederation.identity
}

func resetCompanionRemoteInvites() {
	companionRemoteInvites.Lock()
	companionRemoteInvites.loaded = false
	companionRemoteInvites.items = map[string]companionRemoteInvite{}
	companionRemoteInvites.Unlock()
	companionRemoteSnapForgetAll()
}

func companionRemoteInviteKey(origin, groupID, contact string) string {
	return companionHubBaseURL(origin) + "|" + strings.TrimSpace(groupID) + "|" + strings.TrimSpace(contact)
}

func companionRemoteInvitePath() string {
	path := companionDocGroupStatePath()
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".json") + "-remote-invites.json"
}

func loadCompanionRemoteInvites() {
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	if companionRemoteInvites.loaded {
		return
	}
	companionRemoteInvites.loaded = true
	if companionRemoteInvites.items == nil {
		companionRemoteInvites.items = map[string]companionRemoteInvite{}
	}
	path := companionRemoteInvitePath()
	if path == "" {
		return
	}
	raw, err := readCompanionFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var stored struct {
		Items map[string]companionRemoteInvite `json:"items"`
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Items == nil {
		return
	}
	companionRemoteInvites.items = stored.Items
}

func persistCompanionRemoteInvitesLocked() error {
	path := companionRemoteInvitePath()
	if path == "" {
		return nil
	}
	body, err := json.Marshal(struct {
		Items map[string]companionRemoteInvite `json:"items"`
	}{Items: companionRemoteInvites.items})
	if err != nil {
		return err
	}
	return writeCompanionFile(path, body)
}

// readCompanionFile and writeCompanionFile share the group file's directory
// rules without taking the group lock.
func readCompanionFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func writeCompanionFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func companionHubBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.User != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

func companionLocalHubID() string {
	identity := companionFederationIdentity()
	if identity == nil {
		return ""
	}
	payload := identity.CurrentLoginHubPayload()
	if payload == nil {
		return ""
	}
	return strings.TrimSpace(payload.ID)
}

func companionLocalOrigin() string {
	if companionLocalOriginFn != nil {
		return companionHubBaseURL(companionLocalOriginFn())
	}
	identity := companionFederationIdentity()
	if identity == nil {
		return ""
	}
	payload := identity.CurrentLoginHubPayload()
	if payload == nil {
		return ""
	}
	return companionHubBaseURL(payload.BaseURL)
}

func companionHubIsLocal(home companionHomeHub) bool {
	if id := companionLocalHubID(); id != "" && id == strings.TrimSpace(home.HubID) {
		return true
	}
	local := companionLocalOrigin()
	base := companionHubBaseURL(home.BaseURL)
	return local != "" && base != "" && local == base
}

func companionDiscoverHomeHub(ctx context.Context, contact string) (companionHomeHub, bool) {
	if companionHomeHubLookup != nil {
		home, ok := companionHomeHubLookup(ctx, contact)
		if !ok {
			return companionHomeHub{}, false
		}
		home.BaseURL = companionHubBaseURL(home.BaseURL)
		if home.BaseURL == "" || companionHubIsLocal(home) {
			return companionHomeHub{}, false
		}
		return home, true
	}
	centerSvc := companionFederationCenter()
	if centerSvc == nil {
		return companionHomeHub{}, false
	}
	defaultID, hubs, err := centerSvc.ResolveEntryHubs(ctx, contact)
	if err != nil {
		log.Printf("[companion-group] resolve home hub: %v", err)
		return companionHomeHub{}, false
	}
	var picked center.EntryHubView
	found := false
	for _, hub := range hubs {
		if defaultID != "" && strings.TrimSpace(hub.HubID) == defaultID {
			picked = hub
			found = true
			break
		}
	}
	if !found && len(hubs) > 0 {
		picked = hubs[0]
		found = true
	}
	if !found {
		return companionHomeHub{}, false
	}
	home := companionHomeHub{HubID: strings.TrimSpace(picked.HubID), BaseURL: companionHubBaseURL(picked.BaseURL), Tenant: strings.TrimSpace(picked.TenantID)}
	if home.BaseURL == "" || companionHubIsLocal(home) {
		return companionHomeHub{}, false
	}
	return home, true
}

func companionOriginIsTrusted(ctx context.Context, origin string) bool {
	origin = companionHubBaseURL(origin)
	if origin == "" {
		return false
	}
	if companionOriginTrusted != nil {
		return companionOriginTrusted(ctx, origin)
	}
	centerSvc := companionFederationCenter()
	if centerSvc == nil {
		return true
	}
	ok, err := centerSvc.OriginTrusted(ctx, origin)
	if err != nil {
		log.Printf("[companion-group] origin trust %s: %v", origin, err)
		return false
	}
	return ok
}

func companionPostJSON(ctx context.Context, endpoint string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := companionFederationClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, nil
}

func companionGet(ctx context.Context, endpoint, secret string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if companionReaderGrantPattern.MatchString(secret) {
		req.Header.Set(companionReaderGrantHeader, secret)
	}
	resp, err := companionFederationClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, raw, nil
}

func companionRelayGroupInvite(ctx context.Context, group companionDocGroup, contact, secret, homeURL string) {
	homeURL = companionHubBaseURL(homeURL)
	origin := companionLocalOrigin()
	if homeURL == "" || origin == "" || !companionReaderGrantPattern.MatchString(secret) {
		if homeURL != "" && origin == "" {
			log.Printf("[companion-group] skip relay %s: this hub has no public address", group.ID)
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	status, raw, err := companionPostJSON(ctx, homeURL+"/api/mobile/documents/groups/remote-invites", map[string]string{
		"group_id":   group.ID,
		"name":       group.Name,
		"owner":      group.OwnerLabel,
		"contact":    contact,
		"origin_hub": origin,
		"secret":     secret,
	})
	if err != nil || status < 200 || status >= 300 {
		log.Printf("[companion-group] relay invite %s to %s: status %d err %v", group.ID, homeURL, status, err)
		if len(raw) > 0 && len(raw) < 300 {
			log.Printf("[companion-group] relay invite body: %s", raw)
		}
	}
}

func companionRelayGroupRevoke(ctx context.Context, homeURL, groupID, contact, secret string) {
	homeURL = companionHubBaseURL(homeURL)
	origin := companionLocalOrigin()
	if homeURL == "" || origin == "" || secret == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	status, _, err := companionPostJSON(ctx, homeURL+"/api/mobile/documents/groups/remote-invites/revoke", map[string]string{
		"group_id":   groupID,
		"contact":    contact,
		"origin_hub": origin,
		"secret":     secret,
	})
	if err != nil || status < 200 || status >= 300 {
		log.Printf("[companion-group] revoke relay %s at %s: status %d err %v", groupID, homeURL, status, err)
	}
}

func companionRelayMembers(ctx context.Context, members []companionDocGroupMember, groupID string) {
	for _, member := range members {
		if member.HomeHubURL == "" || member.InviteSecret == "" {
			continue
		}
		companionRelayGroupRevoke(ctx, member.HomeHubURL, groupID, member.Contact, member.InviteSecret)
	}
}

func companionSecretMatches(stored, got string) bool {
	if !companionReaderGrantPattern.MatchString(stored) || !companionReaderGrantPattern.MatchString(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(got)) == 1
}

func companionGroupMemberBySecret(groupID, contact, secret string) (companionDocGroupMember, bool) {
	if !companionGroupTokenPattern.MatchString(groupID) || contact == "" || secret == "" {
		return companionDocGroupMember{}, false
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		return companionDocGroupMember{}, false
	}
	for _, member := range group.Members {
		if member.Contact != contact {
			continue
		}
		if companionSecretMatches(member.InviteSecret, secret) {
			return member, true
		}
	}
	return companionDocGroupMember{}, false
}

// companionGroupMemberGrant reports the accepted member a group document may
// be opened for. The grant is the invite secret their hub stored at accept.
func companionGroupMemberGrant(groupID, grant string) (string, bool) {
	if !companionGroupTokenPattern.MatchString(groupID) || !companionReaderGrantPattern.MatchString(grant) {
		return "", false
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		return "", false
	}
	for _, member := range group.Members {
		if member.Status != "accepted" || !companionSecretMatches(member.InviteSecret, grant) {
			continue
		}
		label := strings.TrimSpace(member.Contact)
		if label == "" {
			label = "跨 Hub 读者"
		}
		return label, true
	}
	return "", false
}

func companionRequestSecret(r *http.Request) string {
	if r == nil {
		return ""
	}
	if grant := companionReaderGrantHeaderValue(r); grant != "" {
		return grant
	}
	secret := strings.TrimSpace(r.URL.Query().Get("secret"))
	if companionReaderGrantPattern.MatchString(secret) {
		return secret
	}
	return ""
}

// CompanionGroupInviteProofHandler confirms a relay. The secret is the only
// credential. A wrong secret is the same answer as a missing group.
//
//	GET /api/mobile/documents/groups/{id}/invite-proof
func CompanionGroupInviteProofHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		contact := normalizeCompanionContact(r.URL.Query().Get("contact"))
		member, ok := companionGroupMemberBySecret(groupID, contact, companionRequestSecret(r))
		if !ok || (member.Status != "pending" && member.Status != "accepted") {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": member.Status})
	}
}

// CompanionGroupRemoteDecisionHandler accepts, declines, ignores, or leaves
// from the invitee's hub. The invitee's hub calls it with the secret.
//
//	POST /api/mobile/documents/groups/{id}/remote-decision
func CompanionGroupRemoteDecisionHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		var body struct {
			Contact string `json:"contact"`
			Secret  string `json:"secret"`
			Action  string `json:"action"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body)
		contact := normalizeCompanionContact(body.Contact)
		secret := strings.TrimSpace(body.Secret)
		action := strings.TrimSpace(body.Action)
		switch action {
		case "accept", "decline", "ignore", "leave":
		default:
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "未知的邀请处理")
			return
		}
		if !companionGroupTokenPattern.MatchString(groupID) {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		loadCompanionDocGroups()
		companionDocGroups.Lock()
		group, ok := companionDocGroups.items[groupID]
		if !ok {
			companionDocGroups.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		index := -1
		for i, member := range group.Members {
			if member.Contact == contact && companionSecretMatches(member.InviteSecret, secret) {
				index = i
				break
			}
		}
		if index < 0 {
			companionDocGroups.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		member := group.Members[index]
		if action == "accept" && member.Status == "accepted" {
			companionDocGroups.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reader_grant": member.InviteSecret})
			return
		}
		if action == "leave" && member.Status != "accepted" {
			companionDocGroups.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		if action != "leave" && action != "accept" && member.Status != "pending" {
			companionDocGroups.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		previous := cloneCompanionGroup(group)
		noticeID := member.NotificationID
		switch action {
		case "decline", "leave":
			next := make([]companionDocGroupMember, 0, len(group.Members)-1)
			for i, item := range group.Members {
				if i == index {
					continue
				}
				next = append(next, item)
			}
			group.Members = next
		case "ignore":
			group.Members[index].Muted = true
			group.Members[index].NotificationID = ""
		default:
			group.Members[index].Status = "accepted"
			group.Members[index].JoinedAt = time.Now().UTC()
			group.Members[index].Muted = false
			group.Members[index].NotificationID = ""
		}
		companionDocGroups.items[groupID] = group
		if err := persistCompanionDocGroupsLocked(); err != nil {
			companionDocGroups.items[groupID] = previous
			companionDocGroups.Unlock()
			writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
			return
		}
		grant := ""
		if action == "accept" {
			grant = group.Members[index].InviteSecret
		}
		companionDocGroups.Unlock()
		if noticeID != "" {
			companionGroupInviteNotifierFor().revokeInvites(r.Context(), noticeID)
		}
		if action == "decline" || action == "leave" || action == "accept" {
			reconcileGroupHostSeats(r.Context(), identity, groupID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reader_grant": grant})
	}
}

// CompanionGroupRemoteSharesHandler lists the group documents an accepted
// remote member may open.
//
//	GET /api/mobile/documents/groups/{id}/remote-shares
func CompanionGroupRemoteSharesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		contact := normalizeCompanionContact(r.URL.Query().Get("contact"))
		member, ok := companionGroupMemberBySecret(groupID, contact, companionRequestSecret(r))
		if !ok || member.Status != "accepted" {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "已不在群组")
			return
		}
		loadCompanionDocShares()
		now := time.Now().UTC()
		companionDocShares.Lock()
		shares := make([]map[string]any, 0)
		for _, share := range companionDocShares.items {
			if share.Visibility != "group" || share.GroupID != groupID || share.expired(now) {
				continue
			}
			view := companionShareView(share, r)
			view["group_id"] = share.GroupID
			view["host"] = false
			companionShareAttachNotes(view, share)
			shares = append(shares, view)
		}
		companionDocShares.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
	}
}

func companionRemoteInviteMatches(invite companionRemoteInvite, ownerID string, contacts map[string]struct{}) bool {
	if ownerID != "" && invite.UserID == ownerID {
		return true
	}
	_, ok := contacts[invite.Contact]
	return ok
}

func companionRemoteInvitesFor(ownerID string, contacts map[string]struct{}) []companionRemoteInvite {
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	out := make([]companionRemoteInvite, 0)
	for _, invite := range companionRemoteInvites.items {
		if companionRemoteInviteMatches(invite, ownerID, contacts) {
			out = append(out, invite)
		}
	}
	return out
}

func companionRemoteInviteViews(ownerID string, contacts map[string]struct{}) []map[string]any {
	invites := companionRemoteInvitesFor(ownerID, contacts)
	out := make([]map[string]any, 0)
	for _, invite := range invites {
		if invite.Status != "pending" || invite.Muted {
			continue
		}
		invitedAt := ""
		if !invite.InvitedAt.IsZero() {
			invitedAt = invite.InvitedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, map[string]any{
			"id":              invite.GroupID,
			"name":            invite.Name,
			"icon":            "book",
			"owner":           invite.Owner,
			"contact":         invite.Contact,
			"invited_at":      invitedAt,
			"notification_id": invite.NotificationID,
		})
	}
	return out
}

func companionRemoteGroupViews(ownerID string, contacts map[string]struct{}) []map[string]any {
	invites := companionRemoteInvitesFor(ownerID, contacts)
	out := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, invite := range invites {
		if invite.Status != "accepted" {
			continue
		}
		if _, ok := seen[invite.GroupID]; ok {
			continue
		}
		seen[invite.GroupID] = struct{}{}
		created := invite.InvitedAt.UTC().Format(time.RFC3339)
		out = append(out, map[string]any{
			"id":    invite.GroupID,
			"name":  invite.Name,
			"icon":  "book",
			"owner": invite.Owner,
			"admin": false,
			// The group lives on another Hub. It opens the documents shared
			// inside it, but a new share cannot be published to it from here.
			"remote":     true,
			"created_at": created,
		})
	}
	return out
}

// CompanionGroupRemoteInviteHandler stores an invitation that was created on
// another hub and raises the local notice.
//
//	POST /api/mobile/documents/groups/remote-invites
func CompanionGroupRemoteInviteHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		var body struct {
			GroupID   string `json:"group_id"`
			Name      string `json:"name"`
			Owner     string `json:"owner"`
			Contact   string `json:"contact"`
			OriginHub string `json:"origin_hub"`
			Secret    string `json:"secret"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "无法读取邀请")
			return
		}
		groupID := strings.TrimSpace(body.GroupID)
		contact := normalizeCompanionContact(body.Contact)
		origin := companionHubBaseURL(body.OriginHub)
		secret := strings.TrimSpace(body.Secret)
		name, nameErr := companionGroupName(body.Name)
		if nameErr != "" || !companionGroupTokenPattern.MatchString(groupID) || contact == "" || origin == "" || !companionReaderGrantPattern.MatchString(secret) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "无法读取邀请")
			return
		}
		if !companionOriginIsTrusted(r.Context(), origin) {
			writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "无法确认分享所在的 Hub")
			return
		}
		if !companionRemoteProof(r.Context(), origin, groupID, contact, secret) {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		userID := companionContactUserIDAcrossTenants(r.Context(), identity, contact)
		if userID == "" {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到这个用户")
			return
		}
		owner := strings.TrimSpace(body.Owner)
		if owner == "" {
			owner = "一位用户"
		}
		loadCompanionRemoteInvites()
		companionRemoteInvites.Lock()
		key := companionRemoteInviteKey(origin, groupID, contact)
		invite := companionRemoteInvites.items[key]
		invite.GroupID = groupID
		invite.Name = name
		invite.Owner = owner
		invite.Contact = contact
		invite.OriginHub = origin
		invite.Secret = secret
		if invite.Status != "accepted" {
			invite.Status = "pending"
			invite.Muted = false
		}
		invite.UserID = userID
		if invite.InvitedAt.IsZero() {
			invite.InvitedAt = time.Now().UTC()
		}
		companionRemoteInvites.items[key] = invite
		if err := persistCompanionRemoteInvitesLocked(); err != nil {
			delete(companionRemoteInvites.items, key)
			companionRemoteInvites.Unlock()
			writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
			return
		}
		companionRemoteInvites.Unlock()
		companionRemoteSnapForgetAll()
		if invite.Status == "pending" {
			if notifier := companionGroupInviteNotifierFor(); notifier != nil {
				noticeID := notifier.notifyInvite(r.Context(), companionDocGroup{
					ID:         groupID,
					Name:       name,
					OwnerLabel: owner,
				}, contact, userID)
				if noticeID != "" {
					companionStoreRemoteNotice(key, noticeID)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func companionStoreRemoteNotice(key, noticeID string) {
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	invite, ok := companionRemoteInvites.items[key]
	if !ok || invite.Status != "pending" {
		return
	}
	invite.NotificationID = noticeID
	companionRemoteInvites.items[key] = invite
	if err := persistCompanionRemoteInvitesLocked(); err != nil {
		log.Printf("[companion-group] persist remote notice: %v", err)
	}
}

func companionRemoteProof(ctx context.Context, origin, groupID, contact, secret string) bool {
	q := url.Values{}
	q.Set("contact", contact)
	endpoint := origin + "/api/mobile/documents/groups/" + url.PathEscape(groupID) + "/invite-proof?" + q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	status, _, err := companionGet(ctx, endpoint, secret)
	return err == nil && status == http.StatusOK
}

// CompanionGroupRemoteRevokeHandler withdraws a relayed invitation.
//
//	POST /api/mobile/documents/groups/remote-invites/revoke
func CompanionGroupRemoteRevokeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		var body struct {
			GroupID   string `json:"group_id"`
			Contact   string `json:"contact"`
			OriginHub string `json:"origin_hub"`
			Secret    string `json:"secret"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body)
		groupID := strings.TrimSpace(body.GroupID)
		contact := normalizeCompanionContact(body.Contact)
		origin := companionHubBaseURL(body.OriginHub)
		secret := strings.TrimSpace(body.Secret)
		loadCompanionRemoteInvites()
		companionRemoteInvites.Lock()
		key := companionRemoteInviteKey(origin, groupID, contact)
		invite, ok := companionRemoteInvites.items[key]
		if !ok || !companionSecretMatches(invite.Secret, secret) {
			companionRemoteInvites.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
			return
		}
		noticeID := invite.NotificationID
		delete(companionRemoteInvites.items, key)
		if err := persistCompanionRemoteInvitesLocked(); err != nil {
			companionRemoteInvites.items[key] = invite
			companionRemoteInvites.Unlock()
			writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
			return
		}
		companionRemoteInvites.Unlock()
		companionRemoteSnapForgetAll()
		companionGroupInviteNotifierFor().revokeInvites(r.Context(), noticeID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func companionDecideRemoteInvite(ctx context.Context, w http.ResponseWriter, ownerID string, contacts map[string]struct{}, groupID, action string) bool {
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	var key string
	var invite companionRemoteInvite
	found := false
	for id, item := range companionRemoteInvites.items {
		if item.GroupID != groupID || !companionRemoteInviteMatches(item, ownerID, contacts) {
			continue
		}
		if action == "accept" && item.Status == "accepted" {
			key, invite, found = id, item, true
			break
		}
		if item.Status == "pending" && !item.Muted {
			key, invite, found = id, item, true
			break
		}
		if action == "ignore" && item.Status == "pending" {
			key, invite, found = id, item, true
			break
		}
	}
	if !found {
		companionRemoteInvites.Unlock()
		return false
	}
	if action == "accept" {
		noticeID := invite.NotificationID
		already := invite.Status == "accepted"
		// The origin call waits on another hub. It must not keep this lock.
		companionRemoteInvites.Unlock()
		if !already && !companionRemoteDecideOrigin(ctx, invite, "accept") {
			writeError(w, http.StatusBadGateway, "GROUP_FAILED", "暂时无法连接对方 Hub")
			return true
		}
		if !already {
			companionRemoteInvites.Lock()
			current, ok := companionRemoteInvites.items[key]
			if !ok || !companionSecretMatches(current.Secret, invite.Secret) {
				companionRemoteInvites.Unlock()
				writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
				return true
			}
			noticeID = current.NotificationID
			current.Status = "accepted"
			current.Muted = false
			current.NotificationID = ""
			companionRemoteInvites.items[key] = current
			if err := persistCompanionRemoteInvitesLocked(); err != nil {
				companionRemoteInvites.items[key] = current
				current.Status = "pending"
				current.NotificationID = noticeID
				companionRemoteInvites.items[key] = current
				companionRemoteInvites.Unlock()
				writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
				return true
			}
			companionRemoteInvites.Unlock()
		}
		companionRemoteSnapForgetAll()
		companionGroupInviteNotifierFor().revokeInvites(ctx, noticeID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return true
	}
	original := invite
	noticeID := invite.NotificationID
	if action == "ignore" {
		invite.Muted = true
		invite.NotificationID = ""
		companionRemoteInvites.items[key] = invite
	} else {
		delete(companionRemoteInvites.items, key)
	}
	if err := persistCompanionRemoteInvitesLocked(); err != nil {
		companionRemoteInvites.items[key] = original
		companionRemoteInvites.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return true
	}
	companionRemoteInvites.Unlock()
	companionRemoteSnapForgetAll()
	// The origin is told after the local row is settled. A missed call leaves
	// the admin able to invite again; the local bell is already quiet.
	go companionRemoteDecideOrigin(context.Background(), original, action)
	companionGroupInviteNotifierFor().revokeInvites(ctx, noticeID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func companionRemoteDecideOrigin(ctx context.Context, invite companionRemoteInvite, action string) bool {
	origin := companionHubBaseURL(invite.OriginHub)
	if origin == "" || !companionGroupTokenPattern.MatchString(invite.GroupID) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	status, _, err := companionPostJSON(ctx, origin+"/api/mobile/documents/groups/"+url.PathEscape(invite.GroupID)+"/remote-decision", map[string]string{
		"contact": invite.Contact,
		"secret":  invite.Secret,
		"action":  action,
	})
	if err != nil || status < 200 || status >= 300 {
		log.Printf("[companion-group] remote decision %s %s: status %d err %v", invite.GroupID, action, status, err)
		return false
	}
	return true
}

func companionLeaveRemoteInvite(ctx context.Context, w http.ResponseWriter, ownerID string, contacts map[string]struct{}, groupID string) bool {
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	var key string
	var invite companionRemoteInvite
	found := false
	for id, item := range companionRemoteInvites.items {
		if item.GroupID != groupID || item.Status != "accepted" || !companionRemoteInviteMatches(item, ownerID, contacts) {
			continue
		}
		key, invite, found = id, item, true
		break
	}
	if !found {
		companionRemoteInvites.Unlock()
		return false
	}
	delete(companionRemoteInvites.items, key)
	if err := persistCompanionRemoteInvitesLocked(); err != nil {
		companionRemoteInvites.items[key] = invite
		companionRemoteInvites.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return true
	}
	companionRemoteInvites.Unlock()
	companionRemoteSnapForgetAll()
	go companionRemoteDecideOrigin(context.Background(), invite, "leave")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func companionRemoteSnapForgetAll() {
	companionRemoteSnapCache.Lock()
	companionRemoteSnapCache.gen++
	companionRemoteSnapCache.items = map[string]companionRemoteSnapshot{}
	companionRemoteSnapCache.Unlock()
}

func companionRemoteShareSnapshot(ctx context.Context, identity *auth.IdentityService, principal *auth.ViewerPrincipal, ownerID string) ([]map[string]any, uint64) {
	if identity == nil || principal == nil || ownerID == "" {
		return nil, 0
	}
	contacts := companionViewerContactSet(identity, ctx, principal)
	companionRemoteSnapCache.Lock()
	gen := companionRemoteSnapCache.gen
	cached, cachedOK := companionRemoteSnapCache.items[ownerID]
	if cachedOK && cached.gen == gen && time.Since(cached.at) < companionRemoteSnapTTL {
		shares := cached.shares
		stamp := cached.stamp
		companionRemoteSnapCache.Unlock()
		return shares, stamp
	}
	// A stale snapshot is enough for this response. Rebuilding it waits on
	// the other hubs, and this handler is also what tells a participant that
	// a home chat moved. Doing that wait on the request held the home
	// transcript behind every cross-hub round trip. The next poll sees the
	// refresh.
	if cachedOK && cached.gen == gen {
		shares := cached.shares
		stamp := cached.stamp
		start := false
		if !companionRemoteSnapCache.refreshing[ownerID] {
			companionRemoteSnapCache.refreshing[ownerID] = true
			start = true
		}
		companionRemoteSnapCache.Unlock()
		if start {
			copied := *principal
			go companionRemoteShareSnapshotRefresh(identity, &copied, ownerID)
		}
		return shares, stamp
	}
	companionRemoteSnapCache.Unlock()

	return companionRemoteShareSnapshotFetch(ctx, ownerID, contacts, gen)
}

func companionRemoteShareSnapshotRefresh(identity *auth.IdentityService, principal *auth.ViewerPrincipal, ownerID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[companion-group] remote snapshot refresh %s: %v", ownerID, recovered)
		}
		companionRemoteSnapCache.Lock()
		delete(companionRemoteSnapCache.refreshing, ownerID)
		companionRemoteSnapCache.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	contacts := companionViewerContactSet(identity, ctx, principal)
	companionRemoteSnapCache.Lock()
	gen := companionRemoteSnapCache.gen
	companionRemoteSnapCache.Unlock()
	companionRemoteShareSnapshotFetch(ctx, ownerID, contacts, gen)
}

func companionRemoteShareSnapshotFetch(ctx context.Context, ownerID string, contacts map[string]struct{}, gen uint64) ([]map[string]any, uint64) {
	invites := companionRemoteInvitesFor(ownerID, contacts)
	// Invites and the documents inside a group are stored in maps. Ranging
	// those maps makes this stamp a different number on every cache miss
	// even when nothing changed, and the desktop then refetches the full
	// share list on every poll.
	sort.Slice(invites, func(i, j int) bool {
		if invites[i].OriginHub != invites[j].OriginHub {
			return invites[i].OriginHub < invites[j].OriginHub
		}
		if invites[i].GroupID != invites[j].GroupID {
			return invites[i].GroupID < invites[j].GroupID
		}
		if invites[i].Contact != invites[j].Contact {
			return invites[i].Contact < invites[j].Contact
		}
		return invites[i].Status < invites[j].Status
	})
	stampHash := fnv.New64a()
	type job struct {
		invite companionRemoteInvite
		shares []map[string]any
		ok     bool
	}
	jobs := make([]job, 0, len(invites))
	for _, invite := range invites {
		fmt.Fprintf(stampHash, "%s|%s|%s|%t\n", invite.OriginHub, invite.GroupID, invite.Status, invite.Muted)
		if invite.Status != "accepted" {
			continue
		}
		jobs = append(jobs, job{invite: invite})
	}
	if len(jobs) > 4 {
		jobs = jobs[:4]
	}
	failed := false
	if len(jobs) > 0 {
		fetchCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		var wg sync.WaitGroup
		for i := range jobs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				jobs[i].shares, jobs[i].ok = companionFetchRemoteShares(fetchCtx, jobs[i].invite)
			}(i)
		}
		wg.Wait()
		cancel()
		for _, job := range jobs {
			if !job.ok {
				failed = true
				break
			}
		}
	}
	// A timed-out origin lists zero documents. Caching that stamp makes the
	// next successful read look like a change, and the desktop refetches the
	// full share list on every poll for as long as the origin blips.
	if failed {
		companionRemoteSnapCache.Lock()
		if cached, ok := companionRemoteSnapCache.items[ownerID]; ok && cached.gen == gen {
			cached.at = time.Now()
			companionRemoteSnapCache.items[ownerID] = cached
			companionRemoteSnapCache.Unlock()
			return cached.shares, cached.stamp
		}
		companionRemoteSnapCache.Unlock()
	}
	shares := make([]map[string]any, 0)
	for _, job := range jobs {
		ordered := append([]map[string]any(nil), job.shares...)
		sort.Slice(ordered, func(i, j int) bool {
			left, _ := ordered[i]["token"].(string)
			right, _ := ordered[j]["token"].(string)
			return left < right
		})
		for _, share := range ordered {
			// chat_revision belongs in the stamp: a host line on a share that
			// lives on another Hub moves only that counter, and the home Hub's
			// remote_seq is the sole signal the participant's quiet poll sees.
			fmt.Fprintf(stampHash, "%v|%v|%v|%v|%v\n", share["token"], share["revision"], share["notes_revision"], share["marks_revision"], share["chat_revision"])
			shares = append(shares, share)
		}
	}
	stamp := stampHash.Sum64()
	companionRemoteSnapCache.Lock()
	if companionRemoteSnapCache.gen == gen {
		if len(companionRemoteSnapCache.items) > 256 {
			companionRemoteSnapCache.items = map[string]companionRemoteSnapshot{}
		}
		companionRemoteSnapCache.items[ownerID] = companionRemoteSnapshot{gen: gen, at: time.Now(), shares: shares, stamp: stamp}
	}
	companionRemoteSnapCache.Unlock()
	return shares, stamp
}

func companionFetchRemoteShares(ctx context.Context, invite companionRemoteInvite) ([]map[string]any, bool) {
	origin := companionHubBaseURL(invite.OriginHub)
	if origin == "" || !companionReaderGrantPattern.MatchString(invite.Secret) {
		return nil, true
	}
	q := url.Values{}
	q.Set("contact", invite.Contact)
	endpoint := origin + "/api/mobile/documents/groups/" + url.PathEscape(invite.GroupID) + "/remote-shares?" + q.Encode()
	status, raw, err := companionGet(ctx, endpoint, invite.Secret)
	if err != nil || status != http.StatusOK {
		return nil, false
	}
	var payload struct {
		Shares []json.RawMessage `json:"shares"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	out := make([]map[string]any, 0, len(payload.Shares))
	for _, item := range payload.Shares {
		var view map[string]any
		if json.Unmarshal(item, &view) != nil {
			continue
		}
		token, _ := view["token"].(string)
		if !companionShareTokenPattern.MatchString(strings.TrimSpace(token)) {
			continue
		}
		view["hub_origin"] = origin
		view["reader_grant"] = invite.Secret
		view["visibility"] = "group"
		out = append(out, view)
	}
	return out, true
}
