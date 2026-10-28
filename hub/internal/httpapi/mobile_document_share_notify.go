package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

// A share made to a person or a reading group raises one Hub notification for
// its recipients. The row is persisted and pushed over the WebSocket, so an
// offline recipient still sees it after reconnecting. The bell detail offers
// 打开; the notification source id is the share token.
//
// Public and password shares have no known recipients, so they raise nothing.
// A later audience change revokes the old notice and raises one for the new
// audience; a sync or a password/expiry edit does not notify again.

const companionShareNotificationSource = notification.SourceCompanionShare

var companionShareNotifyMu sync.Mutex

func companionNotificationServices() (*auth.IdentityService, *notification.Service) {
	n := companionGroupInviteNotifierFor()
	if n == nil {
		return nil, nil
	}
	return n.identity, n.svc
}

func isCompanionShareNotificationSource(source string) bool {
	return strings.TrimSpace(source) == companionShareNotificationSource
}

// companionShareNotifyRecipients resolves the people who may open this share.
// Only accounts with a verified contact become recipients; a bare link still
// works for anyone else. The owner never notifies themself.
func companionShareNotifyRecipients(ctx context.Context, identity *auth.IdentityService, share companionDocShare) ([]string, string) {
	if identity == nil {
		return nil, ""
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(userID string) {
		userID = strings.TrimSpace(userID)
		if userID == "" || userID == share.OwnerID {
			return
		}
		if _, ok := seen[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
		out = append(out, userID)
	}
	switch share.Visibility {
	case "group":
		loadCompanionDocGroups()
		companionDocGroups.Lock()
		group, ok := companionDocGroups.items[strings.TrimSpace(share.GroupID)]
		var members []companionDocGroupMember
		if ok {
			members = append(members, group.Members...)
		}
		companionDocGroups.Unlock()
		if !ok {
			return nil, ""
		}
		for _, member := range members {
			if member.Status != "accepted" {
				continue
			}
			userID := strings.TrimSpace(member.UserID)
			if userID == "" {
				userID = companionContactUserID(ctx, identity, group.TenantID, member.Contact)
			}
			add(userID)
		}
		return out, strings.TrimSpace(group.Name)
	case "person":
		add(companionContactUserID(ctx, identity, share.TenantID, share.Target))
		return out, ""
	default:
		return nil, ""
	}
}

func companionShareNotifyContent(share companionDocShare, groupName string) string {
	owner := strings.TrimSpace(share.OwnerLabel)
	if owner == "" {
		owner = "一位用户"
	}
	name := strings.TrimSpace(share.Name)
	if name == "" {
		name = "文档"
	}
	if share.Visibility == "group" {
		if strings.TrimSpace(groupName) == "" {
			groupName = "阅读群组"
		}
		return fmt.Sprintf("%s 在阅读群组「%s」分享了文档《%s》。在伴读通知里点「打开」即可查看。", owner, groupName, name)
	}
	return fmt.Sprintf("%s 向你分享了文档《%s》。在伴读通知里点「打开」即可查看。", owner, name)
}

// refreshCompanionShareNotification replaces the notice for one share. It is
// called after a share is created or its audience changes.
func refreshCompanionShareNotification(share companionDocShare) {
	identitySvc, svc := companionNotificationServices()
	token := strings.TrimSpace(share.Token)
	if svc == nil || token == "" {
		return
	}
	ctx := context.Background()
	companionShareNotifyMu.Lock()
	defer companionShareNotifyMu.Unlock()
	// Re-read inside the serialized section. A delete can land between the
	// handler's unlock and this goroutine; creating a notice for a link that is
	// already gone would leave a dead bell entry.
	current, ok := companionShareByToken(token)
	if !ok {
		revokeCompanionShareNotificationLocked(ctx, svc, token)
		return
	}
	share = current
	recipients, groupName := companionShareNotifyRecipients(ctx, identitySvc, share)
	revokeCompanionShareNotificationLocked(ctx, svc, token)
	if len(recipients) == 0 {
		return
	}
	createCompanionShareNotificationLocked(ctx, svc, share, recipients, groupName)
}

// ensureCompanionShareNotification is the duplicate-create recovery path: an
// already live link may predate this notification feature or have lost its
// notice. Raise one only when none exists, so repeated duplicate attempts do
// not stack notices.
func ensureCompanionShareNotification(share companionDocShare) {
	identitySvc, svc := companionNotificationServices()
	token := strings.TrimSpace(share.Token)
	if svc == nil || token == "" {
		return
	}
	ctx := context.Background()
	companionShareNotifyMu.Lock()
	defer companionShareNotifyMu.Unlock()
	current, ok := companionShareByToken(token)
	if !ok {
		return
	}
	existing, err := svc.FindBySource(ctx, companionShareNotificationSource, token)
	if err != nil {
		log.Printf("[companion-share] find notification %s: %v", token, err)
		return
	}
	if existing != nil {
		if existing.Status == notification.StatusPublished {
			return
		}
		// A settled (revoked or expired) row still owns the source key; drop it
		// and raise a fresh notice for this live share.
		if err := svc.DeleteNotification(ctx, existing.ID); err != nil {
			log.Printf("[companion-share] drop settled notification %s: %v", existing.ID, err)
			return
		}
	}
	recipients, groupName := companionShareNotifyRecipients(ctx, identitySvc, current)
	if len(recipients) == 0 {
		return
	}
	createCompanionShareNotificationLocked(ctx, svc, current, recipients, groupName)
}

func createCompanionShareNotificationLocked(ctx context.Context, svc *notification.Service, share companionDocShare, recipients []string, groupName string) {
	created, err := svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        "分享文档",
		Content:      companionShareNotifyContent(share, groupName),
		Category:     notification.CategoryCustom,
		Priority:     notification.PriorityImportant,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  recipients,
		CreatedBy:    "companion-share",
		Source:       companionShareNotificationSource,
		SourceID:     share.Token,
	})
	if err != nil {
		log.Printf("[companion-share] create notification for %s: %v", share.Token, err)
		return
	}
	if err := svc.PublishNotification(ctx, created.ID); err != nil {
		log.Printf("[companion-share] publish notification for %s: %v", share.Token, err)
	}
}

