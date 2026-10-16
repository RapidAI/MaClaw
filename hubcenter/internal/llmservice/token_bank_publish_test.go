package llmservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// publishTestService builds a service with the three tier arrays seeded, which
// is what InitLLMModule does before any share can be published.
func publishTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure token bank arrays: %v", err)
	}
	return svc, ctx
}

func addPublishTestServiceGroup(t *testing.T, svc *Service, ctx context.Context, id string) {
	t.Helper()
	if err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID: id, Name: id, AccessPolicy: "free",
	}); err != nil {
		t.Fatalf("add service group %s: %v", id, err)
	}
}

func publishSpec(shareID, model, groupID string) TokenBankPublishSpec {
	return TokenBankPublishSpec{
		ShareID:        shareID,
		OwnerUserID:    "owner-1",
		DisplayName:    "my-share",
		Model:          model,
		ArrayID:        TokenBankArrayMid,
		APIURL:         "https://api.example.com/v1",
		APIKey:         "sk-upstream-secret",
		Protocol:       "openai",
		ServiceGroupID: groupID,
	}
}

// mustGetProvider fails the test instead of panicking when a member is absent.
// GetProvider reports absence as (nil, nil), not as an error.
func mustGetProvider(t *testing.T, svc *Service, ctx context.Context, id string) *llmpool.ProviderConfig {
	t.Helper()
	provider, err := svc.GetProvider(ctx, id)
	if err != nil {
		t.Fatalf("GetProvider(%s) error = %v", id, err)
	}
	if provider == nil {
		t.Fatalf("provider %s does not exist", id)
	}
	return provider
}

// TestTokenBankMemberIDRoundTrips pins the encoding contract. Every downstream
// consumer — settlement, the admin UI, the unpublish path — recovers share and
// model from the id alone, so a model name with characters that are meaningful
// elsewhere in the system must survive the round trip.
func TestTokenBankMemberIDRoundTrips(t *testing.T) {
	cases := []struct{ share, model string }{
		{"share-1", "gpt-4o-mini"},
		{"share-1", "meta-llama/Llama-3.3-70B-Instruct"},
		{"share_2", "claude-3-5-sonnet@20240620"},
		{"share-3", "模型/中文名"},
		// A model that itself contains the separator: without encoding this
		// would parse as share="share-4", model="x" with a trailing "__y".
		{"share-4", "x__y"},
	}
	for _, tc := range cases {
		id := TokenBankMemberID(tc.share, tc.model)
		if id == "" {
			t.Fatalf("TokenBankMemberID(%q, %q) = empty", tc.share, tc.model)
		}
		gotShare, gotModel, ok := ParseTokenBankMemberID(id)
		if !ok {
			t.Fatalf("ParseTokenBankMemberID(%q) not recognized", id)
		}
		if gotShare != tc.share || gotModel != tc.model {
			t.Fatalf("round trip of (%q, %q) = (%q, %q)", tc.share, tc.model, gotShare, gotModel)
		}
	}
	// A non-member id must be rejected rather than parsed into a bogus share.
	if IsTokenBankMemberID("openai-official") {
		t.Fatal("a plain provider id must not be treated as a token bank member")
	}
	if IsTokenBankMemberID("tbk_share-without-separator") {
		t.Fatal("a member id without a separator must not parse")
	}
}

