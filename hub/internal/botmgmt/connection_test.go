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

// tokenForClaims builds an expired token that names a different principal,
// to simulate re-saving the connection against another tenant or user.
func tokenForClaims(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".not-checked"
}

func unauthenticated(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

// renewalStub answers the calls a token renewal needs: instance listing only
// for a minted bearer, the credential-for-token exchange, and the admin
// credential creation and revocation guarded by the admin secret. created
// counts minted credentials; revoked counts their deletions.
func renewalStub(created, revoked *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/instances":
			if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != "fresh-token" {
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
		case r.Method == http.MethodDelete &&
			strings.HasPrefix(r.URL.Path, "/api/v1/admin/tenants/") && strings.Contains(r.URL.Path, "/credentials/"):
			if r.Header.Get("X-MaClaw-Admin-Secret") != "admin-key" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"admin owner is required"}`))
				return
			}
			*revoked++
			_, _ = w.Write([]byte(`{"id":"revoked"}`))
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
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
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
	if created != 1 || revoked != 0 {
		t.Fatalf("second probe churned credentials: created = %d revoked = %d", created, revoked)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil ||
		stored.Connection.ID != "cred1" ||
		stored.Connection.APIKey != "ak_conn" ||
		stored.Connection.APISecret != "sk_conn" ||
		stored.Connection.UserID != "u1" ||
		stored.Connection.TenantID != "t1" {
		t.Fatalf("stored connection credential = %+v", stored.Connection)
	}
}

func TestTestConnectionReplacesRevokedCredential(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
	defer srv.Close()
	svc, settings := newStubService(srv)
	ctx := context.Background()
	// The stored credential no longer answers: only the freshly minted one
	// exchanges for a bearer, so the hub has to renew in place.
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     srv.URL,
		AccessToken: expiredToken(),
		AdminSecret: "admin-key",
		Connection: &connectionCredential{
			ID: "cred_old", TenantID: "t1", UserID: "u1", APIKey: "ak_old", APISecret: "sk_old",
		},
	})

	count, err := svc.TestConnection(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d want 1", count)
	}
	if created != 1 || revoked != 1 {
		t.Fatalf("credentials created = %d revoked = %d, want 1 and 1", created, revoked)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil || stored.Connection.APIKey != "ak_conn" || stored.Connection.ID != "cred1" {
		t.Fatalf("stored connection credential = %+v", stored.Connection)
	}
}

func TestDecodeTokenClaimsRejectsGarbage(t *testing.T) {
	garbage := []string{
		"",
		"noseparator",
		"abc.def",
		"$$$$.####",                          // undecodable base64
		base64Claims(`just a string`) + ".x", // valid base64, not JSON
		base64Claims(`{"tenant_id":"t1"}`) + ".x",               // claims incomplete
		base64Claims(`{"tenant_id":" ","user_id":"u1"}`) + ".x", // blank tenant
		base64Claims(`{"tenant_id":"t1","user_id":" "}`) + ".x", // blank user
	}
	for _, token := range garbage {
		if _, ok := decodeTokenClaims(token); ok {
			t.Fatalf("decodeTokenClaims(%q) accepted garbage", token)
		}
	}
	claims, ok := decodeTokenClaims(expiredToken())
	if !ok || claims.TenantID != "t1" || claims.UserID != "u1" {
		t.Fatalf("decodeTokenClaims(expiredToken) = %+v ok = %v", claims, ok)
	}
}

// base64Claims encodes one payload the way a MaClawSrv token carries it.
func base64Claims(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func TestConnectionMatchesToken(t *testing.T) {
	cred := &connectionCredential{TenantID: "t1", UserID: "u1"}
	if !connectionMatchesToken(record{AccessToken: expiredToken(), Connection: cred}) {
		t.Fatal("matching principal rejected")
	}
	// Any token the hub cannot decode retires the credential: it can no
	// longer be tied to the principal it was minted for.
	if connectionMatchesToken(record{AccessToken: "junk.token", Connection: cred}) {
		t.Fatal("undecodable token kept the credential")
	}
	other := &connectionCredential{TenantID: "t1", UserID: "u9"}
	if connectionMatchesToken(record{AccessToken: expiredToken(), Connection: other}) {
		t.Fatal("mismatched principal accepted")
	}
	if connectionMatchesToken(record{AccessToken: expiredToken()}) {
		t.Fatal("no credential should not match anything")
	}
}

func TestTestConnectionKeeps401WhenTokenUndecodable(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
	defer srv.Close()
	svc, settings := newStubService(srv)
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     srv.URL,
		AccessToken: "not-a-maclaw-token",
		AdminSecret: "admin-key",
	})

	// The hub cannot tell which user the connection belongs to, so renewal
	// refuses; the token-rejected 401 stays the answer the admin sees.
	_, err := svc.TestConnection(context.Background(), "tenant-a")
	var statusErr *upstream.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusUnauthorized {
		t.Fatalf("TestConnection error = %v want the original 401", err)
	}
	if created != 0 || revoked != 0 {
		t.Fatalf("credentials created = %d revoked = %d, want none", created, revoked)
	}
}

func TestTestConnectionKeeps401WithoutAdminSecret(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
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
	if created != 0 || revoked != 0 {
		t.Fatalf("credentials created = %d revoked = %d, want none", created, revoked)
	}
}

func TestTestConnectionSurfacesRenewalStatusError(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
	defer srv.Close()
	svc, settings := newStubService(srv)
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     srv.URL,
		AccessToken: expiredToken(),
		AdminSecret: "wrong-secret",
	})

	// The admin secret no longer opens the admin API, so the renewal fails
	// with its own status; that concrete reason is more useful to the admin
	// than the connection's original token rejection.
	_, err := svc.TestConnection(context.Background(), "tenant-a")
	var statusErr *upstream.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusForbidden {
		t.Fatalf("TestConnection error = %v want the renewal 403", err)
	}
	if created != 0 || revoked != 0 {
		t.Fatalf("credentials created = %d revoked = %d, want none", created, revoked)
	}
}

func TestSaveAdminSecretBootstrapsConnectionCredential(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
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
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
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

	// Re-saving the same settings keeps the credential: there is nothing to
	// re-provision.
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, expiredToken(), true); err != nil {
		t.Fatalf("same-token SaveConnection: %v", err)
	}
	stored := storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil {
		t.Fatal("same-token save dropped the credential")
	}
	if created != 1 {
		t.Fatalf("credentials created = %d want 1", created)
	}

	// A different token changes the connection user, so the old credential
	// must not survive the save. The save clears the record before it can be
	// cited, so the revocation falls to the MaClawSrv side.
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, tokenForClaims(
		`{"tenant_id":"t2","user_id":"u9","exp":"2020-01-01T00:00:00Z"}`), true); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	stored = storedRecord(t, settings, "tenant-a")
	if stored.Connection == nil || stored.Connection.UserID != "u9" {
		t.Fatalf("connection credential = %+v, want one for u9", stored.Connection)
	}
	if created != 2 || revoked != 0 {
		t.Fatalf("credentials created = %d revoked = %d, want 2 and 0", created, revoked)
	}
}

func TestSaveConnectionReprovisionsOnURLChange(t *testing.T) {
	created, revoked := 0, 0
	oldSrv := renewalStub(&created, &revoked)
	defer oldSrv.Close()
	newSrv := renewalStub(&created, &revoked)
	defer newSrv.Close()
	svc, settings := newStubService(oldSrv)
	ctx := context.Background()
	seedRecord(t, settings, "tenant-a", record{
		BaseURL:     oldSrv.URL,
		AccessToken: expiredToken(),
		AdminSecret: "admin-key",
		Connection: &connectionCredential{
			ID: "cred_old", TenantID: "t1", UserID: "u1", APIKey: "ak_conn", APISecret: "sk_conn",
		},
	})

	// Moving the connection to another MaClawSrv retires the credential even
	// though the token did not change; the fresh one answers on the new URL.
	if _, err := svc.SaveConnection(ctx, "tenant-a", newSrv.URL, "", false); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	stored := storedRecord(t, settings, "tenant-a")
	if stored.BaseURL != newSrv.URL {
		t.Fatalf("BaseURL = %q want %q", stored.BaseURL, newSrv.URL)
	}
	if stored.Connection == nil || stored.Connection.UserID != "u1" {
		t.Fatalf("connection credential = %+v, want one for u1", stored.Connection)
	}
	if created != 1 || revoked != 0 {
		t.Fatalf("credentials created = %d revoked = %d, want 1 and 0", created, revoked)
	}
}

func TestCreateBotMintsFromConnectionCredential(t *testing.T) {
	created, revoked := 0, 0
	srv := renewalStub(&created, &revoked)
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
			ID: "cred_old", TenantID: "t1", UserID: "u1", APIKey: "ak_conn", APISecret: "sk_conn",
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
