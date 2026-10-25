package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Companion reading groups are separate from document shares and from the
// admin notification store. The creator is the only admin. An invited person
// stays pending until they accept, and only accepted members can see a group
// share. Removing a member, leaving, or dissolving the group drops that
// access immediately. Dissolving deletes the group record and leaves the
// owner's papers in place. The icon is a preset id, not an uploaded image.

const companionDocGroupPathEnv = "MACLAW_COMPANION_DOC_GROUP_PATH"

var companionGroupTokenPattern = regexp.MustCompile(`^cg_[0-9a-f]{32}$`)

type companionDocGroupMember struct {
	Contact   string    `json:"contact"`
	Status    string    `json:"status"`
	UserID    string    `json:"user_id,omitempty"`
	InvitedAt time.Time `json:"invited_at"`
	JoinedAt  time.Time `json:"joined_at,omitempty"`
}

type companionDocGroup struct {
	ID         string                    `json:"id"`
	TenantID   string                    `json:"tenant_id"`
	OwnerID    string                    `json:"owner_id"`
	OwnerLabel string                    `json:"owner_label"`
	Name       string                    `json:"name"`
	Icon       string                    `json:"icon,omitempty"`
	CreatedAt  time.Time                 `json:"created_at"`
	Members    []companionDocGroupMember `json:"members,omitempty"`
}

var companionDocGroups = struct {
	sync.Mutex
	loaded bool
	items  map[string]companionDocGroup
}{items: map[string]companionDocGroup{}}

func companionDocGroupStatePath() string {
	if p := strings.TrimSpace(os.Getenv(companionDocGroupPathEnv)); p != "" {
		return p
	}
	state := mobileStatePath()
	if state == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(state), "companion-doc-groups.json")
}

func loadCompanionDocGroups() {
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	if companionDocGroups.loaded {
		return
	}
	companionDocGroups.loaded = true
	if companionDocGroups.items == nil {
		companionDocGroups.items = map[string]companionDocGroup{}
	}
	path := companionDocGroupStatePath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored struct {
		Items map[string]companionDocGroup `json:"items"`
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Items == nil {
		return
	}
	companionDocGroups.items = stored.Items
}

func persistCompanionDocGroupsLocked() error {
	path := companionDocGroupStatePath()
	if path == "" {
		return nil
	}
	body, err := json.Marshal(struct {
		Items map[string]companionDocGroup `json:"items"`
	}{Items: companionDocGroups.items})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func resetCompanionDocGroups() {
	companionDocGroups.Lock()
	companionDocGroups.loaded = false
	companionDocGroups.items = map[string]companionDocGroup{}
	companionDocGroups.Unlock()
}

func newCompanionGroupID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cg_" + hex.EncodeToString(buf), nil
}

func normalizeCompanionContact(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		if strings.ContainsAny(value, " \t") || strings.Count(value, "@") != 1 {
			return ""
		}
		local, domain, _ := strings.Cut(value, "@")
		if local == "" || !strings.Contains(domain, ".") || len(value) > 200 {
			return ""
		}
		return value
	}
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	phone := digits.String()
	if len(phone) < 6 || len(phone) > 15 {
		return ""
	}
	return phone
}

func cloneCompanionGroup(group companionDocGroup) companionDocGroup {
	if group.Members != nil {
		group.Members = append([]companionDocGroupMember(nil), group.Members...)
	}
	return group
}

var companionGroupIcons = map[string]struct{}{
	"book": {}, "people": {}, "star": {}, "pen": {}, "leaf": {}, "flask": {},
}

func companionGroupIcon(icon string) string {
	icon = strings.TrimSpace(icon)
	if _, ok := companionGroupIcons[icon]; ok {
		return icon
	}
	return "book"
}

func companionGroupName(name string) (string, string) {
	name = strings.TrimSpace(name)
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	runes := []rune(cleaned)
	if len(runes) < 1 {
		return "", "请填写群组名称"
	}
	if len(runes) > 40 {
		return "", "群组名称过长"
	}
	return cleaned, ""
}

