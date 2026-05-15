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
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case digitalEmployeeSensitivePolicyDeny:
		return digitalEmployeeSensitivePolicyDeny
	case digitalEmployeeSensitivePolicyAllow:
		return digitalEmployeeSensitivePolicyAllow
	case digitalEmployeeSensitivePolicyConfirm:
		return digitalEmployeeSensitivePolicyConfirm
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
	switch strings.ToLower(strings.TrimSpace(decision)) {
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
	veSensitiveKeywordRe  = regexp.MustCompile(`(?i)(\x{5bc6}\x{7801}|\x{53e3}\x{4ee4}|\x{51ed}\x{8bc1}|\x{5bc6}\x{94a5}|\x{8bbf}\x{95ee}\x{4ee4}\x{724c}|\x{4ee4}\x{724c}|api\s*key|secret|password|credential|token|private\s*key)`)
	veSensitiveVerbRe     = regexp.MustCompile(`(?i)(\x{67e5}\x{8be2}|\x{67e5}\x{770b}|\x{663e}\x{793a}|\x{63d0}\x{4f9b}|\x{53d1}\x{7ed9}|\x{8bfb}\x{53d6}|\x{544a}\x{8bc9}|\x{83b7}\x{53d6}|\x{8981}|\x{7ed9}\x{6211}|show|get|provide|tell|reveal|send|read|lookup|query)`)
	veSensitiveQuestionRe = regexp.MustCompile(`(?i)(\x{591a}\x{5c11}|\x{662f}\x{4ec0}\x{4e48}|\x{662f}\x{591a}\x{5c11}|\x{67e5}\x{4e00}\x{4e0b}|\x{770b}\x{4e00}\x{4e0b}|\x{7ed9}\x{4e00}\x{4e0b}|\x{53d1}\x{4e00}\x{4e0b}|what\s*(?:is|'s))`)
)

func detectDigitalEmployeeSensitiveQuery(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !veSensitiveKeywordRe.MatchString(trimmed) {
		return "", false
	}
	if !veSensitiveVerbRe.MatchString(trimmed) && !veSensitiveQuestionRe.MatchString(trimmed) && !strings.ContainsAny(trimmed, "\u003f\uff1f") {
		return "", false
	}
	query := trimmed
	if len([]rune(query)) > 300 {
		query = string([]rune(query)[:300]) + "..."
	}
	return query, true
}

func (h *VEMessageHandler) shouldAnnounceSensitivePermissionRequest() bool {
	if h == nil || h.app == nil {
		return false
	}
	return h.app.GetDigitalEmployeeSensitiveQueryPolicy() == digitalEmployeeSensitivePolicyConfirm
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
