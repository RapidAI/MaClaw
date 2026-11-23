package agentservice

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

// TestRawManagedCapabilitySavesKeepAdminDocument exercises the raw storage
// saves. Their snapshots are not the one UpdateUserVisibleConfig commits.
// Each save keeps an admin replacement and an admin clear that land after
// the snapshot, persists its own field, and does not store a document when
// the row has none.
func TestRawManagedCapabilitySavesKeepAdminDocument(t *testing.T) {
	replaced := `{"skills":[{"name":"raw","body":"synthetic-raw-v2"}]}`
	for _, path := range []struct {
		name  string
		save  func(t *testing.T, svc *Service, p Principal)
		field func(t *testing.T, cfg corelib.AppConfig)
	}{
		{name: "mcp", save: saveStaleMCPServer, field: func(t *testing.T, cfg corelib.AppConfig) {
			t.Helper()
			if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "aux-mcp" {
				t.Fatalf("mcp servers = %+v", cfg.MCPServers)
			}
		}},
		{name: "mailbox", save: saveStaleMailbox, field: func(t *testing.T, cfg corelib.AppConfig) {
			t.Helper()
			if len(cfg.MailboxAccounts) != 1 || cfg.MailboxAccounts[0].Address != "aux@example.com" {
				t.Fatalf("mailbox accounts = %+v", cfg.MailboxAccounts)
			}
		}},
		{name: "skill-market-token", save: saveStaleSkillMarketToken, field: func(t *testing.T, cfg corelib.AppConfig) {
			t.Helper()
			if cfg.SkillMarketSessionToken != "synthetic-market-token" {
				t.Fatalf("skill market token = %q", cfg.SkillMarketSessionToken)
			}
		}},
		{name: "hub-center", save: saveStaleHubCenter, field: func(t *testing.T, cfg corelib.AppConfig) {
			t.Helper()
			if cfg.RemoteHubCenterURL != "https://hubs.example.test" {
				t.Fatalf("hub center = %q", cfg.RemoteHubCenterURL)
			}
		}},
	} {
		t.Run(path.name+"/replaced", func(t *testing.T) {
			svc, p := newRawManagedSaveService(t)
			seedRawManagedDoc(t, svc, p, `{"skills":[{"name":"raw","body":"synthetic-raw-seed"}]}`)
			armRawManagedAdmin(t, svc, p, replaced)
			path.save(t, svc, p)
			assertRawManagedDoc(t, svc, p, replaced)
			path.field(t, rawManagedConfig(t, svc, p).AppConfig)
		})
		t.Run(path.name+"/cleared", func(t *testing.T) {
			svc, p := newRawManagedSaveService(t)
			seedRawManagedDoc(t, svc, p, `{"skills":[{"name":"raw","body":"synthetic-raw-seed"}]}`)
			armRawManagedAdmin(t, svc, p, "")
			path.save(t, svc, p)
			assertRawManagedDoc(t, svc, p, "")
			path.field(t, rawManagedConfig(t, svc, p).AppConfig)
		})
	}

	t.Run("full-config-does-not-inject", func(t *testing.T) {
		svc, p := newRawManagedSaveService(t)
		seedRawManagedDoc(t, svc, p, "")
		forged := json.RawMessage(`{"skills":[{"name":"forged","body":"synthetic-raw-forged"}]}`)
		current := rawManagedConfig(t, svc, p)
		full := current.AppConfig
		full.DesktopBotCapabilities = forged
		full.MaclawLLMModel = "synthetic-raw-model"
		if err := svc.saveRawUserConfig(p, full); err != nil {
			t.Fatalf("save raw config: %v", err)
		}
		assertRawManagedDoc(t, svc, p, "")
		if got := rawManagedConfig(t, svc, p).AppConfig.MaclawLLMModel; got != "synthetic-raw-model" {
			t.Fatalf("model = %q", got)
		}

		market := rawManagedConfig(t, svc, p)
		market.AppConfig.DesktopBotCapabilities = forged
		market.AppConfig.RemoteHubID = "hub-raw"
		market.AppConfig.RemoteUserID = p.UserID
		market.AppConfig.RemoteMachineID = "machine-raw"
		market.AppConfig.RemoteViewerToken = "viewer-raw"
		login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session_token":"synthetic-market-token"}`))
		}))
		t.Cleanup(login.Close)
		if token := svc.skillMarketAuthToken(context.Background(), p, market, login.URL, "raw@example.com"); token != "synthetic-market-token" {
			t.Fatalf("token = %q", token)
		}
		assertRawManagedDoc(t, svc, p, "")
		if got := rawManagedConfig(t, svc, p).AppConfig.SkillMarketSessionToken; got != "synthetic-market-token" {
			t.Fatalf("stored token = %q", got)
		}
	})
}

