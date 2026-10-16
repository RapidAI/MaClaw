package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

type admissionPullRecorder struct {
	calls  int
	groups []string
}

func (p *admissionPullRecorder) PullTokenBankForAdmissionShortfall(_ context.Context, _, _ string, chargedGroupIDs []string, _ int64) (bool, error) {
	p.calls++
	p.groups = append([]string(nil), chargedGroupIDs...)
	return true, nil
}

type gatingAdmissionPuller struct {
	mu       sync.Mutex
	inflight int
	max      int
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
}

func (p *gatingAdmissionPuller) PullTokenBankForAdmissionShortfall(context.Context, string, string, []string, int64) (bool, error) {
	p.mu.Lock()
	p.inflight++
	if p.inflight > p.max {
		p.max = p.inflight
	}
	p.mu.Unlock()
	p.once.Do(func() { close(p.entered) })
	<-p.release
	p.mu.Lock()
	p.inflight--
	p.mu.Unlock()
	return true, nil
}

func TestPullTokenBankForQuoteShortfallSerializesConcurrentPulls(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &gatingAdmissionPuller{entered: make(chan struct{}), release: make(chan struct{})}
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
			errCh <- err
		}()
	}
	select {
	case <-puller.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first admission pull")
	}
	time.Sleep(20 * time.Millisecond)
	puller.mu.Lock()
	max := puller.max
	puller.mu.Unlock()
	close(puller.release)
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if max != 1 {
		t.Fatalf("max inflight=%d, want one admission pull at a time", max)
	}
}

func TestPullTokenBankForQuoteShortfallSkipsACancelledRequest(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
	if pulled || puller.calls != 0 || err == nil {
		t.Fatalf("pulled=%v calls=%d err=%v, want no bank call after the request is cancelled", pulled, puller.calls, err)
	}
}

func TestPullTokenBankForQuoteShortfallSkipsACancelledWaiter(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	mu := lockTokenBankAdmission("user@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	puller := &admissionPullRecorder{}
	done := make(chan struct{})
	var pulled bool
	var err error
	go func() {
		pulled, err = pullTokenBankForQuoteShortfall(ctx, reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the cancelled waiter")
	}
	if pulled || puller.calls != 0 || err == nil {
		t.Fatalf("pulled=%v calls=%d err=%v, want no bank call after the waiter is cancelled", pulled, puller.calls, err)
	}
}

func TestPullTokenBankForQuoteShortfallLetsOtherAccountsPull(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	const first = "user@example.com"
	second := ""
	for i := 0; i < 64 && second == ""; i++ {
		candidate := "other" + strings.Repeat("x", i) + "@example.com"
		if tokenBankAdmissionStripe(candidate) != tokenBankAdmissionStripe(first) {
			second = candidate
		}
	}
	if second == "" {
		t.Fatal("no second account on another admission stripe")
	}
	puller := &gatingAdmissionPuller{entered: make(chan struct{}), release: make(chan struct{})}
	errCh := make(chan error, 2)
	for _, email := range []string{first, second} {
		go func(email string) {
			_, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", email, "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
			errCh <- err
		}(email)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		puller.mu.Lock()
		max := puller.max
		puller.mu.Unlock()
		if max >= 2 {
			close(puller.release)
			for i := 0; i < 2; i++ {
				if err := <-errCh; err != nil {
					t.Fatal(err)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(puller.release)
	t.Fatal("timed out waiting for two accounts to pull at once")
}

func TestPullTokenBankForQuoteShortfallPullsWhenTheCardCannotCoverTheFloor(t *testing.T) {
	now := time.Now().UTC()
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 5.34, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15.042, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want one pull when 15.042 exceeds the 5.34 card", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenTheRestoredCeilingExceedsTheCard(t *testing.T) {
	now := time.Now().UTC()
	quote, ok := llmpool.NewPricingQuoteSnapshot(
		"req-restored", "req-restored:1", "maclaw",
		llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4.75,
		}},
		1, 1, 100, 65_536, now.Add(time.Minute),
	)
	if !ok {
		t.Fatal("quote")
	}
	one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1)
	if !oneOK {
		t.Fatal("requote")
	}
	const card = 0.409
	if llmpool.MicrocreditsToCredits(one.ReservedMicrocredits) > card {
		t.Fatalf("floor = %.3f, want the card to cover one output token", llmpool.MicrocreditsToCredits(one.ReservedMicrocredits))
	}
	if llmpool.MicrocreditsToCredits(quote.ReservedMicrocredits) <= card {
		t.Fatalf("reserved = %.3f, want the restored ceiling above the card", llmpool.MicrocreditsToCredits(quote.ReservedMicrocredits))
	}
	ctx := withLLMBillingState(context.Background(), now, "req-restored")
	rememberLLMPricingQuote(ctx, quote)
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(card), "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", llmpool.MicrocreditsToCredits(one.ReservedMicrocredits), []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the restored ceiling exceeds %.3f and one token does not", pulled, puller.calls, card)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenAHoldClampsAvailableToZero(t *testing.T) {
	now := time.Now().UTC()
	quote, ok := llmpool.NewPricingQuoteSnapshot(
		"req-held", "req-held:1", "maclaw",
		llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4.75,
		}},
		1, 1, 100, 65_536, now.Add(time.Minute),
	)
	if !ok {
		t.Fatal("quote")
	}
	one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1)
	if !oneOK {
		t.Fatal("requote")
	}
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: now.Add(time.Hour),
	}}
	ctx := withLLMBillingState(context.Background(), now, "req-held")
	rememberLLMPricingQuote(ctx, quote)
	puller := &admissionPullRecorder{}
	floor := llmpool.MicrocreditsToCredits(one.ReservedMicrocredits)
	pulled, err := pullTokenBankForQuoteShortfall(ctx, reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", floor, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when a hold clamps available to 0 even though the floor %.3f fits the pre-hold card", pulled, puller.calls, floor)
	}
}

