package llmservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/trae"
)

// seedQoderLogin parks a finished login in the service's session map so the
// apply path can be exercised without a browser or a network round trip.
func seedQoderLogin(svc *Service, edition string, token *qoder.Token, models []QoderModel) string {
	id, err := newLoginSessionID()
	if err != nil {
		panic(err)
	}
	profile, _ := qoder.ProfileByEdition(edition)
	svc.qoderMu.Lock()
	defer svc.qoderMu.Unlock()
	if svc.qoderLogins == nil {
		svc.qoderLogins = map[string]*qoderLogin{}
	}
	svc.qoderLogins[id] = &qoderLogin{
		id:        id,
		edition:   canonicalQoderEdition(edition),
		profile:   profile,
		status:    qoderStatusReady,
		token:     token,
		models:    models,
		keepUntil: time.Now().Add(qoderLoginKeep),
	}
	return id
}

func seedTraeLogin(svc *Service, edition string, token *trae.Token, models []TraeModel) string {
	id, err := newLoginSessionID()
	if err != nil {
		panic(err)
	}
	profile, _ := trae.ProfileByEdition(edition)
	svc.traeMu.Lock()
	defer svc.traeMu.Unlock()
	if svc.traeLogins == nil {
		svc.traeLogins = map[string]*traeLogin{}
	}
	svc.traeLogins[id] = &traeLogin{
		id:        id,
		edition:   canonicalTraeEdition(edition),
		profile:   profile,
		status:    traeStatusReady,
		token:     token,
		models:    models,
		keepUntil: time.Now().Add(traeLoginKeep),
	}
	return id
}

