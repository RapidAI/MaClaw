package tool

import (
	"strings"
	"testing"
	"time"
)

// dynamicSearchRegistry declares the one capability family a pin can govern in
// these tests, with two implementations behind it so a pin has something to
// choose between.
func dynamicSearchRegistry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	registry := NewCapabilityRegistry("v1")
	if err := registry.Register(CapabilityDescriptor{
		ID: "information.search.web", Version: "v1",
		Qualifiers: map[string]QualifierConstraint{"freshness": {Values: []string{"current"}, Required: true}},
		Effects:    []EffectClass{EffectReadOnly},
	}); err != nil {
		t.Fatalf("register search capability: %v", err)
	}
	return registry
}

func dynamicSearchProvider(kind, providerID, implementationID string, quality float64, ready bool) ProviderSpec {
	authorization, err := NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	if err != nil {
		panic(err)
	}
	return ProviderSpec{
		AdapterName: providerID + "_" + implementationID,
		Binding: ProviderBinding{
			Kind: kind, ProviderID: providerID, ImplementationID: implementationID,
			SchemaDigest: SchemaDigest([]byte(providerID + implementationID)),
		},
		ParameterAuthorization: authorization,
		Provides: []CapabilityProvision{
			{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: quality},
		},
		Effects: []EffectClass{EffectReadOnly},
		Ready:   ready,
	}
}

func dynamicSearchSnapshot(t *testing.T, registry *CapabilityRegistry, providers []ProviderSpec) ToolCatalogSnapshot {
	t.Helper()
	catalog := NewToolCatalog(registry)
	snapshot, err := catalog.Publish(providers, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("publish catalog: %v", err)
	}
	return snapshot
}

func dynamicSearchNeed() CapabilityNeed {
	return CapabilityNeed{
		ID: "search", Capability: "information.search.web",
		Qualifiers: map[string]string{"freshness": "current"},
		Polarity:   NeedRequire, Required: true,
	}
}

// A higher-quality sibling is what quality ranking picks by default. That is
// the substitution the pin exists to prevent, so the test asserts the pinned
// provider wins even when it is the lower-ranked one.
func TestProviderPinOverridesQualityRanking(t *testing.T) {
	registry := dynamicSearchRegistry(t)
	host := dynamicSearchProvider("builtin", "core", "host-websearch", 3, true)
	vendor := dynamicSearchProvider("mcp", "web-search-prime", "web_search_prime", 1, true)
	snapshot := dynamicSearchSnapshot(t, registry, []ProviderSpec{host, vendor})

	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-pin", TurnID: "turn-pin", Snapshot: snapshot,
		Needs: []CapabilityNeed{dynamicSearchNeed()},
		Pins:  []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 {
		t.Fatalf("selections = %#v unmet = %#v", plan.Selections, plan.Unmet)
	}
	if got := plan.Selections[0].Provider.ProviderID; got != "web-search-prime" {
		t.Fatalf("pinned provider not selected: %q", got)
	}
}

// Without a pin the same catalog selects the higher-quality builtin. If this
// ever changes, the pin test above would pass for the wrong reason.
func TestUnpinnedSearchPrefersHigherQualityProvider(t *testing.T) {
	registry := dynamicSearchRegistry(t)
	host := dynamicSearchProvider("builtin", "core", "host-websearch", 3, true)
	vendor := dynamicSearchProvider("mcp", "web-search-prime", "web_search_prime", 1, true)
	snapshot := dynamicSearchSnapshot(t, registry, []ProviderSpec{host, vendor})

	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-open", TurnID: "turn-open", Snapshot: snapshot,
		Needs: []CapabilityNeed{dynamicSearchNeed()},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].Provider.ProviderID != "core" {
		t.Fatalf("unpinned selection = %#v", plan.Selections)
	}
}

// A pin names an implementation; it must not open a gateway. A pinned provider
// that is Ready=false is exactly the silent-substitution case, so the planner
// reports it instead of falling through to the builtin.
func TestProviderPinFailsClosedWhenPinnedProviderNotReady(t *testing.T) {
	registry := dynamicSearchRegistry(t)
	host := dynamicSearchProvider("builtin", "core", "host-websearch", 3, true)
	vendor := dynamicSearchProvider("mcp", "web-search-prime", "web_search_prime", 3, false)
	snapshot := dynamicSearchSnapshot(t, registry, []ProviderSpec{host, vendor})

	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-blocked", TurnID: "turn-blocked", Snapshot: snapshot,
		Needs: []CapabilityNeed{dynamicSearchNeed()},
		Pins:  []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 0 {
		t.Fatalf("a blocked pinned provider must not fall through to a sibling: %#v", plan.Selections)
	}
	if len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != UnmetReasonPinnedProviderUnavailable {
		t.Fatalf("unmet = %#v", plan.Unmet)
	}
}