func TestPullTokenBankForQuoteShortfallLeavesACardThatCoversTheFloor(t *testing.T) {
	now := time.Now().UTC()
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 5.34, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 3.5, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the card covers the floor", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallSkipsUnlimitedAndPeriodLimits(t *testing.T) {
	now := time.Now().UTC()
	unlimited := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "open", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), unlimited, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("unlimited grant pulled=%v calls=%d", pulled, puller.calls)
	}
	metered := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 1, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	// The period-limit label is the model's winning denial. This card can pay
	// 0.5, so the label must not withdraw. The same card cannot pay 15, and
	// the label must not hide that shortfall.
	labeled := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), metered, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 0.5, []string{"paid"}, nil, labeled)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || labeled.calls != 0 {
		t.Fatalf("period-limit label on a card that covers the floor pulled=%v calls=%d", pulled, labeled.calls)
	}
	short := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), metered, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 15, []string{"paid"}, nil, short)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || short.calls != 1 {
		t.Fatalf("period-limit label on a short card pulled=%v calls=%d", pulled, short.calls)
	}
	for _, code := range []string{"LLM_SERVICE_GRANT_QUEUED", "LLM_SERVICE_GRANT_EXPIRED"} {
		queued := &admissionPullRecorder{}
		pulled, err = pullTokenBankForQuoteShortfall(context.Background(), metered, "u1", "user@example.com", code, 15, []string{"paid"}, nil, queued)
		if err != nil {
			t.Fatal(err)
		}
		if pulled || queued.calls != 0 {
			t.Fatalf("%s pulled=%v calls=%d", code, pulled, queued.calls)
		}
	}
	futureOnly := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "later", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 20, StartsAt: now.Add(2 * time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
		}},
	}
	waiting := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), futureOnly, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 0, []string{"paid"}, nil, waiting)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || waiting.calls != 0 {
		t.Fatalf("queued grant pulled=%v calls=%d, want no pull when the grant has not started", pulled, waiting.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenThePeriodWindowIsExhausted(t *testing.T) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "period", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 100, CreditsUsed: 10, Permanent: true,
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
			PeriodLimits: llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:  llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the period window is exhausted and no point card can pay", pulled, puller.calls)
	}

	reg.Grants = append(reg.Grants, llmservice.Grant{
		ID: "point", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
		Source: "card", CreditsTotal: 20, StartsAt: now.Add(time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
	})
	covered := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 0, []string{"paid"}, nil, covered)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || covered.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when a queued point card already covers the exhausted period", pulled, covered.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenAHeldPointCardLeavesNothingFree(t *testing.T) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{
			{
				ID: "period", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
				CreditsTotal: 100, CreditsUsed: 10, Permanent: true,
				StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
				PeriodLimits: llmservice.CreditPeriodLimits{Daily: 10},
				PeriodUsage:  llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
			},
			paidGrant("point", "paid", 20),
		},
		BillingReservations: []llmservice.BillingReservation{{
			RequestID: "hold", UserID: "u1", Email: "user@example.com",
			ServiceGroupIDs: []string{"paid"}, Credits: 20, ExpiresAt: now.Add(time.Hour),
		}},
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_PERIOD_LIMITED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the point card is entirely held and this call has nothing free", pulled, puller.calls)
	}
}

