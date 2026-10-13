package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

const systemFreeCreditDebitKeep = 500

// systemFreeCreditDebit is one system-free charge already added to an owner's used total.
type systemFreeCreditDebit struct {
	RequestID string  `json:"request_id"`
	OwnerID   string  `json:"owner_id"`
	Credits   float64 `json:"credits"`
}

// SystemFreeBotMatch is a bot viewer token and the credits budget it spends.
type SystemFreeBotMatch struct {
	HubUserID string
	Limit     float64
	Used      float64
}

// SystemFreeCreditsView is what the desktop bot detail page shows.
// Limit 0 and Unlimited mean consumption is not capped. Applies is false when
// this owner's bots are not currently routed through system-free.
type SystemFreeCreditsView struct {
	Applies   bool    `json:"applies"`
	Unlimited bool    `json:"unlimited"`
	Limit     float64 `json:"limit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
}

func normalizeSystemFreeCreditsLimit(v float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, fmt.Errorf("%w: system-free credits limit must be zero or positive", ErrInvalidInput)
	}
	return roundBotCredits(v), nil
}

func roundBotCredits(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return math.Round(v*1000) / 1000
}

// LookupBotLLMToken reports whether rawToken is a viewer token Hub wrote for
// a bot owner. Those calls bill the bot credits budget instead of the
// new-user benefit allowance.
func (s *Service) LookupBotLLMToken(ctx context.Context, tenantID, rawToken string) (SystemFreeBotMatch, bool) {
	rawToken = strings.TrimSpace(rawToken)
	if s == nil || s.System == nil || rawToken == "" {
		return SystemFreeBotMatch{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SystemFreeBotMatch{}, false
	}
	want := hashBotToken(rawToken)
	for i := range rec.Principals {
		stored := strings.TrimSpace(rec.Principals[i].LLMViewerToken)
		if stored == "" || hashBotToken(stored) != want {
			continue
		}
		owner := strings.TrimSpace(rec.Principals[i].HubUserID)
		if owner == "" {
			continue
		}
		return SystemFreeBotMatch{
			HubUserID: owner,
			Limit:     rec.LLM.SystemFreeCreditsLimit,
			Used:      systemFreeCreditsUsed(rec, owner),
		}, true
	}
	return SystemFreeBotMatch{}, false
}

// SystemFreeCreditsForOwner is the allowance and remaining credits for one
// bot owner's system-free usage.
func (s *Service) SystemFreeCreditsForOwner(ctx context.Context, tenantID, hubUserID string) (SystemFreeCreditsView, error) {
	if s == nil || s.System == nil {
		return SystemFreeCreditsView{}, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return SystemFreeCreditsView{}, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SystemFreeCreditsView{}, err
	}
	return systemFreeCreditsView(rec, hubUserID), nil
}

// AddSystemFreeCreditsUsed adds credits to an owner's system-free total.
// A limit of zero does not track usage. A repeated request id is ignored.
func (s *Service) AddSystemFreeCreditsUsed(ctx context.Context, tenantID, ownerID, requestID string, credits float64) error {
	if s == nil || s.System == nil {
		return ErrSettingsUnavailable
	}
	ownerID = strings.TrimSpace(ownerID)
	requestID = strings.TrimSpace(requestID)
	credits = roundBotCredits(credits)
	if ownerID == "" || credits <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if rec.LLM.SystemFreeCreditsLimit <= 0 {
		return nil
	}
	if requestID != "" {
		for _, debit := range rec.SystemFreeCreditDebits {
			if debit.RequestID == requestID {
				return nil
			}
		}
	}
	if rec.SystemFreeCreditsUsed == nil {
		rec.SystemFreeCreditsUsed = map[string]float64{}
	}
	rec.SystemFreeCreditsUsed[ownerID] = roundBotCredits(systemFreeCreditsUsed(rec, ownerID) + credits)
	if requestID != "" {
		rec.SystemFreeCreditDebits = append(rec.SystemFreeCreditDebits, systemFreeCreditDebit{
			RequestID: requestID,
			OwnerID:   ownerID,
			Credits:   credits,
		})
		if len(rec.SystemFreeCreditDebits) > systemFreeCreditDebitKeep {
			rec.SystemFreeCreditDebits = append([]systemFreeCreditDebit(nil), rec.SystemFreeCreditDebits[len(rec.SystemFreeCreditDebits)-systemFreeCreditDebitKeep:]...)
		}
	}
	return s.save(ctx, tenantID, rec)
}

func systemFreeCreditsUsed(rec record, ownerID string) float64 {
	if rec.SystemFreeCreditsUsed == nil {
		return 0
	}
	return roundBotCredits(rec.SystemFreeCreditsUsed[strings.TrimSpace(ownerID)])
}

func systemFreeCreditsView(rec record, hubUserID string) SystemFreeCreditsView {
	limit := roundBotCredits(rec.LLM.SystemFreeCreditsLimit)
	used := systemFreeCreditsUsed(rec, hubUserID)
	view := SystemFreeCreditsView{
		Applies:   systemFreeRouteActive(rec, hubUserID),
		Unlimited: limit <= 0,
		Limit:     limit,
		Used:      used,
	}
	if !view.Unlimited {
		view.Remaining = roundBotCredits(limit - used)
		if view.Remaining < 0 {
			view.Remaining = 0
		}
	}
	return view
}

func systemFreeRouteActive(rec record, hubUserID string) bool {
	if effectiveOwnerLLMOverride(rec, hubUserID) != nil {
		return false
	}
	// Blank, the legacy system-free id, and the official id are one route.
	// That route bills the owner's redeemed allowance, so this cap does not apply.
	current := strings.TrimSpace(rec.LLM.Current)
	if isBotDefaultServiceGroup(current) {
		return false
	}
	return strings.EqualFold(current, legacyBotServiceGroupID)
}

func hashBotToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}
