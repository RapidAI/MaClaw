package llmservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestTokenBankAudienceAllows(t *testing.T) {
	audiences := []llmpool.TokenBankAudience{
		{HubID: "Hub-A", TenantID: "Ten-1"},
		{HubID: "Hub-B"},
		{TenantID: "Ten-2"},
	}
	if !TokenBankAudienceAllows("public", nil, "other", "other") {
		t.Fatal("public allows everyone")
	}
	if !TokenBankAudienceAllows("", nil, "", "") {
		t.Fatal("empty visibility allows everyone")
	}
	if TokenBankAudienceAllows("private", nil, "Hub-A", "Ten-1") {
		t.Fatal("private with no audience matches nobody")
	}
	if !TokenBankAudienceAllows("private", audiences, "hub-a", "ten-1") {
		t.Fatal("both ids match case-insensitively")
	}
	if TokenBankAudienceAllows("private", audiences, "hub-a", "other") {
		t.Fatal("a row with both ids requires both")
	}
	if !TokenBankAudienceAllows("private", audiences, "hub-b", "any-tenant") {
		t.Fatal("hub-only row allows any tenant on that hub")
	}
	if !TokenBankAudienceAllows("private", audiences, "any-hub", "ten-2") {
		t.Fatal("tenant-only row allows any hub")
	}
	if TokenBankAudienceAllows("private", audiences, "nope", "nope") {
		t.Fatal("unlisted request is dropped")
	}
}

func TestTokenBankShareWindowSkipsMembersOutsideCheapHours(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	fridayMorning := time.Date(2026, 10, 2, 3, 30, 0, 0, loc)
	fridayNoon := time.Date(2026, 10, 2, 12, 0, 0, 0, loc)
	if fridayMorning.Weekday() != time.Friday {
		t.Fatalf("fixture weekday = %s", fridayMorning.Weekday())
	}
	cheapID := TokenBankMemberID("share-cheap", "gpt")
	alwaysID := TokenBankMemberID("share-always", "gpt")
	cheap := &llmpool.ProviderConfig{
		ID: cheapID,
		TokenBankShareWindow: llmpool.TokenBankShareWindow{
			Days:  []int{5},
			Start: "00:00",
			End:   "08:00",
		},
	}
	always := &llmpool.ProviderConfig{ID: alwaysID}
	morning := PrepareTokenBankEgress([]*llmpool.ProviderConfig{cheap, always}, "", "", "bucket", fridayMorning)
	if len(morning) != 2 {
		t.Fatalf("Friday 03:30 Beijing should keep the cheap member, got %v", memberIDs(morning))
	}
	noon := PrepareTokenBankEgress([]*llmpool.ProviderConfig{cheap, always}, "", "", "bucket", fridayNoon)
	if len(noon) != 1 || noon[0].ID != alwaysID {
		t.Fatalf("Friday noon should leave only the always-on member, got %v", memberIDs(noon))
	}
	closed := PrepareTokenBankEgress([]*llmpool.ProviderConfig{cheap}, "", "", "bucket", fridayNoon)
	if len(closed) != 0 {
		t.Fatalf("a closed window is not dialed even when it is the only member, got %v", memberIDs(closed))
	}
	if !tokenBankMemberServing(always, "", "", fridayNoon) {
		t.Fatal("a share with no window stays dialable")
	}
	if tokenBankMemberServing(cheap, "hub", "tenant", fridayNoon) {
		t.Fatal("the cheap member is not dialed outside its window")
	}
	closedWindow := &llmpool.ProviderConfig{
		ID:                   cheapID,
		TokenBankShareWindow: llmpool.TokenBankShareWindow{Start: "99:99"},
	}
	if !tokenBankCachedMemberAllowed(closedWindow, &ProxyRequest{HubID: "hub", TenantID: "tenant"}) {
		t.Fatal("a cache hit does not dial the key, so a closed window still returns the stored completion")
	}
	private := &llmpool.ProviderConfig{ID: alwaysID, TokenBankVisibility: "private"}
	if tokenBankCachedMemberAllowed(private, &ProxyRequest{HubID: "hub", TenantID: "tenant"}) {
		t.Fatal("a private cache hit still needs an audience")
	}
	if cheap.CreditMultiplier != 0 || len(cheap.CreditMultiplierSchedule) != 0 {
		t.Fatal("the dial window must not become a billing schedule")
	}
}

