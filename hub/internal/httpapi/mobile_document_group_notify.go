package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// A reading-group invitation is delivered as a real Hub notification so an
// invitee who is offline still gets it: the row is persisted, pushed over the
// WebSocket when they are online, and pulled on their next connect. The
// notification is only the delivery channel — the invite list stays the source
// of truth for what the bell shows, so marking a notification read does not
// decide the invitation. Accepting, declining, ignoring, being removed, or the
// group being dissolved revokes the notification.

// companionGroupInviteNotificationSource is the notification source name. It
// lives in the notification package because the admin console excludes it.
const companionGroupInviteNotificationSource = notification.SourceCompanionGroupInvite

type companionGroupInviteNotifier struct {
	identity *auth.IdentityService
	svc      *notification.Service
}

var companionGroupNotifier struct {
	sync.RWMutex
	value *companionGroupInviteNotifier
}

// ConfigureCompanionGroupInviteNotifier wires the notification service that
// delivers reading-group invitations. A nil service disables delivery; the
// invite list keeps working as before.
func ConfigureCompanionGroupInviteNotifier(identity *auth.IdentityService, svc *notification.Service) {
	companionGroupNotifier.Lock()
	defer companionGroupNotifier.Unlock()
	if identity == nil || svc == nil {
		companionGroupNotifier.value = nil
		return
	}
	companionGroupNotifier.value = &companionGroupInviteNotifier{identity: identity, svc: svc}
}

func companionGroupInviteNotifierFor() *companionGroupInviteNotifier {
	companionGroupNotifier.RLock()
	defer companionGroupNotifier.RUnlock()
	return companionGroupNotifier.value
}

// companionGroupInviteSourceID keys one notification per person and group, so a
// person invited under both an email and a phone row still gets one notice.
func companionGroupInviteSourceID(groupID, userID string) string {
	return groupID + ":" + userID
}

// companionContactUserID resolves an invited contact to the Hub account that
// owns it. Ownership has to be proven: the address must be the account email or
// a verified identity. An identity nobody verified is only a claim, and
// treating it as ownership would hand one person's invitation to another.
//
// The user id is the durable credential. An account registered by phone keeps a
// synthesized account email such as "phone:1709...", so the address someone was
// invited under is usually an identity rather than the account email — the id
// stays the same either way.
// companionContactDirectory is the user store's cross-tenant contact lookup.
// The repository interface stays unchanged for callers that do not need it.
type companionContactDirectory interface {
	ListUsersByContact(ctx context.Context, contact string) ([]*store.User, error)
}

func companionContactUserID(ctx context.Context, identity *auth.IdentityService, tenantID, contact string) string {
	if id := companionContactUserIDInTenant(ctx, identity, tenantID, contact); id != "" {
		return id
	}
	// The group's tenant is not the only place this address can live. One
	// account on this Hub, in any tenant, is enough to deliver the notice.
	// Two accounts that share the address are left unresolved: a push must
	// not choose between them. Either of them can still see the invite,
	// because the invite list matches the address itself.
	return companionContactUserIDAcrossTenants(ctx, identity, contact)
}

func companionContactUserIDAcrossTenants(ctx context.Context, identity *auth.IdentityService, contact string) string {
	if identity == nil {
		return ""
	}
	contact = strings.TrimSpace(contact)
	repo := identity.UsersRepo()
	if contact == "" || repo == nil {
		return ""
	}
	directory, ok := repo.(companionContactDirectory)
	if !ok {
		return ""
	}
	users, err := directory.ListUsersByContact(ctx, contact)
	if err != nil || len(users) != 1 || users[0] == nil {
		return ""
	}
	return strings.TrimSpace(users[0].ID)
}

