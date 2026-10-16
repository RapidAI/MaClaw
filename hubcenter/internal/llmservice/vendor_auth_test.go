package llmservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// A provider that switches from one account login to another must not keep
// the previous vendor's credential: the stale refresh token would renew a
// session the admin has already replaced.
func TestSwitchAuthKindDropsOtherVendorCredential(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:                    "wb-cn",
		Name:                  "WorkBuddy",
		APIURL:                "https://copilot.tencent.com/v2",
		APIKey:                "access-token",
		Protocol:              "openai",
		AuthKind:              llmpool.ProviderAuthWorkBuddy,
		WorkBuddyEdition:      "china",
		WorkBuddyRefreshToken: "refresh-token",
		Models:                []string{"glm-5.3"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:                "wb-cn",
		Name:              "Qoder 国内版",
		APIURL:            "https://api2-v2.qoder.sh/model/v1",
		AuthKind:          llmpool.ProviderAuthQoder,
		QoderEdition:      "china",
		QoderRefreshToken: "qoder-refresh",
		QoderUserID:       "qoder-user",
		QoderMachineID:    "machine-1",
		Models:            []string{"qwen3.8-max"},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	stored, err := svc.GetProvider(ctx, "wb-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.WorkBuddyRefreshToken != "" || stored.WorkBuddyEdition != "" || stored.WorkBuddyUserID != "" {
		t.Fatalf("workbuddy credential survived: %#v", stored)
	}
	if stored.QoderRefreshToken != "qoder-refresh" || stored.QoderUserID != "qoder-user" || stored.QoderMachineID != "machine-1" {
		t.Fatalf("qoder credential = %#v", stored)
	}
	if stored.AuthKind != llmpool.ProviderAuthQoder || stored.QoderEdition != "china" {
		t.Fatalf("auth kind = %#v", stored)
	}
}

// An update that omits the login credential (the admin form does) must keep
// the stored one, or every later save would log the provider out.
func TestUpdateWithoutCredentialKeepsLoginToken(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		seed   llmpool.ProviderConfig
		update llmpool.ProviderConfig
	}{
		{
			name: "qoder",
			seed: llmpool.ProviderConfig{
				ID: "qoder-cn", Name: "Qoder 国内版", APIURL: "https://api2-v2.qoder.sh/model/v1",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthQoder,
				QoderEdition: "china", QoderRefreshToken: "refresh", QoderExpiresAt: 4102444800,
				QoderUserID: "user-1", QoderMachineID: "machine-1", Models: []string{"qwen3.8-max"},
			},
			update: llmpool.ProviderConfig{ID: "qoder-cn", Name: "Qoder 国内版 renamed"},
		},
		{
			name: "trae",
			seed: llmpool.ProviderConfig{
				ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
				TraeEdition: "china", TraeRefreshToken: "refresh", TraeExpiresAt: 4102444800,
				TraeUserID: "user-1", TraeMachineID: "machine-1", TraeDeviceID: "device-1",
				Models: []string{"glm-5.2"},
			},
			update: llmpool.ProviderConfig{ID: "trae-cn", Name: "Trae 国内版 renamed"},
		},
		{
			name: "lobsterai",
			seed: llmpool.ProviderConfig{
				ID: "lobsterai", Name: "LobsterAI", APIURL: "https://lobsterai-server.youdao.com",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthLobsterAI,
				LobsterAIRefreshToken: "refresh", LobsterAIExpiresAt: 4102444800,
				LobsterAIUserID: "user-1", LobsterAISessionUUID: "uuid-1",
				LobsterAIFirstKeyfrom: "1735689600000", Models: []string{"glm-5.3"},
			},
			update: llmpool.ProviderConfig{ID: "lobsterai", Name: "LobsterAI renamed"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&mockSystemSettings{data: map[string]string{}})
			if err := svc.AddProvider(ctx, tc.seed); err != nil {
				t.Fatalf("add: %v", err)
			}
			if err := svc.UpdateProvider(ctx, tc.update); err != nil {
				t.Fatalf("update: %v", err)
			}
			stored, err := svc.GetProvider(ctx, tc.seed.ID)
			if err != nil || stored == nil {
				t.Fatalf("stored err=%v provider=%#v", err, stored)
			}
			if stored.APIKey != "access" {
				t.Fatalf("api key lost = %q", stored.APIKey)
			}
			switch tc.name {
			case "qoder":
				if stored.QoderRefreshToken != "refresh" || stored.QoderUserID != "user-1" || stored.QoderMachineID != "machine-1" {
					t.Fatalf("qoder credential = %#v", stored)
				}
			case "trae":
				if stored.TraeRefreshToken != "refresh" || stored.TraeUserID != "user-1" || stored.TraeMachineID != "machine-1" || stored.TraeDeviceID != "device-1" {
					t.Fatalf("trae credential = %#v", stored)
				}
			case "lobsterai":
				if stored.LobsterAIRefreshToken != "refresh" || stored.LobsterAIUserID != "user-1" || stored.LobsterAISessionUUID != "uuid-1" {
					t.Fatalf("lobsterai credential = %#v", stored)
				}
			}
		})
	}
}