func TestTokenBankPublishCreatesMemberAndJoinsArray(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")

	result, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-1", "gpt-4o-mini", "default"),
	})
	if err != nil {
		t.Fatalf("PublishTokenBankShare() error = %v", err)
	}
	memberID := TokenBankMemberID("share-1", "gpt-4o-mini")
	if len(result.Added) != 1 || result.Added[0] != memberID {
		t.Fatalf("added = %v, want [%s]", result.Added, memberID)
	}

	provider := mustGetProvider(t, svc, ctx, memberID)
	// §3.4-A1: the member joins the tier array, and carries no multiplier of
	// its own. The tier rate is applied only when crediting the owner.
	if provider.ArrayID != TokenBankArrayMid {
		t.Fatalf("array_id = %q, want %q", provider.ArrayID, TokenBankArrayMid)
	}
	if provider.CreditMultiplier != 0 && provider.CreditMultiplier != 1 {
		t.Fatalf("credit_multiplier = %v, want neutral", provider.CreditMultiplier)
	}
	if len(provider.Models) != 1 || provider.Models[0] != "gpt-4o-mini" {
		t.Fatalf("models = %v, want [gpt-4o-mini]", provider.Models)
	}
	if provider.APIKey != "sk-upstream-secret" {
		t.Fatalf("api key was not carried onto the member")
	}

	// The model must now be routable through the group, or the share is
	// published-but-unreachable — the exact failure this file exists to stop.
	// The route target is the tier ARRAY: several owners may share a model of
	// the same name, and they all land in one array for load balancing (§3.4).
	group, model, err := svc.FindServiceGroupForModel(ctx, "default", "gpt-4o-mini")
	if err != nil {
		t.Fatalf("FindServiceGroupForModel() error = %v", err)
	}
	if group == nil || model == nil {
		t.Fatal("the published model is not reachable through its service group")
	}
	if !containsProviderID(model.ProviderIDs, TokenBankArrayMid) {
		t.Fatalf("service group routes = %v, want to contain %s", model.ProviderIDs, TokenBankArrayMid)
	}

	// The array membership is derived, and must now list the member.
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayMid)
	if arr == nil || !containsProviderID(arr.MemberIDs, memberID) {
		t.Fatalf("array %s members = %v, want to contain %s", TokenBankArrayMid, arr.MemberIDs, memberID)
	}
}

func TestTokenBankPublishIsIdempotent(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	specs := []TokenBankPublishSpec{publishSpec("share-1", "gpt-4o-mini", "default")}

	if _, err := svc.PublishTokenBankShare(ctx, specs); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	second, err := svc.PublishTokenBankShare(ctx, specs)
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if len(second.Added) != 0 {
		t.Fatalf("second publish added %v, want nothing", second.Added)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	count := 0
	for _, p := range reg.Providers {
		if strings.EqualFold(p.ID, TokenBankMemberID("share-1", "gpt-4o-mini")) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("member appears %d times, want 1", count)
	}
	// And the service group must not accumulate a duplicate route entry.
	group := findServiceGroupByID(reg, "default")
	if group == nil {
		t.Fatal("service group disappeared")
	}
	for _, m := range group.Models {
		if strings.EqualFold(m.Name, "gpt-4o-mini") && len(m.ProviderConfigs) != 1 {
			t.Fatalf("provider configs = %d, want 1", len(m.ProviderConfigs))
		}
	}
}

// TestTokenBankRepublishDropsRetiredModels is the failure that motivated
// reconciling the whole share rather than upserting model by model: an owner
// unselects a model, and if the stale member survives it keeps dispatching to
// an upstream the owner believes they stopped sharing.
func TestTokenBankRepublishDropsRetiredModels(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")

	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-1", "gpt-4o", "default"),
		publishSpec("share-1", "gpt-4o-mini", "default"),
	}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	// Re-publish with only one model; the other must lose its member.
	result, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-1", "gpt-4o", "default"),
	})
	if err != nil {
		t.Fatalf("republish: %v", err)
	}
	retired := TokenBankMemberID("share-1", "gpt-4o-mini")
	if len(result.Removed) != 1 || result.Removed[0] != retired {
		t.Fatalf("removed = %v, want [%s]", result.Removed, retired)
	}
	if provider, err := svc.GetProvider(ctx, retired); err != nil {
		t.Fatalf("GetProvider(%s) error = %v", retired, err)
	} else if provider != nil {
		t.Fatalf("retired member %s still exists", retired)
	}
	// It must also be gone from every group's routing table, not merely from
	// the provider list. A route naming a member that no longer resolves would
	// stay in the dispatch candidate list and fail at send time.
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for _, group := range reg.ServiceGroups {
		for _, m := range group.Models {
			if containsProviderID(m.ProviderIDs, retired) {
				t.Fatalf("retired member %s still routed by group %s", retired, group.ID)
			}
		}
	}
	// The retired model must not keep the tier array. The array still holds
	// gpt-4o, and that route has to stay.
	for _, group := range reg.ServiceGroups {
		for _, m := range group.Models {
			routed := containsProviderID(m.ProviderIDs, TokenBankArrayMid)
			switch strings.ToLower(strings.TrimSpace(m.Name)) {
			case "gpt-4o-mini":
				if routed {
					t.Fatalf("retired model %s still routes %s", m.Name, TokenBankArrayMid)
				}
			case "gpt-4o":
				if !routed {
					t.Fatalf("live model %s lost its array route", m.Name)
				}
			}
		}
	}
	// The array must survive: the same tier still holds the live model, and
	// dropping the route would take that model offline too.
	arr := findProviderArray(reg, TokenBankArrayMid)
	if arr == nil || !containsProviderID(arr.MemberIDs, TokenBankMemberID("share-1", "gpt-4o")) {
		t.Fatalf("live sibling was evicted along with the retired member: %v", arr)
	}
}

