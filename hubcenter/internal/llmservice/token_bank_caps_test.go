package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	// 80 is above the cap. The sibling 10 does not pull this field down.
	if body["max_completion_tokens"] != int64(30) {
		t.Fatalf("max_completion_tokens = %#v, want the cap 30", body["max_completion_tokens"])
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
	if plainBody["max_tokens"] != int64(1) {
		t.Fatalf("member max_tokens = %#v, want the cap 1", plainBody["max_tokens"])
	}
	unlimited := &llmpool.ProviderConfig{ID: "openai"}
	unlimitedBody := map[string]any{"max_tokens": 100}
	if err := applyTokenBankRequestCaps(unlimited, unlimitedBody); err != nil {
		t.Fatal(err)
	}
	if unlimitedBody["max_tokens"] != 100 {
		t.Fatalf("unset cap changed max_tokens to %#v", unlimitedBody["max_tokens"])
	}
}

func TestTokenBankInputCapIgnoresPictureBlocks(t *testing.T) {
	picture := strings.Repeat("A", 4000)
	imageURL := map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:image/jpeg;base64," + picture,
		},
	}
	anthropic := map[string]any{
		"type": "Image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/jpeg",
			"data":       picture,
		},
	}
	responses := map[string]any{
		"type":      "input_image",
		"image_url": "data:image/jpeg;base64," + picture,
	}
	bare := map[string]any{"image_url": map[string]any{"url": "data:image/png;base64," + picture}}
	body := map[string]any{
		"messages": []any{map[string]any{
			"content": []any{"abcde", imageURL, anthropic, responses, bare},
		}},
	}
	if got := estimateTokenBankInputTokens(body); got != 2 {
		t.Fatalf("picture pages estimated %d tokens, want 2 from the text alone", got)
	}
	provider := &llmpool.ProviderConfig{
		ID:                       TokenBankMemberID("share-1", "gpt"),
		MaxInputTokensPerRequest: 2,
	}
	if err := applyTokenBankRequestCaps(provider, body); err != nil {
		t.Fatal(err)
	}

	over := map[string]any{
		"messages": []any{map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "abcdefghi"},
				imageURL,
			},
		}},
	}
	// "text" (4) + "abcdefghi" (9) = 13 runes -> ceil(13/4) = 4. The data URL stays out.
	err := applyTokenBankRequestCaps(provider, over)
	var cap *TokenBankRequestCapError
	if !errors.As(err, &cap) || cap.Estimate != 4 || cap.Limit != 2 {
		t.Fatalf("text beside a picture = %v, want estimate 4 limit 2", err)
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

func TestMemberOutputCapKeepsSmallerAndTruncates(t *testing.T) {
	const cap int64 = 16384
	huge := map[string]any{"max_tokens": 100000}
	ApplyMemberOutputCap(huge, cap)
	if huge["max_tokens"] != cap {
		t.Fatalf("huge max_tokens = %#v, want %d", huge["max_tokens"], cap)
	}
	if _, ok := huge["max_completion_tokens"]; ok {
		t.Fatal("a missing sibling was invented")
	}

	small := map[string]any{"max_completion_tokens": 128}
	ApplyMemberOutputCap(small, cap)
	if small["max_completion_tokens"] != 128 {
		t.Fatalf("small max_completion_tokens = %#v, want 128", small["max_completion_tokens"])
	}
	if _, ok := small["max_tokens"]; ok {
		t.Fatal("max_tokens was added beside a smaller max_completion_tokens")
	}

	// Each field is capped alone. 2000 is under the cap, so it stays, and it
	// does not pull the other field down with it.
	both := map[string]any{"max_tokens": 100000, "max_completion_tokens": float64(2000)}
	ApplyMemberOutputCap(both, cap)
	if both["max_tokens"] != cap || both["max_completion_tokens"] != float64(2000) {
		t.Fatalf("both = max %#v completion %#v, want %d and 2000", both["max_tokens"], both["max_completion_tokens"], cap)
	}
	under := map[string]any{"max_tokens": 10000, "max_completion_tokens": 2000}
	ApplyMemberOutputCap(under, cap)
	if under["max_tokens"] != 10000 || under["max_completion_tokens"] != 2000 {
		t.Fatalf("under-cap pair changed: %#v %#v", under["max_tokens"], under["max_completion_tokens"])
	}
	zero := map[string]any{"max_tokens": 0, "max_completion_tokens": 100}
	ApplyMemberOutputCap(zero, cap)
	if zero["max_tokens"] != 0 || zero["max_completion_tokens"] != 100 {
		t.Fatalf("zero spread onto the sibling: %#v %#v", zero["max_tokens"], zero["max_completion_tokens"])
	}
	fraction := map[string]any{"max_tokens": 16384.9, "max_completion_tokens": 10.5}
	ApplyMemberOutputCap(fraction, cap)
	if fraction["max_tokens"] != cap || fraction["max_completion_tokens"] != 10.5 {
		t.Fatalf("fraction = %#v %#v", fraction["max_tokens"], fraction["max_completion_tokens"])
	}
	bad := map[string]any{"max_tokens": "nope", "max_completion_tokens": 12}
	ApplyMemberOutputCap(bad, cap)
	if bad["max_tokens"] != cap || bad["max_completion_tokens"] != 12 {
		t.Fatalf("bad sibling rewrote the other field: %#v %#v", bad["max_tokens"], bad["max_completion_tokens"])
	}
	responses := map[string]any{"max_output_tokens": 100000}
	ApplyMemberOutputCap(responses, cap)
	if responses["max_output_tokens"] != cap {
		t.Fatalf("max_output_tokens = %#v", responses["max_output_tokens"])
	}
	if _, ok := responses["max_tokens"]; ok {
		t.Fatal("max_output_tokens caused max_tokens to be invented")
	}

	overBoth := map[string]any{"max_tokens": 100000, "max_completion_tokens": 200000}
	ApplyMemberOutputCap(overBoth, cap)
	if overBoth["max_tokens"] != cap || overBoth["max_completion_tokens"] != cap {
		t.Fatalf("over both = %#v / %#v, want %d", overBoth["max_tokens"], overBoth["max_completion_tokens"], cap)
	}

	omitted := map[string]any{"model": "typhoon"}
	ApplyMemberOutputCap(omitted, cap)
	if omitted["max_tokens"] != cap {
		t.Fatalf("omitted max_tokens = %#v, want the cap", omitted["max_tokens"])
	}
	ApplyMemberOutputCap(omitted, 0)
	if omitted["max_tokens"] != cap {
		t.Fatal("a zero cap rewrote a body that was already capped")
	}

	caller := map[string]any{"max_tokens": 100000}
	provider := &llmpool.ProviderConfig{ID: "typhoon", MaxOutputTokensPerRequest: cap}
	copied := tokenBankCappedRequestBody(provider, caller)
	if err := applyTokenBankRequestCaps(provider, copied); err != nil {
		t.Fatal(err)
	}
	if caller["max_tokens"] != 100000 {
		t.Fatalf("caller body changed to %#v", caller["max_tokens"])
	}
	if copied["max_tokens"] != cap {
		t.Fatalf("copied max_tokens = %#v", copied["max_tokens"])
	}
}

func TestMemberOutputCapOnForwardStreamAndTools(t *testing.T) {
	var mu sync.Mutex
	var saw []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		mu.Lock()
		saw = append(saw, payload)
		mu.Unlock()
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") || payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	provider := &llmpool.ProviderConfig{
		ID:                        "typhoon",
		Name:                      "Typhoon",
		APIURL:                    srv.URL + "/v1",
		APIKey:                    "sk-test",
		Protocol:                  "openai",
		MaxOutputTokensPerRequest: 16384,
	}
	tool := map[string]any{"type": "function", "function": map[string]any{"name": "ping"}}
	cases := []struct {
		name   string
		stream bool
		body   map[string]any
		want   int64
		comp   any
	}{
		{name: "stream huge with tools", stream: true, body: map[string]any{"max_tokens": 100000, "tools": []any{tool}}, want: 16384},
		{name: "stream small", stream: true, body: map[string]any{"max_tokens": 128}, want: 128},
		{name: "stream both", stream: true, body: map[string]any{"max_tokens": 100000, "max_completion_tokens": 2000}, want: 16384, comp: 2000},
		{name: "stream omitted", stream: true, body: map[string]any{}, want: 16384},
		{name: "forward huge", body: map[string]any{"max_tokens": 100000}, want: 16384},
		{name: "forward omitted", body: map[string]any{}, want: 16384},
		{name: "forward small completion", body: map[string]any{"max_completion_tokens": 64}, want: 0, comp: 64},
		{name: "forward both over cap", body: map[string]any{"max_tokens": 90000, "max_completion_tokens": 80000}, want: 16384, comp: int64(16384)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{
				"model":    "typhoon",
				"messages": []any{map[string]any{"role": "user", "content": "ping"}},
			}
			for key, value := range tc.body {
				body[key] = value
			}
			mu.Lock()
			before := len(saw)
			mu.Unlock()
			if tc.stream {
				dst := newLockedResponseRecorder()
				streamed, err := streamProviderToWriter(context.Background(), srv.Client(), provider, body, "typhoon", "typhoon", dst)
				if err != nil || streamed == nil || streamed.statusCode != http.StatusOK {
					t.Fatalf("stream err=%v result=%#v body=%s", err, streamed, dst.BodyString())
				}
			} else {
				resp, err := forwardToProvider(context.Background(), srv.Client(), provider, body, "typhoon", "typhoon")
				if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
					t.Fatalf("forward err=%v resp=%#v", err, resp)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(saw) != before+1 {
				t.Fatalf("captured %d bodies, want one new", len(saw)-before)
			}
			got := saw[before]
			if tc.want > 0 {
				n, ok := tokenBankBodyInt(got["max_tokens"])
				if !ok || n != tc.want {
					t.Fatalf("max_tokens = %#v, want %d", got["max_tokens"], tc.want)
				}
			} else if _, ok := got["max_tokens"]; ok {
				t.Fatalf("max_tokens was invented: %#v", got["max_tokens"])
			}
			if tc.comp != nil {
				if got["max_completion_tokens"] != tc.comp {
					n, ok := tokenBankBodyInt(got["max_completion_tokens"])
					want, wantOK := tokenBankBodyInt(tc.comp)
					if !ok || !wantOK || n != want {
						t.Fatalf("max_completion_tokens = %#v, want %#v", got["max_completion_tokens"], tc.comp)
					}
				}
			}
			if tc.body["tools"] != nil {
				tools, _ := got["tools"].([]any)
				if len(tools) != 1 {
					t.Fatalf("tools = %#v", got["tools"])
				}
			}
		})
	}
}