// revokeCompanionShareNotification withdraws the notice when its share is
// deleted or its audience becomes one with no registered recipients.
func revokeCompanionShareNotification(token string) {
	_, svc := companionNotificationServices()
	if svc == nil || strings.TrimSpace(token) == "" {
		return
	}
	ctx := context.Background()
	companionShareNotifyMu.Lock()
	defer companionShareNotifyMu.Unlock()
	revokeCompanionShareNotificationLocked(ctx, svc, token)
}

func revokeCompanionShareNotificationLocked(ctx context.Context, svc *notification.Service, token string) {
	existing, err := svc.FindBySource(ctx, companionShareNotificationSource, token)
	if err != nil {
		log.Printf("[companion-share] find notification %s: %v", token, err)
		return
	}
	if existing == nil {
		return
	}
	if err := svc.RevokeNotification(ctx, existing.ID); err != nil {
		log.Printf("[companion-share] revoke notification %s: %v", existing.ID, err)
	}
	// A settled share notice is never raised again under this token; remove the
	// row so the source key stays unambiguous.
	if err := svc.DeleteNotification(ctx, existing.ID); err != nil {
		log.Printf("[companion-share] delete notification %s: %v", existing.ID, err)
	}
}

// companionShareAudienceChanged is the only update that raises or withdraws a
// notice. Password and expiry edits leave the same people able to open it.
func companionShareAudienceChanged(previous, next companionDocShare) bool {
	return previous.Visibility != next.Visibility ||
		previous.Target != next.Target ||
		previous.GroupID != next.GroupID
}

func companionShareAudienceHostNotice(share companionDocShare, pendingCleared, hostCleared bool) string {
	admin := companionShareAdminLabel(share)
	name := strings.TrimSpace(share.Name)
	switch {
	case pendingCleared && hostCleared:
		return fmt.Sprintf("%s 修改了分享范围，文档《%s》的主持人转移已取消，主持权已收回给管理员。", admin, name)
	case pendingCleared:
		return fmt.Sprintf("%s 修改了分享范围，文档《%s》的主持人转移已取消。", admin, name)
	default:
		return fmt.Sprintf("%s 修改了分享范围，文档《%s》由管理员重新担任主持人。", admin, name)
	}
}