// TestTokenBankRepublishKeepsAdminPause pins that a republish refreshes
// credentials and routing but does not silently un-pause a share an admin
// suspended.
func TestTokenBankRepublishKeepsAdminPause(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	specs := []TokenBankPublishSpec{publishSpec("share-1", "gpt-4o-mini", "default")}

	if _, err := svc.PublishTokenBankShare(ctx, specs); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := svc.SetTokenBankSharePaused(ctx, "share-1", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := svc.PublishTokenBankShare(ctx, specs); err != nil {
		t.Fatalf("republish: %v", err)
	}
	provider := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-1", "gpt-4o-mini"))
	if !provider.Paused {
		t.Fatal("republishing a share un-paused a member an admin had suspended")
	}
}

// TestTokenBankRepublishKeepsMemberCapabilityTags is the desktop "adjust
// models" path. Republish overlays the share onto the existing member:
// operator edits and the member token price stay, share fields refresh, and
// the consumer multiplier and account login do not remain on the member.
func TestTokenBankRepublishKeepsMemberCapabilityTags(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	first := publishSpec("share-1", "gpt-4o", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{first}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	memberID := TokenBankMemberID("share-1", "gpt-4o")
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		idx := providerIndex(reg, memberID)
		if idx < 0 {
			t.Fatal("member missing before republish")
		}
		reg.Providers[idx].CapabilityTags = []string{"tools", "vision"}
		reg.Providers[idx].DispatchWeight = 4
		reg.Providers[idx].ModelMap = map[string]string{"gpt-4o": "gpt-4o-2024"}
		reg.Providers[idx].AllowedNodeIDs = []string{"hc-1"}
		reg.Providers[idx].RequestsPerDay = 200
		reg.Providers[idx].CreditMultiplier = 9
		reg.Providers[idx].Timezone = "UTC"
		reg.Providers[idx].TokenPricing = llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6}
		reg.Providers[idx].AuthKind = llmpool.ProviderAuthWorkBuddy
		reg.Providers[idx].WorkBuddyRefreshToken = "refresh-secret"
		group := findServiceGroupByID(reg, "default")
		if group == nil {
			t.Fatal("service group missing")
		}
		model := findOrCreateModelConfig(group, "gpt-4o")
		model.CapabilityTags = []string{"reasoning"}
		if len(model.ProviderConfigs) == 0 {
			t.Fatal("published model has no route")
		}
		model.ProviderConfigs[0].CapabilityTags = []string{"document"}
		return true, nil
	}); err != nil {
		t.Fatalf("stamp tags: %v", err)
	}

	// Same share, one new model, and a new key, name, and window. Share-owned
	// fields move. Operator fields on the existing member stay. A new member
	// starts without them. Billing and account login do not stick to the member.
	first.APIKey = "sk-rotated"
	first.DisplayName = "renamed"
	first.ShareWindow = llmpool.TokenBankShareWindow{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "08:00"}
	addedSpec := publishSpec("share-1", "gpt-4o-mini", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{first, addedSpec}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	kept := mustGetProvider(t, svc, ctx, memberID)
	if strings.Join(kept.CapabilityTags, ",") != "tools,vision" {
		t.Fatalf("member capability tags = %#v", kept.CapabilityTags)
	}
	if kept.DispatchWeight != 4 || kept.ModelMap["gpt-4o"] != "gpt-4o-2024" ||
		strings.Join(kept.AllowedNodeIDs, ",") != "hc-1" || kept.RequestsPerDay != 200 {
		t.Fatalf("operator fields were rebuilt away: %+v", kept)
	}
	if kept.APIKey != "sk-rotated" || !strings.Contains(kept.Name, "renamed") ||
		kept.TokenBankShareWindow.Start != "22:00" || kept.TokenBankShareWindow.End != "08:00" {
		t.Fatalf("share fields were not refreshed: key=%s name=%s window=%+v", kept.APIKey, kept.Name, kept.TokenBankShareWindow)
	}
	if kept.CreditMultiplier != 1 || kept.Timezone != llmpool.DefaultCreditMultiplierTimezone || len(kept.CreditMultiplierSchedule) != 0 {
		t.Fatalf("billing stuck to the member: multiplier=%v tz=%s schedule=%v", kept.CreditMultiplier, kept.Timezone, kept.CreditMultiplierSchedule)
	}
	if kept.TokenPricing.InputCreditsPer10K != 3 || kept.TokenPricing.OutputCreditsPer10K != 6 {
		t.Fatalf("member price = %#v, want the operator price kept", kept.TokenPricing)
	}
	if kept.AuthKind != "" || kept.WorkBuddyRefreshToken != "" {
		t.Fatalf("account login stuck to the member: kind=%s refresh=%s", kept.AuthKind, kept.WorkBuddyRefreshToken)
	}
	added := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-1", "gpt-4o-mini"))
	if len(added.CapabilityTags) != 0 || added.DispatchWeight != 0 || added.APIKey != "sk-upstream-secret" {
		t.Fatalf("new member = tags %#v weight %d key %s", added.CapabilityTags, added.DispatchWeight, added.APIKey)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	group := findServiceGroupByID(reg, "default")
	if group == nil {
		t.Fatal("service group disappeared")
	}
	var existing, fresh *llmpool.ModelConfig
	for i := range group.Models {
		switch strings.ToLower(strings.TrimSpace(group.Models[i].Name)) {
		case "gpt-4o":
			existing = &group.Models[i]
		case "gpt-4o-mini":
			fresh = &group.Models[i]
		}
	}
	if existing == nil || strings.Join(existing.CapabilityTags, ",") != "reasoning" {
		t.Fatalf("existing model tags = %#v", existing)
	}
	if len(existing.ProviderConfigs) == 0 || strings.Join(existing.ProviderConfigs[0].CapabilityTags, ",") != "document" {
		t.Fatalf("existing route tags = %#v", existing.ProviderConfigs)
	}
	if fresh == nil {
		t.Fatal("new model was not attached")
	}
	if len(fresh.CapabilityTags) != 0 {
		t.Fatalf("new model tags = %#v, want none", fresh.CapabilityTags)
	}
}

func TestTokenBankNewModelInheritsSharePause(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	first := publishSpec("share-1", "gpt-4o", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{first}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := svc.SetTokenBankSharePaused(ctx, "share-1", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// The new model carries no SharePaused of its own. It still has to start
	// paused because a sibling of this share is already paused.
	second := publishSpec("share-1", "gpt-4o-mini", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{first, second}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	for _, model := range []string{"gpt-4o", "gpt-4o-mini"} {
		provider := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-1", model))
		if !provider.Paused {
			t.Fatalf("%s is serving on a paused share", model)
		}
	}

	fresh := publishSpec("share-2", "gpt-4o", "default")
	fresh.SharePaused = true
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{fresh}); err != nil {
		t.Fatalf("publish paused share: %v", err)
	}
	if provider := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-2", "gpt-4o")); !provider.Paused {
		t.Fatal("a share paused in the row was published into traffic")
	}
}

func TestTokenBankStaleTierRouteLeavesTheModel(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	gpt := publishSpec("share-gpt", "gpt-4o", "default")
	claude := publishSpec("share-claude", "claude-3", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{gpt}); err != nil {
		t.Fatalf("publish gpt: %v", err)
	}
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{claude}); err != nil {
		t.Fatalf("publish claude: %v", err)
	}

	if _, err := svc.UnpublishTokenBankShare(ctx, "share-gpt"); err != nil {
		t.Fatalf("unpublish gpt: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertModelRoute := func(reg *Registry, model, arrayID string, want bool) {
		t.Helper()
		group := findServiceGroupByID(reg, "default")
		if group == nil {
			t.Fatal("service group disappeared")
		}
		var found *llmpool.ModelConfig
		for i := range group.Models {
			if strings.EqualFold(group.Models[i].Name, model) {
				found = &group.Models[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("model %s is not on the service group", model)
		}
		if got := containsProviderID(found.ProviderIDs, arrayID); got != want {
			t.Fatalf("model %s routes %s = %v, want %v (%v)", model, arrayID, got, want, found.ProviderIDs)
		}
	}
	assertModelRoute(reg, "gpt-4o", TokenBankArrayMid, false)
	assertModelRoute(reg, "claude-3", TokenBankArrayMid, true)

	gpt.ArrayID = TokenBankArrayHigh
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{gpt}); err != nil {
		t.Fatalf("regrade gpt: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertModelRoute(reg, "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(reg, "gpt-4o", TokenBankArrayMid, false)
	assertModelRoute(reg, "claude-3", TokenBankArrayMid, true)
	moved := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-gpt", "gpt-4o"))
	if moved.ArrayID != TokenBankArrayHigh {
		t.Fatalf("member array = %s, want high", moved.ArrayID)
	}
}

func TestTokenBankPauseAndUnpublishAffectEveryModelOfTheShare(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-1", "gpt-4o", "default"),
		publishSpec("share-1", "gpt-4o-mini", "default"),
		publishSpec("share-2", "gpt-4o", "default"),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	affected, err := svc.SetTokenBankSharePaused(ctx, "share-1", true)
	if err != nil {
		t.Fatalf("pause share-1: %v", err)
	}
	if affected != 2 {
		t.Fatalf("pause affected %d members, want 2", affected)
	}
	// share-2 must be untouched: pausing one share cannot leak to another.
	other := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-2", "gpt-4o"))
	if other.Paused {
		t.Fatal("pausing share-1 paused a member of share-2")
	}

	removed, err := svc.UnpublishTokenBankShare(ctx, "share-1")
	if err != nil {
		t.Fatalf("unpublish share-1: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("unpublish removed %v, want both models of share-1", removed)
	}
	remaining, err := svc.TokenBankShareMemberIDs(ctx, "share-2")
	if err != nil {
		t.Fatalf("TokenBankShareMemberIDs(share-2) error = %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("share-2 members = %v, want 1 untouched", remaining)
	}
}

func TestTokenBankSetMemberKeyRotatesWithoutChangingIdentity(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-1", "gpt-4o-mini", "default"),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	memberID := TokenBankMemberID("share-1", "gpt-4o-mini")

	affected, err := svc.SetTokenBankMemberKey(ctx, "share-1", "https://new.example.com/v1", "sk-rotated", "openai")
	if err != nil {
		t.Fatalf("SetTokenBankMemberKey() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("key rotation affected %d members, want 1", affected)
	}
	provider := mustGetProvider(t, svc, ctx, memberID)
	if provider.APIKey != "sk-rotated" || provider.APIURL != "https://new.example.com/v1" {
		t.Fatalf("credentials were not rotated: url=%s key=%s", provider.APIURL, provider.APIKey)
	}
	// Rotation must not move the member: the id is the settlement link back to
	// the share, and changing it would orphan the earnings history.
	if provider.ArrayID != TokenBankArrayMid {
		t.Fatalf("rotation moved the member to %q", provider.ArrayID)
	}
}

// TestTokenBankUnknownArrayFallsBackToMid pins the degrade path: a bad array on
// a share row must not make its models unreachable. Tier is metadata, and the
// admin regrade path validates strictly on its own.
func TestTokenBankUnknownArrayFallsBackToMid(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	spec := publishSpec("share-1", "gpt-4o-mini", "default")
	spec.ArrayID = "not-a-real-array"
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{spec}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	provider := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-1", "gpt-4o-mini"))
	if provider.ArrayID != TokenBankArrayMid {
		t.Fatalf("array_id = %q, want fallback %q", provider.ArrayID, TokenBankArrayMid)
	}
}

func TestTokenBankPublishWithoutMembersIsNotAnError(t *testing.T) {
	svc, ctx := publishTestService(t)
	// A share whose every model failed probing publishes zero members.
	result, err := svc.PublishTokenBankShare(ctx, nil)
	if err != nil {
		t.Fatalf("PublishTokenBankShare(nil) error = %v", err)
	}
	if len(result.Added) != 0 || len(result.Removed) != 0 {
		t.Fatalf("result = %#v, want empty", result)
	}
}

func TestTokenBankApplyMemberTierRetargetsTheModel(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	if err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID: "other", Name: "other", AccessPolicy: "free",
		Models: []llmpool.ModelConfig{{
			Name:            "gpt-4o",
			ProviderIDs:     []string{"plain-vendor"},
			ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "plain-vendor"}},
		}},
	}); err != nil {
		t.Fatalf("add other group: %v", err)
	}
	gpt := publishSpec("share-gpt", "gpt-4o", "default")
	gptSibling := publishSpec("share-gpt-b", "gpt-4o", "default")
	claude := publishSpec("share-claude", "claude-3", "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{gpt, gptSibling, claude}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	gptID := TokenBankMemberID("share-gpt", "gpt-4o")
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		idx := providerIndex(reg, gptID)
		if idx < 0 {
			t.Fatal("gpt member missing before the tier change")
		}
		reg.Providers[idx].DispatchWeight = 7
		reg.Providers[idx].ModelMap = map[string]string{"gpt-4o": "gpt-4o-2024"}
		reg.Providers[idx].Paused = true
		reg.Providers[idx].TokenBankCanaryUntil = "2026-10-03T00:00:00Z"
		reg.Providers[idx].CapabilityTags = []string{"tools"}
		reg.Providers[idx].APIKey = "sk-keep"
		return true, nil
	}); err != nil {
		t.Fatalf("stamp registry fields: %v", err)
	}

	if err := svc.ApplyTokenBankMemberTier(ctx, gptID, TokenBankTierHigh); err != nil {
		t.Fatalf("apply high: %v", err)
	}
	moved := mustGetProvider(t, svc, ctx, gptID)
	if moved.ArrayID != TokenBankArrayHigh || moved.TokenBankTier != TokenBankTierHigh || moved.TokenBankTierMultiplier != 2 {
		t.Fatalf("member tier = %s/%s/%v, want high", moved.ArrayID, moved.TokenBankTier, moved.TokenBankTierMultiplier)
	}
	if moved.CreditMultiplier != tokenBankArrayCreditMultiplier {
		t.Fatalf("consumer multiplier = %v, want %v", moved.CreditMultiplier, tokenBankArrayCreditMultiplier)
	}
	if moved.DispatchWeight != 7 || moved.ModelMap["gpt-4o"] != "gpt-4o-2024" || !moved.Paused ||
		moved.TokenBankCanaryUntil != "2026-10-03T00:00:00Z" || moved.APIKey != "sk-keep" ||
		strings.Join(moved.CapabilityTags, ",") != "tools" {
		t.Fatalf("tier change rewrote member fields: %+v", moved)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayMid, true)
	assertModelRoute(t, reg, "default", "claude-3", TokenBankArrayMid, true)
	assertModelRoute(t, reg, "default", "claude-3", TokenBankArrayHigh, false)
	assertModelRoute(t, reg, "other", "gpt-4o", TokenBankArrayHigh, false)
	assertModelRoute(t, reg, "other", "gpt-4o", "plain-vendor", true)

	if err := svc.ApplyTokenBankMemberTier(ctx, TokenBankMemberID("share-gpt-b", "gpt-4o"), "low"); err != nil {
		t.Fatalf("apply low: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayLow, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayMid, false)
	assertModelRoute(t, reg, "default", "claude-3", TokenBankArrayMid, true)
	if findProviderArray(reg, TokenBankArrayMid) == nil {
		t.Fatal("empty mid array was deleted")
	}
	kept := mustGetProvider(t, svc, ctx, gptID)
	if kept.DispatchWeight != 7 || kept.TokenBankTierMultiplier != 2 {
		t.Fatalf("first member changed while its sibling moved: weight=%d rate=%v", kept.DispatchWeight, kept.TokenBankTierMultiplier)
	}
	sibling := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-gpt-b", "gpt-4o"))
	if sibling.ArrayID != TokenBankArrayLow || sibling.TokenBankTierMultiplier != 0.5 {
		t.Fatalf("sibling = %s/%v, want low/0.5", sibling.ArrayID, sibling.TokenBankTierMultiplier)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("stamp load: %v", err)
	}
	stamp := reg.UpdatedAt
	if err := svc.ApplyTokenBankMemberTier(ctx, gptID, TokenBankTierHigh); err != nil {
		t.Fatalf("repeat apply: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("repeat load: %v", err)
	}
	if !reg.UpdatedAt.Equal(stamp) {
		t.Fatalf("repeat label rewrote the registry at %s, was %s", reg.UpdatedAt, stamp)
	}
}

func TestTokenBankApplyMemberTierDoesNotCrossServiceGroups(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	addPublishTestServiceGroup(t, svc, ctx, "budget")
	mid := publishSpec("share-mid", "gpt-4o", "default")
	low := publishSpec("share-low", "gpt-4o", "budget")
	low.ArrayID = TokenBankArrayLow
	low.Tier = TokenBankTierLow
	low.TierMultiplier = 0.5
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{mid}); err != nil {
		t.Fatalf("publish mid: %v", err)
	}
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{low}); err != nil {
		t.Fatalf("publish low: %v", err)
	}
	if err := svc.ApplyTokenBankMemberTier(ctx, TokenBankMemberID("share-mid", "gpt-4o"), TokenBankTierHigh); err != nil {
		t.Fatalf("apply high: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayMid, false)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayLow, false)
	assertModelRoute(t, reg, "budget", "gpt-4o", TokenBankArrayLow, true)
	assertModelRoute(t, reg, "budget", "gpt-4o", TokenBankArrayHigh, false)
	assertModelRoute(t, reg, "budget", "gpt-4o", TokenBankArrayMid, false)
}

func TestTokenBankApplyMemberTierLeavesForeignStaleTier(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	addPublishTestServiceGroup(t, svc, ctx, "budget")
	high := publishSpec("share-high", "gpt-4o", "default")
	high.ArrayID = TokenBankArrayHigh
	high.Tier = TokenBankTierHigh
	high.TierMultiplier = 2
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{high}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Budget still names an empty low tier. Repeating the high label knows no
	// earlier array, so it must not treat that empty tier as this member's route.
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		group := findServiceGroupByID(reg, "budget")
		if group == nil {
			t.Fatal("budget group missing")
		}
		model := findOrCreateModelConfig(group, "gpt-4o")
		model.ProviderIDs = []string{TokenBankArrayLow}
		model.ProviderConfigs = nil
		return true, nil
	}); err != nil {
		t.Fatalf("plant stale route: %v", err)
	}
	if err := svc.ApplyTokenBankMemberTier(ctx, TokenBankMemberID("share-high", "gpt-4o"), TokenBankTierHigh); err != nil {
		t.Fatalf("repeat high: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertModelRoute(t, reg, "budget", "gpt-4o", TokenBankArrayHigh, false)
	assertModelRoute(t, reg, "budget", "gpt-4o", TokenBankArrayLow, false)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayLow, false)
}

func TestTokenBankApplyMemberTierRepairsStrandedMember(t *testing.T) {
	svc, ctx := publishTestService(t)
	addPublishTestServiceGroup(t, svc, ctx, "default")
	if _, err := svc.PublishTokenBankShare(ctx, []TokenBankPublishSpec{
		publishSpec("share-moved", "gpt-4o", "default"),
		publishSpec("share-stay", "gpt-4o", "default"),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	movedID := TokenBankMemberID("share-moved", "gpt-4o")
	// The previous label wrote only ArrayID. The route still names mid, and a
	// sibling keeps that array serving the model.
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		idx := providerIndex(reg, movedID)
		if idx < 0 {
			t.Fatal("moved member missing")
		}
		reg.Providers[idx].ArrayID = TokenBankArrayHigh
		reg.Providers[idx].TokenBankTierMultiplier = 0
		return true, nil
	}); err != nil {
		t.Fatalf("strand member: %v", err)
	}
	if err := svc.ApplyTokenBankMemberTier(ctx, movedID, TokenBankTierHigh, TokenBankArrayMid); err != nil {
		t.Fatalf("repair: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayHigh, true)
	assertModelRoute(t, reg, "default", "gpt-4o", TokenBankArrayMid, true)
	moved := mustGetProvider(t, svc, ctx, movedID)
	if moved.TokenBankTier != TokenBankTierHigh || moved.TokenBankTierMultiplier != 2 {
		t.Fatalf("repaired member = %s/%v", moved.TokenBankTier, moved.TokenBankTierMultiplier)
	}
	stay := mustGetProvider(t, svc, ctx, TokenBankMemberID("share-stay", "gpt-4o"))
	if stay.ArrayID != TokenBankArrayMid || stay.TokenBankTierMultiplier != 1 {
		t.Fatalf("sibling = %s/%v, want mid/1", stay.ArrayID, stay.TokenBankTierMultiplier)
	}
}

func TestAddModelRouteTargetDoesNotHideProviderIDs(t *testing.T) {
	cfg := llmpool.ModelConfig{ProviderIDs: []string{TokenBankArrayMid, "plain-vendor"}}
	addModelRouteTarget(&cfg, TokenBankArrayHigh)
	if len(cfg.ProviderConfigs) != 0 {
		t.Fatalf("configs = %+v, want ProviderIDs to stay the source", cfg.ProviderConfigs)
	}
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{{
		ID:     "default",
		Models: []llmpool.ModelConfig{cfg},
	}}}
	collapseServiceGroupArrayRoutes(reg)
	got := reg.ServiceGroups[0].Models[0].ProviderIDs
	if !containsProviderID(got, TokenBankArrayHigh) || !containsProviderID(got, "plain-vendor") {
		t.Fatalf("collapsed route = %v, want high and plain-vendor", got)
	}

	filled := llmpool.ModelConfig{
		ProviderIDs:     []string{TokenBankArrayMid},
		ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: TokenBankArrayMid}},
	}
	addModelRouteTarget(&filled, TokenBankArrayHigh)
	if !containsProviderConfig(filled.ProviderConfigs, TokenBankArrayHigh) {
		t.Fatalf("configs = %+v, want the new array", filled.ProviderConfigs)
	}
}

func assertModelRoute(t *testing.T, reg *Registry, groupID, model, arrayID string, want bool) {
	t.Helper()
	group := findServiceGroupByID(reg, groupID)
	if group == nil {
		t.Fatalf("service group %s disappeared", groupID)
	}
	var found *llmpool.ModelConfig
	for i := range group.Models {
		if strings.EqualFold(group.Models[i].Name, model) {
			found = &group.Models[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("model %s is not on %s", model, groupID)
	}
	if got := containsProviderID(found.ProviderIDs, arrayID); got != want {
		t.Fatalf("%s/%s routes %s = %v, want %v (%v)", groupID, model, arrayID, got, want, found.ProviderIDs)
	}
}