// A pin is scoped to its capability. It must not remove providers from a
// capability the user never named.
func TestProviderPinDoesNotAffectOtherCapabilities(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{provider})

	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-scoped", TurnID: "turn-scoped", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Polarity: NeedRequire, Required: true}},
		Pins:  []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].Provider.ProviderID != "core" {
		t.Fatalf("capability-scoped selection = %#v", plan.Selections)
	}
}

// An incomplete pin carries no identity. It must narrow nothing rather than
// match any provider of some family.
func TestProviderPinWithoutProviderIdentityIsInert(t *testing.T) {
	registry := dynamicSearchRegistry(t)
	host := dynamicSearchProvider("builtin", "core", "host-websearch", 3, true)
	snapshot := dynamicSearchSnapshot(t, registry, []ProviderSpec{host})

	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-inert", TurnID: "turn-inert", Snapshot: snapshot,
		Needs: []CapabilityNeed{dynamicSearchNeed()},
		Pins:  []ProviderPin{{Capability: "information.search.web", Kind: "mcp"}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Selections) != 1 || plan.Selections[0].Provider.ProviderID != "core" {
		t.Fatalf("an identity-less pin must be inert: %#v", plan.Selections)
	}
}

// An incidental mention must not cost the user their turn. A capability the
// host only wanted opportunistically — an ambient lookup, say — drops out as
// omitted when the named implementation cannot serve it; only a required need
// is worth a refusal.
func TestProviderPinOnlyRefusesRequiredNeeds(t *testing.T) {
	registry := dynamicSearchRegistry(t)
	host := dynamicSearchProvider("builtin", "core", "host-websearch", 3, true)
	vendor := dynamicSearchProvider("mcp", "web-search-prime", "web_search_prime", 3, false)
	snapshot := dynamicSearchSnapshot(t, registry, []ProviderSpec{host, vendor})

	optional := dynamicSearchNeed()
	optional.ID = "search-optional"
	optional.Required = false
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-optional", TurnID: "turn-optional", Snapshot: snapshot,
		Needs: []CapabilityNeed{optional},
		Pins:  []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("an optional need must not refuse the turn: %#v", plan.Unmet)
	}
	if len(plan.Omitted) != 1 || plan.Omitted[0].ReasonCode != UnmetReasonPinnedProviderUnavailable {
		t.Fatalf("omitted = %#v", plan.Omitted)
	}
}

func TestPinForCapabilityTakesFirstMatch(t *testing.T) {
	pins := []ProviderPin{
		{Capability: "information.search.web", Kind: "mcp", ProviderID: "first"},
		{Capability: "information.search.web", Kind: "mcp", ProviderID: "second"},
	}
	pin, ok := PinForCapability(pins, "information.search.web")
	if !ok || pin.ProviderID != "first" {
		t.Fatalf("pin = %#v ok = %v", pin, ok)
	}
	if _, ok := PinForCapability(pins, "information.lookup"); ok {
		t.Fatal("a pin must not answer for a capability it does not name")
	}
}

// Matching folds the two spellings a vendor product is registered under and
// rejects a hit inside a longer identifier: "web_search" naming the builtin is
// not a reference to "web_search_prime".
func TestProviderReferenceMatching(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		id        string
		tool      string
		wantNamed bool
	}{
		{"tool name in text", "使用 web_search_prime mcp 搜索", "web-search-prime", "web_search_prime", true},
		{"server id in text", "用 web-search-prime 搜索", "web-search-prime", "web_search_prime", true},
		{"hyphen and underscore are one spelling", "用 web-search-prime 搜", "web_search_prime", "other_tool", true},
		{"shorter name inside a longer one", "用 web_search_prime 搜索", "other-server", "web_search", false},
		{"bare word tool is ineligible", "帮我 search 一下", "web-search-prime", "search", false},
		{"ordinary prose never matches", "搜索一些公开资料", "web-search-prime", "web_search_prime", false},
		{"too short a reference", "用 ai 查", "ai", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			named := ProviderIDNamed(tc.text, tc.id) || ProviderToolNamed(tc.text, tc.tool)
			if named != tc.wantNamed {
				t.Fatalf("named = %v want %v (text=%q id=%q tool=%q)", named, tc.wantNamed, tc.text, tc.id, tc.tool)
			}
		})
	}
}