func newRawManagedSaveService(t *testing.T) (*Service, Principal) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{TenantID: "tenant-raw", UserID: "user-raw"}
	if err := svc.EnsurePrincipal(context.Background(), p, "raw@example.com", "Raw"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetRawManagedSaveBeforeLockForTest(nil) })
	return svc, p
}

func seedRawManagedDoc(t *testing.T, svc *Service, p Principal, doc string) {
	t.Helper()
	var raw json.RawMessage
	if doc != "" {
		raw = json.RawMessage(doc)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{
		MaclawLLMUrl:           "https://llm.example/v1",
		MaclawLLMKey:           "synthetic-llm-key",
		MaclawLLMModel:         "synthetic-model",
		DesktopBotCapabilities: raw,
	}); err != nil {
		t.Fatal(err)
	}
}

func armRawManagedAdmin(t *testing.T, svc *Service, p Principal, doc string) {
	t.Helper()
	SetRawManagedSaveBeforeLockForTest(func() {
		SetRawManagedSaveBeforeLockForTest(nil)
		current, err := svc.GetRawUserConfig(context.Background(), p)
		if err != nil {
			t.Errorf("read config for admin write: %v", err)
			return
		}
		next := current.AppConfig
		if doc == "" {
			next.DesktopBotCapabilities = nil
		} else {
			next.DesktopBotCapabilities = json.RawMessage(doc)
		}
		if _, err := svc.UpdateUserConfig(context.Background(), p, next); err != nil {
			t.Errorf("admin write: %v", err)
		}
	})
}

func saveStaleMCPServer(t *testing.T, svc *Service, p Principal) {
	t.Helper()
	view, err := svc.CreateMCPServer(context.Background(), p, MCPServerCreateInput{
		Kind:        "remote",
		Name:        "aux-mcp",
		EndpointURL: "https://mcp.example.test/rpc",
		AuthType:    "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.Name != "aux-mcp" {
		t.Fatalf("created mcp = %+v", view)
	}
}

func saveStaleMailbox(t *testing.T, svc *Service, p Principal) {
	t.Helper()
	if _, err := svc.ReplaceMailboxAccounts(context.Background(), p, []mailbox.Account{{
		ID: "box-aux", Name: "Aux", Address: "aux@example.com", Enabled: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
}

func saveStaleSkillMarketToken(t *testing.T, svc *Service, p Principal) {
	t.Helper()
	login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_token":"synthetic-market-token"}`))
	}))
	t.Cleanup(login.Close)
	current := rawManagedConfig(t, svc, p)
	current.AppConfig.RemoteHubID = "hub-raw"
	current.AppConfig.RemoteUserID = p.UserID
	current.AppConfig.RemoteMachineID = "machine-raw"
	current.AppConfig.RemoteViewerToken = "viewer-raw"
	current.AppConfig.SkillMarketSessionToken = ""
	if token := svc.skillMarketAuthToken(context.Background(), p, current, login.URL, "raw@example.com"); token != "synthetic-market-token" {
		t.Fatalf("token = %q", token)
	}
}

func saveStaleHubCenter(t *testing.T, svc *Service, p Principal) {
	t.Helper()
	svc.rememberUserHubCenterSelection(p, "https://hubs.example.test", []string{"https://hubs.example.test"})
}

func rawManagedConfig(t *testing.T, svc *Service, p Principal) UserConfig {
	t.Helper()
	cfg, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return *cfg
}

func assertRawManagedDoc(t *testing.T, svc *Service, p Principal, want string) {
	t.Helper()
	got := bytes.TrimSpace(rawManagedConfig(t, svc, p).AppConfig.DesktopBotCapabilities)
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("managed document = %s", got)
		}
	} else if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("managed document = %s, want %s", got, want)
	}
	fileRaw, err := loadUserConfigFromFile(svc.userConfigPath(p.TenantID, p.UserID))
	if err != nil {
		t.Fatal(err)
	}
	fileDoc := bytes.TrimSpace(fileRaw.AppConfig.DesktopBotCapabilities)
	if !bytes.Equal(compactJSON(t, fileDoc), compactJSON(t, got)) {
		t.Fatalf("config file document = %s, store = %s", fileDoc, got)
	}
}

func compactJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact json: %v", err)
	}
	return buf.Bytes()
}