func TestTokenBankCanaryRouting(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	canaryID := TokenBankMemberID("share-canary", "gpt")
	siblingID := TokenBankMemberID("share-sibling", "gpt")
	canary := &llmpool.ProviderConfig{
		ID:                   canaryID,
		APIKey:               "primary-key",
		TokenBankCanaryUntil: now.Add(time.Hour).UTC().Format(time.RFC3339),
	}
	sibling := &llmpool.ProviderConfig{ID: siblingID, APIKey: "sibling-key"}
	hit := canaryBucket(t, true)
	miss := canaryBucket(t, false)

	hitOrder := PrepareTokenBankEgress([]*llmpool.ProviderConfig{sibling, canary}, "", "", hit, now)
	if len(hitOrder) != 2 || hitOrder[0].ID != canaryID {
		t.Fatalf("5%% bucket should try the canary first, got %v", memberIDs(hitOrder))
	}
	missOrder := PrepareTokenBankEgress([]*llmpool.ProviderConfig{sibling, canary}, "", "", miss, now)
	if len(missOrder) != 1 || missOrder[0].ID != siblingID {
		t.Fatalf("the other 95%% should skip the canary when a sibling exists, got %v", memberIDs(missOrder))
	}
	only := PrepareTokenBankEgress([]*llmpool.ProviderConfig{canary}, "", "", miss, now)
	if len(only) != 1 || only[0].ID != canaryID {
		t.Fatal("a canary with no sibling still serves")
	}
	if TokenBankCanaryHit("") {
		t.Fatal("an empty request id is not a canary hit")
	}

	expired := *canary
	expired.TokenBankCanaryUntil = now.Add(-time.Hour).UTC().Format(time.RFC3339)
	if tokenBankMemberInCanary(&expired, now) {
		t.Fatal("after 24h with fewer than 5 samples the member is promoted")
	}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(expired.ID, 500, time.Millisecond, "", "")
	}
	if !tokenBankMemberInCanary(&expired, now) {
		t.Fatal("five failures after the window keep the member limited")
	}
	healthyID := TokenBankMemberID("share-healthy", "gpt")
	healthy := &llmpool.ProviderConfig{ID: healthyID, TokenBankCanaryUntil: expired.TokenBankCanaryUntil}
	for i := 0; i < 5; i++ {
		noteMemberAttempt(healthyID, 200, time.Millisecond, "", "")
	}
	if tokenBankMemberInCanary(healthy, now) {
		t.Fatal("a healthy member is promoted after the window")
	}
}

func TestTokenBankOtherModelIsNotCanarySupply(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	gptID := TokenBankMemberID("share-gpt", "gpt-4o")
	claudeID := TokenBankMemberID("share-claude", "claude-3")
	siblingID := TokenBankMemberID("share-sibling", "gpt-4o")
	gpt := &llmpool.ProviderConfig{
		ID:                   gptID,
		Models:               []string{"gpt-4o"},
		TokenBankCanaryUntil: now.Add(time.Hour).UTC().Format(time.RFC3339),
	}
	claude := &llmpool.ProviderConfig{ID: claudeID, Models: []string{"claude-3"}}
	sibling := &llmpool.ProviderConfig{ID: siblingID, Models: []string{"gpt-4o"}}
	miss := canaryBucket(t, false)

	filtered := tokenBankMembersForModel([]*llmpool.ProviderConfig{claude, gpt}, "gpt-4o")
	got := PrepareTokenBankEgress(filtered, "", "", miss, now)
	if len(got) != 1 || got[0].ID != gptID {
		t.Fatalf("another model's member hid the only supply, got %v", memberIDs(got))
	}

	filtered = tokenBankMembersForModel([]*llmpool.ProviderConfig{claude, sibling, gpt}, "GPT-4o")
	got = PrepareTokenBankEgress(filtered, "", "", miss, now)
	if len(got) != 1 || got[0].ID != siblingID {
		t.Fatalf("a same-model sibling should still take the other 95%%, got %v", memberIDs(got))
	}
}

