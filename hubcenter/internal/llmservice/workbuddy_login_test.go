package llmservice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

func TestWorkBuddyLoginListsModelsAndConnectsProvider(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/auth/state"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"state":"st","authUrl":"https://example.test/login"}}`)
		case strings.Contains(r.URL.Path, "/auth/token") && !strings.Contains(r.URL.Path, "refresh"):
			polls++
			if polls < 2 {
				_, _ = io.WriteString(w, `{"code":11217,"msg":"login ing"}`)
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"accessToken":"access-token","refreshToken":"refresh-token","expiresIn":3600,"domain":"dept"}}`)
		case strings.Contains(r.URL.Path, "/login/account"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"uid":"user-1","enterpriseId":"ent-1","nickname":"Ada"}}`)
		case strings.Contains(r.URL.Path, "/v3/config"):
			if r.Header.Get("Authorization") != "Bearer access-token" {
				http.Error(w, "missing auth", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"brand-new","name":"Brand New","maxInputTokens":1000,"maxOutputTokens":200}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	profile := workbuddy.ChinaProfile()
	profile.APIRoot = srv.URL
	started, err := svc.startWorkBuddyLogin(profile, workbuddy.EditionChina)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	if started.AuthURL != "https://example.test/login" || started.Edition != workbuddy.EditionChina {
		t.Fatalf("start = %#v", started)
	}

	deadline := time.Now().Add(5 * time.Second)
	var status WorkBuddyLoginStatus
	for {
		status, err = svc.WorkBuddyLoginStatus(started.SessionID)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if status.Status == workBuddyStatusReady || status.Status == workBuddyStatusError || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.Status != workBuddyStatusReady {
		t.Fatalf("status = %#v", status)
	}
	foundNew := false
	foundBuiltin := false
	for _, model := range status.Models {
		if model.ID == "brand-new" && model.Name == "Brand New" {
			foundNew = true
		}
		if model.ID == "glm-5.3" {
			foundBuiltin = true
		}
	}
	if !foundNew || !foundBuiltin {
		t.Fatalf("models = %#v", status.Models)
	}

	provider := llmpool.ProviderConfig{
		ID:                 "workbuddy-cn",
		Name:               "",
		AuthKind:           llmpool.ProviderAuthWorkBuddy,
		WorkBuddyEdition:   workbuddy.EditionChina,
		WorkBuddySessionID: started.SessionID,
		Models:             []string{"glm-5.3", "missing-model"},
	}
	if err := svc.ApplyWorkBuddyLogin(&provider, true); err == nil {
		t.Fatal("unknown model was accepted")
	}
	provider.Models = []string{"glm-5.3", "brand-new"}
	if err := svc.ApplyWorkBuddyLogin(&provider, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if provider.Name != workbuddy.NameChina || provider.APIURL != workbuddy.ChinaProfile().ChatURL || provider.APIKey != "access-token" {
		t.Fatalf("provider = %#v", provider)
	}
	if provider.WorkBuddyRefreshToken != "refresh-token" || provider.WorkBuddyUserID != "user-1" || provider.WorkBuddyEnterpriseID != "ent-1" {
		t.Fatalf("credential = %#v", provider)
	}
	if provider.WorkBuddySessionID != "" || provider.Protocol != "openai" {
		t.Fatalf("session leaked or protocol = %#v", provider)
	}
	if err := svc.AddProvider(context.Background(), provider); err != nil {
		t.Fatalf("add: %v", err)
	}
	svc.ForgetWorkBuddyLogin(started.SessionID)
	stored, err := svc.GetProvider(context.Background(), "workbuddy-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.WorkBuddyRefreshToken != "refresh-token" || stored.APIKey != "access-token" || stored.WorkBuddySessionID != "" {
		t.Fatalf("stored = %#v", stored)
	}
	if len(stored.Models) != 2 || stored.Models[0] != "glm-5.3" || stored.Models[1] != "brand-new" {
		t.Fatalf("models = %#v", stored.Models)
	}
}

func TestStartWorkBuddyLoginRejectsUnknownEdition(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if _, err := svc.StartWorkBuddyLogin("nope"); err == nil {
		t.Fatal("expected unknown edition error")
	}
}