func TestProviderToolNamedAcceptsCompoundSpellingsOnly(t *testing.T) {
	if !ProviderToolNamed("调用 get_page_children", "get_page_children") {
		t.Fatal("snake_case tool name must be eligible")
	}
	if !ProviderToolNamed("调用 getPageChildren", "getPageChildren") {
		t.Fatal("camelCase tool name must be eligible")
	}
	if ProviderToolNamed("please search now", "search") {
		t.Fatal("a bare lowercase word must not pin a turn")
	}
}

func TestRegisterReviewedDynamicPromptStaysSealed(t *testing.T) {
	// The registry is package state shared by the whole planner surface. Save
	// and restore it so registering a second spelling here cannot leak into
	// another test's expectation about what the set contains.
	saved := ReviewedDynamicPromptNames()
	reviewedDynamicPromptMu.Lock()
	reviewedDynamicPromptNames = map[string]struct{}{"web_search": {}}
	reviewedDynamicPromptMu.Unlock()
	t.Cleanup(func() {
		reviewedDynamicPromptMu.Lock()
		reviewedDynamicPromptNames = map[string]struct{}{}
		for _, name := range saved {
			reviewedDynamicPromptNames[name] = struct{}{}
		}
		reviewedDynamicPromptMu.Unlock()
	})

	if !AllowReviewedDynamicPrompt("web_search") {
		t.Fatalf("the shipped reviewed prompt must stay registered: %v", ReviewedDynamicPromptNames())
	}
	if err := RegisterReviewedDynamicPrompt("web_search_prime"); err == nil {
		t.Fatal("a vendor tool name must never enter the reviewed prompt set")
	}
	if AllowReviewedDynamicPrompt("web_search_prime") {
		t.Fatal("rejected vendor name was admitted")
	}
	if err := RegisterReviewedDynamicPrompt("web_fetch"); err != nil {
		t.Fatalf("register existing stable host spelling: %v", err)
	}
	if !AllowReviewedDynamicPrompt("web_fetch") {
		t.Fatal("an already-stable host spelling must be registrable")
	}
	for _, name := range ReviewedDynamicPromptNames() {
		if !semanticStableHostAdapter(name) {
			t.Fatalf("reviewed prompt %q is not a stable host spelling", name)
		}
	}
}

func TestCloneProviderPinsDoesNotShareTheLiveSlice(t *testing.T) {
	pins := []ProviderPin{{Capability: "information.search.web", Kind: " mcp ", ProviderID: " web-search-prime "}}
	clone := CloneProviderPins(pins)
	if len(clone) != 1 || clone[0].Kind != "mcp" || clone[0].ProviderID != "web-search-prime" {
		t.Fatalf("clone = %#v", clone)
	}
	if strings.TrimSpace(string(clone[0].Capability)) != "information.search.web" {
		t.Fatalf("capability not preserved: %#v", clone[0])
	}
}

// TestPinChangesRouteIdentity guards the property that made pins unsafe to
// ship before this test existed. The route state store keys on PlanID and
// treats an equal snapshot digest as "this is the same route", so if the pin
// set were absent from the digest then "search with web-search-prime" and
// "search with whatever ranks highest" would be one identity — and whichever
// route state was stored first would be handed back for the other turn,
// resurrecting the wrong implementation's selections.
func TestPinChangesRouteIdentity(t *testing.T) {
	snapshot := semanticSnapshotForPinDigest()
	base := RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: "turn", ChannelScope: "desktop",
		Snapshot: snapshot,
		Needs:    []CapabilityNeed{{ID: "search", Capability: "information.search.web", Polarity: NeedRequire, Required: true, Qualifiers: map[string]string{"freshness": "current"}}},
	}
	pinned := base
	pinned.Pins = []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"}}

	if semanticRouteSnapshotDigest(pinned) == semanticRouteSnapshotDigest(base) {
		t.Fatal("a pin must change the route identity: an unpinned plan and a pinned plan collided")
	}
	// Order must not matter, and neither must inert pins.
	reordered := base
	reordered.Pins = []ProviderPin{
		{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"},
		{Capability: "information.search.web", Kind: "mcp", ProviderID: "web-search-prime"},
	}
	if semanticRouteSnapshotDigest(reordered) != semanticRouteSnapshotDigest(pinned) {
		t.Fatal("pin order or duplication changed the route identity")
	}
	inert := base
	// An incomplete pin is inert everywhere else, so it must not fork identity.
	inert.Pins = []ProviderPin{{Capability: "information.search.web", Kind: "mcp"}, {ProviderID: "web-search-prime"}}
	if semanticRouteSnapshotDigest(inert) != semanticRouteSnapshotDigest(base) {
		t.Fatal("an invalid pin changed the route identity despite being inert")
	}
	different := base
	different.Pins = []ProviderPin{{Capability: "information.search.web", Kind: "mcp", ProviderID: "some-other-server"}}
	if semanticRouteSnapshotDigest(different) == semanticRouteSnapshotDigest(pinned) {
		t.Fatal("two different pins produced one route identity")
	}
}

