package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

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
func companionContactUserID(ctx context.Context, identity *auth.IdentityService, tenantID, contact string) string {
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