func TestTokenBankArrayRotationStaysInsideTheModel(t *testing.T) {
	g1 := TokenBankMemberID("share-1", "gpt-4o")
	g2 := TokenBankMemberID("share-2", "gpt-4o")
	claude := TokenBankMemberID("share-3", "claude-3")
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: g1, ArrayID: TokenBankArrayMid},
			{ID: claude, ArrayID: TokenBankArrayMid},
			{ID: g2, ArrayID: TokenBankArrayMid},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{g1, claude, g2},
		}},
	}
	clearProviderArrayCursor(TokenBankArrayMid, dispatchOrderKey(TokenBankArrayMid, "gpt-4o", []*llmpool.ProviderConfig{{ID: g1}}), dispatchOrderKey(TokenBankArrayMid, "claude-3", []*llmpool.ProviderConfig{{ID: claude}}))

	var got []string
	for i := 0; i < 3; i++ {
		_, _, members := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "gpt-4o")
		if len(members) != 2 {
			t.Fatalf("gpt members = %v", memberIDs(members))
		}
		for _, member := range members {
			if member.ID == claude {
				t.Fatalf("claude entered the gpt rotation: %v", memberIDs(members))
			}
		}
		got = append(got, members[0].ID)
		_, _, other := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "claude-3")
		if len(other) != 1 || other[0].ID != claude {
			t.Fatalf("claude rotation = %v", memberIDs(other))
		}
	}
	if got[0] != g1 || got[1] != g2 || got[2] != g1 {
		t.Fatalf("gpt rotation = %v, want %s then %s then %s", got, g1, g2, g1)
	}
}

func TestTokenBankBillingBandUsesEveryMemberOfTheTier(t *testing.T) {
	hy3 := TokenBankMemberID("share-hy", "hy3")
	glm := TokenBankMemberID("share-glm", "glm-5.3-flash")
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: hy3, ArrayID: TokenBankArrayMid, Models: []string{"hy3"}},
			{ID: glm, ArrayID: TokenBankArrayMid, Models: []string{"glm-5.3-flash"}},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{hy3, glm},
		}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "redeem",
			Models: []llmpool.ModelConfig{
				{
					Name:        "official-mid",
					ProviderIDs: []string{TokenBankArrayMid, "opencode-1"},
					ProviderConfigs: []llmpool.ModelProviderConfig{
						{ProviderID: TokenBankArrayMid},
						{ProviderID: "opencode-1"},
					},
				},
				{
					Name:            "hy3",
					ProviderIDs:     []string{TokenBankArrayMid},
					ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: TokenBankArrayMid}},
				},
			},
		}},
	}
	clearProviderArrayCursor(TokenBankArrayMid, dispatchOrderKey(TokenBankArrayMid, "hy3", []*llmpool.ProviderConfig{{ID: hy3}}))

	var first []string
	for i := 0; i < 4; i++ {
		_, _, members := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "official-mid")
		if len(members) != 2 {
			t.Fatalf("official-mid members = %v", memberIDs(members))
		}
		first = append(first, members[0].ID)
		if got := memberUpstreamModel(members[0], "", "official-mid", ""); got == "" || isBillingBandName(got) {
			t.Fatalf("upstream for %s = %q", members[0].ID, got)
		}
	}
	if first[0] != hy3 || first[1] != glm || first[2] != hy3 || first[3] != glm {
		t.Fatalf("band rotation = %v, want %s and %s alternating", first, hy3, glm)
	}
	_, _, only := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "hy3")
	if len(only) != 1 || only[0].ID != hy3 {
		t.Fatalf("hy3 request = %v", memberIDs(only))
	}
	_, _, short := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "mid")
	if len(short) != 2 {
		t.Fatalf("mid members = %v", memberIDs(short))
	}

	pruneTokenBankArrayRoutes(reg)
	if !routeTargets(reg, "official-mid", TokenBankArrayMid) || !routeTargets(reg, "hy3", TokenBankArrayMid) {
		t.Fatal("a live tier lost its band or concrete route")
	}
	reg.Providers = reg.Providers[1:]
	pruneTokenBankArrayRoutes(reg)
	if !routeTargets(reg, "official-mid", TokenBankArrayMid) {
		t.Fatal("official-mid was pruned while a tier member remained")
	}
	if routeTargets(reg, "hy3", TokenBankArrayMid) {
		t.Fatal("hy3 stayed routed after its only member left")
	}
	reg.Providers = nil
	pruneTokenBankArrayRoutes(reg)
	if routeTargets(reg, "official-mid", TokenBankArrayMid) {
		t.Fatal("official-mid stayed routed after the tier was emptied")
	}
}

