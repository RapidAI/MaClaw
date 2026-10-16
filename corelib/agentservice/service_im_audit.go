package agentservice

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) ListIMAuditMessages(ctx context.Context, p Principal, in ListIMAuditMessagesInput) ([]IMAuditMessage, error) {
	_ = ctx
	platform := normalizeIMAuditPlatform(in.Platform)
	contact := strings.ToLower(strings.TrimSpace(in.Contact))
	query := strings.ToLower(strings.TrimSpace(in.Query))
	role := in.Role
	instances, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]IMAuditMessage, 0)
	for _, inst := range instances {
		sessions, err := s.store.ListSessions(p.TenantID, p.UserID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			for _, msg := range messages {
				if role != "" && msg.Role != role {
					continue
				}
				if in.Since != nil && msg.CreatedAt.Before(*in.Since) {
					continue
				}
				if in.Until != nil && msg.CreatedAt.After(*in.Until) {
					continue
				}
				msgPlatform := inferIMAuditPlatform(msg.Metadata, sess.Metadata)
				if msgPlatform == "" {
					continue
				}
				if platform != "" && msgPlatform != platform {
					continue
				}
				msgContact := inferIMAuditContact(msg.Metadata, sess.Metadata)
				if contact != "" && !strings.Contains(strings.ToLower(msgContact), contact) {
					continue
				}
				if query != "" && !imAuditMessageMatchesQuery(msg, sess, inst, msgPlatform, msgContact, query) {
					continue
				}
				out = append(out, IMAuditMessage{
					Message:      msg,
					SessionID:    sess.ID,
					SessionTitle: sess.Title,
					InstanceID:   inst.ID,
					InstanceName: inst.Name,
					Platform:     msgPlatform,
					ContactID:    msgContact,
					Metadata:     cloneMap(msg.Metadata),
					CreatedAt:    msg.CreatedAt,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Service) ListIMAuditContacts(ctx context.Context, p Principal, platform string) ([]IMAuditContact, error) {
	messages, err := s.ListIMAuditMessages(ctx, p, ListIMAuditMessagesInput{Platform: platform})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*IMAuditContact)
	for _, item := range messages {
		if item.Platform == "" || item.ContactID == "" {
			continue
		}
		key := item.Platform + "\x00" + item.ContactID
		contact := byKey[key]
		if contact == nil {
			contact = &IMAuditContact{Platform: item.Platform, ContactID: item.ContactID}
			byKey[key] = contact
		}
		contact.MessageCount++
		if item.CreatedAt.After(contact.LastMessageAt) {
			contact.LastMessageAt = item.CreatedAt
		}
		if contact.DisplayName == "" {
			contact.DisplayName = strings.TrimSpace(item.Metadata["im_user_name"])
		}
	}
	out := make([]IMAuditContact, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastMessageAt.Equal(out[j].LastMessageAt) {
			return out[i].LastMessageAt.After(out[j].LastMessageAt)
		}
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].ContactID < out[j].ContactID
	})
	return out, nil
}

func (s *Service) GetIMAuditStats(ctx context.Context, p Principal, in ListIMAuditMessagesInput) (*IMAuditStats, error) {
	messages, err := s.ListIMAuditMessages(ctx, p, in)
	if err != nil {
		return nil, err
	}
	stats := &IMAuditStats{ByPlatform: map[string]int{}}
	contacts := map[string]struct{}{}
	for _, item := range messages {
		stats.Messages++
		if item.Platform != "" {
			stats.ByPlatform[item.Platform]++
		}
		if item.Platform != "" && item.ContactID != "" {
			contacts[item.Platform+"\x00"+item.ContactID] = struct{}{}
		}
		if stats.LastMessageAt == nil || item.CreatedAt.After(*stats.LastMessageAt) {
			ts := item.CreatedAt
			stats.LastMessageAt = &ts
		}
	}
	stats.Contacts = len(contacts)
	stats.Platforms = len(stats.ByPlatform)
	return stats, nil
}

func (s *Service) DeleteIMAuditMessagesBefore(ctx context.Context, p Principal, in ListIMAuditMessagesInput, before time.Time) (*DeleteIMAuditMessagesOutput, error) {
	_ = ctx
	if before.IsZero() {
		return nil, fmt.Errorf("before is required")
	}
	platform := normalizeIMAuditPlatform(in.Platform)
	contact := strings.ToLower(strings.TrimSpace(in.Contact))
	query := strings.ToLower(strings.TrimSpace(in.Query))
	role := in.Role
	instances, err := s.store.ListInstances(p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	deleted := 0
	for _, inst := range instances {
		sessions, err := s.store.ListSessions(p.TenantID, p.UserID, inst.ID)
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0)
			for _, msg := range messages {
				if !msg.CreatedAt.Before(before) {
					continue
				}
				if role != "" && msg.Role != role {
					continue
				}
				if in.Since != nil && msg.CreatedAt.Before(*in.Since) {
					continue
				}
				if in.Until != nil && msg.CreatedAt.After(*in.Until) {
					continue
				}
				msgPlatform := inferIMAuditPlatform(msg.Metadata, sess.Metadata)
				if msgPlatform == "" {
					continue
				}
				if platform != "" && msgPlatform != platform {
					continue
				}
				msgContact := inferIMAuditContact(msg.Metadata, sess.Metadata)
				if contact != "" && !strings.Contains(strings.ToLower(msgContact), contact) {
					continue
				}
				if query != "" && !imAuditMessageMatchesQuery(msg, sess, inst, msgPlatform, msgContact, query) {
					continue
				}
				ids = append(ids, msg.ID)
			}
			n, err := s.store.DeleteMessages(sess.ID, ids)
			if err != nil {
				return nil, err
			}
			deleted += n
		}
	}
	if deleted > 0 {
		meta := map[string]string{"before": before.Format(time.RFC3339Nano), "deleted": strconv.Itoa(deleted)}
		if platform != "" {
			meta["platform"] = platform
		}
		if contact != "" {
			meta["contact"] = contact
		}
		if query != "" {
			meta["query"] = query
		}
		if role != "" {
			meta["role"] = string(role)
		}
		if in.Since != nil {
			meta["since"] = in.Since.Format(time.RFC3339Nano)
		}
		if in.Until != nil {
			meta["until"] = in.Until.Format(time.RFC3339Nano)
		}
		_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "im_audit.messages_deleted", ResourceType: "im_audit", ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: meta})
	}
	return &DeleteIMAuditMessagesOutput{Deleted: deleted, Before: before}, nil
}

