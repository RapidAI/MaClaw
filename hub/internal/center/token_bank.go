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

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
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
// A positive remainder does not top up here. A request that cannot start on
// that remainder pulls through PullTokenBankForAdmissionShortfall instead.
//
// An automatic id whose grant is already stored is confirmed here, including
// while other credits remain. The confirm calls Finish only. A withdraw of
// that id would replay the debit. The stored sequence moves only after that
// confirm. A request that still cannot start uses the next open id.
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
	var errs []error
	for _, user := range users {
		if user == nil || strings.TrimSpace(user.Email) == "" {
			continue
		}
		// The service-account row is created for API keys. It has no token-bank
		// account, so a pull fails every tick. Each user's error is kept; one
		// missing account must not hide a later confirm failure.
		if llmservice.IsSystemLLMUser(user.ID, user.Email) {
			continue
		}
		// The confirm call can take a round trip. The sequence is read again
		// inside that call. Reload the registry and the clock afterwards: the
		// snapshot and timestamp from before the call still say this user has
		// nothing to spend, so the fresh head would withdraw another share.
		reg, err := llmservice.LoadRegistry(ctx, s.settings)
		if err != nil {
			errs = append(errs, err)
			return errors.Join(errs...)
		}
		seq, ackErr := s.ackDeliveredAutoSeqs(ctx, reg, record.HubID, user.Email, groupID)
		if ackErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", user.Email, ackErr))
		}
		if seq < 0 {
			continue
		}
		reg, err = llmservice.LoadRegistry(ctx, s.settings)
		if err != nil {
			errs = append(errs, err)
			return errors.Join(errs...)
		}
		if llmservice.UserHasAvailableCredits(ctx, reg, user.ID, user.Email, time.Now().UTC()) {
			continue
		}
		openSeq, requestID, open := firstOpenAutoRequest(reg, record.HubID, user.Email, groupID, seq)
		if !open {
			continue
		}
		_, pullErr := llmservice.PullTokenBankGrant(ctx, s.settings, s, user.ID, user.Email, groupID, requestID, 0, false, "self", "")
		if pullErr != nil {
			if !errors.Is(pullErr, llmservice.ErrTokenBankNothingToWithdraw) {
				errs = append(errs, fmt.Errorf("%s: %w", user.Email, pullErr))
			}
			continue
		}
		// Advance only the head. An earlier id whose confirm failed stays the
		// head, and the share just written is a later id. The next tick confirms
		// the head and then this share, without debiting either one again.
		if openSeq == seq {
			if err := llmservice.AdvanceTokenBankAutoSeq(ctx, s.settings, user.Email, groupID, seq); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", user.Email, err))
			}
		}
	}
	return errors.Join(errs...)
}

// tokenBankAutoAckWindow is how many already-written automatic ids one pass
// confirms. Each confirm is a HubCenter call. The stored head stays on the
// first one that fails.
const tokenBankAutoAckWindow = 8

// tokenBankAutoOpenWindow is how far past a stuck head a debit may look for
// an id that has no local grant. Confirming the head can lag behind funding.
// Stopping at the confirm window would refuse the bank after a handful of
// shares while that head is still unconfirmed.
const tokenBankAutoOpenWindow = 64

func tokenBankAckSettled(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already bound") || strings.Contains(msg, "withdrawal_not_found")
}

// ackDeliveredAutoSeqs confirms stored automatic ids whose grants are already
// local, and advances the stored sequence only after each confirm. The first
// failed confirm stays the head so the next pass retries it. A compare-and-set
// that does not move also stops the walk. A negative sequence means the head
// could not be read; callers must not debit sequence 0 in that case.
func (s *Service) ackDeliveredAutoSeqs(ctx context.Context, reg *llmservice.Registry, hubID, email, groupID string) (int64, error) {
	seq, err := llmservice.TokenBankAutoSeq(ctx, s.settings, email, groupID)
	if err != nil {
		// Negative means the head is unknown. Callers must not fund seq 0.
		return -1, err
	}
	var errs []error
	for n := 0; n < tokenBankAutoAckWindow; n++ {
		requestID := llmservice.TokenBankAutoRequestID(hubID, email, groupID, seq)
		grantID := llmservice.TokenBankGrantIDForRequest(reg, requestID)
		if grantID == "" {
			break
		}
		if err := s.FinishTokenBankGrant(ctx, requestID, grantID, false); err != nil && !tokenBankAckSettled(err) {
			errs = append(errs, fmt.Errorf("confirm %s: %w", requestID, err))
			break
		}
		if err := llmservice.AdvanceTokenBankAutoSeq(ctx, s.settings, email, groupID, seq); err != nil {
			return seq, err
		}
		stored, err := llmservice.TokenBankAutoSeq(ctx, s.settings, email, groupID)
		if err != nil {
			return -1, err
		}
		if stored == seq {
			break
		}
		seq = stored
	}
	return seq, errors.Join(errs...)
}

// firstOpenAutoRequest is the first automatic id at or after seq that has no
// local grant. That id is safe to debit. An id that already has a grant has
// delivered its credits, spent or not.
func firstOpenAutoRequest(reg *llmservice.Registry, hubID, email, groupID string, seq int64) (int64, string, bool) {
	if seq < 0 {
		return 0, "", false
	}
	for n := 0; n < tokenBankAutoOpenWindow; n++ {
		requestID := llmservice.TokenBankAutoRequestID(hubID, email, groupID, seq)
		if llmservice.TokenBankGrantIDForRequest(reg, requestID) == "" {
			return seq, requestID, true
		}
		seq++
	}
	return 0, "", false
}

