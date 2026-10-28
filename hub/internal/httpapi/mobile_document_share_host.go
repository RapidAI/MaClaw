package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

// The admin (the share owner) may hand the host seat to another participant.
// The nominee must accept before the seat moves. The owner keeps admin rights
// forever: they can nominate someone else, revoke the current host, or delete
// the share. Every participant receives a notice about the request and result.

var companionHostNotifyMu sync.Mutex

func companionShareParticipantUserIDs(ctx context.Context, identity *auth.IdentityService, share companionDocShare) []string {
	if identity == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(userID string) {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return
		}
		if _, ok := seen[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
		out = append(out, userID)
	}
	add(share.OwnerID)
	add(companionShareHostID(share))
	add(share.PendingHostID)
	for userID := range share.Importers {
		add(userID)
	}
	switch share.Visibility {
	case "group":
		for userID := range companionGroupAcceptedUserIDs(ctx, identity, share.GroupID) {
			add(userID)
		}
	case "person":
		add(companionContactUserID(ctx, identity, share.TenantID, share.Target))
	}
	return out
}

// companionShareNomineeAccess reports whether userID is a participant and
// whether they can still open the share. Group membership is resolved once.
func companionShareNomineeAccess(ctx context.Context, identity *auth.IdentityService, share companionDocShare, userID string) (participant, open bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, false
	}
	ownerID := strings.TrimSpace(share.OwnerID)
	hostID := companionShareHostID(share)
	pendingID := strings.TrimSpace(share.PendingHostID)
	_, imported := share.Importers[userID]
	switch share.Visibility {
	case "public":
		return userID == ownerID || userID == hostID || userID == pendingID || imported, true
	case "private":
		open = userID == ownerID || imported
		return open || userID == hostID || userID == pendingID, open
	case "person":
		targetID := companionContactUserID(ctx, identity, share.TenantID, share.Target)
		open = userID == ownerID || (targetID != "" && userID == targetID)
		return open || imported || userID == hostID || userID == pendingID, open
	case "group":
		_, member := companionGroupAcceptedUserIDs(ctx, identity, share.GroupID)[userID]
		open = userID == ownerID || member
		return open || imported || userID == hostID || userID == pendingID, open
	default:
		return userID == ownerID || userID == hostID || userID == pendingID || imported, userID == ownerID
	}
}

func companionShareUserIsParticipant(ctx context.Context, identity *auth.IdentityService, share companionDocShare, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, id := range companionShareParticipantUserIDs(ctx, identity, share) {
		if id == userID {
			return true
		}
	}
	return false
}

func companionShareHostView(share companionDocShare, r *http.Request) map[string]any {
	view := companionShareView(share, r)
	if strings.TrimSpace(share.PendingHostID) != "" {
		view["pending_host"] = true
		view["pending_host_label"] = strings.TrimSpace(share.PendingHostLabel)
	}
	return view
}