// An update that explicitly selects a static key must not resurrect the
// account it replaced. mergeWorkBuddyAuth runs first and rewrites api_key to
// "", which made an explicit static-key choice indistinguishable from an
// omission; the vendor merge then read the stored account back and left a dead
// refresh token on a plain-key row.
func TestStaticKeyUpdateDropsVendorAccount(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		seed  llmpool.ProviderConfig
		kind  string
	}{
		{
			name: "qoder",
			seed: llmpool.ProviderConfig{
				ID: "qoder-cn", Name: "Qoder 国内版", APIURL: "https://api2-v2.qoder.sh/model/v1",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthQoder,
				QoderEdition: "china", QoderRefreshToken: "refresh", QoderUserID: "user-1",
				QoderMachineID: "machine-1", Models: []string{"qwen3.8-max"},
			},
			kind: llmpool.ProviderAuthQoder,
		},
		{
			name: "trae",
			seed: llmpool.ProviderConfig{
				ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
				APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
				TraeEdition: "china", TraeRefreshToken: "refresh", TraeUserID: "user-1",
				TraeMachineID: "machine-1", TraeDeviceID: "device-1", Models: []string{"glm-5.2"},
			},
			kind: llmpool.ProviderAuthTrae,
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
			kind: llmpool.ProviderAuthLobsterAI,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&mockSystemSettings{data: map[string]string{}})
			if err := svc.AddProvider(ctx, tc.seed); err != nil {
				t.Fatalf("add: %v", err)
			}
			if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
				ID: tc.seed.ID, Name: tc.seed.Name, APIURL: tc.seed.APIURL,
				APIKey: "sk-manual", Protocol: "openai",
				AuthKind: llmpool.ProviderAuthAPIKey, Models: tc.seed.Models,
			}); err != nil {
				t.Fatalf("update: %v", err)
			}
			stored, err := svc.GetProvider(ctx, tc.seed.ID)
			if err != nil || stored == nil {
				t.Fatalf("stored err=%v provider=%#v", err, stored)
			}
			if stored.APIKey != "sk-manual" {
				t.Fatalf("static key lost = %q", stored.APIKey)
			}
			if stored.AuthKind == tc.kind {
				t.Fatalf("auth kind resurrected the account = %q", stored.AuthKind)
			}
			if stored.QoderRefreshToken != "" || stored.TraeRefreshToken != "" || stored.LobsterAIRefreshToken != "" {
				t.Fatalf("account survived a static-key update = %#v", stored)
			}
			if stored.QoderUserID != "" || stored.TraeUserID != "" || stored.LobsterAIUserID != "" ||
				stored.QoderMachineID != "" || stored.TraeMachineID != "" || stored.TraeDeviceID != "" {
				t.Fatalf("account identity survived = %#v", stored)
			}
		})
	}
}

