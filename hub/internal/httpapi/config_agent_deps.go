package httpapi

import (
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
)

// ConfigAgentDeps holds optional runtime dependencies for config-agent tools
// beyond system settings (invites, identity, invitation codes).
type ConfigAgentDeps struct {
	System    store.SystemSettingsRepository
	Audit     store.AdminAuditRepository
	Invites   store.EmailInviteRepository
	Identity  *auth.IdentityService
	Codes     *invitation.Service
	Security  *security.SecurityService
	Feishu    *feishu.Notifier
	WeCom     *wecom.Plugin
	DingTalk  *dingtalk.Plugin
	QQBot     *qqbot.Plugin
	IMRuntime TenantIMRuntimeReloader
	BridgeDir string
}

type configAgentSession struct {
	SessionID   string
	AdminUserID string
	TenantID    string
	PendingPlan *configAgentPlan
	History     []string
	ExpiresAt   time.Time
}

type configAgentSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*configAgentSession
}

var globalConfigAgentSessions = &configAgentSessionStore{sessions: map[string]*configAgentSession{}}

func (s *configAgentSessionStore) put(sess *configAgentSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, item := range s.sessions {
		if now.After(item.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	s.sessions[sess.SessionID] = sess
}

func (s *configAgentSessionStore) get(id string) *configAgentSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.sessions[id]
	if item == nil {
		return nil
	}
	if time.Now().After(item.ExpiresAt) {
		delete(s.sessions, id)
		return nil
	}
	return item
}

func (s *configAgentSessionStore) clear(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}