func companionContactUserIDInTenant(ctx context.Context, identity *auth.IdentityService, tenantID, contact string) string {
	if identity == nil {
		return ""
	}
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return ""
	}
	lookupCtx := auth.WithTenant(ctx, tenantID)
	emailContact := strings.Contains(contact, "@")
	var (
		user *store.User
		err  error
	)
	if emailContact {
		user, err = identity.LookupUserByEmail(lookupCtx, contact)
	} else {
		user, err = identity.LookupUserByPhone(lookupCtx, contact)
	}
	if err != nil || user == nil {
		return ""
	}
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return ""
	}
	if emailContact {
		if strings.EqualFold(strings.TrimSpace(user.Email), contact) {
			return userID
		}
	} else if companionDigits(user.Email) == companionDigits(contact) {
		return userID
	}
	repo := identity.UsersRepo()
	if repo == nil {
		return ""
	}
	identities, idErr := repo.ListIdentitiesByUser(ctx, user.TenantID, userID)
	if idErr != nil {
		return ""
	}
	for _, identityRow := range identities {
		if identityRow == nil || !identityRow.Verified {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(identityRow.Type)) {
		case "email":
			if emailContact && strings.EqualFold(strings.TrimSpace(identityRow.Value), contact) {
				return userID
			}
		case "phone":
			if !emailContact && companionDigits(identityRow.Value) == companionDigits(contact) {
				return userID
			}
		}
	}
	return ""
}

// companionDigits keeps only decimal digits, matching how phone contacts are
// stored.
func companionDigits(value string) string {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
}

// notifyInvite publishes the invitation notice for one contact and returns the
// notification id so the member row can revoke it later. It returns "" when the
// contact has no owner or delivery is not configured.
func (n *companionGroupInviteNotifier) notifyInvite(ctx context.Context, group companionDocGroup, contact, userID string) string {
	if n == nil || n.svc == nil || userID == "" {
		return ""
	}
	// Inviting yourself is a member-list oddity, not an invitation: the owner
	// never sees a pending row for their own group, so a notice would land with
	// no counterpart in the invite list.
	if ownerID := strings.TrimSpace(group.OwnerID); ownerID != "" && userID == ownerID {
		return ""
	}
	sourceID := companionGroupInviteSourceID(group.ID, userID)
	if existing, err := n.svc.FindBySource(ctx, companionGroupInviteNotificationSource, sourceID); err != nil {
		log.Printf("[companion-group] find invite notification %s: %v", sourceID, err)
	} else if existing != nil {
		if existing.Status == notification.StatusPublished {
			// The person already has this invitation in the bell. Re-inviting
			// the same group must not stack a second notice on top of it.
			return existing.ID
		}
		// A revoked notice keeps its source key. Drop it first, otherwise the
		// next lookup could return the dead row instead of this new one.
		if delErr := n.svc.DeleteNotification(ctx, existing.ID); delErr != nil {
			log.Printf("[companion-group] drop settled invite notification %s: %v", existing.ID, delErr)
		}
	}
	owner := strings.TrimSpace(group.OwnerLabel)
	if owner == "" {
		owner = "一位用户"
	}
	created, err := n.svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        "入群邀请",
		Content:      fmt.Sprintf("%s 邀请你加入阅读群组「%s」。接受后可以在伴读的文档管理里看到群组文章。", owner, group.Name),
		Category:     notification.CategoryCustom,
		Priority:     notification.PriorityImportant,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  []string{userID},
		CreatedBy:    "companion-group",
		Source:       companionGroupInviteNotificationSource,
		SourceID:     sourceID,
	})
	if err != nil {
		log.Printf("[companion-group] create invite notification %s: %v", sourceID, err)
		return ""
	}
	// Publish is what pushes the notice to a connected machine. A push failure
	// still leaves a published row for the next pull, so the notice id is
	// returned either way: it is what a later revoke needs.
	if err := n.svc.PublishNotification(ctx, created.ID); err != nil {
		log.Printf("[companion-group] publish invite notification %s: %v", created.ID, err)
	}
	return created.ID
}

// revokeInvites withdraws the given notifications. A notification that is
// already gone, revoked, or expired is skipped; revocation is best effort
// because the invite list stops returning the row either way.
func (n *companionGroupInviteNotifier) revokeInvites(ctx context.Context, ids ...string) {
	if n == nil || n.svc == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if err := n.svc.RevokeNotification(ctx, id); err != nil {
			log.Printf("[companion-group] revoke invite notification %s: %v", id, err)
			continue
		}
		// A settled invitation is never raised again under this id. Dropping the
		// row keeps the notification table from collecting one dead row per
		// invitation and keeps the source key unambiguous for a re-invite.
		if err := n.svc.DeleteNotification(ctx, id); err != nil {
			log.Printf("[companion-group] delete settled invite notification %s: %v", id, err)
		}
	}
}