func routeTargets(reg *Registry, model, providerID string) bool {
	if reg == nil {
		return false
	}
	for i := range reg.ServiceGroups {
		for j := range reg.ServiceGroups[i].Models {
			cfg := &reg.ServiceGroups[i].Models[j]
			if !strings.EqualFold(strings.TrimSpace(cfg.Name), model) {
				continue
			}
			return containsProviderID(cfg.ProviderIDs, providerID)
		}
	}
	return false
}

func TestTokenBankClosedWindowDoesNotConsumeARotationTurn(t *testing.T) {
	closedID := TokenBankMemberID("share-closed", "gpt-4o")
	openA := TokenBankMemberID("share-open-a", "gpt-4o")
	openB := TokenBankMemberID("share-open-b", "gpt-4o")
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{
				ID:                   closedID,
				ArrayID:              TokenBankArrayMid,
				TokenBankShareWindow: llmpool.TokenBankShareWindow{Start: "99:99"},
			},
			{ID: openA, ArrayID: TokenBankArrayMid},
			{ID: openB, ArrayID: TokenBankArrayMid},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{closedID, openA, openB},
		}},
	}
	clearProviderArrayCursor(dispatchOrderKey(TokenBankArrayMid, "gpt-4o", []*llmpool.ProviderConfig{{ID: openA}}))
	var got []string
	for i := 0; i < 4; i++ {
		_, _, members := arrayEgressCandidates(reg, TokenBankArrayMid, acceptLiveProvider, "gpt-4o")
		if len(members) != 2 {
			t.Fatalf("call %d = %v", i, memberIDs(members))
		}
		for _, member := range members {
			if member.ID == closedID {
				t.Fatalf("closed member stayed in rotation: %v", memberIDs(members))
			}
		}
		got = append(got, members[0].ID)
	}
	if got[0] != openA || got[1] != openB || got[2] != openA || got[3] != openB {
		t.Fatalf("rotation = %v, want %s and %s alternating", got, openA, openB)
	}
}

func TestTokenBankShareWindowClosedRouteIsDistinctFromAMissingProvider(t *testing.T) {
	closedID := TokenBankMemberID("share-closed", "gpt-4o")
	openID := TokenBankMemberID("share-open", "gpt-4o")
	otherID := TokenBankMemberID("share-other", "claude-3")
	closed := llmpool.TokenBankShareWindow{Start: "99:99"}
	mixed := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: closedID, ArrayID: TokenBankArrayMid, TokenBankShareWindow: closed},
			{ID: openID, ArrayID: TokenBankArrayMid},
			{ID: otherID, ArrayID: TokenBankArrayMid, TokenBankShareWindow: closed},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{closedID, openID, otherID},
		}},
	}
	if arrayRouteShareWindowClosed(mixed, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "", "") {
		t.Fatal("an open gpt sibling keeps the route available")
	}
	onlyClosed := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: closedID, ArrayID: TokenBankArrayMid, TokenBankShareWindow: closed},
			{ID: otherID, ArrayID: TokenBankArrayMid},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{closedID, otherID},
		}},
	}
	if !arrayRouteShareWindowClosed(onlyClosed, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "", "") {
		t.Fatal("gpt-4o has only a closed member")
	}
	if arrayRouteShareWindowClosed(onlyClosed, TokenBankArrayMid, acceptLiveProvider, "claude-3", "", "") {
		t.Fatal("claude-3 is always on and must not inherit gpt-4o's closed window")
	}
	if arrayRouteShareWindowClosed(&Registry{}, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "", "") {
		t.Fatal("a missing provider is not a closed window")
	}
	privateID := TokenBankMemberID("share-private", "gpt-4o")
	privateClosed := &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:                   privateID,
			ArrayID:              TokenBankArrayMid,
			TokenBankVisibility:  "private",
			TokenBankAudiences:   []llmpool.TokenBankAudience{{HubID: "hub-a", TenantID: "ten-1"}},
			TokenBankShareWindow: closed,
		}},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{privateID},
		}},
	}
	if arrayRouteShareWindowClosed(privateClosed, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "hub-b", "ten-9") {
		t.Fatal("another tenant must not learn a private share's hours")
	}
	if !arrayRouteShareWindowClosed(privateClosed, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "hub-a", "ten-1") {
		t.Fatal("the invited tenant is told the share is outside its window")
	}
}

