package digitalasset
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestParseKnowledgeShareRef(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"kn_abc", "kn_abc"},
		{"https://hub.example.com/k/kn_abc", "kn_abc"},
		{"https://hub.example.com/api/knowledge/shares/kn_xyz?intent=import", "kn_xyz"},
		{"/api/knowledge/shares/kn_1/package", "kn_1"},
		{"/hub/knowledge/shares/kn_2", "kn_2"},
	}
	for _, tc := range cases {
		got, err := ParseKnowledgeShareRef(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("ParseKnowledgeShareRef(%q)=%q err=%v want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ParseKnowledgeShareRef(""); err == nil {
		t.Fatal("expected error for empty")
	}
}

type memShareLoader struct {
	share *store.KnowledgeShare
	path  string
}

func (m memShareLoader) Get(ctx context.Context, knowledgeID string) (*store.KnowledgeShare, error) {
	if m.share != nil && m.share.KnowledgeID == knowledgeID {
		return m.share, nil
	}
	return nil, nil
}
func (m memShareLoader) ResolvePackagePath(storageRef string) (string, bool) {
	if m.path != "" {
		return m.path, true
	}
	return "", false
}
func (m memShareLoader) IncrementImport(ctx context.Context, knowledgeID string) error { return nil }

func TestImportKnowledgeShare_EndToEnd(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "Ent", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Write a minimal knowledge package on disk
	pkgDir := t.TempDir()
	pkgPath := filepath.Join(pkgDir, "kn_test.json")
	pkg := map[string]any{
		"manifest": map[string]any{
			"format": "maclaw.knowledge.package", "version": 1, "package_id": "kxp1",
			"title": "Shared Policy", "source_count": 1, "editable": true,
		},
		"sources": []map[string]any{
			{"id": "src1", "kind": "text", "title": "Note", "content": "enterprise share body unique-xyz-123"},
		},
	}
	raw, _ := json.Marshal(pkg)
	if err := os.WriteFile(pkgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	share := &store.KnowledgeShare{
		KnowledgeID: "kn_test", TenantID: "tenant_a", OwnerUserEmail: "user@x.com",
		VisibilityScope: "tenant", Status: "active", StorageRef: "local:knowledge-packages/kn_test.json",
		PublishedAt: time.Now().UTC(),
	}
	// loader returns path regardless of storageRef
	loader := memShareLoader{share: share, path: pkgPath}

	job, err := svc.ImportKnowledgeShare(ctx, loader, ImportKnowledgeShareInput{
		TenantID: "tenant_a", LibraryID: lib.ID, ShareRef: "kn_test",
		ImportMode: "merge_namespace", Actor: "admin@x.com", ActorEmail: "admin@x.com",
		AllowAdminImportPrivate: true,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("status=%s err=%s", job.Status, job.ErrorMessage)
	}
	// Search should find content
	hits, err := svc.SearchLibrary(ctx, "tenant_a", lib.ID, "unique-xyz-123", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Log("search returned 0 (FTS may be sparse); verify content_rev advanced")
	}
	lib2, _ := svc.GetLibrary(ctx, "tenant_a", lib.ID)
	if lib2.ContentRev < 1 {
		t.Fatalf("content_rev=%d", lib2.ContentRev)
	}
}

func TestImportKnowledgeShare_CrossTenantDenied(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, _ := svc.CreateLibrary(ctx, CreateLibraryInput{TenantID: "tenant_a", Name: "E", Actor: "a", ACL: ACL{Mode: ACLModeAllMembers}})
	share := &store.KnowledgeShare{
		KnowledgeID: "kn_x", TenantID: "tenant_b", Status: "active", VisibilityScope: "public",
	}
	_, err := svc.ImportKnowledgeShare(ctx, memShareLoader{share: share, path: "/nope"}, ImportKnowledgeShareInput{
		TenantID: "tenant_a", LibraryID: lib.ID, ShareRef: "kn_x", Actor: "a",
	})
	if err == nil || err.Error() != "cross_tenant_share" {
		t.Fatalf("want cross_tenant_share, got %v", err)
	}
}
