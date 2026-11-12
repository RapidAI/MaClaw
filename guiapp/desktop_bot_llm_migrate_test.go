package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func migratePinFalse() *bool {
	f := false
	return &f
}

func TestPlanLocalBotRoleMigrationDoesNotOverwriteStoredHubChoice(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-turbo"},
		},
	}
	for _, server := range []DesktopBotLLM{
		{Mode: "default", Choice: "default", Stored: true},
		{Mode: "custom", Choice: "custom", Stored: true, Roles: &DesktopBotLLMRoles{
			Foreground: DesktopBotLLMRole{Name: "Newer", Model: "new-model"},
		}},
	} {
		plan := planLocalBotRoleMigration(cfg, server, nil)
		if plan.Upload {
			t.Fatalf("stored hub choice was overwritten: %#v", plan.Input)
		}
		if len(plan.Clear) != 1 || plan.Clear[0] != maclawLLMRoleForeground || !plan.Mark {
			t.Fatalf("local pins should be cleared without upload: %#v", plan)
		}
	}
}

func TestPlanLocalBotRoleMigrationLeavesDefaultWhenNothingIsPinned(t *testing.T) {
	plan := planLocalBotRoleMigration(corelib.AppConfig{}, DesktopBotLLM{Mode: "default", Stored: false}, nil)
	if plan.Upload || !plan.Mark || len(plan.Clear) != 0 {
		t.Fatalf("empty account = %#v", plan)
	}
}

func TestPlanLocalBotRoleMigrationDoesNotUploadKeys(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max"},
			{ID: "oauth", Name: "OAuth", URL: "https://oauth.example/v1", Key: "", Model: "oauth-model"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground:  corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "hub", Model: "official-mid"},
			Background:  corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-turbo"},
			ComputerUse: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "oauth", Model: "oauth-model"},
		},
	}
	plan := planLocalBotRoleMigration(cfg, DesktopBotLLM{Mode: "default", Stored: false}, nil)
	if !plan.Upload || plan.Input.Roles == nil {
		t.Fatal("official pin should upload")
	}
	if plan.Input.Key != "" || plan.Input.Roles.Foreground.Key != "" || !plan.Input.Roles.Foreground.Official {
		t.Fatalf("upload carried a key or missed the official pin: %#v", plan.Input)
	}
	if !plan.Input.Roles.Background.Inherit {
		t.Fatalf("keyed role must stay unset on the hub: %#v", plan.Input.Roles.Background)
	}
	if len(plan.Pending) != 2 || plan.Mark {
		t.Fatalf("pending = %#v mark=%v", plan.Pending, plan.Mark)
	}
	for _, reason := range plan.Reasons {
		if stringsContainsSecret(reason) {
			t.Fatalf("reason leaked a secret: %s", reason)
		}
	}
}

func stringsContainsSecret(text string) bool {
	return strings.Contains(text, "sk-qwen") || strings.Contains(text, "viewer")
}

