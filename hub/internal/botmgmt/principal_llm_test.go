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
	}); err != nil {
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
		{name: "system-free reserved", input: PrincipalLLMInput{Mode: "custom", Name: llmCurrentSystemFree, URL: "https://x.example/v1", Key: "k", Model: "m"}},
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