func TestPrepareLLMPricingQuotePricesAHeldCardThatCannotStartTheRequest(t *testing.T) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{
			{
				ID: "period", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
				CreditsTotal: 100, CreditsUsed: 10, Permanent: true,
				StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
				PeriodLimits: llmservice.CreditPeriodLimits{Daily: 10},
				PeriodUsage:  llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
			},
			paidGrant("point", "paid", 5),
		},
		BillingReservations: []llmservice.BillingReservation{{
			RequestID: "hold", UserID: "u1", Email: "user@example.com",
			ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: now.Add(time.Hour),
		}},
	}
	model := &llmservice.AuthorizedModel{
		Name:                  "held-floor",
		ProviderIDs:           []string{"p1"},
		ProviderServiceGroups: map[string][]string{"p1": {"paid"}},
		ProviderBillingModes:  map[string]string{"p1": llmpool.BillingModePaid},
		ProviderTokenPricing:  map[string]llmpool.TokenPricing{"p1": {InputCreditsPer10K: 100000, OutputCreditsPer10K: 100000}},
	}
	body := map[string]any{"max_tokens": 16}
	denial, err := prepareLLMPricingQuote(context.Background(), reg, nil, "u1", "user@example.com", model, "p1", body, now)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" || denial.NeedCredits <= 5 || !strings.Contains(denial.Message, "held by in-flight requests") {
		t.Fatalf("denial=%#v err=%v, want a held-card denial priced above the 5 credit card", denial, err)
	}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", denial.Code, denial.NeedCredits, []string{"paid"}, model, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a withdraw when the frozen card cannot pay the priced floor", pulled, puller.calls)
	}

	model.ProviderTokenPricing["p1"] = llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 1}
	cheap, err := prepareLLMPricingQuote(context.Background(), reg, nil, "u1", "user@example.com", model, "p1", body, now)
	if err == nil || cheap.NeedCredits <= 0 || cheap.NeedCredits >= 5 {
		t.Fatalf("denial=%#v err=%v, want a priced floor the 5 credit card can pay after the hold releases", cheap, err)
	}
	idle := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", cheap.Code, cheap.NeedCredits, []string{"paid"}, model, idle)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || idle.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a withdraw when the hold leaves nothing free even though the pre-hold card could pay %.3f", pulled, idle.calls, cheap.NeedCredits)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenAHoldExceedsTheCard(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 6, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when a 10 credit hold clamps available to 0 but the 5.34 card cannot pay 6", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallUsesFreeCreditsNotThePreHoldCard(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 1.49, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	short := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 4, []string{"paid"}, nil, short)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || short.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when free credits are below 4 even though the 5.34 card could pay after the hold releases", pulled, short.calls)
	}
	covered := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 3, []string{"paid"}, nil, covered)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || covered.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when free credits still cover 3", pulled, covered.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenReservationExceedsFreeCredits(t *testing.T) {
	now := time.Now().UTC()
	quote, ok := llmpool.NewPricingQuoteSnapshot(
		"req-partial", "req-partial:1", "local",
		llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 4.75,
		}},
		1, 1, 100, 9_000, now.Add(time.Minute),
	)
	if !ok {
		t.Fatal("quote")
	}
	one, oneOK := requoteAtOutputLimit(quote, quote.InputTokenEstimate, 1)
	if !oneOK {
		t.Fatal("requote")
	}
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 1.49, ExpiresAt: now.Add(time.Hour),
	}}
	free := creditsToMicrocredits(llmservice.AvailableCreditsForServiceGroupsForUserID(reg, "u1", "user@example.com", []string{"paid"}, now))
	gross := creditsToMicrocredits(llmservice.SpendableCreditsForServiceGroupsForUserID(reg, "u1", "user@example.com", []string{"paid"}, now))
	if one.ReservedMicrocredits >= free || quote.ReservedMicrocredits <= free || quote.ReservedMicrocredits >= gross {
		t.Fatalf("floor=%d reserved=%d free=%d gross=%d, want the one-token price under the free slice and the restored ceiling between free and pre-hold", one.ReservedMicrocredits, quote.ReservedMicrocredits, free, gross)
	}
	ctx := withLLMBillingState(context.Background(), now, "req-partial")
	rememberLLMPricingQuote(ctx, quote)
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", llmpool.MicrocreditsToCredits(one.ReservedMicrocredits), []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the restored ceiling exceeds free credits and still fits the pre-hold card", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallKeepsThePricedFloor(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(context.Background(), now, "req")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID: "local", LogicalModel: "mid", ReservedMicrocredits: 1000,
	})
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", 15.042, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want the 15.042 floor to pull even when a stored quote reserves 0.001", pulled, puller.calls)
	}
	idle := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, idle)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || idle.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the denial has no floor and the stored 0.001 quote fits the card", pulled, idle.calls)
	}
}

