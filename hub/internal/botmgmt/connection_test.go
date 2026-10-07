package botmgmt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// expiredToken builds a token in the MaClawSrv shape whose claims name
// tenant t1 and user u1 but whose expiry sits in the past. The hub cannot
// and does not check the signature, so any suffix works.
func expiredToken() string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"tenant_id":"t1","user_id":"u1","exp":"2020-01-01T00:00:00Z"}`))
	return payload + ".not-checked"
}

func unauthenticated(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

// renewalStub answers the three calls a token renewal needs: instance
// listing only for the fresh bearer, the credential-for-token exchange, and
// the admin credential creation guarded by the admin secret. created counts
// how many credentials were minted.
func renewalStub(created *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/instances":
			if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == expiredToken() {
				unauthenticated(w)
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"inst_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["api_key"] == "ak_conn" && in["api_secret"] == "sk_conn" {
				_, _ = w.Write([]byte(`{"access_token":"fresh-token","token_type":"Bearer","principal":{"user_id":"u1"}}`))
				return
			}
			unauthenticated(w)
		case r.Method == http.MethodPost &&
			strings.HasPrefix(r.URL.Path, "/api/v1/admin/tenants/") && strings.HasSuffix(r.URL.Path, "/credentials"):
			if r.Header.Get("X-MaClaw-Admin-Secret") != "admin-key" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"admin owner is required"}`))
				return
			}
			*created++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"cred1","api_key":"ak_conn","api_secret":"sk_conn"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
}

func newStubService(srv *httptest.Server) (*Service, *memSettings) {
	settings := &memSettings{}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	return svc, settings
}

func seedRecord(t *testing.T, settings *memSettings, tenantID string, rec record) {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey(tenantID), string(raw)); err != nil {
		t.Fatal(err)
	}
}

func storedRecord(t *testing.T, settings *memSettings, tenantID string) record {
	t.Helper()
	value, err := settings.Get(context.Background(), storageKey(tenantID))
	if err != nil {
		t.Fatal(err)
	}
	var rec record
	if err := json.Unmarshal([]byte(value), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTestConnectionRenewsExpiredTokenWithAdminSecret(t *testing.T) {
	created := 0
	srv := renewalStub(&created)
	defer srv.Close()
	svc, settings := newStubService(srv)
	ctx := context.Background()
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     srv.URL,
		AccessToken: expiredToken(),
		AdminSecret: "admin-key",
	})

	count, err := svc.TestConnection(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d want 1", count)
	}
	if created != 1 {
		t.Fatalf("credentials created = %d want 1", created)
	}

	// The credential persisted, so a second probe mints straight from it.
	if count, err = svc.TestConnection(ctx, "tenant-a"); err != nil || count != 1 {
		t.Fatalf("second TestConnection = (%d, %v)", count, err)
	}
	if created != 1 {
		t.Fatalf("second probe minted another credential: %d", created)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil ||
		stored.Connection.APIKey != "ak_conn" ||
		stored.Connection.APISecret != "sk_conn" ||
		stored.Connection.UserID != "u1" ||
		stored.Connection.TenantID != "t1" {
		t.Fatalf("stored connection credential = %+v", stored.Connection)
	}
}

func TestTestConnectionKeeps401WithoutAdminSecret(t *testing.T) {
	created := 0
	srv := renewalStub(&created)
	defer srv.Close()
	svc, _ := newStubService(srv)
	if _, err := svc.SaveConnection(context.Background(), "tenant-a", srv.URL, expiredToken(), true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	_, err := svc.TestConnection(context.Background(), "tenant-a")
	var statusErr *upstream.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusUnauthorized {
		t.Fatalf("TestConnection error = %v want the upstream 401", err)
	}
	if created != 0 {
		t.Fatalf("credentials created = %d want 0", created)
	}
}

func TestSaveAdminSecretBootstrapsConnectionCredential(t *testing.T) {
	created := 0
	srv := renewalStub(&created)
	defer srv.Close()
	svc, settings := newStubService(srv)
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, expiredToken(), true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-key"); err != nil {
		t.Fatalf("SaveAdminSecret: %v", err)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.AdminSecret != "admin-key" {
		t.Fatalf("admin secret not saved: %q", stored.AdminSecret)
	}
	if stored.Connection == nil || stored.Connection.APIKey != "ak_conn" {
		t.Fatalf("connection credential not provisioned: %+v", stored.Connection)
	}
}

func TestSaveConnectionRetiresCredentialOnTokenChange(t *testing.T) {
	created := 0
	srv := renewalStub(&created)
	defer srv.Close()
	svc, settings := newStubService(srv)
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, expiredToken(), true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-key"); err != nil {
		t.Fatalf("SaveAdminSecret: %v", err)
	}
	if stored := storedRecord(t, settings, "tenant-a"); stored.Connection == nil {
		t.Fatal("credential never provisioned")
	}

	// A different token changes the connection user, so the old credential
	// must not survive the save.
	changed := base64.RawURLEncoding.EncodeToString([]byte(
		`{"tenant_id":"t2","user_id":"u9","exp":"2020-01-01T00:00:00Z"}`)) + ".not-checked"
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, changed, true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil || stored.Connection.UserID != "u9" {
		t.Fatalf("connection credential = %+v, want one for u9", stored.Connection)
	}
	if created != 2 {
		t.Fatalf("credentials created = %d want 2", created)
	}
}

func TestCreateBotMintsFromConnectionCredential(t *testing.T) {
	created := 0
	srv := renewalStub(&created)
	defer srv.Close()
	instanceCreated := false
	instances := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances" {
			if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == "fresh-token" {
				instanceCreated = true
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"inst_new"}`))
				return
			}
			unauthenticated(w)
			return
		}
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer instances.Close()

	svc, settings := newStubService(instances)
	svc.HTTP = instances.Client()
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     instances.URL,
		AccessToken: expiredToken(),
		AdminSecret: "admin-key",
		Connection: &connectionCredential{
			TenantID: "t1", UserID: "u1", APIKey: "ak_conn", APISecret: "sk_conn",
		},
	})

	// An ownerless bot rides the shared connection, whose access token is
	// expired; the minted credential has to carry it.
	bot, err := svc.CreateBot(context.Background(), "tenant-a", "duty", "")
	if err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if !instanceCreated || bot.InstanceID != "inst_new" {
		t.Fatalf("bot = %+v instanceCreated = %v", bot, instanceCreated)
	}
}
