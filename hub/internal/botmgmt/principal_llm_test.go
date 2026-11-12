package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const principalLLMConfigPath = "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config"
const ownerLLMTestEndpoint = "https://hub.example/api/llm/v1"

// ownerLLMRecorder stands in for MaClawSrv and keeps every admin config write.
type ownerLLMRecorder struct {
	mu      sync.Mutex
	configs []map[string]any
}

// handler serves just enough of the MaClawSrv admin surface for an LLM push.
// The GET answers with a config the operator could really be holding: one they
// filled in themselves, so anything this service restores or clears has to
// survive that round trip unchanged.
func (r *ownerLLMRecorder) serve(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == principalLLMConfigPath:
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh","maclaw_llm_protocol":"anthropic","bot_display_name":"duty"}}`))
		case req.Method == http.MethodPut && req.URL.Path == principalLLMConfigPath:
			var body struct {
				AppConfig map[string]any `json:"app_config"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("config body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.mu.Lock()
			r.configs = append(r.configs, body.AppConfig)
			r.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, req)
		}
	}
}

func (r *ownerLLMRecorder) last(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.configs) == 0 {
		t.Fatal("no MaClawSrv config write reached the service")
	}
	return r.configs[len(r.configs)-1]
}

func (r *ownerLLMRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.configs)
}

func newPrincipalLLMFixture(t *testing.T, issuer OwnerLLMIssuer, rec record) (*Service, *ownerLLMRecorder) {
	t.Helper()
	recorder := &ownerLLMRecorder{}
	srv := httptest.NewServer(recorder.serve(t))
	t.Cleanup(srv.Close)
	rec.BaseURL = srv.URL
	rec.AdminSecret = "admin-secret"
	if rec.Principals == nil {
		rec.Principals = []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice",
			APIKey: "key-alice", APISecret: "sec-alice",
		}}
	}
	if rec.Bots == nil {
		rec.Bots = []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}}
	}
	if rec.Grants == nil {
		rec.Grants = []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}}
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC) }
	svc.OwnerLLM = issuer
	return svc, recorder
}

func ownerLLMProviderEntry(t *testing.T, cfg map[string]any, name string) (map[string]any, bool) {
	t.Helper()
	entries, _ := cfg["maclaw_llm_providers"].([]any)
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		got, _ := item["name"].(string)
		if ok && strings.EqualFold(strings.TrimSpace(got), name) {
			return item, true
		}
	}
	return nil, false
}

func ownerLLMTestRecordWithAdminDefault() record {
	return record{
		LLM: llmSettings{
			Current: "lp_admin0000000001",
			Providers: []llmProvider{{
				ID: "lp_admin0000000001", Name: "Admin Pick", Protocol: "anthropic",
				URL: "https://api.anthropic.com", Key: "sk-ant-admin", Model: "claude-sonnet-4-5",
			}},
		},
	}
}

func ownerLLMTestCustomInput(key string) PrincipalLLMInput {
	return PrincipalLLMInput{
		Mode: "custom", Name: "MyProv", URL: "https://api.mine.example/v1/",
		Key: key, Model: "mine-large", Protocol: "openai", WireAPI: "responses",
	}
}

func TestOwnerLLMOverrideRoundTripRestoresSystemFreeDefault(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, record{})
	ctx := context.Background()

	view, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "custom" || !view.KeySet || view.Model != "mine-large" {
		t.Fatalf("view after custom = %#v", view)
	}
	applied := recorder.last(t)
	if applied["maclaw_llm_url"] != "https://api.mine.example/v1" ||
		applied["maclaw_llm_key"] != "sk-mine-secret" ||
		applied["maclaw_llm_model"] != "mine-large" ||
		applied["maclaw_llm_protocol"] != "openai" ||
		applied["maclaw_llm_current_provider"] != "MyProv" {
		t.Fatalf("custom config = %#v", applied)
	}
	// The provider has to exist by name in the same array, otherwise
	// current_provider dangles and MaClawSrv refuses the instance.
	entry, ok := ownerLLMProviderEntry(t, applied, "MyProv")
	if !ok {
		t.Fatalf("override missing from providers = %#v", applied["maclaw_llm_providers"])
	}
	// The wire travels only inside the provider entry, so a provider that needs
	// the responses API is broken on the bot side without it.
	if entry["wire_api"] != "responses" {
		t.Fatalf("override entry lost wire_api = %#v", entry)
	}
	if applied["maclaw_bot_llm_mode"] != corelib.MaclawBotLLMModeCustom {
		t.Fatalf("custom mode = %#v", applied["maclaw_bot_llm_mode"])
	}
	assertSameRolePin(t, applied, "foreground", "MyProv", "mine-large")
	assertSameRolePin(t, applied, "background", "MyProv", "mine-large")
	assertSameRolePin(t, applied, "computer_use", "MyProv", "mine-large")
	if profiles, _ := applied["maclaw_llm_profiles"].(map[string]any); profiles["grounder"] != nil {
		t.Fatalf("migrated single provider enabled grounder = %#v", profiles["grounder"])
	}
	if applied["language"] != "zh" || applied["bot_display_name"] != "duty" {
		t.Fatalf("unrelated config changed = %#v", applied)
	}

	restored, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", PrincipalLLMInput{Mode: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Mode != "default" || restored.KeySet || restored.Model != "" {
		t.Fatalf("view after restore = %#v", restored)
	}
	back := recorder.last(t)
	// The flat fields return to the Hub owner route and the custom protocol is
	// cleared rather than left pointing at a provider that is no longer there.
	if _, ok := back["maclaw_llm_protocol"]; ok {
		t.Fatalf("restore left maclaw_llm_protocol = %#v", back["maclaw_llm_protocol"])
	}
	if back["maclaw_llm_url"] != ownerLLMTestEndpoint ||
		back["maclaw_llm_key"] != "viewer-token-1" ||
		back["maclaw_llm_model"] != botLLMModel ||
		back["maclaw_llm_current_provider"] != botLLMProviderName {
		t.Fatalf("restored config = %#v", back)
	}
	if providers, _ := back["maclaw_llm_providers"].([]any); len(providers) != 1 {
		t.Fatalf("restored providers = %#v", back["maclaw_llm_providers"])
	}
	if back["maclaw_bot_llm_mode"] != corelib.MaclawBotLLMModeDefault {
		t.Fatalf("restored mode = %#v", back["maclaw_bot_llm_mode"])
	}
	if _, ok := back["maclaw_llm_profiles"]; ok {
		t.Fatalf("default left role pins %#v", back["maclaw_llm_profiles"])
	}
	if back["language"] != "zh" || back["bot_display_name"] != "duty" {
		t.Fatalf("restore changed unrelated config = %#v", back)
	}

	// Nothing left to reconcile: another push must not rewrite again.
	rec, err := svc.load(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	before := recorder.count()
	if err := svc.writeBotOwnerLLM(ctx, "tenant-a", rec, "alice", false); err != nil {
		t.Fatal(err)
	}
	if recorder.count() != before {
		t.Fatalf("restored default rewrote config %d extra times", recorder.count()-before)
	}
}

// TestOwnerLLMOverrideRestoresAdministratorDefault covers the second default:
// the provider an administrator selected for the tenant.
func TestOwnerLLMOverrideRestoresAdministratorDefault(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, ownerLLMTestRecordWithAdminDefault())
	ctx := context.Background()

	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret")); err != nil {
		t.Fatal(err)
	}
	if custom := recorder.last(t); custom["maclaw_llm_current_provider"] != "MyProv" {
		t.Fatalf("custom did not win over administrator default = %#v", custom)
	}

	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", PrincipalLLMInput{Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	back := recorder.last(t)
	if back["maclaw_llm_current_provider"] != "Admin Pick" ||
		back["maclaw_llm_key"] != "sk-ant-admin" ||
		back["maclaw_llm_model"] != "claude-sonnet-4-5" ||
		back["maclaw_llm_protocol"] != "anthropic" ||
		back["maclaw_llm_url"] != "https://api.anthropic.com" {
		t.Fatalf("restore did not return to administrator default = %#v", back)
	}
	if _, ok := ownerLLMProviderEntry(t, back, "MyProv"); ok {
		t.Fatalf("override provider survived restore = %#v", back["maclaw_llm_providers"])
	}
}

// TestOwnerLLMOverrideRestoresAfterAdministratorChangesDefault is why nothing
// snapshots the previous values. An administrator switching the tenant default
// while an override is live has to carry through; going back to an earlier
// capture would leave the owner on a provider nobody chose.
func TestOwnerLLMOverrideRestoresAfterAdministratorChangesDefault(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, ownerLLMTestRecordWithAdminDefault())
	ctx := context.Background()

	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "lp_newcom000000001", []LLMProviderInput{
		{ID: "lp_admin0000000001", Name: "Admin Pick", Protocol: "anthropic", URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5"},
		{ID: "lp_newcom000000001", Name: "Newcomer", Protocol: "openai", URL: "https://api.new.example/v1", Key: "sk-new", Model: "new-1"},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", PrincipalLLMInput{Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	back := recorder.last(t)
	if back["maclaw_llm_current_provider"] != "Newcomer" || back["maclaw_llm_key"] != "sk-new" {
		t.Fatalf("restore ignored the new administrator default = %#v", back)
	}
}

// TestOwnerLLMOverrideRestoreUsesLiveToken pins down why a restore has to be
// recomputed rather than replayed. The viewer token rotates, so a value
// captured when the override was installed would be expired by now.
func TestOwnerLLMOverrideRestoreUsesLiveToken(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, record{})
	ctx := context.Background()

	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret")); err != nil {
		t.Fatal(err)
	}
	// Outlive the viewer-token reuse window so the next write has to mint a
	// replacement, exactly as a long-running deployment would.
	svc.Now = func() time.Time { return time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC) }
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", PrincipalLLMInput{Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.last(t)["maclaw_llm_key"]; got != "viewer-token-2" {
		t.Fatalf("restore wrote %v, expected the re-issued token", got)
	}
}

func TestOwnerLLMOverrideRejectsReservedAndConflictingNames(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		input PrincipalLLMInput
	}{
		{name: "hub reserved", input: PrincipalLLMInput{Mode: "custom", Name: botLLMProviderName, URL: "https://x.example/v1", Key: "k", Model: "m"}},
		{name: "official group reserved", input: PrincipalLLMInput{Mode: "custom", Name: llmDefaultServiceGroup, URL: "https://x.example/v1", Key: "k", Model: "m"}},
		{name: "system-free reserved", input: PrincipalLLMInput{Mode: "custom", Name: legacyBotServiceGroupID, URL: "https://x.example/v1", Key: "k", Model: "m"}},
		{name: "tenant provider name", input: PrincipalLLMInput{Mode: "custom", Name: "Admin Pick", URL: "https://x.example/v1", Key: "k", Model: "m"}},
		{name: "missing key", input: PrincipalLLMInput{Mode: "custom", Name: "MyProv", URL: "https://x.example/v1", Model: "m"}},
		{name: "bad url", input: PrincipalLLMInput{Mode: "custom", Name: "MyProv", URL: "not-a-url", Key: "k", Model: "m"}},
		{name: "bad protocol", input: PrincipalLLMInput{Mode: "custom", Name: "MyProv", URL: "https://x.example/v1", Key: "k", Model: "m", Protocol: "gemini"}},
		{name: "bad wire api", input: PrincipalLLMInput{Mode: "custom", Name: "MyProv", URL: "https://x.example/v1", Key: "k", Model: "m", WireAPI: "streaming"}},
		{name: "unknown mode", input: PrincipalLLMInput{Mode: "sometimes", Name: "MyProv", URL: "https://x.example/v1", Key: "k", Model: "m"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, ownerLLMTestRecordWithAdminDefault())
			_, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", tc.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want invalid input", err)
			}
			if recorder.count() != 0 {
				t.Fatalf("rejected save still pushed %d writes to MaClawSrv", recorder.count())
			}
		})
	}
}

func TestOwnerLLMOverrideCarriesResponsesWebsocketWire(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, ownerLLMTestRecordWithAdminDefault())
	ctx := context.Background()
	// The OpenAI codex preset ships wire_api "responses-ws". A provider chosen
	// from the desktop list has to reach MaClawSrv carrying it unchanged, or the
	// bot would talk chat-completions to an endpoint that answers neither.
	in := ownerLLMTestCustomInput("sk-oauth")
	in.Name = "OpenAI"
	in.URL = "https://chatgpt.com/backend-api/codex"
	in.Model = "gpt-5-codex"
	in.WireAPI = "responses-ws"
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", in); err != nil {
		t.Fatal(err)
	}
	pushed := recorder.last(t)
	entry, ok := ownerLLMProviderEntry(t, pushed, "OpenAI")
	if !ok {
		t.Fatalf("override provider missing = %#v", pushed["maclaw_llm_providers"])
	}
	if entry["wire_api"] != "responses-ws" {
		t.Fatalf("wire_api = %#v, want responses-ws", entry["wire_api"])
	}
}

func TestOwnerLLMOverrideKeepsStoredKeyWhenBlank(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, record{})
	ctx := context.Background()
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret")); err != nil {
		t.Fatal(err)
	}
	next := ownerLLMTestCustomInput("")
	next.Model = "mine-larger"
	view, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", next)
	if err != nil {
		t.Fatal(err)
	}
	if !view.KeySet || view.Model != "mine-larger" {
		t.Fatalf("view = %#v", view)
	}
	if got := recorder.last(t)["maclaw_llm_key"]; got != "sk-mine-secret" {
		t.Fatalf("blank key did not keep the stored one: %v", got)
	}
}

func TestPushBotOwnerLLMWritesRoleProviderNamesWithoutKeys(t *testing.T) {
	rec := ownerLLMTestRecordWithAdminDefault()
	rec.LLM.Roles = LLMRoles{
		Background:  LLMRoleAssignment{Mode: "pin", Provider: "lp_admin0000000001"},
		ComputerUse: LLMRoleAssignment{Mode: "pin", Provider: "maclaw_official_group"},
		Grounder:    LLMRoleAssignment{Mode: "inherit"},
	}
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, rec)
	ctx := context.Background()
	loaded, err := svc.load(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.writeBotOwnerLLM(ctx, "tenant-a", loaded, "alice", true); err != nil {
		t.Fatal(err)
	}
	cfg := recorder.last(t)
	raw, err := json.Marshal(cfg["maclaw_hub_bot_roles"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-ant") || strings.Contains(string(raw), "lp_admin") {
		t.Fatalf("roles payload leaked a key or stored provider id: %s", raw)
	}
	roles, _ := cfg["maclaw_hub_bot_roles"].(map[string]any)
	if roles == nil {
		t.Fatal("expected maclaw_hub_bot_roles to be written")
	}
	bg, _ := roles["background"].(map[string]any)
	cu, _ := roles["computer_use"].(map[string]any)
	gr, _ := roles["grounder"].(map[string]any)
	fg, _ := roles["foreground"].(map[string]any)
	if bg["mode"] != "pin" || bg["provider"] != "Admin Pick" {
		t.Fatalf("background role = %#v", bg)
	}
	if cu["mode"] != "pin" || cu["provider"] != "hub-llm" {
		t.Fatalf("computer_use role = %#v", cu)
	}
	if gr["mode"] != "inherit" || gr["provider"] != nil {
		t.Fatalf("grounder role = %#v", gr)
	}
	if fg["mode"] != "inherit" {
		t.Fatalf("foreground role = %#v", fg)
	}

	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "lp_admin0000000001", []LLMProviderInput{{
		ID: "lp_admin0000000001", Name: "Admin Pick", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}, nil, &LLMRoles{}); err != nil {
		t.Fatal(err)
	}
	loaded, err = svc.load(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.writeBotOwnerLLM(ctx, "tenant-a", loaded, "alice", true); err != nil {
		t.Fatal(err)
	}
	back := recorder.last(t)
	if _, ok := back["maclaw_hub_bot_roles"]; ok {
		t.Fatalf("default roles should remove maclaw_hub_bot_roles, got %#v", back["maclaw_hub_bot_roles"])
	}
}

func assertSameRolePin(t *testing.T, cfg map[string]any, role, provider, model string) {
	t.Helper()
	profiles, _ := cfg["maclaw_llm_profiles"].(map[string]any)
	pin, _ := profiles[role].(map[string]any)
	if pin == nil {
		t.Fatalf("role %s pin missing in %#v", role, profiles)
	}
	if pin["inherit_hub"] != false || pin["model"] != model || pin["provider_id"] != corelib.MaclawLLMLegacyProviderID(provider) {
		t.Fatalf("role %s pin = %#v", role, pin)
	}
}

func TestOwnerLLMOverrideMigratesIntoRoleRows(t *testing.T) {
	rec := record{OwnerLLMOverrides: map[string]*ownerLLMOverride{
		"alice": {Name: "MyProv", URL: "https://api.mine.example/v1", Key: "sk-mine-secret", Model: "mine-large", Protocol: "openai"},
	}}
	svc, _ := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, rec)
	ctx := context.Background()
	view, err := svc.GetPrincipalLLM(ctx, "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "custom" || view.Name != "MyProv" || view.Model != "mine-large" || !view.KeySet {
		t.Fatalf("legacy view = %#v", view)
	}
	if view.Roles == nil || view.Roles.Foreground.Model != "mine-large" || view.Roles.Background.Name != "MyProv" || view.Roles.ComputerUse.Model != "mine-large" || !view.Roles.Grounder.Unused {
		t.Fatalf("migrated roles = %#v", view.Roles)
	}
	stored, err := svc.load(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerLLMOverrides != nil {
		t.Fatalf("legacy override survived migration: %#v", stored.OwnerLLMOverrides)
	}
	if stored.OwnerBotLLMs["alice"] == nil || stored.OwnerBotLLMs["alice"].Mode != "custom" || stored.OwnerBotLLMs["alice"].Grounder != nil {
		t.Fatalf("stored choice = %#v", stored.OwnerBotLLMs["alice"])
	}
	// A second read must not invent a different choice.
	again, err := svc.GetPrincipalLLM(ctx, "tenant-a", "alice")
	if err != nil || again.Model != view.Model || again.Roles.Background.Model != "mine-large" {
		t.Fatalf("second read = %#v err=%v", again, err)
	}
}

func TestOwnerBotRolesKeepExistingRowsOverLegacyOverride(t *testing.T) {
	rec := record{
		OwnerLLMOverrides: map[string]*ownerLLMOverride{
			"alice": {Name: "Old", URL: "https://old.example/v1", Key: "sk-old", Model: "old-model", Protocol: "openai"},
		},
		OwnerBotLLMs: map[string]*ownerBotLLM{
			"alice": {
				Mode:        "custom",
				Foreground:  &ownerLLMOverride{Name: "Kept", URL: "https://kept.example/v1", Key: "sk-kept", Model: "kept-fg", Protocol: "openai"},
				Background:  &ownerLLMOverride{Name: "Kept", URL: "https://kept.example/v1", Key: "sk-kept", Model: "kept-bg", Protocol: "openai"},
				ComputerUse: &ownerLLMOverride{Name: "Kept", URL: "https://kept.example/v1", Key: "sk-kept", Model: "kept-cu", Protocol: "openai"},
			},
		},
	}
	svc, _ := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, rec)
	view, err := svc.GetPrincipalLLM(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if view.Name != "Kept" || view.Roles.Foreground.Model != "kept-fg" || view.Roles.Background.Model != "kept-bg" || view.Roles.ComputerUse.Model != "kept-cu" {
		t.Fatalf("role rows were replaced by the legacy override: %#v", view)
	}
	stored, err := svc.load(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerLLMOverrides != nil {
		t.Fatalf("legacy override kept beside role rows: %#v", stored.OwnerLLMOverrides)
	}
}

func TestSavePrincipalLLMRolesAreTheOnlySrvSource(t *testing.T) {
	rec := ownerLLMTestRecordWithAdminDefault()
	rec.LLM.Roles = LLMRoles{
		Background: LLMRoleAssignment{Mode: "pin", Provider: "lp_admin0000000001"},
	}
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, rec)
	ctx := context.Background()
	in := PrincipalLLMInput{
		Mode: "custom",
		Roles: &PrincipalLLMRolesInput{
			Foreground:  PrincipalLLMRoleInput{Name: "MyProv", URL: "https://api.mine.example/v1", Key: "sk-mine", Model: "mine-fg", Protocol: "openai"},
			Background:  PrincipalLLMRoleInput{Name: "MyProv", URL: "https://api.mine.example/v1", Key: "sk-mine", Model: "mine-bg"},
			ComputerUse: PrincipalLLMRoleInput{Official: true, Model: "official-mid"},
			Grounder:    PrincipalLLMRoleInput{Name: "MyProv", URL: "https://api.mine.example/v1", Key: "sk-mine", Model: "mine-ground"},
		},
	}
	view, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "custom" || view.Name != "MyProv" || view.Model != "mine-fg" || !view.Roles.ComputerUse.Official || view.Roles.Grounder.Model != "mine-ground" {
		t.Fatalf("view = %#v", view)
	}
	pushed := recorder.last(t)
	if _, ok := pushed["maclaw_hub_bot_roles"]; ok {
		t.Fatalf("custom mode still wrote administrator role pins: %#v", pushed["maclaw_hub_bot_roles"])
	}
	if pushed["maclaw_bot_llm_mode"] != "custom" || pushed["maclaw_llm_current_provider"] != "MyProv" || pushed["maclaw_llm_model"] != "mine-fg" {
		t.Fatalf("flat custom config = %#v", pushed)
	}
	assertSameRolePin(t, pushed, "foreground", "MyProv", "mine-fg")
	assertSameRolePin(t, pushed, "background", "MyProv", "mine-bg")
	assertSameRolePin(t, pushed, "computer_use", "hub-llm", "official-mid")
	assertSameRolePin(t, pushed, "grounder", "MyProv", "mine-ground")

	raw, err := json.Marshal(pushed)
	if err != nil {
		t.Fatal(err)
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	fg, err := agentservice.ResolveLLMRole(cfg, agentservice.LLMRoleForeground)
	if err != nil || fg.Model != "mine-fg" || fg.ProviderName != "MyProv" || fg.Key != "sk-mine" {
		t.Fatalf("foreground resolve = %+v err=%v", fg, err)
	}
	bg, err := agentservice.ResolveLLMRole(cfg, agentservice.LLMRoleBackground)
	if err != nil || bg.Model != "mine-bg" || bg.ProviderName != "MyProv" {
		t.Fatalf("background resolve = %+v err=%v", bg, err)
	}
	cu, err := agentservice.ResolveLLMRole(cfg, agentservice.LLMRoleComputerUse)
	if err != nil || cu.Model != "official-mid" || cu.ProviderName != "hub-llm" {
		t.Fatalf("computer use resolve = %+v err=%v", cu, err)
	}
	gr, err := agentservice.ResolveLLMRole(cfg, agentservice.LLMRoleGrounder)
	if err != nil || gr.Model != "mine-ground" {
		t.Fatalf("grounder resolve = %+v err=%v", gr, err)
	}
}

func TestLegacyBotLLMEndpointMapsOntoRoleRows(t *testing.T) {
	svc, recorder := newPrincipalLLMFixture(t, &countingOwnerLLM{url: ownerLLMTestEndpoint}, record{})
	ctx := context.Background()
	// An older GUI still sends one provider and no roles object.
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", ownerLLMTestCustomInput("sk-mine-secret")); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.load(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	choice := stored.OwnerBotLLMs["alice"]
	if choice == nil || choice.Foreground.Model != "mine-large" || choice.Background.Model != "mine-large" || choice.ComputerUse.Model != "mine-large" || choice.Grounder != nil {
		t.Fatalf("legacy body did not fill the three roles: %#v", choice)
	}
	// Saving default from that GUI clears the roles again.
	if _, err := svc.SavePrincipalLLM(ctx, "tenant-a", "alice", PrincipalLLMInput{Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	if recorder.last(t)["maclaw_bot_llm_mode"] != "default" {
		t.Fatalf("legacy default did not restore the service: %#v", recorder.last(t)["maclaw_bot_llm_mode"])
	}
}