func TestPullTokenBankForQuoteShortfallIgnoresAnUnrequotableReservation(t *testing.T) {
	now := time.Now().UTC()
	ctx := withLLMBillingState(context.Background(), now, "req")
	rememberLLMPricingQuote(ctx, llmpool.PricingQuoteSnapshot{
		ProviderID: "local", LogicalModel: "mid", ReservedMicrocredits: 20 * 1_000_000,
	})
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(ctx, paidCardRegistry(5.34), "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if pulled || puller.calls != 0 {
		t.Fatalf("pulled=%v calls=%d, want no pull when the quote cannot be priced at one output token and the card still has 5.34", pulled, puller.calls)
	}
}

func TestPullTokenBankForQuoteShortfallPullsWhenHoldsHideAnUnpricedCard(t *testing.T) {
	reg := paidCardRegistry(5.34)
	reg.BillingReservations = []llmservice.BillingReservation{{
		RequestID: "hold", UserID: "u1", Email: "user@example.com",
		ServiceGroupIDs: []string{"paid"}, Credits: 10, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	puller := &admissionPullRecorder{}
	pulled, err := pullTokenBankForQuoteShortfall(context.Background(), reg, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, puller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || puller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when holds hide a 5.34 card and the denial has no floor", pulled, puller.calls)
	}

	empty := paidCardRegistry(1)
	empty.Grants[0].CreditsUsed = 1
	emptyPuller := &admissionPullRecorder{}
	pulled, err = pullTokenBankForQuoteShortfall(context.Background(), empty, "u1", "user@example.com", "LLM_SERVICE_CREDITS_EXHAUSTED", 0, []string{"paid"}, nil, emptyPuller)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || emptyPuller.calls != 1 {
		t.Fatalf("pulled=%v calls=%d, want a pull when the card itself is empty", pulled, emptyPuller.calls)
	}
}

func TestCoverFilteredModelsPullsWhenThePeriodWindowIsExhausted(t *testing.T) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{{
			ID: "period", UserID: "u1", Email: "user@example.com", ServiceGroupID: "paid",
			CreditsTotal: 100, CreditsUsed: 10, Permanent: true,
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
			PeriodLimits: llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:  llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}},
	}
	models := []llmservice.AuthorizedModel{{Name: "auto", ChargedServiceGroupIDs: []string{"paid"}}}
	denied := map[string]llmBillingDenial{
		"auto": {Code: "LLM_SERVICE_PERIOD_LIMITED"},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["auto"])
	if puller.calls != 1 || !sameGroups(puller.seen[0], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want one pull for the exhausted period window", puller.calls, puller.seen)
	}
}

func TestCoverFilteredModelsPullsThePointCardBehindAPeriodLimit(t *testing.T) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "welcome", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{
			{
				ID: "period", UserID: "u1", Email: "user@example.com", ServiceGroupID: "welcome",
				CreditsTotal: 100, CreditsUsed: 10, Permanent: true,
				StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
				PeriodLimits: llmservice.CreditPeriodLimits{Daily: 10},
				PeriodUsage:  llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
			},
			paidGrant("point", "paid", 1),
		},
	}
	models := []llmservice.AuthorizedModel{{
		Name:        "auto",
		ProviderIDs: []string{"official", "card"},
		ProviderServiceGroups: map[string][]string{
			"official": {"welcome"},
			"card":     {"paid"},
		},
	}}
	denied := map[string]llmBillingDenial{
		"auto": {Code: "LLM_SERVICE_PERIOD_LIMITED", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		if sameGroups(groups, []string{"welcome"}) {
			return false, llmservice.ErrTokenBankGroupNotCharged
		}
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["auto"])
	if puller.calls != 2 || !sameGroups(puller.seen[0], []string{"welcome"}) || !sameGroups(puller.seen[1], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want the period group skipped and the short point card pulled", puller.calls, puller.seen)
	}
}

