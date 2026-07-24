package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// newShareImportTestHub serves a minimal Hub knowledge-share API: metadata at
// /api/knowledge/shares/{id}?intent=import and the package at /pkg.json.
func newShareImportTestHub(t *testing.T) *httptest.Server {
	t.Helper()
	var hub *httptest.Server
	hub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/knowledge/shares/"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"knowledge_id": "kn_test",
				"title":        "Test Share",
				"package_url":  hub.URL + "/pkg.json",
			})
		case r.URL.Path == "/pkg.json":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"manifest": map[string]interface{}{
					"format":       "maclaw.knowledge.package",
					"version":      1,
					"package_id":   "kxp_test",
					"title":        "Test Share",
					"source_count": 2,
				},
				"sources": []map[string]interface{}{
					{"id": "ps1", "kind": "text", "title": "Doc A", "content": "hello knowledge"},
					{"id": "ps2", "kind": "text", "title": "Doc B", "content": "shared brain"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hub.Close)
	return hub
}

func newRealStoreCallbacks(t *testing.T) (*coreAgentCallbacks, *knowledge.SQLiteStore) {
	t.Helper()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cb := &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: store,
		principal:      Principal{TenantID: "tenant-a", UserID: "user-a"},
	}
	return cb, store
}

func shareImportResult(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	if strings.HasPrefix(raw, "Error:") {
		t.Fatalf("tool returned error: %s", raw)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("bad tool JSON %q: %v", raw, err)
	}
	return out
}

// TestKnowledgeImportShareEndToEnd exercises the full share-import chain
// (metadata fetch → package download → validation → import) against a real
// SQLite store, covering both dry_run preview and the actual import.
func TestKnowledgeImportShareEndToEnd(t *testing.T) {
	hub := newShareImportTestHub(t)
	cb, store := newRealStoreCallbacks(t)

	// 1. Dry-run: classifies without writing.
	raw := cb.executeKnowledgeImportShare(map[string]interface{}{
		"share_link": hub.URL + "/hub/knowledge/shares/kn_test",
		"dry_run":    true,
	})
	res := shareImportResult(t, raw)
	if res["status"] != "dry_run" || res["dry_run"] != true {
		t.Fatalf("expected dry_run status, got %v", res["status"])
	}
	if total, _ := res["total"].(float64); total != 2 {
		t.Fatalf("expected 2 sources in package, got %v", res["total"])
	}
	sources, err := store.ListSources(context.Background(), knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("dry_run must not write, found %d sources", len(sources))
	}

	// 2. Real import: writes to the store under the caller's scope.
	raw = cb.executeKnowledgeImportShare(map[string]interface{}{
		"share_link": hub.URL + "/hub/knowledge/shares/kn_test",
	})
	res = shareImportResult(t, raw)
	if res["status"] != "imported" {
		t.Fatalf("expected imported status, got %v (%s)", res["status"], raw)
	}
	if imported, _ := res["imported"].(float64); imported != 2 {
		t.Fatalf("expected 2 imported sources, got %v (%s)", res["imported"], raw)
	}
	sources, err = store.ListSources(context.Background(), knowledge.ListSourcesOptions{TenantID: "tenant-a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 stored sources, got %d", len(sources))
	}

	// 3. knowledge_import_hub_share dispatch defaults to dry_run=true: a second
	//    call must preview (not duplicate-write).
	result := cb.ExecuteToolStructured("knowledge_import_hub_share", `{"share_link":"`+hub.URL+`/hub/knowledge/shares/kn_test"}`)
	res = shareImportResult(t, result.Result)
	if res["status"] != "dry_run" {
		t.Fatalf("hub_share dispatch should default to dry_run, got %v", res["status"])
	}
}
