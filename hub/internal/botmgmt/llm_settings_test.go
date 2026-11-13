package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSaveLLMSettingsKeepsKeyOutOfTheView(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Claude", Protocol: "Anthropic",
		URL: "https://api.anthropic.com/", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.LLM.DefaultServiceGroup != "maclaw_official_group" || view.LLM.Current != "maclaw_official_group" || len(view.LLM.Providers) != 1 {
		t.Fatalf("view = %#v", view.LLM)
	}
	got := view.LLM.Providers[0]
	if got.ID != "lp_abcdef0123456789" || got.Protocol != "anthropic" || got.URL != "https://api.anthropic.com" || !got.KeySet {
		t.Fatalf("provider = %#v", got)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sk-ant-secret") {
		t.Fatalf("view leaked the key: %s", encoded)
	}

	again, err := svc.SaveLLMSettings(ctx, "tenant-a", got.ID, []LLMProviderInput{{
		ID: got.ID, Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.LLM.Current != got.ID || !again.LLM.Providers[0].KeySet {
		t.Fatalf("blank key dropped the saved key: %#v", again.LLM)
	}
	raw, err := svc.System.Get(ctx, storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "sk-ant-secret") {
		t.Fatal("stored settings lost the key")
	}
}

func TestSaveLLMSettingsRejectsProtocolNameAndMissingKey(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	cases := []LLMProviderInput{
		{Name: "Other", Protocol: "azure", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "hub-llm", Protocol: "openai", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "system-free", Protocol: "openai", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "Other", Protocol: "openai", URL: "ftp://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "Other", Protocol: "openai", URL: "https://llm.example/v1", Model: "m"},
		{ID: "not-an-id", Name: "Other", Protocol: "openai", URL: "https://llm.example/v1", Model: "m"},
	}
	for _, in := range cases {
		if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{in}, nil, nil); err == nil {
			t.Fatalf("accepted %#v", in)
		}
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "missing", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Other", Protocol: "openai",
		URL: "https://llm.example/v1", Key: "sk-1", Model: "m",
	}}, nil, nil); err == nil {
		t.Fatal("accepted an unknown current provider")
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{
		{Name: "智谱编程", Protocol: "anthropic", URL: "https://open.bigmodel.cn/api/anthropic", Key: "sk-1", Model: "glm"},
		{Name: "Zhipu GLM Coding", Protocol: "anthropic", URL: "https://open.bigmodel.cn/api/anthropic", Key: "sk-2", Model: "glm"},
	}, nil, nil); err == nil || !strings.Contains(err.Error(), "llm provider name is duplicated") {
		t.Fatalf("aliased names: %v", err)
	}
}

func TestSaveLLMSettingsDerivesCurrentFromTheForegroundRole(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "OpenAI", Protocol: "openai",
		URL: "https://api.openai.com/v1", Key: "sk-1", Model: "gpt-4o",
	}}, nil, &LLMRoles{
		Foreground:  LLMRoleAssignment{Mode: "pin", Provider: "lp_abcdef0123456789", Model: "gpt-4o-mini"},
		Background:  LLMRoleAssignment{Mode: "inherit"},
		ComputerUse: LLMRoleAssignment{Mode: "pin", Provider: "maclaw_official_group", Model: "should-not-survive"},
		Grounder:    LLMRoleAssignment{Mode: "unused"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.LLM.Current != "lp_abcdef0123456789" {
		t.Fatalf("current = %q, want the foreground pin", view.LLM.Current)
	}
	if view.LLM.Roles.Foreground.Model != "gpt-4o-mini" {
		t.Fatalf("foreground model = %q", view.LLM.Roles.Foreground.Model)
	}
	// The official route schedules its own model: a stored model on an
	// official pin is dropped and the pin is canonicalized.
	if view.LLM.Roles.ComputerUse.Provider != "maclaw_official_group" || view.LLM.Roles.ComputerUse.Model != "" {
		t.Fatalf("computer use = %+v", view.LLM.Roles.ComputerUse)
	}

	// A foreground model override is what the flat fallback runs, so a stale
	// role pin falls back to the same model instead of the provider default.
	snapshot := svc.llmSettingsSnapshot(ctx, "tenant-a")
	app := map[string]any{}
	applyServiceBotLLMFlat(app, snapshot, "https://hub.example/api/llm/v1", "viewer-token")
	if app["maclaw_llm_current_provider"] != "OpenAI" || app["maclaw_llm_model"] != "gpt-4o-mini" {
		t.Fatalf("flat fallback = %v", app)
	}

	// The owner-facing effective view shows the role model override, too.
	roles := effectiveServiceRoles(snapshot)
	if roles.Foreground.Name != "OpenAI" || roles.Foreground.Model != "gpt-4o-mini" {
		t.Fatalf("effective foreground = %+v", roles.Foreground)
	}
	if !roles.ComputerUse.Official || roles.ComputerUse.Model != "auto" {
		t.Fatalf("effective computer use = %+v", roles.ComputerUse)
	}
}

func TestSaveLLMSettingsRejectsABadRoleModel(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	long := strings.Repeat("m", 129)
	cases := []LLMRoles{
		{Foreground: LLMRoleAssignment{Mode: "pin", Provider: "lp_abcdef0123456789", Model: "bad\nmodel"}},
		{Foreground: LLMRoleAssignment{Mode: "pin", Provider: "lp_abcdef0123456789", Model: long}},
	}
	for _, roles := range cases {
		_, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
			ID: "lp_abcdef0123456789", Name: "OpenAI", Protocol: "openai",
			URL: "https://api.openai.com/v1", Key: "sk-1", Model: "gpt-4o",
		}}, nil, &roles)
		if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "llm role model is invalid") {
			t.Fatalf("roles = %+v err = %v", roles, err)
		}
	}
}

