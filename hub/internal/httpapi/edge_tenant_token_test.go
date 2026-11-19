package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestNodeTokenRejectsAnotherTenantAndGlobalTokenDoesNot(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "global-token")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if enrolled.HubToken == "" {
		t.Fatal("node hub token is empty")
	}
	prev := edgeRegistry
	edgeRegistry = reg
	t.Cleanup(func() { edgeRegistry = prev })

	allow := func(token, body string) (bool, int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/session", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ok := authorizeDesktopAPI(rec, req)
		return ok, rec.Code, rec.Body.String()
	}

	if ok, code, body := allow("global-token", `{"tenant_id":"other","user_id":"alice"}`); !ok {
		t.Fatalf("global token status=%d body=%s", code, body)
	}
	if ok, code, body := allow(enrolled.HubToken, `{"tenant_id":"other","user_id":"alice"}`); ok || code != http.StatusForbidden || !strings.Contains(body, "EDGE_TENANT") {
		t.Fatalf("other tenant ok=%v status=%d body=%s", ok, code, body)
	}
	if ok, code, body := allow(enrolled.HubToken, `{"tenant_id":"tenant-a","user_id":"alice"}`); !ok {
		t.Fatalf("own tenant status=%d body=%s", code, body)
	}
}
