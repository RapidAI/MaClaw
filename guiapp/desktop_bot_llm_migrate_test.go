package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func migratePinFalse() *bool {
	f := false
	return &f
}

func TestPlanLocalBotRoleMigrationKeepsPinsOverExpandedOverride(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max", Protocol: "openai"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-turbo"},
			Grounder:   corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-vl"},
		},
	}
	server := DesktopBotLLM{
		Mode: "custom", Name: "Old", URL: "https://old.example/v1", Model: "old-m",
		Roles: &DesktopBotLLMRoles{
			Foreground:  DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m", KeySet: true},
			Background:  DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m", KeySet: true},
			ComputerUse: DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m", KeySet: true},
			Grounder:    DesktopBotLLMRole{Unused: true},
		},
	}
	input, apply := planLocalBotRoleMigration(cfg, server)
	if !apply || input.Roles == nil {
		t.Fatal("expected a custom upload")
	}
	if input.Roles.Foreground.Name != "Qwen" || input.Roles.Foreground.Model != "qwen-turbo" || input.Roles.Foreground.Key != "sk-qwen" {
		t.Fatalf("foreground = %#v", input.Roles.Foreground)
	}
	if input.Roles.Background.Name != "Old" || input.Roles.Background.Model != "old-m" || input.Roles.Background.Key != "" {
		t.Fatalf("background should keep the stored provider without resending the key: %#v", input.Roles.Background)
	}
	if input.Roles.ComputerUse.Model != "old-m" {
		t.Fatalf("computer use = %#v", input.Roles.ComputerUse)
	}
	if input.Roles.Grounder.Unused || input.Roles.Grounder.Model != "qwen-vl" || input.Roles.Grounder.Name != "Qwen" {
		t.Fatalf("pinned grounder = %#v", input.Roles.Grounder)
	}
}

func TestPlanLocalBotRoleMigrationUsesOfficialForUnpinnedRoles(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "local", Name: "Local", URL: "https://local.example/v1", Key: "sk-local", Model: "local-chat", Protocol: "openai"},
		},
		HubRoleModels: corelib.MaclawLLMHubRoleModels{Background: "worker-model", ComputerUse: "vision-model"},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			ComputerUse: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "local", Model: "local-vision"},
		},
	}
	input, apply := planLocalBotRoleMigration(cfg, DesktopBotLLM{Mode: "default"})
	if !apply {
		t.Fatal("expected a custom upload")
	}
	if !input.Roles.Foreground.Official || input.Roles.Foreground.Model != "auto" {
		t.Fatalf("foreground = %#v", input.Roles.Foreground)
	}
	if !input.Roles.Background.Official || input.Roles.Background.Model != "worker-model" {
		t.Fatalf("background = %#v", input.Roles.Background)
	}
	if input.Roles.ComputerUse.Name != "Local" || input.Roles.ComputerUse.Model != "local-vision" {
		t.Fatalf("computer use = %#v", input.Roles.ComputerUse)
	}
	if !input.Roles.Grounder.Unused {
		t.Fatalf("grounder = %#v", input.Roles.Grounder)
	}
}

func TestPlanLocalBotRoleMigrationSkipsWhenNothingIsPinned(t *testing.T) {
	cfg := corelib.AppConfig{MaclawLLMProfiles: &corelib.MaclawLLMProfiles{}}
	if _, apply := planLocalBotRoleMigration(cfg, DesktopBotLLM{Mode: "custom", Name: "Old", Model: "old-m"}); apply {
		t.Fatal("follow-hub rows are not a custom choice")
	}
}

func TestPlanLocalBotRoleMigrationSkipsUnresolvedPin(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "gone", Model: "m"},
		},
	}
	if _, apply := planLocalBotRoleMigration(cfg, DesktopBotLLM{Mode: "default"}); apply {
		t.Fatal("a pin with no provider must not replace the hub choice")
	}
}

func TestGetDesktopBotLLMUploadsLocalRolePinsOnce(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "qwen", Name: "Qwen", URL: "https://dashscope.example/v1", Key: "sk-qwen", Model: "qwen-max", Protocol: "openai"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: migratePinFalse(), ProviderID: "qwen", Model: "qwen-turbo"},
		},
	})
	puts := 0
	prev := desktopBotExchangeOverride
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if path != "/api/v1/bots/llm" {
			return http.StatusNotFound, fmt.Errorf("unexpected path %s", path)
		}
		switch method {
		case http.MethodGet:
			return http.StatusOK, jsonRoundTrip(dest, DesktopBotLLM{
				Mode: "custom", Name: "Old", URL: "https://old.example/v1", Model: "old-m", KeySet: true,
				Roles: &DesktopBotLLMRoles{
					Foreground:  DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m"},
					Background:  DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m"},
					ComputerUse: DesktopBotLLMRole{Name: "Old", URL: "https://old.example/v1", Model: "old-m"},
					Grounder:    DesktopBotLLMRole{Unused: true},
				},
			})
		case http.MethodPut:
			puts++
			raw, err := json.Marshal(body)
			if err != nil {
				return http.StatusBadRequest, err
			}
			var in DesktopBotLLMInput
			if err := json.Unmarshal(raw, &in); err != nil {
				return http.StatusBadRequest, err
			}
			if in.Mode != "custom" || in.Roles == nil || in.Roles.Foreground.Model != "qwen-turbo" || in.Roles.Background.Name != "Old" || !in.Roles.Grounder.Unused {
				t.Errorf("upload = %#v", in)
			}
			return http.StatusOK, jsonRoundTrip(dest, DesktopBotLLM{
				Mode: "custom", Name: "Qwen", Model: "qwen-turbo", KeySet: true,
				Roles: &DesktopBotLLMRoles{
					Foreground:  DesktopBotLLMRole{Name: "Qwen", Model: "qwen-turbo"},
					Background:  DesktopBotLLMRole{Name: "Old", Model: "old-m"},
					ComputerUse: DesktopBotLLMRole{Name: "Old", Model: "old-m"},
					Grounder:    DesktopBotLLMRole{Unused: true},
				},
			})
		default:
			return http.StatusMethodNotAllowed, fmt.Errorf("unexpected method %s", method)
		}
	}
	t.Cleanup(func() { desktopBotExchangeOverride = prev })

	got, err := app.GetDesktopBotLLM()
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles == nil || got.Roles.Foreground.Model != "qwen-turbo" || puts != 1 {
		t.Fatalf("first read = %#v puts=%d", got, puts)
	}
	if _, err := app.GetDesktopBotLLM(); err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("second read uploaded again (%d)", puts)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DesktopBotLLMRolesMigrated {
		t.Fatal("migration marker was not stored")
	}
}

func jsonRoundTrip(dest any, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}