func TestSyncDesktopBotLLMUploadsOnceAndSkipsOldHub(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "alice",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "hub", Model: "official-mid"},
		},
	})
	var mu sync.Mutex
	puts := 0
	respondRoles := true
	prev := desktopBotExchangeOverride
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if path != "/api/v1/bots/llm" {
			return http.StatusNotFound, fmt.Errorf("unexpected path %s", path)
		}
		if method == http.MethodGet {
			return http.StatusOK, jsonRoundTrip(dest, DesktopBotLLM{Mode: "default", Choice: "default", Stored: false})
		}
		mu.Lock()
		puts++
		mu.Unlock()
		raw, _ := json.Marshal(body)
		var in DesktopBotLLMInput
		_ = json.Unmarshal(raw, &in)
		if in.Key != "" || (in.Roles != nil && in.Roles.Foreground.Key != "") {
			t.Errorf("automatic upload included a key: %s", raw)
		}
		out := DesktopBotLLM{Mode: "custom", Choice: "custom", Stored: true}
		if respondRoles {
			out.Roles = &DesktopBotLLMRoles{Foreground: DesktopBotLLMRole{Official: true, Model: "official-mid"}}
		}
		return http.StatusOK, jsonRoundTrip(dest, out)
	}
	t.Cleanup(func() { desktopBotExchangeOverride = prev })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = app.SyncDesktopBotLLM() }()
	go func() { defer wg.Done(); _, _ = app.SyncDesktopBotLLM() }()
	wg.Wait()
	if puts != 1 {
		t.Fatalf("concurrent sync uploaded %d times", puts)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if roleAssignmentPinned(cfg.MaclawLLMProfiles.Foreground) {
		t.Fatal("uploaded pin was left locally")
	}
	if !cfg.DesktopBotLLMAccounts["alice"].Migrated {
		t.Fatal("marker was not stored for the hub account")
	}

	respondRoles = false
	app2 := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "bob",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "hub", Model: "official-mid"},
		},
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
		},
	})
	if _, err := app2.SyncDesktopBotLLM(); err != nil {
		t.Fatal(err)
	}
	cfg, err = app2.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !roleAssignmentPinned(cfg.MaclawLLMProfiles.Foreground) {
		t.Fatal("old hub cleared a pin it did not accept")
	}
	if cfg.DesktopBotLLMAccounts["bob"].Migrated {
		t.Fatal("old hub was marked migrated")
	}
}

func TestResolveMaclawLLMRoleFollowsCachedHubChoice(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "alice",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max", Protocol: "openai"},
		},
		HubRoleModels: corelib.MaclawLLMHubRoleModels{Foreground: "auto", Background: "worker-model"},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "stale-local"},
		},
	})
	app.rememberDesktopBotLLM("alice", DesktopBotLLM{
		Mode: "custom", Choice: "custom", Stored: true,
		Roles: &DesktopBotLLMRoles{
			Foreground: DesktopBotLLMRole{Name: "Qwen", Model: "qwen-turbo"},
			Background: DesktopBotLLMRole{Inherit: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil || fg.Model != "qwen-turbo" || fg.ProviderName != "Qwen" {
		t.Fatalf("foreground = %+v err=%v", fg, err)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil || bg.Model == "stale-local" {
		t.Fatalf("inherit role used a local pin: %+v err=%v", bg, err)
	}
	app.rememberDesktopBotLLM("alice", DesktopBotLLM{Mode: "default", Choice: "default", Stored: true})
	fg, err = app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil || fg.Model == "stale-local" {
		t.Fatalf("default choice still used the local pin: %+v err=%v", fg, err)
	}
}

func TestSyncDesktopBotLLMKeepsPendingPinsAcrossASecondSync(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "alice",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "hub", Model: "official-mid"},
			Background: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-turbo"},
		},
	})
	stored := false
	puts := 0
	prev := desktopBotExchangeOverride
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if path != "/api/v1/bots/llm" {
			return http.StatusNotFound, fmt.Errorf("unexpected path %s", path)
		}
		if method == http.MethodGet {
			view := DesktopBotLLM{Mode: "default", Choice: "default", Stored: stored}
			if stored {
				view.Mode = "custom"
				view.Choice = "custom"
				view.Roles = &DesktopBotLLMRoles{Foreground: DesktopBotLLMRole{Official: true, Model: "official-mid"}}
			}
			return http.StatusOK, jsonRoundTrip(dest, view)
		}
		puts++
		raw, _ := json.Marshal(body)
		var in DesktopBotLLMInput
		_ = json.Unmarshal(raw, &in)
		if in.Key != "" || (in.Roles != nil && (in.Roles.Foreground.Key != "" || in.Roles.Background.Key != "")) {
			t.Errorf("upload included a key: %s", raw)
		}
		stored = true
		return http.StatusOK, jsonRoundTrip(dest, DesktopBotLLM{
			Mode: "custom", Choice: "custom", Stored: true,
			Roles: &DesktopBotLLMRoles{
				Foreground: DesktopBotLLMRole{Official: true, Model: "official-mid"},
				Background: DesktopBotLLMRole{Inherit: true},
			},
		})
	}
	t.Cleanup(func() { desktopBotExchangeOverride = prev })

	first, err := app.SyncDesktopBotLLM()
	if err != nil {
		t.Fatal(err)
	}
	if !first.NeedsRoleSave || puts != 1 {
		t.Fatalf("first sync needs save=%v puts=%d", first.NeedsRoleSave, puts)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if roleAssignmentPinned(cfg.MaclawLLMProfiles.Foreground) {
		t.Fatal("official pin should have been cleared")
	}
	if !roleAssignmentPinned(cfg.MaclawLLMProfiles.Background) {
		t.Fatal("key-needing pin was cleared on the first sync")
	}
	if cfg.DesktopBotLLMAccounts["alice"].Migrated {
		t.Fatal("marker set while a pin is still pending")
	}

	second, err := app.SyncDesktopBotLLM()
	if err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("second sync uploaded again (%d)", puts)
	}
	if !second.NeedsRoleSave {
		t.Fatal("save-once hint disappeared while the key-needing pin remains")
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !roleAssignmentPinned(cfg.MaclawLLMProfiles.Background) {
		t.Fatal("second sync cleared the pending pin")
	}
	if cfg.DesktopBotLLMAccounts["alice"].Migrated {
		t.Fatal("second sync marked the account done")
	}
}