// CompanionDocumentShareHostHandler nominates a participant as the next host
// (POST) or lets the admin cancel a pending nomination / revoke the current
// host (DELETE).
//
//	POST   /api/mobile/documents/shares/{token}/host
//	DELETE /api/mobile/documents/shares/{token}/host?action=cancel|revoke
//
// action=cancel only withdraws a nomination that is still waiting. action=revoke
// only takes the seat back, and refuses while a nomination is waiting, so a
// stale cancel cannot unseat a host who just accepted. An empty action keeps
// the older combined behavior.
func CompanionDocumentShareHostHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST or DELETE")
			return
		}
		_, viewerID, ok := companionRequireViewer(w, r, identity)
		if !ok {
			return
		}
		token := strings.TrimSpace(r.PathValue("token"))
		if !companionShareTokenPattern.MatchString(token) {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		var contact string
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			var body struct {
				Contact string `json:"contact"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写参与者邮箱或手机号")
				return
			}
			contact = strings.TrimSpace(body.Contact)
			if contact == "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "请填写参与者邮箱或手机号")
				return
			}
		}

		loadCompanionDocShares()
		companionDocShares.Lock()
		snapshot, found := companionDocShares.items[token]
		companionDocShares.Unlock()
		if !found {
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if snapshot.expired(time.Now().UTC()) {
			writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
			return
		}
		if !companionShareIsAdmin(snapshot, viewerID) {
			writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有分享者管理员可以管理主持人")
			return
		}

		if r.Method == http.MethodPost {
			nomineeID := companionContactUserID(r.Context(), identity, snapshot.TenantID, contact)
			if nomineeID == "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "该联系人还不是 Hub 用户")
				return
			}
			if nomineeID == strings.TrimSpace(snapshot.OwnerID) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "你已经是管理员")
				return
			}
			if nomineeID == companionShareHostID(snapshot) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "对方已经是主持人")
				return
			}
			participant, canOpen := companionShareNomineeAccess(r.Context(), identity, snapshot, nomineeID)
			if !participant {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "只能指定这份文档的参与者")
				return
			}
			// Importers stay on the share after they leave a group. Being on that
			// list is not enough: the nominee has to be able to open it.
			if !canOpen {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "对方已不能打开这份文档")
				return
			}
			now := time.Now().UTC()
			companionDocShares.Lock()
			share, ok := companionDocShares.items[token]
			if !ok {
				companionDocShares.Unlock()
				writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
				return
			}
			if share.expired(now) {
				companionDocShares.Unlock()
				writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
				return
			}
			if !companionShareIsAdmin(share, viewerID) {
				companionDocShares.Unlock()
				writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有分享者管理员可以管理主持人")
				return
			}
			if nomineeID == companionShareHostID(share) {
				companionDocShares.Unlock()
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "对方已经是主持人")
				return
			}
			if share.Visibility != snapshot.Visibility || share.Target != snapshot.Target || share.GroupID != snapshot.GroupID {
				companionDocShares.Unlock()
				writeError(w, http.StatusConflict, "SHARE_CONFLICT", "分享已更新，请重试")
				return
			}
			previous := share
			share.PendingHostID = nomineeID
			share.PendingHostLabel = contact
			share.PendingHostSince = now
			companionDocShares.items[token] = share
			if err := persistCompanionDocSharesLocked(); err != nil {
				companionDocShares.items[token] = previous
				companionDocShares.Unlock()
				writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存主持人转移")
				return
			}
			companionDocShares.Unlock()
			// Group membership can end after the check above and before this
			// write. Other audiences only change with the share fields already
			// re-checked under the lock. Drop the nomination instead of leaving
			// an invite that can only fail.
			if share.Visibility == "group" && !companionShareUserCanOpen(r.Context(), identity, share, nomineeID) {
				if _, clearedOK := clearPendingCompanionHost(token, nomineeID); clearedOK {
					writeError(w, http.StatusBadRequest, "BAD_REQUEST", "对方已不能打开这份文档")
					return
				}
				writeError(w, http.StatusConflict, "SHARE_CONFLICT", "分享已更新，请重试")
				return
			}
			go notifyCompanionHostTransferRequest(share, nomineeID, contact, share.PendingHostSince)
			writeJSON(w, http.StatusOK, companionShareHostView(share, r))
			return
		}

		action := strings.TrimSpace(r.URL.Query().Get("action"))
		switch action {
		case "", "cancel", "revoke":
		default:
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "无法管理主持人")
			return
		}

		// DELETE: cancel withdraws a nomination; revoke returns the seat to the
		// admin. The two are separate so a cancel that loses the race with
		// accept does not take the seat back.
		companionDocShares.Lock()
		share, ok := companionDocShares.items[token]
		if !ok {
			companionDocShares.Unlock()
			writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
			return
		}
		if share.expired(time.Now().UTC()) {
			companionDocShares.Unlock()
			writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
			return
		}
		if !companionShareIsAdmin(share, viewerID) {
			companionDocShares.Unlock()
			writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "只有分享者管理员可以管理主持人")
			return
		}
		previous := share
		adminLabel := companionShareAdminLabel(share)
		docName := strings.TrimSpace(share.Name)
		pendingID := strings.TrimSpace(share.PendingHostID)
		hostID := strings.TrimSpace(share.HostID)
		ownerID := strings.TrimSpace(share.OwnerID)
		var content string
		var extra string
		switch {
		case action == "cancel" || (action == "" && pendingID != ""):
			if pendingID == "" {
				companionDocShares.Unlock()
				writeError(w, http.StatusConflict, "SHARE_CONFLICT", "这次主持人转移已经结束")
				return
			}
			share.PendingHostID = ""
			share.PendingHostLabel = ""
			share.PendingHostSince = time.Time{}
			content = fmt.Sprintf("%s 已取消文档《%s》的主持人转移。", adminLabel, docName)
			extra = pendingID
		case action == "revoke" || action == "":
			if action == "revoke" && pendingID != "" {
				companionDocShares.Unlock()
				writeError(w, http.StatusConflict, "SHARE_CONFLICT", "请先取消尚未接受的提名")
				return
			}
			if hostID == "" || hostID == ownerID {
				companionDocShares.Unlock()
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "当前没有可收回的主持人")
				return
			}
			share.HostID = ""
			share.HostLabel = ""
			content = fmt.Sprintf("%s 已收回《%s》的主持权，管理员重新担任主持人。", adminLabel, docName)
			extra = hostID
		}
		companionDocShares.items[token] = share
		if err := persistCompanionDocSharesLocked(); err != nil {
			companionDocShares.items[token] = previous
			companionDocShares.Unlock()
			writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存主持人管理")
			return
		}
		companionDocShares.Unlock()
		go notifyCompanionHostTransferResult(share, content, companionHostSeatStampOf(share), extra)
		writeJSON(w, http.StatusOK, companionShareHostView(share, r))
	}
}

// CompanionDocumentShareHostAcceptHandler moves the seat after the nominee
// accepts. The owner remains admin and can revoke it later.
//
//	POST /api/mobile/documents/shares/{token}/host/accept
func CompanionDocumentShareHostAcceptHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decideCompanionShareHost(identity, w, r, true)
	}
}

// CompanionDocumentShareHostDeclineHandler leaves the host unchanged.
//
//	POST /api/mobile/documents/shares/{token}/host/decline
func CompanionDocumentShareHostDeclineHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decideCompanionShareHost(identity, w, r, false)
	}
}

func decideCompanionShareHost(identity *auth.IdentityService, w http.ResponseWriter, r *http.Request, accept bool) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	_, viewerID, ok := companionRequireViewer(w, r, identity)
	if !ok {
		return
	}
	token := strings.TrimSpace(r.PathValue("token"))
	if !companionShareTokenPattern.MatchString(token) {
		writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
		return
	}
	loadCompanionDocShares()
	companionDocShares.Lock()
	snapshot, found := companionDocShares.items[token]
	companionDocShares.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
		return
	}
	if snapshot.expired(time.Now().UTC()) {
		writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
		return
	}
	if strings.TrimSpace(snapshot.PendingHostID) == "" || strings.TrimSpace(snapshot.PendingHostID) != viewerID {
		writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "没有等待你处理的主持人转移")
		return
	}
	// A nomination can outlive the nominee's access: the group membership that
	// made them a participant can be revoked while the request is waiting, and
	// the seat must not move to someone who cannot open the document. The
	// reconcile below is not enough on its own because it can lose this race.
	// The nomination is withdrawn here too: this nominee can never accept, and
	// leaving it pending would keep a request in their bell that always fails.
	if !companionShareUserCanOpen(r.Context(), identity, snapshot, viewerID) {
		if cleared, ok := clearPendingCompanionHost(token, viewerID); ok {
			go notifyCompanionHostTransferResult(cleared,
				fmt.Sprintf("对方已不在文档《%s》的分享范围里，主持人转移已取消。", strings.TrimSpace(cleared.Name)),
				companionHostSeatStampOf(cleared), viewerID)
		}
		writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "你已不在这份文档的分享范围里")
		return
	}
	companionDocShares.Lock()
	share, found := companionDocShares.items[token]
	if !found {
		companionDocShares.Unlock()
		writeError(w, http.StatusNotFound, "SHARE_NOT_FOUND", "分享链接不存在")
		return
	}
	if share.expired(time.Now().UTC()) {
		companionDocShares.Unlock()
		writeError(w, http.StatusGone, "SHARE_EXPIRED", "分享链接已过期")
		return
	}
	if strings.TrimSpace(share.PendingHostID) == "" || strings.TrimSpace(share.PendingHostID) != viewerID {
		companionDocShares.Unlock()
		writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "没有等待你处理的主持人转移")
		return
	}
	previous := share
	nomineeLabel := strings.TrimSpace(share.PendingHostLabel)
	if nomineeLabel == "" {
		nomineeLabel = "对方"
	}
	var content string
	if accept {
		share.HostID = share.PendingHostID
		share.HostLabel = nomineeLabel
		share.PendingHostID = ""
		share.PendingHostLabel = ""
		share.PendingHostSince = time.Time{}
		content = fmt.Sprintf("%s 已接任文档《%s》的主持人。", nomineeLabel, strings.TrimSpace(share.Name))
	} else {
		share.PendingHostID = ""
		share.PendingHostLabel = ""
		share.PendingHostSince = time.Time{}
		content = fmt.Sprintf("%s 拒绝接任文档《%s》的主持人，主持人保持不变。", nomineeLabel, strings.TrimSpace(share.Name))
	}
	companionDocShares.items[token] = share
	if err := persistCompanionDocSharesLocked(); err != nil {
		companionDocShares.items[token] = previous
		companionDocShares.Unlock()
		writeError(w, http.StatusInternalServerError, "SHARE_FAILED", "无法保存主持人转移")
		return
	}
	companionDocShares.Unlock()
	// Access can disappear between the check above and this write: a removal
	// reconciles the seat it saw, then this accept lands the nominee anyway.
	// Put the seat back before anyone is told they took it.
	if accept && !companionShareUserCanOpen(r.Context(), identity, share, viewerID) {
		if reverted, revertedOK := revertCompanionHost(token, viewerID); revertedOK {
			go notifyCompanionHostTransferResult(reverted,
				fmt.Sprintf("对方已不在文档《%s》的分享范围里，主持人转移已取消。", strings.TrimSpace(reverted.Name)),
				companionHostSeatStampOf(reverted), viewerID)
		}
		writeError(w, http.StatusForbidden, "SHARE_FORBIDDEN", "你已不在这份文档的分享范围里")
		return
	}
	// The person who decided is named even though the pending seat is gone: for
	// a private share they may not be an importer yet, and they should see the
	// outcome of the request they answered.
	go notifyCompanionHostTransferResult(share, content, companionHostSeatStampOf(share), viewerID)
	writeJSON(w, http.StatusOK, companionShareHostView(share, r))
}

// clearPendingCompanionHost withdraws a nomination that is still waiting for
// nomineeID. It rewrites only the pending fields on the live row, so a note or
// a newer nomination written in between is kept. It reports false when there
// was nothing of theirs to withdraw.
func clearPendingCompanionHost(token, nomineeID string) (companionDocShare, bool) {
	companionDocShares.Lock()
	defer companionDocShares.Unlock()
	share, ok := companionDocShares.items[token]
	if !ok || strings.TrimSpace(share.PendingHostID) != strings.TrimSpace(nomineeID) {
		return companionDocShare{}, false
	}
	previous := share
	share.PendingHostID = ""
	share.PendingHostLabel = ""
	share.PendingHostSince = time.Time{}
	companionDocShares.items[token] = share
	if err := persistCompanionDocSharesLocked(); err != nil {
		companionDocShares.items[token] = previous
		return companionDocShare{}, false
	}
	return share, true
}

// revertCompanionHost gives the seat back to the owner when hostID still holds
// it. The bool is true when the caller should publish that cancellation. A
// nomination that landed in the meantime is left in place and is not overwritten
// by that notice.
func revertCompanionHost(token, hostID string) (companionDocShare, bool) {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return companionDocShare{}, false
	}
	companionDocShares.Lock()
	defer companionDocShares.Unlock()
	share, ok := companionDocShares.items[token]
	if !ok || strings.TrimSpace(share.HostID) != hostID {
		return companionDocShare{}, false
	}
	// A nomination that arrived after the accept owns the bell. Clearing the
	// bad host is still required; publishing over that nomination is not.
	pending := strings.TrimSpace(share.PendingHostID)
	previous := share
	share.HostID = ""
	share.HostLabel = ""
	companionDocShares.items[token] = share
	if err := persistCompanionDocSharesLocked(); err != nil {
		companionDocShares.items[token] = previous
		return companionDocShare{}, false
	}
	if pending != "" {
		return share, false
	}
	return share, true
}

// companionHostSeatStamp is the host seat a notification describes. A later
// nomination or accept changes the stamp, and the older notice must not revoke
// the newer one.
type companionHostSeatStamp struct {
	HostID           string
	PendingHostID    string
	PendingHostSince time.Time
}

func companionHostSeatStampOf(share companionDocShare) companionHostSeatStamp {
	return companionHostSeatStamp{
		HostID:           strings.TrimSpace(share.HostID),
		PendingHostID:    strings.TrimSpace(share.PendingHostID),
		PendingHostSince: share.PendingHostSince,
	}
}

func (s companionHostSeatStamp) matches(share companionDocShare) bool {
	other := companionHostSeatStampOf(share)
	return s.HostID == other.HostID &&
		s.PendingHostID == other.PendingHostID &&
		s.PendingHostSince.Equal(other.PendingHostSince)
}

func companionShareAdminLabel(share companionDocShare) string {
	if label := strings.TrimSpace(share.OwnerLabel); label != "" {
		return label
	}
	return "管理员"
}

// companionGroupAcceptedUserIDs resolves one group's accepted members. Callers
// that walk every share of a group reuse the set instead of looking the group
// up once per seat.
func companionGroupAcceptedUserIDs(ctx context.Context, identity *auth.IdentityService, groupID string) map[string]struct{} {
	out := map[string]struct{}{}
	if identity == nil || strings.TrimSpace(groupID) == "" {
		return out
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group, ok := companionDocGroups.items[strings.TrimSpace(groupID)]
	var members []companionDocGroupMember
	tenantID := ""
	if ok {
		members = append(members, group.Members...)
		tenantID = group.TenantID
	}
	companionDocGroups.Unlock()
	if !ok {
		return out
	}
	for _, member := range members {
		if member.Status != "accepted" {
			continue
		}
		id := strings.TrimSpace(member.UserID)
		if id == "" {
			id = companionContactUserID(ctx, identity, tenantID, member.Contact)
		}
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// companionShareUserCanOpen reports whether userID can still read this share.
// The host seat itself does not count: a removed member who once imported the
// file stays in Importers, but a group or person share no longer opens for them.
func companionShareUserCanOpen(ctx context.Context, identity *auth.IdentityService, share companionDocShare, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	if userID == strings.TrimSpace(share.OwnerID) {
		return true
	}
	switch share.Visibility {
	case "public":
		return true
	case "private":
		if share.Importers == nil {
			return false
		}
		_, ok := share.Importers[userID]
		return ok
	case "person":
		if identity == nil {
			return false
		}
		return companionContactUserID(ctx, identity, share.TenantID, share.Target) == userID
	case "group":
		_, ok := companionGroupAcceptedUserIDs(ctx, identity, share.GroupID)[userID]
		return ok
	default:
		return false
	}
}

func notifyCompanionHostTransferRequest(share companionDocShare, nomineeID, nomineeLabel string, since time.Time) {
	identitySvc, svc := companionNotificationServices()
	if svc == nil {
		return
	}
	ctx := context.Background()
	pendingStill := func(current companionDocShare) bool {
		return strings.TrimSpace(current.PendingHostID) == strings.TrimSpace(nomineeID) && current.PendingHostSince.Equal(since)
	}
	companionHostNotifyMu.Lock()
	current, ok := companionShareByToken(share.Token)
	if !ok || !pendingStill(current) {
		companionHostNotifyMu.Unlock()
		return
	}
	share = current
	companionHostNotifyMu.Unlock()
	// Member lookup talks to identity. Keep it outside the notify lock so one
	// share does not stall every other host notice.
	participants := companionShareParticipantUserIDs(ctx, identitySvc, share)
	companionHostNotifyMu.Lock()
	defer companionHostNotifyMu.Unlock()
	current, ok = companionShareByToken(share.Token)
	if !ok || !pendingStill(current) {
		return
	}
	share = current
	revokeCompanionHostTransferLocked(ctx, svc, share.Token)
	if len(participants) == 0 {
		return
	}
	adminLabel := strings.TrimSpace(share.OwnerLabel)
	if adminLabel == "" {
		adminLabel = "管理员"
	}
	docName := strings.TrimSpace(share.Name)
	if docName == "" {
		docName = "文档"
	}
	if strings.TrimSpace(nomineeLabel) == "" {
		nomineeLabel = "一位参与者"
	}
	nomineeID = strings.TrimSpace(nomineeID)
	createCompanionHostNotificationLocked(ctx, svc, share, []string{nomineeID}, "主持人转移",
		fmt.Sprintf("%s 希望你接任文档《%s》的主持人。接受后由你回复批注并同步文档。", adminLabel, docName),
		notification.SourceCompanionHostTransfer)
	others := make([]string, 0, len(participants))
	for _, userID := range participants {
		if strings.TrimSpace(userID) != nomineeID {
			others = append(others, userID)
		}
	}
	if len(others) > 0 {
		createCompanionHostNotificationLocked(ctx, svc, share, others, "主持人变更",
			fmt.Sprintf("%s 提名 %s 接任文档《%s》的主持人，等待对方接受。", adminLabel, nomineeLabel, docName),
			notification.SourceCompanionHostTransferNotice)
	}
}

// reconcileGroupHostSeats keeps a group share usable after its audience
// changes: a seat must not stay with someone who can no longer open the
// document. A pending nomination is cancelled and a current host reverts to
// the owner. Both can happen together. Used when a member is removed, leaves,
// or the group is dissolved.
func reconcileGroupHostSeats(ctx context.Context, identity *auth.IdentityService, groupID string) {
	if identity == nil || strings.TrimSpace(groupID) == "" {
		return
	}
	loadCompanionDocShares()
	now := time.Now().UTC()
	groupID = strings.TrimSpace(groupID)
	companionDocShares.Lock()
	tokens := make([]string, 0, 4)
	for _, share := range companionDocShares.items {
		// A share that left this group, or has already expired, is not this
		// membership change. Dropping its seat here would publish a notice
		// for a link the group no longer owns.
		if share.Visibility != "group" || share.expired(now) || strings.TrimSpace(share.GroupID) != groupID {
			continue
		}
		tokens = append(tokens, share.Token)
	}
	companionDocShares.Unlock()
	if len(tokens) == 0 {
		return
	}
	readers := companionGroupAcceptedUserIDs(ctx, identity, groupID)
	for _, token := range tokens {
		pendingID, hostID, share, changed := dropUnreachableCompanionHostSeats(ctx, identity, token, readers)
		if !changed {
			continue
		}
		name := strings.TrimSpace(share.Name)
		var content string
		switch {
		case pendingID != "" && hostID != "":
			content = fmt.Sprintf("文档《%s》的主持人转移已取消，主持权已收回给管理员。", name)
		case pendingID != "":
			content = fmt.Sprintf("对方已不在群组，文档《%s》的主持人转移已取消。", name)
		default:
			content = fmt.Sprintf("主持人已不在群组，文档《%s》由管理员重新担任主持人。", name)
		}
		go notifyCompanionHostTransferResult(share, content, companionHostSeatStampOf(share), pendingID, hostID)
	}
}

// dropUnreachableCompanionHostSeats clears a pending nominee and a current
// host who can no longer open the document. It rewrites only those fields on
// the live row, so a note or a newer nomination is not replaced by a stale copy.
// The returned ids are the people who lost the seat; a concurrent change of
// either id is left in place.
func dropUnreachableCompanionHostSeats(ctx context.Context, identity *auth.IdentityService, token string, groupReaders map[string]struct{}) (pendingID, hostID string, share companionDocShare, changed bool) {
	current, ok := companionShareByToken(token)
	if !ok {
		return "", "", companionDocShare{}, false
	}
	pendingID = strings.TrimSpace(current.PendingHostID)
	hostID = strings.TrimSpace(current.HostID)
	ownerID := strings.TrimSpace(current.OwnerID)
	if current.Visibility == "group" && groupReaders == nil {
		groupReaders = companionGroupAcceptedUserIDs(ctx, identity, current.GroupID)
	}
	canOpen := func(id string) bool {
		if current.Visibility != "group" {
			return companionShareUserCanOpen(ctx, identity, current, id)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		if id == ownerID {
			return true
		}
		_, ok := groupReaders[id]
		return ok
	}
	dropPending := pendingID != "" && !canOpen(pendingID)
	dropHost := hostID != "" && hostID != ownerID && !canOpen(hostID)
	if !dropPending && !dropHost {
		return "", "", current, false
	}

	companionDocShares.Lock()
	share, ok = companionDocShares.items[token]
	if !ok {
		companionDocShares.Unlock()
		return "", "", companionDocShare{}, false
	}
	if !dropPending || strings.TrimSpace(share.PendingHostID) != pendingID {
		dropPending = false
		pendingID = ""
	}
	if !dropHost || strings.TrimSpace(share.HostID) != hostID {
		dropHost = false
		hostID = ""
	}
	if !dropPending && !dropHost {
		companionDocShares.Unlock()
		return "", "", share, false
	}
	previous := share
	if dropPending {
		share.PendingHostID = ""
		share.PendingHostLabel = ""
		share.PendingHostSince = time.Time{}
	}
	if dropHost {
		share.HostID = ""
		share.HostLabel = ""
	}
	companionDocShares.items[token] = share
	if err := persistCompanionDocSharesLocked(); err != nil {
		companionDocShares.items[token] = previous
		companionDocShares.Unlock()
		return "", "", previous, false
	}
	companionDocShares.Unlock()
	return pendingID, hostID, share, true
}

func revokeCompanionHostTransferNotifications(token string) {
	_, svc := companionNotificationServices()
	if svc == nil || strings.TrimSpace(token) == "" {
		return
	}
	ctx := context.Background()
	companionHostNotifyMu.Lock()
	defer companionHostNotifyMu.Unlock()
	revokeCompanionHostTransferLocked(ctx, svc, token)
}

func notifyCompanionHostTransferResult(share companionDocShare, content string, stamp companionHostSeatStamp, extra ...string) {
	identitySvc, svc := companionNotificationServices()
	if svc == nil || strings.TrimSpace(content) == "" {
		return
	}
	ctx := context.Background()
	companionHostNotifyMu.Lock()
	current, ok := companionShareByToken(share.Token)
	// The seat moved again after this result was decided. Revoking now would
	// delete the newer transfer request and publish a stale outcome.
	if !ok || !stamp.matches(current) {
		companionHostNotifyMu.Unlock()
		return
	}
	share = current
	companionHostNotifyMu.Unlock()
	participants := companionShareParticipantUserIDs(ctx, identitySvc, share)
	companionHostNotifyMu.Lock()
	defer companionHostNotifyMu.Unlock()
	current, ok = companionShareByToken(share.Token)
	if !ok || !stamp.matches(current) {
		return
	}
	revokeCompanionHostTransferLocked(ctx, svc, share.Token)
	seen := make(map[string]struct{}, len(participants)+len(extra))
	for _, userID := range participants {
		seen[userID] = struct{}{}
	}
	for _, userID := range extra {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		participants = append(participants, userID)
	}
	if len(participants) == 0 {
		return
	}
	createCompanionHostNotificationLocked(ctx, svc, share, participants, "主持人变更", content,
		notification.SourceCompanionHostTransferNotice)
}

func createCompanionHostNotificationLocked(ctx context.Context, svc *notification.Service, share companionDocShare, audience []string, title, content, source string) {
	if len(audience) == 0 {
		return
	}
	created, err := svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        title,
		Content:      content,
		Category:     notification.CategoryCustom,
		Priority:     notification.PriorityImportant,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  audience,
		CreatedBy:    "companion-share",
		Source:       source,
		SourceID:     share.Token,
	})
	if err != nil {
		log.Printf("[companion-share] create %s for %s: %v", source, share.Token, err)
		return
	}
	if err := svc.PublishNotification(ctx, created.ID); err != nil {
		log.Printf("[companion-share] publish %s for %s: %v", source, share.Token, err)
	}
}

func revokeCompanionHostTransferLocked(ctx context.Context, svc *notification.Service, token string) {
	for _, source := range []string{notification.SourceCompanionHostTransfer, notification.SourceCompanionHostTransferNotice} {
		// FindBySource returns only the newest row. A settled copy can sit
		// beside it, and that older request would stay actionable in the bell.
		seen := map[string]struct{}{}
		for range 8 {
			existing, err := svc.FindBySource(ctx, source, token)
			if err != nil {
				log.Printf("[companion-share] find %s for %s: %v", source, token, err)
				break
			}
			if existing == nil {
				break
			}
			if _, ok := seen[existing.ID]; ok {
				break
			}
			seen[existing.ID] = struct{}{}
			if err := svc.RevokeNotification(ctx, existing.ID); err != nil {
				log.Printf("[companion-share] revoke %s for %s: %v", source, token, err)
			}
			if err := svc.DeleteNotification(ctx, existing.ID); err != nil {
				log.Printf("[companion-share] delete %s for %s: %v", source, token, err)
				break
			}
		}
	}
}