func TestPreferLLMBillingDenialKeepsThePointCardFloor(t *testing.T) {
	got := preferLLMBillingDenial(
		llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15.042, HeldCredits: 1},
		llmBillingDenial{Code: "LLM_SERVICE_PERIOD_LIMITED", Message: "current period credit limit is exhausted"},
	)
	if got.Code != "LLM_SERVICE_PERIOD_LIMITED" || got.NeedCredits != 15.042 || got.HeldCredits != 0 {
		t.Fatalf("denial = %#v, want the period code, the point-card floor, and no borrowed hold", got)
	}
	held := preferLLMBillingDenial(
		llmBillingDenial{Code: "LLM_SERVICE_PERIOD_LIMITED", Message: "current period credit limit is exhausted"},
		llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", Message: "insufficient credits for this request: need 20.000 credits, available 0.000 (20.000 held by in-flight requests)"},
	)
	if !strings.Contains(held.Message, "held by in-flight requests") {
		t.Fatalf("denial = %#v, want the held point card instead of the period window", held)
	}
	short := preferLLMBillingDenial(
		llmBillingDenial{Code: "LLM_SERVICE_PERIOD_LIMITED", Message: "current period credit limit is exhausted", NeedCredits: 1},
		llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 56.003, Message: "insufficient credits for this request: need 56.003 credits, available 6.430 (993.570 held by in-flight requests)"},
	)
	if short.Code != "LLM_SERVICE_PERIOD_LIMITED" || short.NeedCredits != 56.003 {
		t.Fatalf("denial = %#v, want the period window to keep the priced shortfall floor", short)
	}
	smaller := preferLLMBillingDenial(
		llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 3},
		llmBillingDenial{Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	)
	if smaller.NeedCredits != 15 {
		t.Fatalf("floor = %v, want the larger sibling floor", smaller.NeedCredits)
	}
}

func TestCoverFilteredModelsPullsOnlyTheShortModel(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{
			paidGrant("paid-card", "paid", 5),
			paidGrant("rich-card", "other", 100),
		},
	}
	models := []llmservice.AuthorizedModel{
		{Name: "rich", ChargedServiceGroupIDs: []string{"other"}},
		{Name: "poor", ChargedServiceGroupIDs: []string{"paid"}},
	}
	denied := map[string]llmBillingDenial{
		"rich": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 1},
		"poor": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["rich"])
	if puller.calls != 1 || !sameGroups(puller.seen[0], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want one pull for the paid card only", puller.calls, puller.seen)
	}
}

func TestCoverFilteredModelsDoesNotAddProviderWallets(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{
			paidGrant("paid-card", "paid", 5),
			paidGrant("rich-card", "other", 100),
		},
	}
	models := []llmservice.AuthorizedModel{{
		Name:        "split",
		ProviderIDs: []string{"a", "b"},
		ProviderServiceGroups: map[string][]string{
			"a": {"paid"},
			"b": {"other"},
		},
	}}
	denied := map[string]llmBillingDenial{
		"split": {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["split"])
	if puller.calls != 1 || !sameGroups(puller.seen[0], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want one pull for the short provider wallet", puller.calls, puller.seen)
	}
}

func TestCoverFilteredModelsSkipsAGroupTheBankDoesNotPay(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "other", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		Grants: []llmservice.Grant{paidGrant("paid-card", "paid", 5)},
	}
	models := []llmservice.AuthorizedModel{
		{Name: "other-model", ChargedServiceGroupIDs: []string{"other"}},
		{Name: "paid-model", ChargedServiceGroupIDs: []string{"paid"}},
	}
	denied := map[string]llmBillingDenial{
		"other-model": {Code: "LLM_SERVICE_CREDITS_EXHAUSTED"},
		"paid-model":  {Code: "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST", NeedCredits: 15},
	}
	puller := &scriptedAdmissionPuller{decide: func(groups []string) (bool, error) {
		if sameGroups(groups, []string{"other"}) {
			return false, llmservice.ErrTokenBankGroupNotCharged
		}
		return false, nil
	}}
	coverFilteredModelsFromTokenBank(context.Background(), nil, puller, "u1", "user@example.com", map[string]any{}, nil, models, reg, nil, denied, denied["other-model"])
	if puller.calls != 2 || !sameGroups(puller.seen[0], []string{"other"}) || !sameGroups(puller.seen[1], []string{"paid"}) {
		t.Fatalf("calls=%d seen=%v, want the uncharged group skipped and the paid card tried next", puller.calls, puller.seen)
	}
}