func TestMemberOutputCapSurvivesExtraBody(t *testing.T) {
	var saw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&saw); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	provider := &llmpool.ProviderConfig{
		ID:                        "typhoon",
		Name:                      "Typhoon",
		APIURL:                    srv.URL + "/v1",
		APIKey:                    "sk-test",
		Protocol:                  "openai",
		MaxOutputTokensPerRequest: 16384,
		ExtraBody: map[string]any{
			"max_tokens":            float64(100000),
			"max_completion_tokens": float64(32),
		},
	}
	body := map[string]any{
		"model":      "typhoon",
		"messages":   []any{map[string]any{"role": "user", "content": "ping"}},
		"max_tokens": 128,
	}
	resp, err := forwardToProvider(context.Background(), srv.Client(), provider, body, "typhoon", "typhoon")
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("forward err=%v resp=%#v", err, resp)
	}
	n, ok := tokenBankBodyInt(saw["max_tokens"])
	if !ok || n != 16384 {
		t.Fatalf("max_tokens = %#v, want the cap after extra_body raised it", saw["max_tokens"])
	}
	completion, ok := tokenBankBodyInt(saw["max_completion_tokens"])
	if !ok || completion != 32 {
		t.Fatalf("max_completion_tokens = %#v, want the smaller extra_body value", saw["max_completion_tokens"])
	}
	if body["max_tokens"] != 128 {
		t.Fatalf("caller body changed to %#v", body["max_tokens"])
	}
}

