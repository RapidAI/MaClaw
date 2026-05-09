package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store/sqlite"
)

func TestRegisterCenterHandlerRequiresMachineAndCompanyIdentity(t *testing.T) {
	provider, centerSvc, _ := newApprovalTestServices(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/register", RegisterCenterHandler(centerSvc))

	req := httptest.NewRequest(http.MethodPost, "/api/centers/register", strings.NewReader(`{"company_name":"Acme","admin_email":"admin@example.com"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "machine_id and company_id") {
		t.Fatalf("body = %s, want missing identity diagnostic", res.Body.String())
	}
	stores := sqlite.NewStore(provider)
	centers, err := stores.Centers.List(context.Background())
	if err != nil {
		t.Fatalf("list centers: %v", err)
	}
	if len(centers) != 0 {
		t.Fatalf("centers should not be created without dedupe identity: %+v", centers)
	}
}

func TestRegisterCenterHandlerRejectsTrailingJSONWithoutCreatingCenter(t *testing.T) {
	provider, centerSvc, _ := newApprovalTestServices(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/centers/register", RegisterCenterHandler(centerSvc))

	body := `{"machine_id":"machine-1","company_id":"company-1","company_name":"Acme","admin_email":"admin@example.com"} {"machine_id":"machine-2","company_id":"company-2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/centers/register", strings.NewReader(body))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", res.Code, res.Body.String())
	}
	var errorBody map[string]string
	if err := json.NewDecoder(res.Body).Decode(&errorBody); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorBody["error"] != "INVALID_JSON" {
		t.Fatalf("error body = %+v, want INVALID_JSON", errorBody)
	}
	stores := sqlite.NewStore(provider)
	centers, err := stores.Centers.List(context.Background())
	if err != nil {
		t.Fatalf("list centers: %v", err)
	}
	if len(centers) != 0 {
		t.Fatalf("centers should not be created after trailing JSON: %+v", centers)
	}
}
