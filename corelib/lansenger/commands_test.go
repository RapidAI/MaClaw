package lansenger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNormalizeBotCommands(t *testing.T) {
	in := []BotCommand{
		{Command: "/summary", Description: "摘要"},
		{Command: "summary", Description: "dup"}, // dedupe
		{Command: "bad-name", Description: "x"},  // invalid
		{Command: "", Description: "empty"},
		{Command: "help", Description: ""}, // default desc
	}
	out := NormalizeBotCommands(in)
	if len(out) != 2 {
		t.Fatalf("got %d commands: %#v", len(out), out)
	}
	if out[0].Command != "summary" || out[0].Description != "摘要" {
		t.Fatalf("first = %#v", out[0])
	}
	if out[1].Command != "help" || out[1].Description != "help" {
		t.Fatalf("second = %#v", out[1])
	}
}

func TestSupportedBotCommandsIncludesSummary(t *testing.T) {
	cmds := NormalizeBotCommands(SupportedBotCommands())
	if len(cmds) == 0 {
		t.Fatal("expected at least summary")
	}
	found := false
	for _, c := range cmds {
		if c.Command == "summary" {
			found = true
			if c.Description == "" {
				t.Fatal("summary needs description")
			}
			if c.I18nDescription == nil || c.I18nDescription.ZhHans == "" {
				t.Fatal("summary needs zhHans i18n")
			}
		}
	}
	if !found {
		t.Fatalf("summary not in %#v", cmds)
	}
}

func TestSyncSupportedBotCommandsGroupScopeOnly(t *testing.T) {
	var (
		deletes atomic.Int32
		creates atomic.Int32
	)
	type createBody struct {
		ScopeType int          `json:"scopeType"`
		Commands  []BotCommand `json:"commands"`
	}
	type deleteBody struct {
		ScopeType int `json:"scopeType"`
	}
	var mu sync.Mutex
	scopesDeleted := map[int]int{}
	scopesCreated := map[int]int{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok-cmd", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app_token") != "tok-cmd" {
			t.Errorf("missing/wrong app_token on delete: %q", r.URL.RawQuery)
		}
		raw, _ := io.ReadAll(r.Body)
		var body deleteBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("delete body: %v", err)
		}
		deletes.Add(1)
		mu.Lock()
		scopesDeleted[body.ScopeType]++
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app_token") != "tok-cmd" {
			t.Errorf("missing/wrong app_token on create: %q", r.URL.RawQuery)
		}
		raw, _ := io.ReadAll(r.Body)
		var body createBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("create body: %v", err)
		}
		creates.Add(1)
		mu.Lock()
		scopesCreated[body.ScopeType]++
		mu.Unlock()
		if len(body.Commands) != 1 || body.Commands[0].Command != "summary" {
			t.Errorf("commands = %#v", body.Commands)
		}
		if strings.HasPrefix(body.Commands[0].Command, "/") {
			t.Error("command must not include leading slash")
		}
		if body.Commands[0].I18nDescription == nil || body.Commands[0].I18nDescription.ZhHans == "" {
			t.Errorf("expected i18n: %#v", body.Commands[0])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	if err := gw.SyncSupportedBotCommands(context.Background()); err != nil {
		t.Fatalf("SyncSupportedBotCommands: %v", err)
	}
	// Product command is group-only (/summary).
	if deletes.Load() != 1 || creates.Load() != 1 {
		t.Fatalf("delete=%d create=%d want 1 each (group only)", deletes.Load(), creates.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if scopesDeleted[CommandScopeGroup] != 1 || scopesCreated[CommandScopeGroup] != 1 {
		t.Fatalf("deleted=%#v created=%#v", scopesDeleted, scopesCreated)
	}
	if scopesDeleted[CommandScopePrivate] != 0 || scopesCreated[CommandScopePrivate] != 0 {
		t.Fatalf("private scope must not be touched: deleted=%#v created=%#v", scopesDeleted, scopesCreated)
	}
}

func TestSyncBotCommandsBothScopesWhenRequested(t *testing.T) {
	var creates atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/delete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	if err := gw.SyncBotCommands(context.Background(), SupportedBotCommands()); err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 2 {
		t.Fatalf("default scopes create=%d want 2", creates.Load())
	}
}

func TestCreateCommandsAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 10001,
			"errMsg":  "permission denied",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	err := gw.CreateCommands(context.Background(), CommandScopeGroup, SupportedBotCommands())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10001 {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestCreateCommandsRejectsEmpty(t *testing.T) {
	gw := NewGateway(Config{AppID: "a", AppSecret: "b", ApiGatewayURL: "http://127.0.0.1:1"}, nil)
	if err := gw.CreateCommands(context.Background(), CommandScopeGroup, nil); err == nil {
		t.Fatal("expected error for empty commands")
	}
	if err := gw.CreateCommands(context.Background(), CommandScopeGroup, []BotCommand{{Command: "bad-name"}}); err == nil {
		t.Fatal("expected error for only-invalid commands")
	}
}

func TestSyncBotCommandsSucceedsWhenDeleteFailsButCreateOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/delete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 10002, "errMsg": "nothing to delete"})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	// Only group scope — delete fails, create must still count as success.
	if err := gw.SyncBotCommands(context.Background(), SupportedBotCommands(), CommandScopeGroup); err != nil {
		t.Fatalf("expected success when create OK: %v", err)
	}
}

func TestSyncBotCommandsScopeOverride(t *testing.T) {
	var creates atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/delete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	if err := gw.SyncBotCommands(context.Background(), SupportedBotCommands(), CommandScopeGroup); err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 {
		t.Fatalf("creates=%d want 1 (group only)", creates.Load())
	}
}

func TestSyncSupportedCommandsBackgroundUsesGroupOnly(t *testing.T) {
	var (
		creates atomic.Int32
		mu      sync.Mutex
		scopes  []int
	)
	type createBody struct {
		ScopeType int `json:"scopeType"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v1/bot/commands/delete", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	mux.HandleFunc("/v1/bot/commands/create", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body createBody
		_ = json.Unmarshal(raw, &body)
		creates.Add(1)
		mu.Lock()
		scopes = append(scopes, body.ScopeType)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	// Directly exercise the Start-path helper (must not default to private+group).
	gw.syncSupportedCommandsBackground(context.Background())
	if creates.Load() != 1 {
		t.Fatalf("creates=%d want 1", creates.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(scopes) != 1 || scopes[0] != CommandScopeGroup {
		t.Fatalf("scopes=%v want only group=%d", scopes, CommandScopeGroup)
	}
}