// PullTokenBankForAdmissionShortfall withdraws one automatic 1/N share into
// the configured token-bank group. The caller has already decided that this
// user's charged balance cannot start the request. The heartbeat still does
// not top up a positive balance. The group must be one this request charges.
// A mismatch returns ErrTokenBankGroupNotCharged without contacting the bank,
// so another model on the same request can still be tried. Amount zero and
// manual false ask HubCenter for the cap. Nothing left and an over-cap reply
// are an empty result, not a failure the request should surface.
//
// An automatic id whose grant is already stored has delivered its credits,
// whether or not they have since been spent. Replaying it cannot fund this
// request. A failed confirm leaves that id as the stored head and the debit
// uses the next open id, so the bank is still withdrawn.
func (s *Service) PullTokenBankForAdmissionShortfall(ctx context.Context, userID, email string, chargedGroupIDs []string, callerSpendMicro int64) (bool, error) {
	if s == nil || s.settings == nil {
		return false, nil
	}
	email = strings.TrimSpace(email)
	if email == "" || llmservice.IsSystemLLMUser(userID, email) {
		return false, nil
	}
	cfg, err := llmservice.LoadTokenBankAutoSettings(ctx, s.settings)
	if err != nil || !cfg.Enabled {
		return false, err
	}
	groupID, err := llmservice.ResolveTokenBankServiceGroup(ctx, s.settings, cfg.ServiceGroupID)
	if err != nil || groupID == "" {
		return false, err
	}
	if !chargedGroupListContains(groupID, chargedGroupIDs) {
		return false, llmservice.ErrTokenBankGroupNotCharged
	}
	record, err := s.loadRegistration(ctx)
	if err != nil || strings.TrimSpace(record.HubID) == "" {
		return false, err
	}
	reg, err := llmservice.LoadRegistry(ctx, s.settings)
	if err != nil {
		return false, err
	}
	// Confirm is a HubCenter round trip. A share that lands during that call is
	// already on the reloaded registry. Debiting the next open id would take a
	// second share after the caller has new credits to reprice.
	beforeSpend := tokenBankChargedSpendableMicro(reg, userID, email, chargedGroupIDs)
	seq, ackErr := s.ackDeliveredAutoSeqs(ctx, reg, record.HubID, email, groupID)
	if seq < 0 {
		return false, ackErr
	}
	reg, err = llmservice.LoadRegistry(ctx, s.settings)
	if err != nil {
		return false, err
	}
	// beforeSpend misses a share that landed after the caller measured the
	// card and before this load. callerSpendMicro is that measurement. Either
	// increase means the caller can reprice, and the next open id stays put.
	nowSpend := tokenBankChargedSpendableMicro(reg, userID, email, chargedGroupIDs)
	if nowSpend > beforeSpend || nowSpend > callerSpendMicro {
		return true, nil
	}
	openSeq, requestID, open := firstOpenAutoRequest(reg, record.HubID, email, groupID, seq)
	if !open {
		if ackErr != nil {
			return false, ackErr
		}
		return false, nil
	}
	_, err = llmservice.PullTokenBankGrant(ctx, s.settings, s, userID, email, groupID, requestID, 0, false, "self", "")
	if err != nil {
		if errors.Is(err, llmservice.ErrTokenBankNothingToWithdraw) || errors.Is(err, llmservice.ErrTokenBankInsufficient) {
			return false, nil
		}
		if errors.Is(err, llmservice.ErrTokenBankGrantPending) {
			fresh, loadErr := llmservice.LoadRegistry(ctx, s.settings)
			if loadErr == nil && llmservice.TokenBankGrantIDForRequest(fresh, requestID) != "" {
				// The debit is spendable. Leave the stored head where the ack
				// walk stopped so the next pass confirms before another share.
				return true, nil
			}
		}
		return false, err
	}
	if openSeq == seq {
		if err := llmservice.AdvanceTokenBankAutoSeq(ctx, s.settings, email, groupID, seq); err != nil {
			// The grant is already on the hub. A stale sequence retries the same
			// request id, and HubCenter will not debit it twice.
			return true, err
		}
	}
	return true, nil
}

// tokenBankChargedSpendableMicro is the charged card before holds, in the
// ledger unit. Admission compares it across the confirm round trip. Available
// credits clamp at zero, so a released hold must not look like a new share.
func tokenBankChargedSpendableMicro(reg *llmservice.Registry, userID, email string, groups []string) int64 {
	if reg == nil || len(groups) == 0 {
		return 0
	}
	credits := llmservice.SpendableCreditsForServiceGroupsForUserID(reg, userID, email, groups, time.Now().UTC())
	return llmpool.CreditsToMicrocredits(credits)
}

func chargedGroupListContains(groupID string, chargedGroupIDs []string) bool {
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	if groupID == "" {
		return false
	}
	for _, id := range chargedGroupIDs {
		if strings.ToLower(strings.TrimSpace(id)) == groupID {
			return true
		}
	}
	return false
}

// compile-time check that the center client satisfies the pull interface.
var _ llmservice.TokenBankCenter = (*Service)(nil)
