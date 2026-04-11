package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	colRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	colSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	roleSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func setup(t *testing.T) (*http.ServeMux, *roleSvc.RoleService) {
	t.Helper()
	p, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	if err := db.Migrate(p.Write); err != nil {
		t.Fatal(err)
	}

	rr := roleRepo.New(p.Write, p.Read)
	rs := roleSvc.New(rr)
	cr := colRepo.New(p.Write, p.Read)
	cs := colSvc.New(cr, rr, rs)
	h := New(cs, rr, rs)

	mux := http.NewServeMux()
	h.RegisterAdminRoutes(mux)
	h.RegisterClientRoutes(mux)
	return mux, rs
}

// createRole is a test helper that creates a role and returns its ID.
func createRole(t *testing.T, rs *roleSvc.RoleService, code string) string {
	t.Helper()
	role, err := rs.Create(roleSvc.CreateRequest{
		Name: "测试角色_" + code, Code: code, Description: "test role",
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	return role.ID
}

func TestCreateAndListColleagues(t *testing.T) {
	mux, rs := setup(t)
	roleID := createRole(t, rs, "office")

	// create colleague
	body := `{"name":"小迪","role_id":"` + roleID + `","description":"擅长通知和纪要","strengths":["通知","纪要"],"tasks":["写通知","会议纪要"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body: %s", w.Code, w.Body.String())
	}

	var created adminDTO
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Name != "小迪" {
		t.Fatalf("name = %q, want 小迪", created.Name)
	}
	if created.RoleID != roleID {
		t.Fatalf("role_id = %q, want %s", created.RoleID, roleID)
	}
	if created.RoleCode != "office" {
		t.Fatalf("role_code = %q, want office", created.RoleCode)
	}

	// list admin
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/admin/colleagues", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("list status = %d", w2.Code)
	}
	var listResp struct {
		Colleagues []adminDTO `json:"colleagues"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &listResp)
	if len(listResp.Colleagues) != 1 {
		t.Fatalf("len = %d, want 1", len(listResp.Colleagues))
	}

	// client list
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/client/colleagues", nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("client list status = %d", w3.Code)
	}
}

func TestCreateRequiresRole(t *testing.T) {
	mux, _ := setup(t)

	// missing role_id
	body := `{"name":"测试","role_id":""}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing role status = %d, want 400", w.Code)
	}

	// non-existent role_id
	body2 := `{"name":"测试","role_id":"nonexistent"}`
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body2)))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("bad role status = %d, want 400", w2.Code)
	}
}

func TestAssignRole(t *testing.T) {
	mux, rs := setup(t)
	officeID := createRole(t, rs, "office")
	dataID := createRole(t, rs, "data")

	// create colleague with office role
	body := `{"name":"阿宁","role_id":"` + officeID + `"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body)))
	var created adminDTO
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// assign new role
	assignBody := `{"role_id":"` + dataID + `","reason":"转岗到数据组"}`
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/admin/colleagues/"+created.ID+"/assign-role", bytes.NewBufferString(assignBody)))
	if w2.Code != http.StatusOK {
		t.Fatalf("assign role status = %d, body: %s", w2.Code, w2.Body.String())
	}

	// verify role changed
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/admin/colleagues/"+created.ID, nil))
	var updated adminDTO
	_ = json.Unmarshal(w3.Body.Bytes(), &updated)
	if updated.RoleID != dataID {
		t.Fatalf("role_id after assign = %q, want %s", updated.RoleID, dataID)
	}
	if updated.RoleCode != "data" {
		t.Fatalf("role_code after assign = %q, want data", updated.RoleCode)
	}

	// check role history
	w4 := httptest.NewRecorder()
	mux.ServeHTTP(w4, httptest.NewRequest(http.MethodGet, "/admin/colleagues/"+created.ID+"/role-history", nil))
	if w4.Code != http.StatusOK {
		t.Fatalf("role-history status = %d", w4.Code)
	}
	var histResp struct {
		Logs []struct {
			OldRoleID string `json:"old_role_id"`
			NewRoleID string `json:"new_role_id"`
			Reason    string `json:"reason"`
		} `json:"logs"`
	}
	_ = json.Unmarshal(w4.Body.Bytes(), &histResp)
	// should have 2 entries: initial creation + reassignment
	if len(histResp.Logs) != 2 {
		t.Fatalf("history len = %d, want 2", len(histResp.Logs))
	}
	// verify the reassignment entry exists
	found := false
	for _, l := range histResp.Logs {
		if l.NewRoleID == dataID && l.Reason == "转岗到数据组" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reassignment log not found in history: %+v", histResp.Logs)
	}
}

func TestFilterByRole(t *testing.T) {
	mux, rs := setup(t)
	officeID := createRole(t, rs, "office")
	dataID := createRole(t, rs, "data")

	// create 2 colleagues with different roles
	for _, pair := range []struct{ name, roleID string }{
		{"小迪", officeID}, {"阿宁", dataID},
	} {
		body := `{"name":"` + pair.name + `","role_id":"` + pair.roleID + `"}`
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body)))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: status %d", pair.name, w.Code)
		}
	}

	// filter by office role
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/colleagues?role_id="+officeID, nil))
	var resp struct {
		Colleagues []adminDTO `json:"colleagues"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Colleagues) != 1 {
		t.Fatalf("filter by office: len = %d, want 1", len(resp.Colleagues))
	}
	if resp.Colleagues[0].Name != "小迪" {
		t.Fatalf("filtered name = %q, want 小迪", resp.Colleagues[0].Name)
	}

	// client filter by role
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/client/colleagues?role_id="+dataID, nil))
	var clientResp struct {
		Colleagues []clientDTO `json:"colleagues"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &clientResp)
	if len(clientResp.Colleagues) != 1 {
		t.Fatalf("client filter by data: len = %d, want 1", len(clientResp.Colleagues))
	}
}

func TestSetStatus(t *testing.T) {
	mux, rs := setup(t)
	roleID := createRole(t, rs, "production")

	body := `{"name":"老陈","role_id":"` + roleID + `"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/colleagues", bytes.NewBufferString(body)))
	var created adminDTO
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// disable
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/admin/colleagues/"+created.ID+"/status", bytes.NewBufferString(`{"status":"disabled"}`)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set status = %d", w2.Code)
	}

	// client list should be empty
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/client/colleagues", nil))
	var clientResp struct {
		Colleagues []clientDTO `json:"colleagues"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &clientResp)
	if len(clientResp.Colleagues) != 0 {
		t.Fatalf("client colleagues after disable = %d, want 0", len(clientResp.Colleagues))
	}
}