func TestTokenBankPausedSiblingDoesNotHideAClosedWindow(t *testing.T) {
	pausedOther := TokenBankMemberID("share-paused", "claude-3")
	closedGPT := TokenBankMemberID("share-closed", "gpt-4o")
	pausedGPT := TokenBankMemberID("share-paused-gpt", "gpt-4o")
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: pausedOther, ArrayID: TokenBankArrayMid, Paused: true},
			{ID: closedGPT, ArrayID: TokenBankArrayMid, TokenBankShareWindow: llmpool.TokenBankShareWindow{Start: "99:99"}},
			{ID: pausedGPT, ArrayID: TokenBankArrayMid, Paused: true},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID:        TokenBankArrayMid,
			MemberIDs: []string{pausedOther, closedGPT, pausedGPT},
		}},
	}
	if arrayRouteModelPaused(reg, TokenBankArrayMid, acceptLiveProvider, "gpt-4o") {
		t.Fatal("gpt-4o still has a member that is only outside its window")
	}
	if !arrayRouteShareWindowClosed(reg, TokenBankArrayMid, acceptLiveProvider, "gpt-4o", "", "") {
		t.Fatal("gpt-4o should be reported as outside its window, not paused")
	}
	if !arrayRouteModelPaused(reg, TokenBankArrayMid, acceptLiveProvider, "claude-3") {
		t.Fatal("claude-3's own member is paused")
	}
	plain := &Registry{Providers: []llmpool.ProviderConfig{{ID: "p1", Paused: true}}}
	if !arrayRouteModelPaused(plain, "p1", acceptLiveProvider, "gpt-4") {
		t.Fatal("a paused provider is still paused")
	}
}

func TestUpdateProviderKeepsTokenBankDialWindow(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	id := TokenBankMemberID("share-1", "gpt-4o")
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:                       id,
		Name:                     "night",
		APIURL:                   "https://example.com/v1",
		TokenBankOwnerUserID:     "owner-1",
		TokenBankTier:            "mid",
		TokenBankTierMultiplier:  1,
		TokenBankVisibility:      "private",
		TokenBankAudiences:       []llmpool.TokenBankAudience{{HubID: "hub-a", TenantID: "ten-1"}},
		TokenBankCanaryUntil:     "2026-10-03T00:00:00Z",
		TokenBankExtraKeys:       []string{"sk-extra"},
		MaxInputTokensPerRequest: 1000,
		TokenBankShareWindow: llmpool.TokenBankShareWindow{
			Days:  []int{1, 2, 3, 4, 5},
			Start: "22:00",
			End:   "08:00",
		},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Admin rename and array move PUT the form fields only. The share snapshot
	// is absent, which decodes as the zero value.
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:       id,
		Name:     "renamed",
		APIURL:   "https://example.com/v1",
		Protocol: "openai",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := svc.GetProvider(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("reload: %#v err=%v", got, err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name = %s", got.Name)
	}
	if got.TokenBankShareWindow.Start != "22:00" || got.TokenBankShareWindow.End != "08:00" || len(got.TokenBankShareWindow.Days) != 5 {
		t.Fatalf("window = %+v", got.TokenBankShareWindow)
	}
	if got.TokenBankOwnerUserID != "owner-1" || got.TokenBankTier != "mid" || got.TokenBankTierMultiplier != 1 {
		t.Fatalf("identity = %+v", got)
	}
	if got.TokenBankVisibility != "private" || len(got.TokenBankAudiences) != 1 || got.TokenBankAudiences[0].HubID != "hub-a" {
		t.Fatalf("audience = %s %+v", got.TokenBankVisibility, got.TokenBankAudiences)
	}
	if got.TokenBankCanaryUntil != "2026-10-03T00:00:00Z" || len(got.TokenBankExtraKeys) != 1 || got.MaxInputTokensPerRequest != 1000 {
		t.Fatalf("caps = canary %s keys %v input %d", got.TokenBankCanaryUntil, got.TokenBankExtraKeys, got.MaxInputTokensPerRequest)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:                   id,
		Name:                 "renamed",
		APIURL:               "https://example.com/v1",
		TokenBankVisibility:  "public",
		TokenBankShareWindow: llmpool.TokenBankShareWindow{Start: "00:00", End: "08:00"},
	}); err != nil {
		t.Fatalf("explicit update: %v", err)
	}
	got, err = svc.GetProvider(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("reload explicit: %#v err=%v", got, err)
	}
	if got.TokenBankVisibility != "public" || got.TokenBankShareWindow.Start != "00:00" || got.TokenBankShareWindow.End != "08:00" || len(got.TokenBankShareWindow.Days) != 0 {
		t.Fatalf("explicit share = %s %+v", got.TokenBankVisibility, got.TokenBankShareWindow)
	}
	if got.TokenBankOwnerUserID != "owner-1" || len(got.TokenBankAudiences) != 1 {
		t.Fatalf("explicit update dropped the rest of the share: %+v", got)
	}
}