// companionGroupMemberNotificationIDs collects the notification ids of member
// rows, used when a whole person's invitation is settled.
func companionGroupMemberNotificationIDs(members []companionDocGroupMember) []string {
	out := make([]string, 0, len(members))
	for _, member := range members {
		if id := strings.TrimSpace(member.NotificationID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// companionGroupNotificationStillPending reports whether another row of the same
// group still waits on the same person under the same notification. Removing one
// contact row must not take the notice away from a still-pending sibling row.
func companionGroupNotificationStillPending(members []companionDocGroupMember, notificationID, skipContact string) bool {
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return false
	}
	for _, member := range members {
		if member.Contact == skipContact || member.Status != "pending" || member.Muted {
			continue
		}
		if strings.TrimSpace(member.NotificationID) == notificationID {
			return true
		}
	}
	return false
}

// A document shared to a reading group before someone joined predates their
// audience: the per-share notice was addressed to the members of that moment.
// Instead of re-raising one notice per old share, a member who accepts an
// invitation gets ONE merged digest — "群组里有 N 份文档可阅读" — whose 确认
// opens the companion's 文档管理. The digest is keyed by group and member, so
// a leave-and-rejoin refreshes it rather than stacking rows.

const companionGroupDigestNotificationSource = notification.SourceCompanionGroupDigest

func companionGroupDigestSourceID(groupID, userID string) string {
	return strings.TrimSpace(groupID) + ":" + strings.TrimSpace(userID)
}

// companionGroupShareDigestCount is how many live group shares existed at or
// before joinedAt. Shares created later notify the member on their own, so
// counting them here would double the news. A zero joinedAt counts every live
// share, which is what a remote member's freshly fetched list already is.
func companionGroupShareDigestCount(groupID string, joinedAt time.Time) int {
	if !companionGroupTokenPattern.MatchString(strings.TrimSpace(groupID)) {
		return 0
	}
	loadCompanionDocShares()
	now := time.Now().UTC()
	count := 0
	companionDocShares.Lock()
	for _, share := range companionDocShares.items {
		if share.Visibility != "group" || share.GroupID != groupID || share.expired(now) {
			continue
		}
		if !joinedAt.IsZero() && share.CreatedAt.After(joinedAt) {
			continue
		}
		count++
	}
	companionDocShares.Unlock()
	return count
}

// companionGroupJoinedShareDigest raises or refreshes the merged notice for a
// member who just joined the group. count is how many readable documents the
// digest reports; zero withdraws a stale digest instead of announcing an
// empty group. memberOK re-proves membership under companionShareNotifyMu:
// creation runs off the accept goroutine, so a leave, removal, or dissolve in
// the gap must not let a dead group's notice appear after its revoke already
// ran.
func companionGroupJoinedShareDigest(group companionDocGroup, userID string, count int, memberOK func() bool) {
	_, svc := companionNotificationServices()
	userID = strings.TrimSpace(userID)
	if svc == nil || userID == "" || !companionGroupTokenPattern.MatchString(strings.TrimSpace(group.ID)) {
		return
	}
	ctx := context.Background()
	sourceID := companionGroupDigestSourceID(group.ID, userID)
	companionShareNotifyMu.Lock()
	defer companionShareNotifyMu.Unlock()
	if memberOK != nil && !memberOK() {
		return
	}
	existing, err := svc.FindBySource(ctx, companionGroupDigestNotificationSource, sourceID)
	if err != nil {
		log.Printf("[companion-group] find digest notification %s: %v", sourceID, err)
		return
	}
	if existing != nil {
		// A re-join writes a fresh row: the document count may have moved, and
		// a stale settled row would keep the source key anyway.
		if err := svc.DeleteNotification(ctx, existing.ID); err != nil {
			log.Printf("[companion-group] drop stale digest notification %s: %v", existing.ID, err)
			return
		}
	}
	if count <= 0 {
		return
	}
	groupName := strings.TrimSpace(group.Name)
	if groupName == "" {
		groupName = "阅读群组"
	}
	created, err := svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        "群组文档",
		Content:      fmt.Sprintf("阅读群组「%s」在你加入前已有 %d 份文档。点「确认」打开伴读的文档管理查看。", groupName, count),
		Category:     notification.CategoryCustom,
		Priority:     notification.PriorityImportant,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  []string{userID},
		CreatedBy:    "companion-group",
		Source:       companionGroupDigestNotificationSource,
		SourceID:     sourceID,
	})
	if err != nil {
		log.Printf("[companion-group] create digest notification %s: %v", sourceID, err)
		return
	}
	if err := svc.PublishNotification(ctx, created.ID); err != nil {
		log.Printf("[companion-group] publish digest notification %s: %v", sourceID, err)
	}
}