// semanticSnapshotForPinDigest builds a two-provider snapshot for the same
// search capability the pin tests plan against.
func semanticSnapshotForPinDigest() ToolCatalogSnapshot {
	return ToolCatalogSnapshot{
		Generation: 1, RegistryVersion: "registry-v1",
		Coverage: CatalogCoverage{State: CatalogCoverageComplete},
		Providers: []ProviderSpec{
			dynamicSearchProvider("mcp", "web-search-prime", "web_search_prime", 3, true),
			dynamicSearchProvider("host", "builtin-search", "search", 1, true),
		},
	}
}

// TestShortCJKReferenceCannotPin guards the byte-versus-rune trap: a CJK
// character is three bytes, so a two-syllable display name such as 「文档」
// clears a four-byte floor. CJK is written without separators, so such a name
// also matches inside ordinary prose — and a false pin either diverts the turn
// or, if that server is down, refuses the whole turn.
func TestShortCJKReferenceCannotPin(t *testing.T) {
	for _, name := range []string{"文档", "飞书", "钉钉", "工具"} {
		if ProviderIDNamed("帮我整理一下文档内容", name) {
			t.Fatalf("two-syllable server name %q pinned an ordinary turn", name)
		}
	}
	// Three CJK characters are distinctive enough to be a real product name.
	if !ProviderIDNamed("用智谱搜索查一下", "智谱搜索") {
		t.Fatal("a three-character CJK product name must remain pinnable")
	}
	// The same floor applies to ASCII, counted in runes not bytes.
	if ProviderIDNamed("please web now", "web") {
		t.Fatal("a three-character ASCII name must not pin")
	}
	if !ProviderIDNamed("use notion for this", "notion") {
		t.Fatal("a six-character ASCII name must remain pinnable")
	}
}

// TestReviewedPromptRegistryIsConcurrencySafe exercises the read path against
// a concurrent writer. Under -race this reports a data race; without the lock
// an unsynchronized map write concurrent with a read is a fatal, unrecoverable
// runtime throw rather than a wrong answer.
func TestReviewedPromptRegistryIsConcurrencySafe(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			// web_fetch is a stable host spelling, so registration is accepted.
			_ = RegisterReviewedDynamicPrompt("web_fetch")
		}
	}()
	for i := 0; i < 200; i++ {
		_ = AllowReviewedDynamicPrompt("web_search")
		_ = ReviewedDynamicPromptNames()
		_ = RenderedSemanticFunctionName(reviewedDynamicPromptPrefix+"web_search:dynamic_x", "grant-1")
	}
	<-done
	if !AllowReviewedDynamicPrompt("web_fetch") {
		t.Fatal("registration lost under concurrency")
	}
}

// TestProviderReferenceMatcherMatchesOneShotHelpers pins the matcher's
// equivalence to the one-shot helpers. The matcher exists only to avoid
// re-normalizing the same turn text once per installed server, so a divergence
// would silently reintroduce either the cost or a different pinning decision.
func TestProviderReferenceMatcherMatchesOneShotHelpers(t *testing.T) {
	texts := []string{
		"使用 web_search_prime mcp 搜索张学友资料",
		"用 web-search-prime 查一下",
		"please use notion for this",
		"帮我整理一下文档内容",
		"",
	}
	servers := []string{"web-search-prime", "notion", "文档", "unknown-server"}
	tools := []string{"web_search_prime", "web-search-prime", "search", "getPageChildren"}
	for _, text := range texts {
		matcher := NewProviderReferenceMatcher(text)
		for _, server := range servers {
			want := ProviderIDNamed(text, server)
			if got := matcher.ServerNames(server); got != want {
				t.Fatalf("ServerNames(%q) with %q = %v, want %v", server, text, got, want)
			}
		}
		for _, name := range tools {
			want := ProviderToolNamed(text, name)
			if got := matcher.ToolNames(name); got != want {
				t.Fatalf("ToolNames(%q) with %q = %v, want %v", name, text, got, want)
			}
		}
	}
	// Multiple display names are all consulted, as the one-shot loop did.
	matcher := NewProviderReferenceMatcher("用智谱搜索查一下")
	if !matcher.ServerNames("unrelated-id", "other-name", "智谱搜索") {
		t.Fatal("a later display name must still match")
	}
	if matcher.ServerNames("unrelated-id", "other-name") {
		t.Fatal("an unmatched display name list must not match")
	}
}
