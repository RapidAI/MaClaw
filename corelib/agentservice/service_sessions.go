package agentservice

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (s *Service) CreateSession(ctx context.Context, p Principal, instanceID string, in CreateSessionInput) (*Session, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	if err := s.enforceQuotaLimit(p.TenantID, p.UserID, quotaMetricSessions); err != nil {
		return nil, err
	}
	now := s.now()
	agentID := strings.TrimSpace(in.AgentID)
	if agentID == "" {
		agentID = "default"
	}
	sess := Session{ID: NewID("sess"), TenantID: p.TenantID, UserID: p.UserID, InstanceID: instanceID, AgentID: agentID, Title: strings.TrimSpace(in.Title), Metadata: cloneMap(in.Metadata), CreatedAt: now, UpdatedAt: now}
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.created", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ListSessions(ctx context.Context, p Principal, instanceID string, in ListSessionsInput) ([]Session, error) {
	_ = ctx
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	items, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	if !in.IncludeArchived {
		filtered := make([]Session, 0, len(items))
		for _, item := range items {
			if item.Archived {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}
	return s.enrichSessions(items)
}

func (s *Service) GetSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	v, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	enriched, err := s.enrichSession(v)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) UpdateSession(ctx context.Context, p Principal, instanceID, sessionID string, in UpdateSessionInput) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	changed := false
	if in.Title != nil {
		sess.Title = strings.TrimSpace(*in.Title)
		changed = true
	}
	if in.Metadata != nil {
		sess.Metadata = cloneMap(in.Metadata)
		changed = true
	}
	if !changed {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	sess.UpdatedAt = s.now()
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.updated", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) ArchiveSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.Archived {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	now := s.now()
	sess.Archived = true
	sess.ArchivedAt = &now
	sess.UpdatedAt = now
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.archived", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) RestoreSession(ctx context.Context, p Principal, instanceID, sessionID string) (*Session, error) {
	_ = ctx
	sess, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return nil, err
	}
	if !sess.Archived {
		enriched, enrichErr := s.enrichSession(sess)
		if enrichErr != nil {
			return nil, enrichErr
		}
		return &enriched, nil
	}
	sess.Archived = false
	sess.ArchivedAt = nil
	sess.UpdatedAt = s.now()
	if err := s.store.SaveSession(sess); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.restored", ResourceType: "session", ResourceID: sess.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	enriched, err := s.enrichSession(sess)
	if err != nil {
		return nil, err
	}
	return &enriched, nil
}

func (s *Service) DeleteSession(ctx context.Context, p Principal, instanceID, sessionID string) error {
	_ = ctx
	if _, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return err
	}
	busy, err := s.hasRunningRuns(p.TenantID, p.UserID, instanceID, sessionID)
	if err != nil {
		return err
	}
	if busy {
		return ErrSessionBusy
	}
	botIDs := make(map[string]struct{})
	if messages, listErr := s.store.ListMessages(sessionID); listErr == nil {
		for _, message := range messages {
			if botID := strings.TrimSpace(message.Metadata["bot_id"]); botID != "" {
				botIDs[botID] = struct{}{}
			}
		}
	}
	if err := s.store.DeleteSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return err
	}
	for botID := range botIDs {
		agent.DropConversationPrefix(botID, sessionID)
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "session.deleted", ResourceType: "session", ResourceID: sessionID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID}})
	return nil
}

func (s *Service) GetMessage(ctx context.Context, p Principal, instanceID, sessionID, messageID string) (*Message, error) {
	_ = ctx
	if _, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return nil, err
	}
	message, err := s.store.GetMessage(sessionID, messageID)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func (s *Service) ListMessages(ctx context.Context, p Principal, instanceID, sessionID string, in ListMessagesInput) ([]Message, error) {
	_ = ctx
	if _, err := s.store.GetSession(p.TenantID, p.UserID, instanceID, sessionID); err != nil {
		return nil, err
	}
	items, err := s.store.ListMessages(sessionID)
	if err != nil {
		return nil, err
	}
	role := in.Role
	filtered := make([]Message, 0, len(items))
	for _, item := range items {
		if role != "" && item.Role != role {
			continue
		}
		if in.Since != nil && item.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && item.CreatedAt.After(*in.Until) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}