// companionGroupUserAccepted reports that the account still holds an accepted
// row in the group. The digest writer re-checks under the notification lock
// so a member who left between accept and write does not get a stale notice.
func companionGroupUserAccepted(groupID, userID string) bool {
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		return false
	}
	for _, member := range group.Members {
		if member.Status == "accepted" && strings.TrimSpace(member.UserID) == strings.TrimSpace(userID) {
			return true
		}
	}
	return false
}

// companionGroupJoinedShareDigestLocal is the local-member entry: the share
// inventory is on this hub, so the count can exclude anything created after
// the join. It runs off the request goroutine: the count and the bell write
// must not hold the accept response.
func companionGroupJoinedShareDigestLocal(group companionDocGroup, userID string, joinedAt time.Time) {
	companionGroupJoinedShareDigest(group, userID, companionGroupShareDigestCount(group.ID, joinedAt), func() bool {
		return companionGroupUserAccepted(group.ID, userID)
	})
}

// companionRemoteGroupJoinedShareDigest is the member-side entry for a group
// that lives on another hub. The remote-shares list is fetched with the just
// accepted invite's secret; everything it returns already existed, so the
// whole list counts. userID is the authenticated accepter: an invite row
// written before the account field existed must not lose the notice.
// companionRemoteInviteStillAccepted re-proves that the account's invite row
// is still accepted. The digest writer checks it once before the cross-hub
// fetch and again under the notification lock.
func companionRemoteInviteStillAccepted(key, userID string) bool {
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	current, found := companionRemoteInvites.items[key]
	return found && current.Status == "accepted" && strings.TrimSpace(current.UserID) == userID
}

// companionRemoteAccountStillMember reports the account keeps a seat in the
// group through another invited address: membership rows live one per
// contact, so a revoke on one must not kill notices another still reads.
func companionRemoteAccountStillMember(groupID, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	loadCompanionRemoteInvites()
	companionRemoteInvites.Lock()
	defer companionRemoteInvites.Unlock()
	for _, item := range companionRemoteInvites.items {
		if item.GroupID == groupID && item.Status == "accepted" && strings.TrimSpace(item.UserID) == userID {
			return true
		}
	}
	return false
}

func companionRemoteGroupJoinedShareDigest(invite companionRemoteInvite, userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = strings.TrimSpace(invite.UserID)
	}
	if userID == "" {
		return
	}
	key := companionRemoteInviteKey(companionHubBaseURL(invite.OriginHub), invite.GroupID, invite.Contact)
	memberOK := func() bool {
		return companionRemoteInviteStillAccepted(key, userID)
	}
	if !memberOK() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	shares, ok := companionFetchRemoteShares(ctx, invite)
	if !ok {
		log.Printf("[companion-group] remote digest fetch %s: unavailable", invite.GroupID)
		return
	}
	// A zero count still goes through the writer: it cleans a stale row left
	// by a revoke that never arrived, then announces nothing.
	companionGroupJoinedShareDigest(companionDocGroup{ID: invite.GroupID, Name: invite.Name}, userID, len(shares), memberOK)
}

// revokeCompanionGroupDigests withdraws the merged notices of members who can
// no longer read the group. Empty user ids are skipped: a pending row without
// a resolved account never had a digest.
func revokeCompanionGroupDigests(ctx context.Context, groupID string, userIDs ...string) {
	_, svc := companionNotificationServices()
	if svc == nil || !companionGroupTokenPattern.MatchString(strings.TrimSpace(groupID)) {
		return
	}
	companionShareNotifyMu.Lock()
	defer companionShareNotifyMu.Unlock()
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		sourceID := companionGroupDigestSourceID(groupID, userID)
		existing, err := svc.FindBySource(ctx, companionGroupDigestNotificationSource, sourceID)
		if err != nil {
			log.Printf("[companion-group] find digest notification %s: %v", sourceID, err)
			continue
		}
		if existing == nil {
			continue
		}
		if err := svc.RevokeNotification(ctx, existing.ID); err != nil {
			log.Printf("[companion-group] revoke digest notification %s: %v", existing.ID, err)
		}
		if err := svc.DeleteNotification(ctx, existing.ID); err != nil {
			log.Printf("[companion-group] delete digest notification %s: %v", existing.ID, err)
		}
	}
}
