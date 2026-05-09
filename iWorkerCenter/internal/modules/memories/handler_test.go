package memories

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestClientMemoriesUseTenantHeader(t *testing.T) {
	db := newTestMemoryDB(t)
	h := NewHandler(db, db)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)
	seedSharedMemory(t, db, "mem-a", "tenant-a", "Tenant A memory")
	seedSharedMemory(t, db, "mem-b", "tenant-b", "Tenant B memory")

	req := httptest.NewRequest(http.MethodGet, "/client/memories", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Memories []MemoryEntry `json:"memories"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Memories) != 1 || body.Memories[0].ID != "mem-a" {
		t.Fatalf("unexpected memories: %+v", body.Memories)
	}
}

func TestClientMemoriesRejectsCorruptTagsJSON(t *testing.T) {
	db := newTestMemoryDB(t)
	h := NewHandler(db, db)
	mux := http.NewServeMux()
	h.RegisterClientRoutes(mux)
	_, err := db.Exec(`INSERT INTO shared_memories (id, tenant_id, title, content, level, scope, tags, version, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'enterprise', 'all', ?, 1, 'active', '2026-04-28T00:00:00Z', '2026-04-28T00:00:00Z')`,
		"mem-bad-tags", "tenant-a", "Bad tags", "corrupt metadata", "{bad-json")
	if err != nil {
		t.Fatalf("seed corrupt tags: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/client/memories", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want corrupt tags to fail loudly", res.Code, res.Body.String())
	}
}

func TestUpdateMemoryRejectsEmptyTitleWithoutMutating(t *testing.T) {
	db := newTestMemoryDB(t)
	h := NewHandler(db, db)
	mux := http.NewServeMux()
	h.RegisterAdminRoutes(mux)
	seedSharedMemory(t, db, "mem-a", "tenant-a", "original content")

	req := httptest.NewRequest(http.MethodPut, "/admin/memories/mem-a", bytes.NewBufferString(`{"title":" ","content":"mutated","level":"role","scope":"ops","tags":[],"status":"active"}`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want missing title", res.Code, res.Body.String())
	}
	var content string
	if err := db.QueryRow(`SELECT content FROM shared_memories WHERE tenant_id=? AND id=?`, "tenant-a", "mem-a").Scan(&content); err != nil {
		t.Fatalf("query memory: %v", err)
	}
	if content != "original content" {
		t.Fatalf("content = %q, want unchanged", content)
	}
}

func TestUpdateMemorySupportsEscapedID(t *testing.T) {
	db := newTestMemoryDB(t)
	h := NewHandler(db, db)
	mux := http.NewServeMux()
	h.RegisterAdminRoutes(mux)
	seedSharedMemory(t, db, "mem/team a", "tenant-a", "original content")

	req := httptest.NewRequest(http.MethodPut, "/admin/memories/mem%2Fteam%20a", bytes.NewBufferString(`{"title":"Team memory","content":"updated","level":"team","scope":"ops","tags":["ops"],"status":"active"}`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var content, level, scope string
	if err := db.QueryRow(`SELECT content, level, scope FROM shared_memories WHERE tenant_id=? AND id=?`, "tenant-a", "mem/team a").Scan(&content, &level, &scope); err != nil {
		t.Fatalf("query memory: %v", err)
	}
	if content != "updated" || level != "team" || scope != "ops" {
		t.Fatalf("memory = content:%q level:%q scope:%q", content, level, scope)
	}
}

func newTestMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE shared_memories (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		level TEXT NOT NULL DEFAULT 'enterprise',
		scope TEXT NOT NULL DEFAULT 'all',
		tags TEXT NOT NULL DEFAULT '[]',
		version INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL DEFAULT '2026-04-28T00:00:00Z',
		updated_at TEXT NOT NULL DEFAULT '2026-04-28T00:00:00Z'
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func seedSharedMemory(t *testing.T, db *sql.DB, id, tenantID, content string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO shared_memories (id, tenant_id, title, content, level, scope, tags, version, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'enterprise', 'all', '[]', 1, 'active', '2026-04-28T00:00:00Z', '2026-04-28T00:00:00Z')`, id, tenantID, id, content)
	if err != nil {
		t.Fatalf("seed memory %s: %v", id, err)
	}
}
