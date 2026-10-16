package main

import (
	"context"
	"errors"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"strings"
	"sync"
	"time"
)

type weixinQRTokenRecord struct {
	TenantID  string
	UserID    string
	BaseURL   string
	ExpiresAt time.Time
}

type weixinQRTokenStore struct {
	mu     sync.Mutex
	tokens map[string]weixinQRTokenRecord
}

func (s *weixinQRTokenStore) Put(token string, rec weixinQRTokenRecord, now time.Time) []string {
	if s == nil || strings.TrimSpace(token) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	replaced := s.deletePrincipalLocked(rec.TenantID, rec.UserID)
	s.tokens[strings.TrimSpace(token)] = rec
	return replaced
}

func (s *weixinQRTokenStore) Get(token string, p agentservice.Principal, now time.Time) (weixinQRTokenRecord, bool) {
	if s == nil || strings.TrimSpace(token) == "" {
		return weixinQRTokenRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	rec, ok := s.tokens[strings.TrimSpace(token)]
	if !ok || rec.TenantID != p.TenantID || rec.UserID != p.UserID {
		return weixinQRTokenRecord{}, false
	}
	return rec, true
}

func (s *weixinQRTokenStore) Delete(token string) {
	if s == nil || strings.TrimSpace(token) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, strings.TrimSpace(token))
}

func (s *weixinQRTokenStore) pruneLocked(now time.Time) {
	for token, rec := range s.tokens {
		if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
			delete(s.tokens, token)
		}
	}
}

func (s *weixinQRTokenStore) deletePrincipalLocked(tenantID, userID string) []string {
	var replaced []string
	for token, rec := range s.tokens {
		if rec.TenantID == tenantID && rec.UserID == userID {
			replaced = append(replaced, token)
			delete(s.tokens, token)
		}
	}
	return replaced
}

func (s *HTTPServer) startConfiguredAIModelDownloads(ctx context.Context) {
	if s == nil {
		return
	}
	s.ensureConfiguredAIModelsAsync(s.defaultConfigForAIModels(ctx))
}

func (s *HTTPServer) startConfiguredIMRuntimes(ctx context.Context) {
	if s == nil || s.imRuntime == nil || s.svc == nil {
		return
	}
	activeTenants, err := s.activeTenantSet(ctx)
	if err != nil {
		return
	}
	users, err := s.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return
	}
	for _, user := range users {
		p := agentservice.Principal{TenantID: user.TenantID, UserID: user.ID}
		if _, ok := activeTenants[p.TenantID]; !ok {
			s.stopIMRuntimeForPrincipal(p)
			continue
		}
		cfg, err := s.svc.GetRawUserConfig(ctx, p)
		if err != nil || cfg == nil {
			continue
		}
		s.imRuntime.SyncPrincipal(ctx, p, cfg.AppConfig)
	}
}

func (s *HTTPServer) startConfiguredIMRuntimesForTenant(ctx context.Context, tenantID string) {
	if s == nil || s.imRuntime == nil || s.svc == nil || strings.TrimSpace(tenantID) == "" {
		return
	}
	users, err := s.svc.ListUsers(ctx, tenantID, agentservice.ListUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return
	}
	for _, user := range users {
		s.syncIMRuntimeFromRawConfig(ctx, agentservice.Principal{TenantID: tenantID, UserID: user.ID})
	}
}

func (s *HTTPServer) startConfiguredWeixinRuntimes(ctx context.Context) {
	if s == nil || s.weixinRuntime == nil || s.svc == nil {
		return
	}
	activeTenants, err := s.activeTenantSet(ctx)
	if err != nil {
		return
	}
	users, err := s.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return
	}
	for _, user := range users {
		if _, ok := activeTenants[user.TenantID]; !ok {
			continue
		}
		p := agentservice.Principal{TenantID: user.TenantID, UserID: user.ID}
		cfg, err := s.svc.GetRawUserConfig(ctx, p)
		if err != nil || cfg == nil || !cfg.AppConfig.WeixinEnabled || strings.TrimSpace(cfg.AppConfig.WeixinToken) == "" {
			continue
		}
		s.weixinRuntime.SyncPrincipal(ctx, p, cfg.AppConfig)
	}
}

func (s *HTTPServer) activeTenantSet(ctx context.Context) (map[string]struct{}, error) {
	if s == nil || s.svc == nil {
		return nil, errors.New("service is not available")
	}
	tenants, err := s.svc.ListTenants(ctx, agentservice.ListTenantsInput{Status: agentservice.TenantStatusActive})
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		out[tenant.ID] = struct{}{}
	}
	return out, nil
}

func (s *HTTPServer) startConfiguredWeixinRuntimesForTenant(ctx context.Context, tenantID string) {
	if s == nil || s.weixinRuntime == nil || s.svc == nil || strings.TrimSpace(tenantID) == "" {
		return
	}
	tenant, err := s.svc.GetTenant(ctx, tenantID)
	if err != nil || tenant == nil || tenant.Status != agentservice.TenantStatusActive {
		return
	}
	users, err := s.svc.ListUsers(ctx, tenantID, agentservice.ListUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return
	}
	for _, user := range users {
		p := agentservice.Principal{TenantID: tenantID, UserID: user.ID}
		cfg, err := s.svc.GetRawUserConfig(ctx, p)
		if err != nil || cfg == nil || !cfg.AppConfig.WeixinEnabled || strings.TrimSpace(cfg.AppConfig.WeixinToken) == "" {
			continue
		}
		s.weixinRuntime.SyncPrincipal(ctx, p, cfg.AppConfig)
	}
}