func companionViewerContactSet(identity *auth.IdentityService, ctx context.Context, principal *auth.ViewerPrincipal) map[string]struct{} {
	set := map[string]struct{}{}
	if principal == nil {
		return set
	}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		set[value] = struct{}{}
		set[strings.ToLower(value)] = struct{}{}
	}
	add(principal.Email)
	add(principal.UserID)
	if identity == nil {
		return set
	}
	for _, contact := range identity.ViewerContacts(ctx, principal) {
		add(contact)
	}
	return set
}

func companionGroupIsAdmin(ownerID, groupID string) bool {
	if !companionGroupTokenPattern.MatchString(groupID) || ownerID == "" {
		return false
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	group, ok := companionDocGroups.items[groupID]
	return ok && group.OwnerID == ownerID
}

func companionMemberIsCaller(member companionDocGroupMember, ownerID string, contacts map[string]struct{}) bool {
	if member.Status != "accepted" {
		return false
	}
	if member.UserID != "" && member.UserID == ownerID {
		return true
	}
	return member.Contact != "" && containsContact(contacts, member.Contact)
}

func companionAcceptedGroupIDs(ownerID string, contacts map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	if ownerID == "" {
		return out
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	for id, group := range companionDocGroups.items {
		for _, member := range group.Members {
			if companionMemberIsCaller(member, ownerID, contacts) {
				out[id] = struct{}{}
				break
			}
		}
	}
	return out
}

func companionShareReadAccess(share companionDocShare, ownerID string, contacts, groups map[string]struct{}, password string) (int, string) {
	if share.OwnerID == ownerID {
		return 0, ""
	}
	switch share.Visibility {
	case "group":
		if _, ok := groups[share.GroupID]; ok {
			return 0, ""
		}
		return http.StatusForbidden, "已不在群组"
	case "person":
		if share.Target != "" {
			if _, ok := contacts[share.Target]; ok {
				return 0, ""
			}
		}
		return http.StatusForbidden, "这不是分享给你的文档"
	case "private":
		if _, ok := share.Importers[ownerID]; ok {
			return 0, ""
		}
		if strings.TrimSpace(password) == "" {
			return http.StatusUnauthorized, "需要分享密码"
		}
		if !companionSharePasswordOK(share, password) {
			return http.StatusForbidden, "分享密码不正确"
		}
		return 0, ""
	default:
		return 0, ""
	}
}

func companionShareReceived(share companionDocShare, ownerID string, contacts, groups map[string]struct{}) bool {
	switch share.Visibility {
	case "group":
		_, ok := groups[share.GroupID]
		return ok
	case "person":
		if share.Target == "" {
			return false
		}
		_, ok := contacts[share.Target]
		return ok
	default:
		if share.Importers == nil {
			return false
		}
		_, ok := share.Importers[ownerID]
		return ok
	}
}

func companionGroupView(group companionDocGroup, ownerID string) map[string]any {
	admin := group.OwnerID == ownerID
	view := map[string]any{
		"id":         group.ID,
		"name":       group.Name,
		"icon":       companionGroupIcon(group.Icon),
		"owner":      group.OwnerLabel,
		"admin":      admin,
		"created_at": group.CreatedAt.UTC().Format(time.RFC3339),
	}
	if admin {
		members := make([]map[string]any, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, map[string]any{
				"contact": member.Contact,
				"status":  member.Status,
			})
		}
		view["members"] = members
	}
	return view
}

// latestPendingCompanionInvite is the viewer's newest pending row in this group.
// One person can be pending under more than one contact. The bell shows one row.
func latestPendingCompanionInvite(group companionDocGroup, contacts map[string]struct{}) (companionDocGroupMember, bool) {
	var best companionDocGroupMember
	found := false
	for _, member := range group.Members {
		if member.Status != "pending" || member.Contact == "" {
			continue
		}
		if _, ok := contacts[member.Contact]; !ok {
			continue
		}
		if !found || member.InvitedAt.After(best.InvitedAt) {
			best = member
			found = true
		}
	}
	return best, found
}

func companionGroupInviteView(group companionDocGroup, contact string) map[string]any {
	invitedAt := ""
	for _, member := range group.Members {
		if member.Contact != contact {
			continue
		}
		if !member.InvitedAt.IsZero() {
			invitedAt = member.InvitedAt.UTC().Format(time.RFC3339)
		}
		break
	}
	return map[string]any{
		"id":         group.ID,
		"name":       group.Name,
		"icon":       companionGroupIcon(group.Icon),
		"owner":      group.OwnerLabel,
		"contact":    contact,
		"invited_at": invitedAt,
	}
}

func companionRequireViewer(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, string, bool) {
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
		return nil, "", false
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录 MaClaw Hub")
		return nil, "", false
	}
	return principal, ownerID, true
}