func clearProviderArrayCursor(ids ...string) {
	providerArrayCursor.Lock()
	defer providerArrayCursor.Unlock()
	if providerArrayCursor.next == nil {
		providerArrayCursor.next = map[string]int{}
	}
	for _, id := range ids {
		delete(providerArrayCursor.next, id)
	}
}

func TestTokenBankMemberUpstreamModelStaysOnItsModel(t *testing.T) {
	member := &llmpool.ProviderConfig{
		ID:     TokenBankMemberID("share", "gpt-4o"),
		Models: []string{"gpt-4o"},
	}
	if got := memberUpstreamModel(member, "", "gpt-4o", ""); got != "gpt-4o" {
		t.Fatalf("matching model = %q", got)
	}
	if got := memberUpstreamModel(member, "", "GPT-4o", ""); got != "gpt-4o" {
		t.Fatalf("case-insensitive model = %q", got)
	}
	if got := memberUpstreamModel(member, "claude-3", "claude-3", ""); got != "" {
		t.Fatalf("foreign model = %q, want a skip", got)
	}
	for _, band := range []string{"official-mid", "mid", "official-high", "high", "auto"} {
		if got := memberUpstreamModel(member, "", band, ""); got != "gpt-4o" {
			t.Fatalf("billing band %s = %q, want the member model", band, got)
		}
		// The route key stays the caller's logical model. Dispatch has already
		// canonicalized auto/mid/high before this function sees them; rewriting
		// official-mid into the member catalog name misses the service-group route.
		if got := proxyUpstreamModelForRoute(llmpool.DispatchProviderRoute{}, member, band); got != band {
			t.Fatalf("route key for %s = %q, want the caller's logical model", band, got)
		}
	}
	if got := proxyUpstreamModelForRoute(llmpool.DispatchProviderRoute{}, member, "gpt-4o"); got != "gpt-4o" {
		t.Fatalf("concrete route key = %q", got)
	}
	plain := &llmpool.ProviderConfig{Models: []string{"agnes-3.0-flash", "agnes-2.0-flash"}}
	if got := memberUpstreamModel(plain, "DeepSeek-V4-Flash-0731", "qwen-plus", ""); got != "agnes-3.0-flash" {
		t.Fatalf("non-bank foreign pin = %q", got)
	}
}

func TestTokenBankQuoteUsesTheMemberALiveDialWould(t *testing.T) {
	arrayID := "tb-quote-rotate"
	clearProviderArrayCursor(arrayID)
	t.Cleanup(func() { clearProviderArrayCursor(arrayID) })
	stableA := TokenBankMemberID("share-a", "hy3")
	stableB := TokenBankMemberID("share-b", "glm-5.3-flash")
	canaryID := TokenBankMemberID("share-c", "deepseek-v4")
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: stableA, Models: []string{"hy3"}, ArrayID: arrayID},
			{ID: stableB, Models: []string{"glm-5.3-flash"}, ArrayID: arrayID},
			{ID: canaryID, Models: []string{"deepseek-v4"}, ArrayID: arrayID, TokenBankCanaryUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID: arrayID, MemberIDs: []string{stableA, stableB, canaryID},
		}},
	}
	miss := canaryBucket(t, false)
	hit := canaryBucket(t, true)
	quoteMember := func(requestID string) string {
		t.Helper()
		_, profile, _ := tokenBankRouteMembers(reg, arrayID, acceptLiveProvider, "official-mid", &ProxyRequest{RequestID: requestID}, false)
		if profile == nil {
			t.Fatal("quote selected no member")
		}
		return profile.ID
	}
	if got := quoteMember(miss); got != stableA {
		t.Fatalf("first quote member = %s, want %s", got, stableA)
	}
	if got := quoteMember(hit); got != canaryID {
		t.Fatalf("canary quote member = %s, want %s", got, canaryID)
	}
	if got := quoteMember(miss); got != stableB {
		t.Fatalf("quote after a canary hit = %s, want %s", got, stableB)
	}
	pauseArrayMember(stableA, time.Hour)
	t.Cleanup(func() { clearArrayMemberPause(stableA) })
	if got := quoteMember(miss); got == stableA {
		t.Fatalf("cooling member was quoted: %s", got)
	}
}