// The array-move payload omits auth_kind and every credential field, and it
// does not run the login appliers. Adopting the stored vendor there is what
// keeps a live account alive across a move.
func TestArrayMoveKeepsVendorAccount(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	seed := llmpool.ProviderConfig{
		ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
		APIKey: "access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
		TraeEdition: "china", TraeRefreshToken: "refresh", TraeUserID: "user-1",
		TraeMachineID: "machine-1", TraeDeviceID: "device-1", Models: []string{"glm-5.2"},
	}
	if err := svc.AddProvider(ctx, seed); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
		Protocol: "openai", Models: []string{"glm-5.2"}, Timezone: "Asia/Shanghai",
		CreditMultiplier: 1, ServeWindows: []llmpool.TokenBankShareWindow{},
		ArrayID: "other",
	}); err != nil {
		t.Fatalf("move: %v", err)
	}
	stored, err := svc.GetProvider(ctx, "trae-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.ArrayID != "other" {
		t.Fatalf("array id = %q", stored.ArrayID)
	}
	if stored.AuthKind != llmpool.ProviderAuthTrae {
		t.Fatalf("auth kind lost on move = %q", stored.AuthKind)
	}
	if stored.TraeRefreshToken != "refresh" || stored.TraeDeviceID != "device-1" || stored.TraeMachineID != "machine-1" {
		t.Fatalf("trae credential lost on move = %#v", stored)
	}
	if stored.APIKey != "access" {
		t.Fatalf("access token lost on move = %q", stored.APIKey)
	}
}

// An unknown auth kind must degrade to a static key rather than fail every
// request, and a login session id must never reach the stored registry.
func TestNormalizeProviderAuthDropsSessionAndUnknownKind(t *testing.T) {
	provider := llmpool.ProviderConfig{
		ID:                 "p",
		Name:               "P",
		APIURL:             "https://example.test/v1",
		AuthKind:           "qoder",
		QoderEdition:       "china",
		QoderRefreshToken:  "refresh",
		QoderSessionID:     "session",
		TraeSessionID:      "session",
		LobsterAISessionID: "session",
	}
	normalizeProviderAuth(&provider)
	if provider.QoderSessionID != "" || provider.TraeSessionID != "" || provider.LobsterAISessionID != "" {
		t.Fatalf("session id survived = %#v", provider)
	}
	if provider.AuthKind != llmpool.ProviderAuthQoder || provider.QoderEdition != "china" {
		t.Fatalf("auth = %#v", provider)
	}

	provider.AuthKind = "made-up"
	provider.TraeRefreshToken = "trae-refresh"
	normalizeProviderAuth(&provider)
	if provider.AuthKind != "" {
		t.Fatalf("unknown kind kept = %q", provider.AuthKind)
	}
	if provider.TraeRefreshToken != "" || provider.QoderRefreshToken != "" {
		t.Fatalf("secrets survived = %#v", provider)
	}
}

// The edition is recovered from the provider name when it went missing, so a
// save cannot silently re-point the provider at the other realm. The stored
// value is the admin-facing "china"/"global" form, not the profile id.
func TestNormalizeProviderAuthRecoversEditionFromName(t *testing.T) {
	qoderProvider := llmpool.ProviderConfig{ID: "q", AuthKind: llmpool.ProviderAuthQoder, Name: "Qoder 国际版"}
	normalizeProviderAuth(&qoderProvider)
	if qoderProvider.QoderEdition != "global" {
		t.Fatalf("qoder edition = %q", qoderProvider.QoderEdition)
	}
	traeProvider := llmpool.ProviderConfig{ID: "t", AuthKind: llmpool.ProviderAuthTrae, Name: "Trae 国内版"}
	normalizeProviderAuth(&traeProvider)
	if traeProvider.TraeEdition != "china" {
		t.Fatalf("trae edition = %q", traeProvider.TraeEdition)
	}
}