func TestSlowHubSyncDoesNotBlockRoleResolution(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "alice",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
		},
		HubRoleModels: corelib.MaclawLLMHubRoleModels{Foreground: "auto"},
	})
	release := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	prev := desktopBotExchangeOverride
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && path == "/api/v1/bots/llm" {
			startOnce.Do(func() { close(started) })
			<-release
			return http.StatusOK, jsonRoundTrip(dest, DesktopBotLLM{Mode: "default", Choice: "default", Stored: false})
		}
		return http.StatusNotFound, fmt.Errorf("unexpected %s %s", method, path)
	}
	t.Cleanup(func() {
		desktopBotExchangeOverride = prev
		select {
		case <-release:
		default:
			close(release)
		}
	})
	done := make(chan struct{})
	go func() {
		_, _ = app.SyncDesktopBotLLM()
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("sync did not reach the hub read")
	}
	startedAt := time.Now()
	if _, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(startedAt); waited > time.Second {
		t.Fatalf("role resolution waited %s behind the hub sync", waited)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sync did not finish after the hub read was released")
	}
}

func TestDefaultChoiceUsesEffectiveOfficialModelAndFallsBack(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID: "alice",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: "MaClaw官方", IsHubService: true, URL: "https://hub.example/v1", Key: "viewer", Model: "auto"},
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max"},
		},
		HubRoleModels: corelib.MaclawLLMHubRoleModels{Foreground: "snapshot-model", Background: "worker-model"},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "stale-local"},
		},
	})
	app.rememberDesktopBotLLM("alice", DesktopBotLLM{
		Mode: "default", Choice: "default", Stored: true,
		Effective: &DesktopBotLLMRoles{
			Foreground: DesktopBotLLMRole{Official: true, Model: "official-mid"},
			Background: DesktopBotLLMRole{Name: "Admin Pick", Model: "claude-sonnet"},
			Grounder:   DesktopBotLLMRole{Official: true, Model: "ground-model"},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil || fg.Model != "official-mid" || !strings.Contains(fg.URL, "hub.example") {
		t.Fatalf("foreground = %+v err=%v", fg, err)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil || bg.Model == "claude-sonnet" || bg.Model == "stale-local" {
		t.Fatalf("unavailable admin model did not fall back: %+v err=%v", bg, err)
	}
	gr, err := app.ResolveMaclawLLMGrounder()
	if err != nil || gr.Model != "ground-model" || !gr.SupportsVision {
		t.Fatalf("grounder = %+v err=%v", gr, err)
	}
}

func TestRememberDesktopBotLLMNilApp(t *testing.T) {
	var app *App
	app.rememberDesktopBotLLM("alice", DesktopBotLLM{Mode: "default"})
}

func jsonRoundTrip(dest any, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}