type scriptedAdmissionPuller struct {
	calls  int
	seen   [][]string
	decide func(groups []string) (bool, error)
}

func (p *scriptedAdmissionPuller) PullTokenBankForAdmissionShortfall(_ context.Context, _, _ string, chargedGroupIDs []string, _ int64) (bool, error) {
	p.calls++
	copied := append([]string(nil), chargedGroupIDs...)
	p.seen = append(p.seen, copied)
	if p.decide != nil {
		return p.decide(copied)
	}
	return false, nil
}

func paidCardRegistry(credits float64) *llmservice.Registry {
	return &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: "paid", AccessPolicy: llmservice.AccessPolicyGrantRequired,
		}},
		Grants: []llmservice.Grant{paidGrant("card", "paid", credits)},
	}
}

func paidGrant(id, group string, credits float64) llmservice.Grant {
	now := time.Now().UTC()
	return llmservice.Grant{
		ID: id, UserID: "u1", Email: "user@example.com", ServiceGroupID: group,
		CreditsTotal: credits, Permanent: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0),
	}
}

func TestPrepareAndReserveRefitsAStaleHandlerRegistryBeforeWithdrawing(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeOfficialTestQuote(w, "quote-stale", 2)
	})
	ctx, system, handler, model, body := admissionReserveFixture(t, "req-stale-registry", 10_000, 4)
	puller := &admissionPullRecorder{}
	_, denial, err := prepareAndReserveLLMPricing(ctx, system, &im.LLMProviderRegistry{}, handler, model, body, "u1", "user@example.com", puller)
	if err != nil || denial.Code != "" {
		t.Fatalf("reserve after refit: code=%s err=%v", denial.Code, err)
	}
	if puller.calls != 0 {
		t.Fatalf("pulls=%d, want none when the loaded card can still start the call", puller.calls)
	}
	got, ok := llmQuotePositiveInt64(body["max_tokens"])
	if !ok || got <= 0 || got >= 65_536 {
		t.Fatalf("max_tokens=%d ok=%v, want a ceiling fitted to the loaded 4 credit card", got, ok)
	}
}

func TestPrepareAndReservePullsWhenTheFreshCardCannotCoverTheFloor(t *testing.T) {
	var quoted officialQuoteCapture
	quoted.serve(t, func(call int, _ []byte, w http.ResponseWriter) {
		writeAdmissionOfficialQuote(w, "quote-floor", 1_000_000, 1_000_000)
	})
	ctx, system, handler, model, body := admissionReserveFixture(t, "req-floor-pull", 4, 4)
	puller := &admissionPullRecorder{}
	_, denial, err := prepareAndReserveLLMPricing(ctx, system, &im.LLMProviderRegistry{}, handler, model, body, "u1", "user@example.com", puller)
	if err == nil || denial.Code != "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST" {
		t.Fatalf("code=%s err=%v, want the floor to stay denied", denial.Code, err)
	}
	if puller.calls != 1 || !sameGroups(puller.groups, []string{"official-group"}) {
		t.Fatalf("pulls=%d groups=%v, want one pull from official-group", puller.calls, puller.groups)
	}
}

func admissionReserveFixture(t *testing.T, requestID string, handlerCredits, storedCredits float64) (context.Context, *testLLMServiceSystemSettings, *llmservice.Registry, *llmservice.AuthorizedModel, map[string]any) {
	t.Helper()
	now := time.Now().UTC()
	ctx := officialFitContext(t, requestID, now)
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if err := llmservice.SaveRegistry(ctx, system, officialFitRegistry(now, storedCredits)); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return ctx, system, officialFitRegistry(now, handlerCredits), officialFitModel(), map[string]any{"model": "official-mid", "max_tokens": 65_536}
}

func writeAdmissionOfficialQuote(w http.ResponseWriter, token string, inputPer10k, outputPer10k float64) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token": token,
		"quote": map[string]any{
			"provider_id":         "agnes",
			"upstream_model":      "upstream-model",
			"service_group_id":    "official-group",
			"provider_multiplier": 1,
			"expires_at":          time.Now().UTC().Add(time.Minute),
			"pricing": map[string]any{
				"input_credits_per_10k":  inputPer10k,
				"output_credits_per_10k": outputPer10k,
			},
		},
	})
}

func sameGroups(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
