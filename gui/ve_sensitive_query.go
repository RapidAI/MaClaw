package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	digitalEmployeeSensitivePolicyConfirm = "confirm"
	digitalEmployeeSensitivePolicyDeny    = "deny"
	digitalEmployeeSensitivePolicyAllow   = "allow"
)

type digitalEmployeeSensitiveRequest struct {
	RequestID      string `json:"request_id"`
	SessionID      string `json:"session_id"`
	Query          string `json:"query"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type digitalEmployeeSensitiveApprovalStore struct {
	mu      sync.Mutex
	pending map[string]chan bool
}

var veSensitiveApprovals digitalEmployeeSensitiveApprovalStore

func normalizeDigitalEmployeeSensitivePolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case digitalEmployeeSensitivePolicyDeny, digitalEmployeeSensitivePolicyAllow, digitalEmployeeSensitivePolicyConfirm:
		return strings.TrimSpace(policy)
	default:
		return digitalEmployeeSensitivePolicyConfirm
	}
}

func (a *App) GetDigitalEmployeeSensitiveQueryPolicy() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return digitalEmployeeSensitivePolicyConfirm
	}
	return normalizeDigitalEmployeeSensitivePolicy(cfg.GroupDiscussion.SensitiveQueryPolicy)
}

func (a *App) SaveDigitalEmployeeSensitiveQueryPolicy(policy string) error {
	policy = normalizeDigitalEmployeeSensitivePolicy(policy)
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.GroupDiscussion.SensitiveQueryPolicy = policy
	})
}

func (s *digitalEmployeeSensitiveApprovalStore) register(requestID string) chan bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[string]chan bool)
	}
	ch := make(chan bool, 1)
	s.pending[requestID] = ch
	return ch
}

func (s *digitalEmployeeSensitiveApprovalStore) resolve(requestID string, allowed bool) bool {
	s.mu.Lock()
	ch, ok := s.pending[requestID]
	if ok {
		delete(s.pending, requestID)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- allowed:
	default:
	}
	return true
}

func (s *digitalEmployeeSensitiveApprovalStore) remove(requestID string) {
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
}

func (a *App) RespondDigitalEmployeeSensitiveRequest(requestID, decision string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return fmt.Errorf("request_id is required")
	}
	switch strings.TrimSpace(decision) {
	case "allow":
		if !veSensitiveApprovals.resolve(requestID, true) {
			return fmt.Errorf("request is no longer pending")
		}
	case "deny":
		if !veSensitiveApprovals.resolve(requestID, false) {
			return fmt.Errorf("request is no longer pending")
		}
	default:
		return fmt.Errorf("decision must be allow or deny")
	}
	return nil
}

var (
	veSensitiveKeywordRe = regexp.MustCompile(`(?i)(密码|口令|凭证|密钥|访问令牌|令牌|api\s*key|secret|password|credential|token|private\s*key)`)
	veSensitiveVerbRe    = regexp.MustCompile(`(?i)(查询|查看|显示|提供|发给|告诉|读取|获取|要|给我|show|get|provide|tell|reveal|send|read|lookup|query)`)
)

func detectDigitalEmployeeSensitiveQuery(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !veSensitiveKeywordRe.MatchString(trimmed) {
		return "", false
	}
	if !veSensitiveVerbRe.MatchString(trimmed) && !strings.ContainsAny(trimmed, "?？") {
		return "", false
	}
	query := trimmed
	if len([]rune(query)) > 300 {
		query = string([]rune(query)[:300]) + "..."
	}
	return query, true
}

func (h *VEMessageHandler) authorizeSensitiveQuery(ctx context.Context, sessionID, query string) bool {
	if h == nil || h.app == nil {
		return false
	}
	switch h.app.GetDigitalEmployeeSensitiveQueryPolicy() {
	case digitalEmployeeSensitivePolicyAllow:
		return true
	case digitalEmployeeSensitivePolicyDeny:
		return false
	}

	requestID := fmt.Sprintf("ve_sensitive_%d", time.Now().UnixNano())
	ch := veSensitiveApprovals.register(requestID)
	defer veSensitiveApprovals.remove(requestID)

	h.app.emitEvent("digital-employee-sensitive-request", digitalEmployeeSensitiveRequest{
		RequestID:      requestID,
		SessionID:      sessionID,
		Query:          query,
		TimeoutSeconds: 60,
	})

	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	select {
	case allowed := <-ch:
		return allowed
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}