func IMMessageMetadata(in IMMessageMetadataInput) map[string]string {
	metadata := cloneMap(in.Extra)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if platform := normalizeIMAuditPlatform(in.Platform); platform != "" {
		metadata["im_platform"] = platform
	}
	if contactID := strings.TrimSpace(in.ContactID); contactID != "" {
		metadata["contact_id"] = contactID
	}
	if messageID := strings.TrimSpace(in.MessageID); messageID != "" {
		metadata["im_message_id"] = messageID
	}
	if userName := strings.TrimSpace(in.UserName); userName != "" {
		metadata["im_user_name"] = userName
	}
	return metadata
}

func normalizeIMAuditPlatform(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "qq", "qqbot", "qq_bot":
		return "qq"
	case "wechat", "weixin", "wx":
		return "weixin"
	case "telegram", "tg":
		return "telegram"
	case "thirdparty", "third_party", "thirdparty_gateway":
		return "thirdparty"
	case "lansenger", "maclaw":
		return value
	default:
		return value
	}
}

func inferIMAuditPlatform(metas ...map[string]string) string {
	keys := []string{"im_platform", "platform", "channel", "source", "source_platform", "gateway"}
	for _, meta := range metas {
		for _, key := range keys {
			if value := normalizeIMAuditPlatform(metaValue(meta, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func inferIMAuditContact(metas ...map[string]string) string {
	keys := []string{"im_user_id", "contact_id", "external_user_id", "sender_id", "chat_id", "openid", "unionid", "conversation_id"}
	for _, meta := range metas {
		for _, key := range keys {
			if value := strings.TrimSpace(metaValue(meta, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func metaValue(meta map[string]string, key string) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(meta[key])
}

func imAuditMessageMatchesQuery(msg Message, sess Session, inst Instance, platform, contact, query string) bool {
	values := []string{msg.Content, string(msg.Role), msg.InputType, msg.OutputType, sess.Title, inst.Name, platform, contact}
	for k, v := range msg.Metadata {
		values = append(values, k, v)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