// CompanionDocumentGroupsHandler lists groups and creates one.
//
//	GET  /api/mobile/documents/groups
//	POST /api/mobile/documents/groups
func CompanionDocumentGroupsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeCompanionGroupList(w, ownerID, companionViewerContactSet(identity, r.Context(), principal))
		case http.MethodPost:
			createCompanionGroup(w, r, principal, ownerID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		}
	}
}

func writeCompanionGroupList(w http.ResponseWriter, ownerID string, contacts map[string]struct{}) {
	loadCompanionDocGroups()
	groups := make([]map[string]any, 0)
	companionDocGroups.Lock()
	for _, group := range companionDocGroups.items {
		if group.OwnerID == ownerID {
			groups = append(groups, companionGroupView(group, ownerID))
			continue
		}
		for _, member := range group.Members {
			if companionMemberIsCaller(member, ownerID, contacts) {
				groups = append(groups, companionGroupView(group, ownerID))
				break
			}
		}
	}
	companionDocGroups.Unlock()
	// Map iteration order changes every reload. Keep the cards still.
	sort.Slice(groups, func(i, j int) bool {
		leftCreated, _ := groups[i]["created_at"].(string)
		rightCreated, _ := groups[j]["created_at"].(string)
		if leftCreated != rightCreated {
			return leftCreated < rightCreated
		}
		leftID, _ := groups[i]["id"].(string)
		rightID, _ := groups[j]["id"].(string)
		return leftID < rightID
	})
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func containsContact(contacts map[string]struct{}, contact string) bool {
	_, ok := contacts[contact]
	return ok
}

func createCompanionGroup(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, ownerID string) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写群组名称")
		return
	}
	name, nameErr := companionGroupName(body.Name)
	if nameErr != "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", nameErr)
		return
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	owned := 0
	for _, group := range companionDocGroups.items {
		if group.OwnerID == ownerID {
			owned++
		}
	}
	if owned >= 50 {
		companionDocGroups.Unlock()
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "群组数量已达上限")
		return
	}
	id, err := newCompanionGroupID()
	if err != nil {
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法创建群组")
		return
	}
	group := companionDocGroup{
		ID:         id,
		TenantID:   strings.TrimSpace(principal.TenantID),
		OwnerID:    ownerID,
		OwnerLabel: companionShareOwnerLabel(principal),
		Name:       name,
		CreatedAt:  time.Now().UTC(),
		Members:    []companionDocGroupMember{},
	}
	companionDocGroups.items[id] = group
	err = persistCompanionDocGroupsLocked()
	if err != nil {
		delete(companionDocGroups.items, id)
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, companionGroupView(group, ownerID))
}

