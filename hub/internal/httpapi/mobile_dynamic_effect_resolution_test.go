package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func resolveUnknownEffectRequest(t *testing.T, body string, admin *store.AdminUser) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/dynamic-effects/op-1/resolve", strings.NewReader(body))
	req.SetPathValue("operationId", "op-1")
	if admin != nil {
		req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
	}
	rec := httptest.NewRecorder()
	mobileResolveUnknownDynamicEffectHandler(nil)(rec, req)
	return rec
}

// The exit replaces a channel's answer with a person's. Every one of these
// guards exists so that substitution cannot happen quietly, and each is
// checked before the request is allowed anywhere near the ledger.
func TestResolveUnknownDynamicEffectRefusesAnUnderspecifiedVerdict(t *testing.T) {
	admin := &store.AdminUser{ID: "admin-1", Username: "ops-oncall", Scope: "global"}
	for _, tc := range []struct {
		name, body, wantCode string
	}{
		{"no confirmation", `{"succeeded":true,"evidence":"checked the console"}`, "CONFIRM_REQUIRED"},
		// An omitted verdict must not decay into "it failed": the state being
		// replaced is precisely that nobody knows.
		{"unstated outcome", `{"confirm":true,"evidence":"checked the console"}`, "INVALID_INPUT"},
		{"no evidence", `{"confirm":true,"succeeded":true}`, "INVALID_INPUT"},
		{"blank evidence", `{"confirm":true,"succeeded":true,"evidence":"   "}`, "INVALID_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := resolveUnknownEffectRequest(t, tc.body, admin)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var got struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
				Code string `json:"code"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if got.Error.Code != tc.wantCode && got.Code != tc.wantCode {
				t.Fatalf("want %s, body=%s", tc.wantCode, rec.Body.String())
			}
		})
	}
}

// A verdict is only as accountable as the identity attached to it, so a caller
// the request cannot name gets no verdict -- and is turned away before the
// service is even consulted.
func TestResolveUnknownDynamicEffectRefusesAnUnnamedOperator(t *testing.T) {
	body := `{"confirm":true,"succeeded":true,"evidence":"checked the console"}`
	for _, tc := range []struct {
		name  string
		admin *store.AdminUser
	}{
		{"no admin on the request", nil},
		{"an admin with no name at all", &store.AdminUser{Scope: "global"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := resolveUnknownEffectRequest(t, body, tc.admin); rec.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Falling back to the account ID keeps an admin without a display username
// accountable rather than anonymous.
func TestResolveUnknownDynamicEffectOperatorPrefersTheNameThenTheID(t *testing.T) {
	for _, tc := range []struct {
		name  string
		admin *store.AdminUser
		want  string
	}{
		{"username wins", &store.AdminUser{ID: "admin-1", Username: "ops-oncall"}, "ops-oncall"},
		{"id is the fallback", &store.AdminUser{ID: "admin-1"}, "admin-1"},
		{"blank username does not shadow the id", &store.AdminUser{ID: "admin-1", Username: "  "}, "admin-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/dynamic-effects/op-1/resolve", nil)
			req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, tc.admin))
			if got := mobileDynamicEffectResolutionOperator(req); got != tc.want {
				t.Fatalf("operator=%q want %q", got, tc.want)
			}
		})
	}
}