func TestMemberProbeOutputBodyUsesCallerThenCap(t *testing.T) {
	huge := int64(100000)
	small := int64(32)
	got := MemberProbeOutputBody(16384, &huge, nil, true)
	if got["max_tokens"] != int64(16384) {
		t.Fatalf("huge probe = %#v", got["max_tokens"])
	}
	got = MemberProbeOutputBody(16384, &small, nil, false)
	if got["max_tokens"] != int64(32) {
		t.Fatalf("small probe = %#v", got["max_tokens"])
	}
	completion := int64(2000)
	got = MemberProbeOutputBody(16384, &huge, &completion, false)
	if got["max_tokens"] != int64(16384) || got["max_completion_tokens"] != int64(2000) {
		t.Fatalf("both probe = %#v", got)
	}
	if MemberProbeFieldBudget(got, 1, "max_completion_tokens", "max_tokens") != 2000 {
		t.Fatal("Responses budget did not read max_completion_tokens")
	}
	if MemberProbeFieldBudget(got, 1, "max_tokens", "max_completion_tokens") != 16384 {
		t.Fatal("Anthropic budget did not read max_tokens")
	}
	zeroFirst := map[string]any{"max_tokens": 0, "max_completion_tokens": int64(32)}
	if MemberProbeFieldBudget(zeroFirst, 64, "max_tokens", "max_completion_tokens") != 0 {
		t.Fatal("a caller-chosen 0 was replaced")
	}
	got = MemberProbeOutputBody(16384, nil, nil, false)
	if got["max_tokens"] != int64(64) {
		t.Fatalf("default probe = %#v, want 64", got["max_tokens"])
	}
	got = MemberProbeOutputBody(16384, nil, nil, true)
	if got["max_tokens"] != int64(256) {
		t.Fatalf("tool probe = %#v, want 256", got["max_tokens"])
	}
	if MemberProbeFieldBudget(got, 1, "max_tokens", "max_completion_tokens") != 256 {
		t.Fatal("field budget did not read max_tokens")
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
