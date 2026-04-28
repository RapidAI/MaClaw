package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

func TestSkillMarketAdminCRUD(t *testing.T) {
	svc := skillmarket.NewService(&memorySkillRepo{items: map[string]*store.Skill{}})
	h := NewSkillMarketHandler(nil, nil, svc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/skills", h.ListAdminSkills())
	mux.HandleFunc("POST /api/admin/skills", h.CreateAdminSkill())
	mux.HandleFunc("PUT /api/admin/skills/{skill_id}", h.UpdateAdminSkill())
	mux.HandleFunc("DELETE /api/admin/skills/{skill_id}", h.DeleteAdminSkill())

	create := httptest.NewRequest(http.MethodPost, "/api/admin/skills", strings.NewReader(`{"id":"skill-admin","name":"Admin Skill","tags":["admin"],"status":"active","price":19,"author":"ops team"}`))
	createRes := httptest.NewRecorder()
	mux.ServeHTTP(createRes, create)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRes.Code, createRes.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/admin/skills/skill-admin", strings.NewReader(`{"name":"Admin Skill Updated","category":"ops","status":"disabled"}`))
	updateRes := httptest.NewRecorder()
	mux.ServeHTTP(updateRes, update)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRes.Code, updateRes.Body.String())
	}
	var updated CloudSkill
	if err := json.NewDecoder(updateRes.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Name != "Admin Skill Updated" || updated.Status != "disabled" {
		t.Fatalf("updated skill = %+v", updated)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/admin/skills", nil)
	listRes := httptest.NewRecorder()
	mux.ServeHTTP(listRes, list)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRes.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/skills/skill-admin", nil)
	deleteRes := httptest.NewRecorder()
	mux.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRes.Code, deleteRes.Body.String())
	}
}
func TestSkillMarketRequiresSkillMarketLicense(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	licSvc := license.NewService(&memoryLicenseRepo{licenses: map[string]*store.License{
		"ctr_1": {ID: "lic_1", CenterID: "ctr_1", Modules: `["compute"]`},
	}}, priv)
	centerAuth := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("center-secret")},
	}}

	mux := http.NewServeMux()
	h := NewSkillMarketHandler(centerAuth, licSvc, newTestSkillMarketService(t))
	mux.HandleFunc("GET /api/centers/{id}/skills/search", h.SearchCenterSkills())

	req := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/skills/search?q=goal", nil)
	req.Header.Set("X-Center-Secret", "center-secret")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.Code)
	}
}

func TestSkillMarketSearchWithEntitlement(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	licSvc := license.NewService(&memoryLicenseRepo{licenses: map[string]*store.License{
		"ctr_1": {ID: "lic_1", CenterID: "ctr_1", Modules: `["compute","skill_market"]`},
	}}, priv)
	centerAuth := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("center-secret")},
	}}

	mux := http.NewServeMux()
	h := NewSkillMarketHandler(centerAuth, licSvc, newTestSkillMarketService(t))
	mux.HandleFunc("GET /api/centers/{id}/skills/search", h.SearchCenterSkills())
	mux.HandleFunc("GET /api/centers/{id}/skills/{skill_id}", h.GetCenterSkill())

	search := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/skills/search?q=goal", nil)
	search.Header.Set("X-Center-Secret", "center-secret")
	searchRes := httptest.NewRecorder()
	mux.ServeHTTP(searchRes, search)
	if searchRes.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", searchRes.Code, searchRes.Body.String())
	}
	var body struct {
		Results []CloudSkill `json:"results"`
	}
	if err := json.NewDecoder(searchRes.Body).Decode(&body); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(body.Results) == 0 || body.Results[0].ID == "" {
		t.Fatalf("expected at least one skill, got %+v", body.Results)
	}
	if body.Results[0].Author == "" {
		t.Fatalf("expected HubCenter-compatible market fields, got %+v", body.Results[0])
	}

	get := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/skills/goal-recovery-loop", nil)
	get.Header.Set("X-Center-Secret", "center-secret")
	getRes := httptest.NewRecorder()
	mux.ServeHTTP(getRes, get)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRes.Code, getRes.Body.String())
	}
}

func TestSkillMarketRejectsMissingCenterSecret(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	licSvc := license.NewService(&memoryLicenseRepo{licenses: map[string]*store.License{}}, priv)
	centerAuth := &mockCenterAuthService{centers: map[string]*store.Center{
		"ctr_1": {ID: "ctr_1", Status: "active", SecretHash: hashTestSecret("center-secret")},
	}}

	mux := http.NewServeMux()
	h := NewSkillMarketHandler(centerAuth, licSvc, newTestSkillMarketService(t))
	mux.HandleFunc("GET /api/centers/{id}/skills/search", h.SearchCenterSkills())

	req := httptest.NewRequest(http.MethodGet, "/api/centers/ctr_1/skills/search", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
}

func TestLicenseAllowsSkillMarket(t *testing.T) {
	cases := map[string]bool{
		`["skill_market"]`: true,
		`["skills"]`:       true,
		`["all"]`:          true,
		`["compute"]`:      false,
		`invalid`:          false,
	}
	for modules, want := range cases {
		if got := licenseAllowsSkillMarket(modules); got != want {
			t.Fatalf("licenseAllowsSkillMarket(%s) = %v, want %v", modules, got, want)
		}
	}
}

type memorySkillRepo struct {
	items map[string]*store.Skill
}

func newTestSkillMarketService(t *testing.T) *skillmarket.Service {
	t.Helper()
	svc := skillmarket.NewService(&memorySkillRepo{items: map[string]*store.Skill{}})
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatalf("seed skill market: %v", err)
	}
	return svc
}

func (m *memorySkillRepo) Create(_ context.Context, s *store.Skill) error {
	copy := *s
	m.items[s.ID] = &copy
	return nil
}

func (m *memorySkillRepo) Update(_ context.Context, s *store.Skill) error {
	copy := *s
	m.items[s.ID] = &copy
	return nil
}

func (m *memorySkillRepo) GetByID(_ context.Context, id string) (*store.Skill, error) {
	if item, ok := m.items[id]; ok {
		copy := *item
		return &copy, nil
	}
	return nil, assertNotFoundError{}
}

func (m *memorySkillRepo) List(context.Context) ([]*store.Skill, error) {
	out := make([]*store.Skill, 0, len(m.items))
	for _, item := range m.items {
		copy := *item
		out = append(out, &copy)
	}
	return out, nil
}

func (m *memorySkillRepo) SearchActive(_ context.Context, query string) ([]*store.Skill, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]*store.Skill, 0, len(m.items))
	for _, item := range m.items {
		if item.Status != "active" {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(item.ID+" "+item.Name+" "+item.Description+" "+item.Category+" "+item.Tags), query) {
			copy := *item
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (m *memorySkillRepo) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

func (m *memorySkillRepo) Count(context.Context) (int, error) {
	return len(m.items), nil
}

type assertNotFoundError struct{}

func (assertNotFoundError) Error() string { return "not found" }