// A foreground pin on the official group without a model is the untouched
// default: it must not push a roles config to MaClawSrv.
func TestSaveLLMSettingsForegroundOfficialPinStaysDefault(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, nil, &LLMRoles{
		Foreground: LLMRoleAssignment{Mode: "pin", Provider: "maclaw_official_group"},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := svc.llmSettingsSnapshot(ctx, "tenant-a")
	if stamp := llmConfigStamp(snapshot); stamp != "" {
		t.Fatalf("stamp = %q, want empty for the untouched default", stamp)
	}
}

func TestSaveLLMSettingsKeepsTheConnection(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", "https://maclaw.example", "token-1", true); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.BaseURL != "https://maclaw.example" || !view.TokenSet || len(view.LLM.Providers) != 1 {
		t.Fatalf("llm save changed the connection: %#v", view)
	}
	again, err := svc.SaveConnection(ctx, "tenant-a", "https://maclaw.example/other", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.BaseURL != "https://maclaw.example/other" || !again.TokenSet || len(again.LLM.Providers) != 1 || !again.LLM.Providers[0].KeySet {
		t.Fatalf("connection save changed the llm settings: %#v", again)
	}
	raw, err := svc.System.Get(ctx, storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "token-1") || !strings.Contains(raw, "sk-ant-secret") {
		t.Fatalf("stored record dropped a secret: %s", raw)
	}
}

func TestSaveLLMSettingsRolesDefaultAndKeepOnOmit(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.LLM.Roles.Foreground.Mode != "inherit" ||
		view.LLM.Roles.Background.Mode != "inherit" ||
		view.LLM.Roles.ComputerUse.Mode != "inherit" ||
		view.LLM.Roles.Grounder.Mode != "unused" {
		t.Fatalf("default roles = %#v", view.LLM.Roles)
	}
	pinned := LLMRoles{
		Foreground:  LLMRoleAssignment{Mode: "inherit"},
		Background:  LLMRoleAssignment{Mode: "pin", Provider: "lp_claude0000000001"},
		ComputerUse: LLMRoleAssignment{Mode: "pin", Provider: "maclaw_official_group"},
		Grounder:    LLMRoleAssignment{Mode: "inherit"},
	}
	saved, err := svc.SaveLLMSettings(ctx, "tenant-a", "lp_claude0000000001", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}, nil, &pinned)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LLM.Roles.Background.Provider != "lp_claude0000000001" ||
		saved.LLM.Roles.ComputerUse.Provider != "maclaw_official_group" ||
		saved.LLM.Roles.Grounder.Mode != "inherit" {
		t.Fatalf("pinned roles = %#v", saved.LLM.Roles)
	}
	// An older client that omits roles must not wipe them.
	kept, err := svc.SaveLLMSettings(ctx, "tenant-a", "lp_claude0000000001", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kept.LLM.Roles.Background.Provider != "lp_claude0000000001" || kept.LLM.Roles.Grounder.Mode != "inherit" {
		t.Fatalf("omitted roles dropped the stored assignment: %#v", kept.LLM.Roles)
	}
	reset := LLMRoles{}
	cleared, err := svc.SaveLLMSettings(ctx, "tenant-a", "lp_claude0000000001", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}, nil, &reset)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.LLM.Roles.Background.Mode != "inherit" || cleared.LLM.Roles.Grounder.Mode != "unused" {
		t.Fatalf("empty roles should reset: %#v", cleared.LLM.Roles)
	}
}

func TestSaveLLMSettingsCoercesRemovedRolePins(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	pinned := LLMRoles{
		Background: LLMRoleAssignment{Mode: "pin", Provider: "lp_claude0000000001"},
		Grounder:   LLMRoleAssignment{Mode: "pin", Provider: "lp_claude0000000001"},
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}}, nil, &pinned); err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.LLM.Roles.Background.Mode != "inherit" || cleared.LLM.Roles.Grounder.Mode != "unused" {
		t.Fatalf("removed provider left dangling pins: %#v", cleared.LLM.Roles)
	}
}

func TestSaveLLMSettingsRejectsInvalidRoleMode(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	bad := LLMRoles{Foreground: LLMRoleAssignment{Mode: "always"}}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", nil, nil, &bad); err == nil || !strings.Contains(err.Error(), "llm role mode is invalid") {
		t.Fatalf("invalid mode: %v", err)
	}
}

func TestLLMConfigStampIncludesRolesAndOmitsKeys(t *testing.T) {
	base := llmSettings{Current: "maclaw_official_group"}
	if llmConfigStamp(base) != "" {
		t.Fatalf("default stamp should be empty: %q", llmConfigStamp(base))
	}
	withRoles := llmSettings{
		Current: "maclaw_official_group",
		Roles: LLMRoles{
			Background: LLMRoleAssignment{Mode: "pin", Provider: "maclaw_official_group"},
		},
	}
	first := llmConfigStamp(withRoles)
	if first == "" {
		t.Fatal("role-only change should get a stamp so MaClawSrv is rewritten")
	}
	withRoles.Roles.Grounder = LLMRoleAssignment{Mode: "inherit"}
	if llmConfigStamp(withRoles) == first {
		t.Fatal("grounder change must change the stamp")
	}
	secret := llmSettings{
		Current: "lp_claude0000000001",
		Providers: []llmProvider{{
			ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
			URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
		}},
	}
	stamp := llmConfigStamp(secret)
	if stamp == "" || strings.Contains(stamp, "sk-ant") || strings.Contains(llmRolesStamp(secret.Roles), "sk-ant") {
		t.Fatalf("stamp leaked a key: %q", stamp)
	}
}
