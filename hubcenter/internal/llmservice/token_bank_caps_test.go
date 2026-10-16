package llmservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

type capNoteSettler struct {
	shareID string
	model   string
	message string
}

func (s *capNoteSettler) SettleTokenBankUsage(context.Context, TokenBankSettlementInput) (TokenBankSettlementOutcome, error) {
	return TokenBankSettlementOutcome{}, nil
}

func (s *capNoteSettler) TokenBankShareForPublish(context.Context, string, string) (TokenBankShareSettlementView, bool, error) {
	return TokenBankShareSettlementView{}, false, nil
}

func (s *capNoteSettler) NoteShareModelError(_ context.Context, shareID, model, message string) error {
	s.shareID, s.model, s.message = shareID, model, message
	return nil
}

func TestTokenBankRequestCapsEstimateAndOutput(t *testing.T) {
	provider := &llmpool.ProviderConfig{
		ID:                        TokenBankMemberID("share-1", "gpt"),
		MaxInputTokensPerRequest:  2,
		MaxOutputTokensPerRequest: 30,
	}
	// 5 runes -> ceil(5/4) = 2, which is still inside the cap.
	body := map[string]any{
		"messages":              []any{map[string]any{"content": "abcde"}},
		"max_tokens":            10,
		"max_completion_tokens": float64(80),
	}
	if err := applyTokenBankRequestCaps(provider, body); err != nil {
		t.Fatal(err)
	}
	if body["max_tokens"] != 10 {
		t.Fatalf("max_tokens = %#v, want 10 left alone", body["max_tokens"])
	}
	if body["max_completion_tokens"] != int64(30) && body["max_completion_tokens"] != 30 {
		t.Fatalf("max_completion_tokens = %#v, want 30", body["max_completion_tokens"])
	}

	over := map[string]any{"messages": []any{map[string]any{"content": "abcdefghi"}}}
	err := applyTokenBankRequestCaps(provider, over)
	var cap *TokenBankRequestCapError
	if !errors.As(err, &cap) || cap.Estimate != 3 || cap.Limit != 2 {
		t.Fatalf("9-rune estimate error = %v, want estimate 3 limit 2", err)
	}

	unset := map[string]any{"messages": []any{map[string]any{"content": "a"}}}
	if err := applyTokenBankRequestCaps(provider, unset); err != nil {
		t.Fatal(err)
	}
	if unset["max_tokens"] != int64(30) {
		t.Fatalf("unset max_tokens = %#v, want cap 30", unset["max_tokens"])
	}

	plain := &llmpool.ProviderConfig{ID: "openai", MaxOutputTokensPerRequest: 1}
	plainBody := map[string]any{"max_tokens": 100}
	if err := applyTokenBankRequestCaps(plain, plainBody); err != nil {
		t.Fatal(err)
	}
	if plainBody["max_tokens"] != 100 {
		t.Fatalf("non-member max_tokens changed to %#v", plainBody["max_tokens"])
	}
}

func TestTokenBankRequestCapNotesShareAndMapsTo400(t *testing.T) {
	note := &capNoteSettler{}
	cfg := &ProxyConfig{TokenBank: note, NodeID: "hc-1"}
	provider := &llmpool.ProviderConfig{
		ID:                       TokenBankMemberID("share-9", "gpt"),
		MaxInputTokensPerRequest: 1,
	}
	body := map[string]any{"messages": []any{map[string]any{"content": "abcdefghij"}}}
	_, err := egressProviderAttempts(context.Background(), cfg, provider, body, "gpt", "gpt", false)
	var cap *TokenBankRequestCapError
	if !errors.As(err, &cap) {
		t.Fatalf("egress error = %v, want cap error", err)
	}
	if note.shareID != "share-9" || note.model != "gpt" || note.message == "" {
		t.Fatalf("note = %+v", note)
	}

	rec := httptest.NewRecorder()
	writeProxyRequestError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestTokenBankMemberProviderCopiesRequestCaps(t *testing.T) {
	got := tokenBankMemberProvider(TokenBankPublishSpec{
		ShareID: "share-1", Model: "gpt", MaxInputTokensPerRequest: 12, MaxOutputTokensPerRequest: 34,
	})
	if got.MaxInputTokensPerRequest != 12 || got.MaxOutputTokensPerRequest != 34 {
		t.Fatalf("caps = %d/%d, want 12/34", got.MaxInputTokensPerRequest, got.MaxOutputTokensPerRequest)
	}
}

func TestTokenBankMemberProviderKeepsShareWindowOffTheBill(t *testing.T) {
	got := tokenBankMemberProvider(TokenBankPublishSpec{
		ShareID:     "share-1",
		Model:       "gpt",
		ShareWindow: llmpool.TokenBankShareWindow{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "08:00"},
	})
	if got.TokenBankShareWindow.Start != "22:00" || got.TokenBankShareWindow.End != "08:00" || len(got.TokenBankShareWindow.Days) != 5 {
		t.Fatalf("window = %+v", got.TokenBankShareWindow)
	}
	if got.CreditMultiplier != 0 || len(got.CreditMultiplierSchedule) != 0 {
		t.Fatalf("billing leaked: multiplier=%v schedule=%+v", got.CreditMultiplier, got.CreditMultiplierSchedule)
	}
	spec := TokenBankPublishSpec{
		ShareID:     "share-1",
		Model:       "gpt",
		ShareWindow: llmpool.TokenBankShareWindow{Days: []int{1}, Start: "22:00", End: "08:00"},
	}
	copied := tokenBankMemberProvider(spec)
	copied.TokenBankShareWindow.Days[0] = 9
	if spec.ShareWindow.Days[0] != 1 {
		t.Fatal("member days alias the spec slice")
	}
}