// CompanionDocumentGroupMembersHandler adds or removes a member.
// The creator is the admin. A new member stays pending until they accept.
//
//	POST   /api/mobile/documents/groups/{id}/members
//	DELETE /api/mobile/documents/groups/{id}/members
func CompanionDocumentGroupMembersHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		if !companionGroupTokenPattern.MatchString(groupID) {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
			return
		}
		var body struct {
			Contact string `json:"contact"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body)
		contact := normalizeCompanionContact(body.Contact)
		if contact == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写要邀请的邮箱或手机号")
			return
		}
		switch r.Method {
		case http.MethodPost:
			addCompanionGroupMember(w, groupID, ownerID, contact)
		case http.MethodDelete:
			removeCompanionGroupMember(w, groupID, ownerID, contact)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST or DELETE")
		}
	}
}

func addCompanionGroupMember(w http.ResponseWriter, groupID, ownerID, contact string) {
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
		return
	}
	if group.OwnerID != ownerID {
		companionDocGroups.Unlock()
		writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "只有群组管理员可以邀请成员")
		return
	}
	for _, member := range group.Members {
		if member.Contact == contact {
			companionDocGroups.Unlock()
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "这位用户已经在群组里")
			return
		}
	}
	if len(group.Members) >= 200 {
		companionDocGroups.Unlock()
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "群组成员已达上限")
		return
	}
	previous := cloneCompanionGroup(group)
	group.Members = append(group.Members, companionDocGroupMember{
		Contact:   contact,
		Status:    "pending",
		InvitedAt: time.Now().UTC(),
	})
	companionDocGroups.items[groupID] = group
	err := persistCompanionDocGroupsLocked()
	if err != nil {
		companionDocGroups.items[groupID] = previous
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, companionGroupView(group, ownerID))
}

func removeCompanionGroupMember(w http.ResponseWriter, groupID, ownerID, contact string) {
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
		return
	}
	if group.OwnerID != ownerID {
		companionDocGroups.Unlock()
		writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "只有群组管理员可以移除成员")
		return
	}
	next := make([]companionDocGroupMember, 0, len(group.Members))
	found := false
	for _, member := range group.Members {
		if member.Contact == contact {
			found = true
			continue
		}
		next = append(next, member)
	}
	if !found {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组里没有这位用户")
		return
	}
	previous := cloneCompanionGroup(group)
	group.Members = next
	companionDocGroups.items[groupID] = group
	err := persistCompanionDocGroupsLocked()
	if err != nil {
		companionDocGroups.items[groupID] = previous
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, companionGroupView(group, ownerID))
}

// CompanionDocumentGroupHandler renames a group or dissolves it.
// Only the creator may change the name and icon, or delete the group.
// Deleting the group does not delete the owner's papers.
//
//	PATCH  /api/mobile/documents/groups/{id}
//	DELETE /api/mobile/documents/groups/{id}
func CompanionDocumentGroupHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		if !companionGroupTokenPattern.MatchString(groupID) {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
			return
		}
		switch r.Method {
		case http.MethodPatch:
			updateCompanionGroup(w, r, groupID, ownerID)
		case http.MethodDelete:
			deleteCompanionGroup(w, groupID, ownerID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH or DELETE")
		}
	}
}

func updateCompanionGroup(w http.ResponseWriter, r *http.Request, groupID, ownerID string) {
	var body struct {
		Name string `json:"name"`
		Icon string `json:"icon"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写群组名称")
		return
	}
	name, nameErr := companionGroupName(body.Name)
	if nameErr != "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", nameErr)
		return
	}
	icon := strings.TrimSpace(body.Icon)
	if _, allowed := companionGroupIcons[icon]; !allowed {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请选择群组图标")
		return
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, found := companionDocGroups.items[groupID]
	if !found {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
		return
	}
	if group.OwnerID != ownerID {
		companionDocGroups.Unlock()
		writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "只有群组管理员可以修改群组")
		return
	}
	previous := cloneCompanionGroup(group)
	group.Name = name
	group.Icon = icon
	companionDocGroups.items[groupID] = group
	if err := persistCompanionDocGroupsLocked(); err != nil {
		companionDocGroups.items[groupID] = previous
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, companionGroupView(group, ownerID))
}