// A finished Qoder login must fill the provider's chat URL, credential and
// device id, and must not leave the session id or an out-of-catalog model.
func TestApplyQoderLoginFillsProvider(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	sessionID := seedQoderLogin(svc, "china", &qoder.Token{
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 4102444800, UserID: "user-1",
	}, []QoderModel{{ID: "qwen3.8-max", Name: "Qwen 3.8 Max"}, {ID: "glm-5.3", Name: "GLM 5.3"}})
	defer svc.ForgetQoderLogin(sessionID)

	provider := llmpool.ProviderConfig{
		ID: "qoder-cn", AuthKind: llmpool.ProviderAuthQoder, QoderEdition: "china",
		QoderSessionID: sessionID, Models: []string{"qwen3.8-max", "glm-5.3"},
	}
	if err := svc.ApplyQoderLogin(&provider, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if provider.APIKey != "access" || provider.QoderRefreshToken != "refresh" || provider.QoderUserID != "user-1" {
		t.Fatalf("credential = %#v", provider)
	}
	if provider.QoderMachineID == "" {
		t.Fatal("machine id missing")
	}
	if provider.Protocol != "openai" || provider.APIURL != qoder.CanonicalChatURL(qoder.CNProfile().ChatURL) {
		t.Fatalf("provider = %#v", provider)
	}
	if provider.QoderSessionID != "" {
		t.Fatal("session id leaked")
	}
	if len(provider.Models) != 2 || provider.Models[0] != "qwen3.8-max" || provider.Models[1] != "glm-5.3" {
		t.Fatalf("models = %#v", provider.Models)
	}
	if provider.Name != qoder.NameCN {
		t.Fatalf("name = %q", provider.Name)
	}

	// A model the catalog does not list must be rejected, and an empty
	// selection must not create a provider with no models.
	fresh := provider
	fresh.Models = []string{"not-a-model"}
	fresh.QoderSessionID = sessionID
	if err := svc.ApplyQoderLogin(&fresh, true); err == nil {
		t.Fatal("unknown model was accepted")
	}
	fresh.Models = nil
	if err := svc.ApplyQoderLogin(&fresh, true); err == nil {
		t.Fatal("empty model list was accepted")
	}
}

// An edition that disagrees with the finished login must be refused: the two
// editions share one chat URL, so accepting it would bind the wrong realm.
func TestApplyQoderLoginRejectsEditionMismatch(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	sessionID := seedQoderLogin(svc, "china", &qoder.Token{AccessToken: "access"}, []QoderModel{{ID: "qwen3.8-max"}})
	defer svc.ForgetQoderLogin(sessionID)

	provider := llmpool.ProviderConfig{
		ID: "qoder-global", AuthKind: llmpool.ProviderAuthQoder, QoderEdition: "global",
		QoderSessionID: sessionID, Models: []string{"qwen3.8-max"},
	}
	if err := svc.ApplyQoderLogin(&provider, true); err == nil {
		t.Fatal("edition mismatch was accepted")
	}
}

// A new provider with no login at all must be refused, but an update that
// omits the session must keep the saved account untouched.
func TestApplyQoderLoginRequiresLoginOnlyOnCreate(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	provider := llmpool.ProviderConfig{ID: "qoder-cn", AuthKind: llmpool.ProviderAuthQoder, QoderEdition: "china"}
	if err := svc.ApplyQoderLogin(&provider, true); err == nil {
		t.Fatal("create without login was accepted")
	}
	provider.APIKey = "saved-access"
	provider.QoderRefreshToken = "saved-refresh"
	provider.QoderUserID = "saved-user"
	provider.QoderMachineID = "saved-machine"
	if err := svc.ApplyQoderLogin(&provider, false); err != nil {
		t.Fatalf("update without login: %v", err)
	}
	if provider.APIKey != "saved-access" || provider.QoderRefreshToken != "saved-refresh" {
		t.Fatalf("credential clobbered = %#v", provider)
	}
	if provider.AuthKind != llmpool.ProviderAuthQoder || provider.QoderEdition != "china" {
		t.Fatalf("auth = %#v", provider)
	}
	if provider.APIURL != qoder.CanonicalChatURL(qoder.CNProfile().ChatURL) {
		t.Fatalf("url = %q", provider.APIURL)
	}
}

// Switching a Qoder provider back to a static key must drop the login
// credential, so the row cannot be replayed as an account.
func TestApplyQoderLoginApiKeyClearsSecrets(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	provider := llmpool.ProviderConfig{
		ID: "qoder-cn", AuthKind: llmpool.ProviderAuthAPIKey,
		QoderEdition: "china", QoderRefreshToken: "refresh", QoderUserID: "user",
		QoderMachineID: "machine", APIKey: "sk-static",
	}
	if err := svc.ApplyQoderLogin(&provider, false); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if provider.QoderRefreshToken != "" || provider.QoderUserID != "" || provider.QoderMachineID != "" || provider.QoderEdition != "" {
		t.Fatalf("secrets survived = %#v", provider)
	}
	if provider.APIKey != "sk-static" {
		t.Fatalf("static key lost = %q", provider.APIKey)
	}
}

func TestApplyTraeLoginFillsProvider(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	sessionID := seedTraeLogin(svc, "china", &trae.Token{
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 4102444800,
		UserID: "user-1", MachineID: "machine-1", DeviceID: "device-1",
	}, []TraeModel{{ID: "glm-5.2", Name: "GLM 5.2"}})
	defer svc.ForgetTraeLogin(sessionID)

	provider := llmpool.ProviderConfig{
		ID: "trae-cn", AuthKind: llmpool.ProviderAuthTrae, TraeEdition: "china",
		TraeSessionID: sessionID, Models: []string{"glm-5.2"},
	}
	if err := svc.ApplyTraeLogin(&provider, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if provider.APIKey != "access" || provider.TraeRefreshToken != "refresh" || provider.TraeUserID != "user-1" {
		t.Fatalf("credential = %#v", provider)
	}
	// The chat request must carry exactly the device pair the token was minted
	// against; a claims-derived fallback is refused upstream.
	if provider.TraeMachineID != "machine-1" || provider.TraeDeviceID != "device-1" {
		t.Fatalf("device pair = %#v", provider)
	}
	if provider.Protocol != "openai" || provider.APIURL != trae.CanonicalChatURL(trae.CNProfile(), trae.CNProfile().ChatHost) {
		t.Fatalf("provider = %#v", provider)
	}
	if provider.TraeSessionID != "" {
		t.Fatal("session id leaked")
	}
	if provider.Name != trae.NameCN {
		t.Fatalf("name = %q", provider.Name)
	}
}

func TestApplyLobsterAILoginFillsProvider(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	provider := llmpool.ProviderConfig{
		ID: "lobsterai", AuthKind: llmpool.ProviderAuthLobsterAI,
		LobsterAISessionID: "missing-session", Models: []string{"glm-5.3"},
	}
	if err := svc.ApplyLobsterAILogin(&provider, true); err == nil {
		t.Fatal("unknown session was accepted")
	}

	provider.LobsterAISessionID = ""
	if err := svc.ApplyLobsterAILogin(&provider, true); err == nil {
		t.Fatal("create without login was accepted")
	}
	provider.APIKey = "saved-access"
	provider.LobsterAIRefreshToken = "saved-refresh"
	provider.LobsterAISessionUUID = "uuid-1"
	provider.LobsterAIFirstKeyfrom = "1735689600000"
	provider.LobsterAIUserID = "user-1"
	if err := svc.ApplyLobsterAILogin(&provider, false); err != nil {
		t.Fatalf("update without login: %v", err)
	}
	if provider.APIKey != "saved-access" || provider.LobsterAIRefreshToken != "saved-refresh" {
		t.Fatalf("credential clobbered = %#v", provider)
	}
	// first_keyfrom is kept lifetime-long upstream and must be replayed on
	// every refresh, so an update that omits it must not drop it.
	if provider.LobsterAIFirstKeyfrom != "1735689600000" || provider.LobsterAISessionUUID != "uuid-1" {
		t.Fatalf("refresh context = %#v", provider)
	}
	if provider.AuthKind != llmpool.ProviderAuthLobsterAI {
		t.Fatalf("auth kind = %q", provider.AuthKind)
	}
}

// A login session the admin cancelled must stop answering, so a stale tab
// cannot apply a credential the admin already abandoned.
func TestVendorLoginLifecycle(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if _, err := svc.StartQoderLogin("china"); err != nil {
		t.Fatalf("start qoder: %v", err)
	}
	if _, err := svc.StartTraeLogin("china"); err != nil {
		t.Fatalf("start trae: %v", err)
	}
	if _, err := svc.StartLobsterAILogin(); err != nil {
		t.Fatalf("start lobsterai: %v", err)
	}
	qoderCount := 0
	svc.qoderMu.Lock()
	qoderCount = len(svc.qoderLogins)
	svc.qoderMu.Unlock()
	if qoderCount != 1 {
		t.Fatalf("qoder logins = %d", qoderCount)
	}
	// An unknown session is an error rather than a silent "still pending".
	if _, err := svc.QoderLoginStatus("does-not-exist"); err == nil {
		t.Fatal("unknown qoder session was accepted")
	}
	if _, err := svc.TraeLoginStatus("does-not-exist"); err == nil {
		t.Fatal("unknown trae session was accepted")
	}
	if _, err := svc.LobsterAILoginStatus("does-not-exist"); err == nil {
		t.Fatal("unknown lobsterai session was accepted")
	}
	// An unknown edition is refused before any network call is made.
	if _, err := svc.StartQoderLogin("nope"); err == nil {
		t.Fatal("unknown qoder edition was accepted")
	}
	if _, err := svc.StartTraeLogin("nope"); err == nil {
		t.Fatal("unknown trae edition was accepted")
	}
	// Cancel and forget are idempotent, so a double click cannot panic. They
	// take the login mutex themselves, so collect the ids before calling them.
	svc.qoderMu.Lock()
	qoderIDs := make([]string, 0, len(svc.qoderLogins))
	for id := range svc.qoderLogins {
		qoderIDs = append(qoderIDs, id)
	}
	svc.qoderMu.Unlock()
	for _, id := range qoderIDs {
		svc.CancelQoderLogin(id)
		svc.CancelQoderLogin(id)
		svc.ForgetQoderLogin(id)
		svc.ForgetQoderLogin(id)
	}
	svc.traeMu.Lock()
	traeIDs := make([]string, 0, len(svc.traeLogins))
	for id := range svc.traeLogins {
		traeIDs = append(traeIDs, id)
	}
	svc.traeMu.Unlock()
	for _, id := range traeIDs {
		svc.CancelTraeLogin(id)
		svc.CancelTraeLogin(id)
		svc.ForgetTraeLogin(id)
		svc.ForgetTraeLogin(id)
	}
	svc.lobsterAIMu.Lock()
	lobsterIDs := make([]string, 0, len(svc.lobsterAILogins))
	for id := range svc.lobsterAILogins {
		lobsterIDs = append(lobsterIDs, id)
	}
	svc.lobsterAIMu.Unlock()
	for _, id := range lobsterIDs {
		svc.CancelLobsterAILogin(id)
		svc.CancelLobsterAILogin(id)
		svc.ForgetLobsterAILogin(id)
		svc.ForgetLobsterAILogin(id)
	}
	svc.qoderMu.Lock()
	left := len(svc.qoderLogins)
	svc.qoderMu.Unlock()
	if left != 0 {
		t.Fatalf("qoder logins left = %d", left)
	}
}

// The admin list must never expose an account-login credential: the frontend
// only needs the auth kind, the edition, and whether a key is stored.
//
// The editor payload carries the auth kind and the edition but never the
// tokens, the url or the protocol — the login handler fills those in before the
// row reaches the registry. This walks the whole handler path so an omitted
// url cannot silently blank a live account provider.
func TestAddProviderRoundTripsVendorLogin(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		seed   llmpool.ProviderConfig
		kind   string
		editor llmpool.ProviderConfig
	}{
		{
			name: "qoder",
			seed: llmpool.ProviderConfig{
				ID: "qoder-cn", Name: "Qoder 国内版", APIURL: "https://api2-v2.qoder.sh/model/v1",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthQoder,
				QoderEdition: "china", QoderRefreshToken: "refresh", QoderUserID: "user-1",
				QoderMachineID: "machine-1", Models: []string{"qwen3.8-max"},
			},
			kind:   llmpool.ProviderAuthQoder,
			editor: llmpool.ProviderConfig{ID: "qoder-cn", Name: "Qoder 国内版", AuthKind: llmpool.ProviderAuthQoder, QoderEdition: "china", Models: []string{"qwen3.8-max"}},
		},
		{
			name: "trae",
			seed: llmpool.ProviderConfig{
				ID: "trae-global", Name: "Trae 国际版", APIURL: "https://coresg-normal.trae.ai",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
				TraeEdition: "global", TraeRefreshToken: "refresh", TraeUserID: "user-1",
				TraeMachineID: "machine-1", TraeDeviceID: "device-1", Models: []string{"gpt-5"},
			},
			kind:   llmpool.ProviderAuthTrae,
			editor: llmpool.ProviderConfig{ID: "trae-global", Name: "Trae 国际版", AuthKind: llmpool.ProviderAuthTrae, TraeEdition: "global", Models: []string{"gpt-5"}},
		},
		{
			name: "lobsterai",
			seed: llmpool.ProviderConfig{
				ID: "lobsterai", Name: "LobsterAI", APIURL: "https://lobsterai-server.youdao.com",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthLobsterAI,
				LobsterAIRefreshToken: "refresh", LobsterAIUserID: "user-1",
				LobsterAISessionUUID: "uuid-1", LobsterAIFirstKeyfrom: "1735689600000",
				Models: []string{"glm-5.3"},
			},
			kind:   llmpool.ProviderAuthLobsterAI,
			editor: llmpool.ProviderConfig{ID: "lobsterai", Name: "LobsterAI", AuthKind: llmpool.ProviderAuthLobsterAI, Models: []string{"glm-5.3"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&mockSystemSettings{data: map[string]string{}})
			if err := svc.AddProvider(ctx, tc.seed); err != nil {
				t.Fatalf("add: %v", err)
			}
			editor := tc.editor
			// adminUpdateLLMProvider runs the login appliers on every save.
			if err := svc.ApplyQoderLogin(&editor, false); err != nil {
				t.Fatalf("apply qoder: %v", err)
			}
			if err := svc.ApplyTraeLogin(&editor, false); err != nil {
				t.Fatalf("apply trae: %v", err)
			}
			if err := svc.ApplyLobsterAILogin(&editor, false); err != nil {
				t.Fatalf("apply lobsterai: %v", err)
			}
			if err := svc.UpdateProvider(ctx, editor); err != nil {
				t.Fatalf("update: %v", err)
			}
			stored, err := svc.GetProvider(ctx, tc.seed.ID)
			if err != nil || stored == nil {
				t.Fatalf("stored err=%v provider=%#v", err, stored)
			}
			if stored.AuthKind != tc.kind {
				t.Fatalf("auth kind = %q want %q", stored.AuthKind, tc.kind)
			}
			if stored.APIKey != "access" {
				t.Fatalf("access token lost = %q", stored.APIKey)
			}
			if stored.APIURL != tc.seed.APIURL {
				t.Fatalf("url rewritten = %q want %q", stored.APIURL, tc.seed.APIURL)
			}
		})
	}
}