// The vendor presets the admin form offers must resolve to the same chat host
// the upstream forwarders target, or the form would save a URL the dispatcher
// does not recognize.
func TestVendorChatHostsMatchPresets(t *testing.T) {
	cases := []struct {
		kind     string
		edition  string
		wantName string
	}{
		{llmpool.ProviderAuthQoder, "china", "Qoder 国内版"},
		{llmpool.ProviderAuthQoder, "global", "Qoder 国际版"},
		{llmpool.ProviderAuthTrae, "china", "Trae 国内版"},
		{llmpool.ProviderAuthTrae, "global", "Trae 国际版"},
		{llmpool.ProviderAuthLobsterAI, "", "LobsterAI"},
	}
	for _, tc := range cases {
		provider := llmpool.ProviderConfig{
			ID: tc.kind + "-" + tc.edition, Name: tc.wantName,
			APIURL: "https://placeholder.invalid", APIKey: "key", Protocol: "openai",
			AuthKind: tc.kind, QoderEdition: tc.edition, TraeEdition: tc.edition,
			Models: []string{"m"},
		}
		switch tc.kind {
		case llmpool.ProviderAuthQoder:
			cfg, ok := QoderMaclawConfig(&provider, "")
			if !ok {
				t.Fatalf("%s %s: qoder config not recognized", tc.kind, tc.edition)
			}
			if cfg.ProviderName != tc.wantName {
				t.Fatalf("qoder %s provider name = %q want %q", tc.edition, cfg.ProviderName, tc.wantName)
			}
			if !strings.HasSuffix(cfg.URL, "qoder.sh/model/v1") {
				t.Fatalf("qoder %s url = %q", tc.edition, cfg.URL)
			}
		case llmpool.ProviderAuthTrae:
			cfg, ok := TraeMaclawConfig(&provider, "")
			if !ok {
				t.Fatalf("%s %s: trae config not recognized", tc.kind, tc.edition)
			}
			if cfg.ProviderName != tc.wantName {
				t.Fatalf("trae %s provider name = %q want %q", tc.edition, cfg.ProviderName, tc.wantName)
			}
			if cfg.URL == "" {
				t.Fatalf("trae %s url empty", tc.edition)
			}
		case llmpool.ProviderAuthLobsterAI:
			cfg, ok := LobsterAIMaclawConfig(&provider, "")
			if !ok {
				t.Fatalf("lobsterai config not recognized")
			}
			if cfg.ProviderName != tc.wantName {
				t.Fatalf("lobsterai provider name = %q want %q", cfg.ProviderName, tc.wantName)
			}
		}
	}
}

// A static-key provider must never be routed through a vendor translator: the
// dispatcher decides per provider, and a false positive would translate a
// plain OpenAI request into the signed wire.
func TestVendorMaclawConfigIgnoresStaticKeyProviders(t *testing.T) {
	static := llmpool.ProviderConfig{
		ID: "plain", Name: "Plain", APIURL: "https://api.openai.com/v1",
		APIKey: "sk-test", Protocol: "openai", Models: []string{"gpt-4o"},
	}
	if _, ok := QoderMaclawConfig(&static, ""); ok {
		t.Fatal("qoder matched a static-key provider")
	}
	if _, ok := TraeMaclawConfig(&static, ""); ok {
		t.Fatal("trae matched a static-key provider")
	}
	if _, ok := LobsterAIMaclawConfig(&static, ""); ok {
		t.Fatal("lobsterai matched a static-key provider")
	}
	staticCfg := corelib.MaclawLLMConfig{
		URL: static.APIURL, Key: static.APIKey, Model: "gpt-4o",
		Protocol: "openai", ProviderName: static.Name,
	}
	if QoderMaclawConfigFromConfig(staticCfg) {
		t.Fatal("qoder from-config matched a static-key provider")
	}
	if TraeMaclawConfigFromConfig(staticCfg) {
		t.Fatal("trae from-config matched a static-key provider")
	}
}

// An account login whose refresh is due must be renewed in place and
// persisted; a token that never expires must be left alone.
func TestVendorCredentialRefreshWindow(t *testing.T) {
	now := time.Now()
	if !qoderCredentialNeedsRefresh(qoderCredential{ExpiresAt: now.Add(-time.Minute).Unix()}, now) {
		t.Fatal("expired token should need refresh")
	}
	if qoderCredentialNeedsRefresh(qoderCredential{ExpiresAt: now.Add(time.Hour).Unix()}, now) {
		t.Fatal("fresh token should not need refresh")
	}
	// The skew window means a token expiring in two minutes is already stale.
	if !qoderCredentialNeedsRefresh(qoderCredential{ExpiresAt: now.Add(120 * time.Second).Unix()}, now) {
		t.Fatal("token inside the skew window should need refresh")
	}
	// Zero expiry means the server never reported one; the token is used as is.
	if qoderCredentialNeedsRefresh(qoderCredential{}, now) {
		t.Fatal("zero expiry should never need refresh")
	}
	if traeCredentialNeedsRefresh(traeCredential{}, now) {
		t.Fatal("trae zero expiry should never need refresh")
	}
	if lobsterAICredentialNeedsRefresh(lobsterAICredential{}, now) {
		t.Fatal("lobsterai zero expiry should never need refresh")
	}
}
