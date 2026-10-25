package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

type computerViewerAuth struct{ viewer *auth.ViewerPrincipal }

func (computerViewerAuth) AuthenticateMachine(context.Context, string, string) (*auth.MachinePrincipal, error) {
	return nil, errors.New("no machine")
}

func (f computerViewerAuth) AuthenticateViewer(_ context.Context, token string) (*auth.ViewerPrincipal, error) {
	if token != "tok" {
		return nil, errors.New("bad token")
	}
	return f.viewer, nil
}

type forgetRecorder struct{ forgot []string }

func (f *forgetRecorder) ForgetDesktopView(tenantID, userID string) {
	f.forgot = append(f.forgot, tenantID+"/"+userID)
}

func TestUserDesktopComputerHandlersAreScopedToCaller(t *testing.T) {
	var body map[string]any
	var path string
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasSuffix(path, "/restart") && body["force"] != true {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"desktop is busy"}`))
			return
		}
		_, _ = w.Write([]byte(`{"action":"x","container":"c","status":"running"}`))
	}))
	defer host.Close()
	ctx := context.Background()
	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	view, err := pool.CreateServer(ctx, "tenant-a", desktoppool.Server{Name: "h", BaseURL: host.URL, AccessToken: "svc", Memory: "3g", CPUs: "1", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", desktoppool.Assignment{Scope: desktoppool.ScopeGlobal, ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	identity := computerViewerAuth{viewer: &auth.ViewerPrincipal{TenantID: "tenant-a", UserID: "alice"}}
	views := &forgetRecorder{}
	call := func(h http.HandlerFunc, method, token, reqBody string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/x", strings.NewReader(reqBody))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	restart := PostUserDesktopComputerHandler(pool, identity, views, "restart")
	reset := PostUserDesktopComputerHandler(pool, identity, views, "reset")
	if rec := call(restart, http.MethodPost, "", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon = %d", rec.Code)
	}
	// A user id in the body is ignored: the caller's own desktop only.
	if rec := call(restart, http.MethodPost, "tok", `{"user_id":"bob"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "DESKTOP_BUSY") {
		t.Fatalf("busy = %d %s", rec.Code, rec.Body.String())
	}
	if body["user_id"] != "alice" {
		t.Fatalf("desktopd body=%v", body)
	}
	if rec := call(restart, http.MethodPost, "tok", `{"force":true}`); rec.Code != http.StatusOK || len(views.forgot) != 1 {
		t.Fatalf("forced restart = %d %s forgot=%v", rec.Code, rec.Body.String(), views.forgot)
	}
	if rec := call(reset, http.MethodPost, "tok", ``); rec.Code != http.StatusOK || path != "/v1/desktops/reset" {
		t.Fatalf("reset = %d path=%s", rec.Code, path)
	}
	if _, err := pool.SetTenantComputerPolicy(ctx, "tenant-a", desktoppool.ComputerPolicy{ResetDisabled: true}); err != nil {
		t.Fatal(err)
	}
	if rec := call(reset, http.MethodPost, "tok", `{}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "DESKTOP_RESET_DISABLED") {
		t.Fatalf("disabled reset = %d %s", rec.Code, rec.Body.String())
	}
	rec := call(GetUserDesktopComputerHandler(pool, identity), http.MethodGet, "tok", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reset_allowed":false`) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
	}
	put := call(PutDesktopComputerPolicyAdminHandler(pool), http.MethodPut, "", `{"reset_disabled":false}`)
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"reset_disabled":false`) {
		t.Fatalf("admin put = %d %s", put.Code, put.Body.String())
	}
}