func (s *HTTPServer) syncWeixinRuntimeFromRawConfig(ctx context.Context, p agentservice.Principal) {
	if s == nil || s.weixinRuntime == nil || s.svc == nil {
		return
	}
	if !s.isActivePrincipal(ctx, p) {
		s.stopWeixinRuntimeForPrincipal(p)
		return
	}
	cfg, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil || cfg == nil {
		return
	}
	s.weixinRuntime.SyncPrincipal(ctx, p, cfg.AppConfig)
}

func (s *HTTPServer) syncIMRuntimeFromRawConfig(ctx context.Context, p agentservice.Principal) {
	if s == nil || s.imRuntime == nil || s.svc == nil {
		return
	}
	if !s.isActivePrincipal(ctx, p) {
		s.stopIMRuntimeForPrincipal(p)
		return
	}
	cfg, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil || cfg == nil {
		return
	}
	s.imRuntime.SyncPrincipal(ctx, p, cfg.AppConfig)
}

func (s *HTTPServer) isActivePrincipal(ctx context.Context, p agentservice.Principal) bool {
	if s == nil || s.svc == nil {
		return false
	}
	tenant, err := s.svc.GetTenant(ctx, p.TenantID)
	if err != nil || tenant == nil || tenant.Status != agentservice.TenantStatusActive {
		return false
	}
	user, err := s.svc.GetUser(ctx, p.TenantID, p.UserID)
	if err != nil || user == nil || user.Status != agentservice.UserStatusActive {
		return false
	}
	return true
}

func (s *HTTPServer) stopWeixinRuntimeForPrincipal(p agentservice.Principal) {
	if s == nil || s.weixinRuntime == nil {
		return
	}
	s.weixinRuntime.StopPrincipal(p)
}

func (s *HTTPServer) stopIMRuntimeForPrincipal(p agentservice.Principal) {
	if s == nil || s.imRuntime == nil {
		return
	}
	s.imRuntime.StopPrincipal(p)
}

func (s *HTTPServer) stopThirdPartyIMForPrincipal(p agentservice.Principal) {
	if s == nil || s.thirdPartyIM == nil {
		return
	}
	s.thirdPartyIM.StopPrincipal(p)
}

func (s *HTTPServer) syncThirdPartyIMFromRawConfig(ctx context.Context, p agentservice.Principal) {
	if s == nil || s.thirdPartyIM == nil || s.svc == nil {
		return
	}
	if !s.isActivePrincipal(ctx, p) {
		s.stopThirdPartyIMForPrincipal(p)
		return
	}
	cfg, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil || cfg == nil || !cfg.AppConfig.ThirdPartyGatewayEnabled || strings.TrimSpace(cfg.AppConfig.ThirdPartyGatewayToken) == "" {
		s.stopThirdPartyIMForPrincipal(p)
	}
}

func (s *HTTPServer) syncThirdPartyIMConfigTransition(p agentservice.Principal, before, after corelib.AppConfig) {
	if s == nil || s.thirdPartyIM == nil {
		return
	}
	beforeToken := strings.TrimSpace(before.ThirdPartyGatewayToken)
	afterToken := strings.TrimSpace(after.ThirdPartyGatewayToken)
	if !after.ThirdPartyGatewayEnabled || afterToken == "" || beforeToken != afterToken {
		s.stopThirdPartyIMForPrincipal(p)
	}
}

func (s *HTTPServer) stopWeixinRuntimesForTenant(ctx context.Context, tenantID string) {
	if s == nil || s.weixinRuntime == nil || s.svc == nil {
		return
	}
	users, err := s.svc.ListUsers(ctx, tenantID, agentservice.ListUsersAdminInput{})
	if err != nil {
		return
	}
	for _, user := range users {
		s.stopWeixinRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: user.ID})
	}
}

func (s *HTTPServer) stopIMRuntimesForTenant(ctx context.Context, tenantID string) {
	if s == nil || s.imRuntime == nil || s.svc == nil {
		return
	}
	users, err := s.svc.ListUsers(ctx, tenantID, agentservice.ListUsersAdminInput{})
	if err != nil {
		return
	}
	for _, user := range users {
		s.stopIMRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: user.ID})
	}
}

func (s *HTTPServer) stopThirdPartyIMForTenant(tenantID string) {
	if s == nil || s.thirdPartyIM == nil {
		return
	}
	s.thirdPartyIM.StopTenant(tenantID)
}

func (s *HTTPServer) rawWeixinAppConfig(ctx context.Context, p agentservice.Principal) (corelib.AppConfig, error) {
	if s == nil || s.svc == nil {
		return corelib.AppConfig{}, errors.New("service is not available")
	}
	cfg, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			return corelib.AppConfig{}, nil
		}
		return corelib.AppConfig{}, err
	}
	if cfg == nil {
		return corelib.AppConfig{}, nil
	}
	return cfg.AppConfig, nil
}
