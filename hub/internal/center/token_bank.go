package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// Token Bank pull client. The hub debits HubCenter with its machine secret,
// then writes a local permanent grant. Desktop callers must come through the
// hub so the grant lands where the agent bills. See the design doc, section 14.

type tokenBankCenterEnvelope struct {
	Status      string `json:"status"`
	Created     bool   `json:"created"`
	RequestID   string `json:"request_id"`
	AmountMicro int64  `json:"amount_micro"`
	HubID       string `json:"hub_id"`
	GrantID     string `json:"grant_id"`
	State       string `json:"state"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Withdrawn   int64  `json:"withdrawn_micro"`
}

func (s *Service) WithdrawTokenBank(ctx context.Context, req llmservice.TokenBankCenterWithdraw) (llmservice.TokenBankCenterWithdrawal, error) {
	var zero llmservice.TokenBankCenterWithdrawal
	body := map[string]any{
		"email":            strings.TrimSpace(req.Email),
		"request_id":       strings.TrimSpace(req.RequestID),
		"service_group_id": strings.TrimSpace(req.ServiceGroupID),
		"amount_micro":     req.AmountMicro,
		"manual":           req.Manual,
		"kind":             strings.TrimSpace(req.Kind),
		"link_id":          strings.TrimSpace(req.LinkID),
	}
	env, err := s.tokenBankCenterCall(ctx, http.MethodPost, "/token-bank/withdraw", body)
	if err != nil {
		return zero, err
	}
	return llmservice.TokenBankCenterWithdrawal{
		Created:     env.Created,
		RequestID:   env.RequestID,
		AmountMicro: env.AmountMicro,
		GrantID:     env.GrantID,
		Status:      env.State,
		HubID:       env.HubID,
	}, nil
}

func (s *Service) FinishTokenBankGrant(ctx context.Context, requestID, grantID string, reissue bool) error {
	_, err := s.tokenBankCenterCall(ctx, http.MethodPost, "/token-bank/grants", map[string]any{
		"request_id": strings.TrimSpace(requestID),
		"grant_id":   strings.TrimSpace(grantID),
		"reissue":    reissue,
	})
	return err
}

// TokenBankHubWithdrawnMicro is the integer total this hub has already pulled
// for the email. Reconcile compares it with local grant credits.
func (s *Service) TokenBankHubWithdrawnMicro(ctx context.Context, email string) (int64, error) {
	env, err := s.tokenBankCenterCall(ctx, http.MethodPost, "/token-bank/reconcile", map[string]any{
		"email": strings.TrimSpace(email),
	})
	if err != nil {
		return 0, err
	}
	return env.Withdrawn, nil
}

func (s *Service) tokenBankCenterCall(ctx context.Context, method, suffix string, body any) (tokenBankCenterEnvelope, error) {
	var zero tokenBankCenterEnvelope
	if s == nil || s.settings == nil {
		return zero, fmt.Errorf("hub center client is not configured")
	}
	record, err := s.loadRegistration(ctx)
	if err != nil {
		return zero, err
	}
	if !record.Registered || strings.TrimSpace(record.HubID) == "" || strings.TrimSpace(record.HubSecret) == "" {
		return zero, fmt.Errorf("hub is not registered with HubCenter")
	}
	baseURLs, err := s.orderedCenterBaseURLs(ctx, record.LastBaseURL)
	if err != nil {
		return zero, err
	}
	if len(baseURLs) == 0 {
		return zero, fmt.Errorf("hub center base url is required")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}
	path := "/api/hubs/" + url.PathEscape(record.HubID) + suffix
	var lastErr error
	for _, baseURL := range baseURLs {
		env, status, callErr := s.tokenBankCenterOnce(ctx, method, baseURL+path, record.HubSecret, payload)
		if callErr != nil {
			lastErr = callErr
			continue
		}
		if status >= 200 && status < 300 {
			return env, nil
		}
		if env.Code == "insufficient_credits" && strings.Contains(strings.ToLower(env.Message), "nothing to withdraw") {
			return zero, llmservice.ErrTokenBankNothingToWithdraw
		}
		if env.Code == "insufficient_credits" {
			// Over the 1/N cap. Distinct from an empty balance so the desktop
			// can authorize a manual withdrawal and replay this request id.
			return zero, llmservice.ErrTokenBankInsufficient
		}
		message := strings.TrimSpace(env.Message)
		if message == "" {
			message = http.StatusText(status)
		}
		if env.Code != "" {
			message = env.Code + ": " + message
		}
		return zero, fmt.Errorf("hub center token bank: %s", message)
	}
	if lastErr != nil {
		return zero, lastErr
	}
	return zero, fmt.Errorf("hub center token bank request failed")
}

func (s *Service) tokenBankCenterOnce(ctx context.Context, method, rawURL, secret string, payload []byte) (tokenBankCenterEnvelope, int, error) {
	var env tokenBankCenterEnvelope
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(payload))
	if err != nil {
		return env, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return env, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return env, resp.StatusCode, err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &env); err != nil {
			return env, resp.StatusCode, fmt.Errorf("decode hub center token bank response: %w", err)
		}
	}
	return env, resp.StatusCode, nil
}

// RunTokenBankAutoOnce pulls for every local user who has no spendable
// credits left, including a new-user period window. It is the default-on
// background path. Amount zero and manual false ask HubCenter for the 1/N cap.
func (s *Service) RunTokenBankAutoOnce(ctx context.Context) error {
	if s == nil || s.settings == nil || s.users == nil {
		return nil
	}
	cfg, err := llmservice.LoadTokenBankAutoSettings(ctx, s.settings)
	if err != nil || !cfg.Enabled {
		return err
	}
	groupID, err := llmservice.ResolveTokenBankServiceGroup(ctx, s.settings, cfg.ServiceGroupID)
	if err != nil || groupID == "" {
		return err
	}
	record, err := s.loadRegistration(ctx)
	if err != nil || strings.TrimSpace(record.HubID) == "" {
		return err
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		return err
	}
	reg, err := llmservice.LoadRegistry(ctx, s.settings)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var first error
	for _, user := range users {
		if user == nil || strings.TrimSpace(user.Email) == "" {
			continue
		}
		// The service-account row is created for API keys. It has no token-bank
		// account, so a pull fails every tick and that error hides a later user.
		if llmservice.IsSystemLLMUser(user.ID, user.Email) {
			continue
		}
		if llmservice.UserHasAvailableCredits(ctx, reg, user.ID, user.Email, now) {
			continue
		}
		seq, seqErr := llmservice.TokenBankAutoSeq(ctx, s.settings, user.Email, groupID)
		if seqErr != nil {
			first = preferErr(first, seqErr)
			continue
		}
		requestID := llmservice.TokenBankAutoRequestID(record.HubID, user.Email, groupID, seq)
		_, pullErr := llmservice.PullTokenBankGrant(ctx, s.settings, s, user.ID, user.Email, groupID, requestID, 0, false, "self", "")
		if pullErr != nil {
			if !errors.Is(pullErr, llmservice.ErrTokenBankNothingToWithdraw) {
				first = preferErr(first, pullErr)
			}
			continue
		}
		if err := llmservice.AdvanceTokenBankAutoSeq(ctx, s.settings, user.Email, groupID, seq); err != nil {
			first = preferErr(first, err)
		}
		reg, err = llmservice.LoadRegistry(ctx, s.settings)
		if err != nil {
			return err
		}
	}
	return first
}

func preferErr(current, next error) error {
	if current != nil {
		return current
	}
	return next
}

// compile-time check that the center client satisfies the pull interface.
var _ llmservice.TokenBankCenter = (*Service)(nil)