func TestTokenBankPublishKeepsCanaryDeadline(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	first := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	spec := publishSpec("share-canary", "gpt-4o-mini", "default")
	spec.Visibility = "private"
	spec.Audiences = []llmpool.TokenBankAudience{{HubID: "hub-a"}}
	spec.CanaryUntil = first
	spec.ExtraKeys = []string{"sk-extra-key"}
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{spec}); err != nil {
		t.Fatal(err)
	}
	memberID := TokenBankMemberID("share-canary", "gpt-4o-mini")
	provider := mustGetProvider(t, svc, ctx, memberID)
	if provider.TokenBankVisibility != "private" || len(provider.TokenBankAudiences) != 1 || provider.TokenBankAudiences[0].HubID != "hub-a" {
		t.Fatalf("published access = %s %+v", provider.TokenBankVisibility, provider.TokenBankAudiences)
	}
	if provider.TokenBankCanaryUntil != first.UTC().Format(time.RFC3339) {
		t.Fatalf("canary = %q", provider.TokenBankCanaryUntil)
	}
	if len(provider.TokenBankExtraKeys) != 1 || provider.TokenBankExtraKeys[0] != "sk-extra-key" {
		t.Fatalf("extra keys = %v", provider.TokenBankExtraKeys)
	}

	spec.CanaryUntil = first.Add(48 * time.Hour)
	spec.Visibility = "public"
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{spec}); err != nil {
		t.Fatal(err)
	}
	updated := mustGetProvider(t, svc, ctx, memberID)
	if updated.TokenBankCanaryUntil != first.UTC().Format(time.RFC3339) {
		t.Fatalf("republish restarted the canary: %s", updated.TokenBankCanaryUntil)
	}
	if updated.TokenBankVisibility != "public" {
		t.Fatalf("republish should apply the new visibility, got %s", updated.TokenBankVisibility)
	}
}

func TestRotateTokenBankKeyCyclesWithoutMutating(t *testing.T) {
	member := &llmpool.ProviderConfig{
		ID:                 TokenBankMemberID("share-rotate", "gpt"),
		APIKey:             "primary-key-aaaa",
		TokenBankExtraKeys: []string{"extra-key-bbbb", "primary-key-aaaa", ""},
	}
	seen := map[string]int{}
	for i := 0; i < 12; i++ {
		got := RotateTokenBankKey(member)
		seen[got.APIKey]++
		if got == member {
			t.Fatal("rotation with two keys must return a copy")
		}
	}
	if member.APIKey != "primary-key-aaaa" {
		t.Fatal("rotation mutated the stored member")
	}
	if seen["primary-key-aaaa"] == 0 || seen["extra-key-bbbb"] == 0 {
		t.Fatalf("keys were not both used: %v", seen)
	}
	msg := RedactProviderSecrets("upstream said primary-key-aaaa and extra-key-bbbb", member)
	if strings.Contains(msg, "primary-key-aaaa") || strings.Contains(msg, "extra-key-bbbb") {
		t.Fatalf("secrets leaked: %s", msg)
	}
	base := errors.New("upstream said extra-key-bbbb")
	wrapped := redactConfiguredError(member, base)
	if strings.Contains(wrapped.Error(), "extra-key-bbbb") || !errors.Is(wrapped, base) {
		t.Fatalf("redacted error = %v", wrapped)
	}
	plain := errors.New("no secret here")
	if redactConfiguredError(member, plain) != plain {
		t.Fatal("an error without a key should stay the same value")
	}
}

func canaryBucket(t *testing.T, want bool) string {
	t.Helper()
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("req-%d", i)
		if TokenBankCanaryHit(id) == want {
			return id
		}
	}
	t.Fatal("could not find a request id on the requested side of the 5% cut")
	return ""
}

func memberIDs(members []*llmpool.ProviderConfig) []string {
	out := make([]string, len(members))
	for i, member := range members {
		out[i] = member.ID
	}
	return out
}
