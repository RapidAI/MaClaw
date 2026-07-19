package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/expert"
	_ "modernc.org/sqlite"
)

func newExpertTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// httptest 在独立 goroutine 服务；单连接保证内存库不分裂。
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	st := expert.NewStore(db)
	if err := st.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewExpertHandler(st).Register(mux, fakeVEMachineAuth{
		principals: map[string]*auth.MachinePrincipal{
			"m1": {TenantID: "tenant-a", UserID: "u1", MachineID: "m1"},
		},
		token: "good-token",
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func expertReq(t *testing.T, method, url, body string, withAuth bool) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if withAuth {
		req.Header.Set("X-Machine-ID", "m1")
		req.Header.Set("Authorization", "Bearer good-token")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

type expertAPIResp struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	UpdatedAt string   `json:"updated_at"`
	Tools     []string `json:"tools"`
	Applied   bool     `json:"applied"`
}

func TestExpertAuthRejected(t *testing.T) {
	srv := newExpertTestServer(t)
	// 无 token / 无机器头 → 401
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/v1/experts", ""},
		{"POST", "/api/v1/experts", `{"name":"x","system_prompt":"p"}`},
		{"GET", "/api/v1/experts/e1", ""},
		{"PATCH", "/api/v1/experts/e1", `{}`},
		{"DELETE", "/api/v1/experts/e1", ""},
	} {
		resp, _ := expertReq(t, tc.method, srv.URL+tc.path, tc.body, false)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s: status=%d", tc.method, tc.path, resp.StatusCode)
		}
	}
	// 错误 token → 401
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/experts", nil)
	req.Header.Set("X-Machine-ID", "m1")
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token: status=%d", resp.StatusCode)
	}
}

func TestExpertListEnvelope(t *testing.T) {
	srv := newExpertTestServer(t)
	body := `{"name":"n1","system_prompt":"p","tools":["bash"]}`
	resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", body, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	var created expertAPIResp
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if !created.Applied {
		t.Fatal("fresh create should be applied=true")
	}

	resp, raw = expertReq(t, "GET", srv.URL+"/api/v1/experts", "", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, raw)
	}
	var env struct {
		Experts []expertAPIResp `json:"experts"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("envelope shape: %v (%s)", err, raw)
	}
	if len(env.Experts) != 1 || env.Experts[0].Name != "n1" || env.Experts[0].ID != created.ID {
		t.Fatalf("envelope: %+v", env)
	}
	// 信封内不含 applied 字段语义，但反序列化结构只取已知字段，这里只校验数组形态。
}

func TestExpertUpsertLWWViaAPI(t *testing.T) {
	srv := newExpertTestServer(t)
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)

	mk := func(name, ts string) string {
		return `{"id":"e1","name":"` + name + `","system_prompt":"p","updated_at":"` + ts + `"}`
	}
	resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("v1", newer), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	// 过期写：HTTP 仍 200，applied=false，服务端值不变
	resp, raw = expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("stale", old), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stale write status: %d %s", resp.StatusCode, raw)
	}
	var r2 expertAPIResp
	if err := json.Unmarshal(raw, &r2); err != nil {
		t.Fatal(err)
	}
	if r2.Applied {
		t.Fatal("stale write should be applied=false")
	}
	if r2.Name != "v1" {
		t.Fatalf("stale write overwrote: %+v", r2)
	}
	// 更新写：applied=true
	resp, raw = expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("v2", time.Now().UTC().Format(time.RFC3339)), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("newer write: %d %s", resp.StatusCode, raw)
	}
	var r3 expertAPIResp
	if err := json.Unmarshal(raw, &r3); err != nil {
		t.Fatal(err)
	}
	if !r3.Applied || r3.Name != "v2" {
		t.Fatalf("newer write: %+v", r3)
	}
}

func TestExpertBadBodies(t *testing.T) {
	srv := newExpertTestServer(t)

	// trailing JSON 拒绝
	resp, _ := expertReq(t, "POST", srv.URL+"/api/v1/experts", `{"name":"x","system_prompt":"p"}{}`, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing json: %d", resp.StatusCode)
	}
	// 空 body 拒绝
	resp, _ = expertReq(t, "POST", srv.URL+"/api/v1/experts", "", true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body: %d", resp.StatusCode)
	}
	// 非 JSON 拒绝
	resp, _ = expertReq(t, "POST", srv.URL+"/api/v1/experts", "not-json", true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json: %d", resp.StatusCode)
	}
}

func TestExpertSystemPromptRequired(t *testing.T) {
	srv := newExpertTestServer(t)
	for _, body := range []string{
		`{"name":"x"}`,
		`{"name":"x","system_prompt":"   "}`,
	} {
		resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", body, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s: status=%d %s", body, resp.StatusCode, raw)
		}
		if !strings.Contains(string(raw), "system_prompt required") {
			t.Fatalf("body %s: unexpected error %s", body, raw)
		}
	}
	// PATCH 空 system_prompt 同样拒绝
	resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", `{"id":"e1","name":"x","system_prompt":"p"}`, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	resp, raw = expertReq(t, "PATCH", srv.URL+"/api/v1/experts/e1", `{"system_prompt":"  "}`, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch empty prompt: %d %s", resp.StatusCode, raw)
	}
}

func TestExpertFutureTimestampClamped(t *testing.T) {
	srv := newExpertTestServer(t)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	body := `{"id":"e1","name":"x","system_prompt":"p","updated_at":"` + future + `"}`
	resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", body, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	var r expertAPIResp
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		t.Fatalf("updated_at unparseable: %q", r.UpdatedAt)
	}
	if parsed.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("future timestamp not clamped: %q", r.UpdatedAt)
	}
}

func TestExpertTombstoneViaAPI(t *testing.T) {
	srv := newExpertTestServer(t)
	t1 := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	mk := func(name, ts string) string {
		return `{"id":"e1","name":"` + name + `","system_prompt":"p","updated_at":"` + ts + `"}`
	}
	resp, raw := expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("v1", t1), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", resp.StatusCode, raw)
	}
	resp, _ = expertReq(t, "DELETE", srv.URL+"/api/v1/experts/e1", "", true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	// 删除后 GET → 404
	resp, _ = expertReq(t, "GET", srv.URL+"/api/v1/experts/e1", "", true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d", resp.StatusCode)
	}
	// 旧 updated_at 重放 → applied:false，且不复活
	resp, raw = expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("zombie", t1), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: %d %s", resp.StatusCode, raw)
	}
	var r expertAPIResp
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if r.Applied {
		t.Fatal("replay at deleted timestamp should be applied=false")
	}
	resp, _ = expertReq(t, "GET", srv.URL+"/api/v1/experts/e1", "", true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("zombie resurrected: %d", resp.StatusCode)
	}
	// 比墓碑新的写入正常复活（+1s 确保严格晚于 deleted_at 的纳秒值）
	resp, raw = expertReq(t, "POST", srv.URL+"/api/v1/experts", mk("reborn", time.Now().UTC().Add(time.Second).Format(time.RFC3339)), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reborn: %d %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Applied || r.Name != "reborn" {
		t.Fatalf("reborn: %+v", r)
	}
}