func deleteCompanionGroup(w http.ResponseWriter, groupID, ownerID string) {
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, found := companionDocGroups.items[groupID]
	if !found {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
		return
	}
	if group.OwnerID != ownerID {
		companionDocGroups.Unlock()
		writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "只有群组管理员可以解散群组")
		return
	}
	previous := cloneCompanionGroup(group)
	delete(companionDocGroups.items, groupID)
	if err := persistCompanionDocGroupsLocked(); err != nil {
		companionDocGroups.items[groupID] = previous
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// CompanionDocumentGroupLeaveHandler removes the caller from a group.
// The creator dissolves the group instead of leaving. One person can have
// an accepted email row and an accepted phone row; leaving drops every
// accepted row that belongs to them. A pending invite stays until they
// accept or decline it.
//
//	POST /api/mobile/documents/groups/{id}/leave
func CompanionDocumentGroupLeaveHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		groupID := strings.TrimSpace(r.PathValue("id"))
		if !companionGroupTokenPattern.MatchString(groupID) {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
			return
		}
		contacts := companionViewerContactSet(identity, r.Context(), principal)
		loadCompanionDocGroups()
		companionDocGroups.Lock()
		group, found := companionDocGroups.items[groupID]
		if !found {
			companionDocGroups.Unlock()
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "群组不存在")
			return
		}
		if group.OwnerID == ownerID {
			companionDocGroups.Unlock()
			writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "管理员请解散群组")
			return
		}
		next := make([]companionDocGroupMember, 0, len(group.Members))
		removed := false
		for _, member := range group.Members {
			if companionMemberIsCaller(member, ownerID, contacts) {
				removed = true
				continue
			}
			next = append(next, member)
		}
		if !removed {
			companionDocGroups.Unlock()
			writeError(w, http.StatusForbidden, "GROUP_FORBIDDEN", "你不在这个群组")
			return
		}
		previous := cloneCompanionGroup(group)
		group.Members = next
		companionDocGroups.items[groupID] = group
		if err := persistCompanionDocGroupsLocked(); err != nil {
			companionDocGroups.items[groupID] = previous
			companionDocGroups.Unlock()
			writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
			return
		}
		companionDocGroups.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// CompanionDocumentGroupInvitesHandler lists invitations waiting for a yes.
//
//	GET /api/mobile/documents/groups/invites
func CompanionDocumentGroupInvitesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		contacts := companionViewerContactSet(identity, r.Context(), principal)
		loadCompanionDocGroups()
		invites := make([]map[string]any, 0)
		companionDocGroups.Lock()
		for _, group := range companionDocGroups.items {
			if group.OwnerID == ownerID {
				continue
			}
			member, ok := latestPendingCompanionInvite(group, contacts)
			if !ok {
				continue
			}
			invites = append(invites, companionGroupInviteView(group, member.Contact))
		}
		companionDocGroups.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
	}
}

// CompanionDocumentGroupAcceptHandler confirms an invitation.
//
//	POST /api/mobile/documents/groups/{id}/accept
func CompanionDocumentGroupAcceptHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		decideCompanionGroupInvite(w, r, identity, principal, ownerID, true)
	}
}

// CompanionDocumentGroupDeclineHandler dismisses an invitation.
//
//	POST /api/mobile/documents/groups/{id}/decline
func CompanionDocumentGroupDeclineHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ownerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		decideCompanionGroupInvite(w, r, identity, principal, ownerID, false)
	}
}

func decideCompanionGroupInvite(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, principal *auth.ViewerPrincipal, ownerID string, accept bool) {
	groupID := strings.TrimSpace(r.PathValue("id"))
	if !companionGroupTokenPattern.MatchString(groupID) {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
		return
	}
	contacts := companionViewerContactSet(identity, r.Context(), principal)
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
		return
	}
	// One notification click settles every pending contact for this person.
	// Leaving a second address pending would put the same group back in the bell.
	now := time.Now().UTC()
	kept := make([]companionDocGroupMember, 0, len(group.Members))
	found := false
	for _, member := range group.Members {
		if member.Status == "pending" && member.Contact != "" {
			if _, isMine := contacts[member.Contact]; isMine {
				found = true
				if !accept {
					continue
				}
				member.Status = "accepted"
				member.UserID = ownerID
				member.JoinedAt = now
			}
		}
		kept = append(kept, member)
	}
	if !found {
		companionDocGroups.Unlock()
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "没有找到入群邀请")
		return
	}
	previous := cloneCompanionGroup(group)
	group.Members = kept
	companionDocGroups.items[groupID] = group
	err := persistCompanionDocGroupsLocked()
	if err != nil {
		companionDocGroups.items[groupID] = previous
		companionDocGroups.Unlock()
		writeError(w, http.StatusInternalServerError, "GROUP_FAILED", "无法保存群组")
		return
	}
	companionDocGroups.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
