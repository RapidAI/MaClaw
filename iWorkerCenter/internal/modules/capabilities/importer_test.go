package capabilities

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func TestImporterSearchCloudUsesCenterSecretHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/skills/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
			t.Fatalf("X-Center-Secret = %q, want secret-abc", got)
		}
		if got := r.URL.Query().Get("q"); got != "goal" {
			t.Fatalf("q = %q, want goal", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []CloudSkill{{ID: "goal-recovery-loop", Name: "Goal recovery loop"}},
		})
	}))
	defer srv.Close()

	imp := NewImporter(nil, srv.URL, "center-1", "secret-abc")
	skills, err := imp.SearchCloud("goal")
	if err != nil {
		t.Fatalf("SearchCloud() error: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != "goal-recovery-loop" {
		t.Fatalf("skills = %+v", skills)
	}
}

func TestImporterSearchCloudRejectsTrailingJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/skills/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":"goal-recovery-loop","name":"Goal recovery loop"}]} {"results":[{"id":"extra"}]}`))
	}))
	defer srv.Close()

	imp := NewImporter(nil, srv.URL, "center-1", "secret-abc")
	if _, err := imp.SearchCloud("goal"); err == nil {
		t.Fatal("SearchCloud() error = nil, want trailing JSON rejection")
	}
}

func TestImporterImportFromCloudWritesTenantScopedCapability(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/center-1/skills/skill-1":
			if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
				t.Fatalf("X-Center-Secret = %q, want secret-abc", got)
			}
			_ = json.NewEncoder(w).Encode(CloudSkill{
				ID:          "skill-1",
				Name:        "Skill One",
				Description: "A cloud skill",
				Category:    "ops",
				Version:     "1.2.3",
				RiskLevel:   "medium",
			})
		case "/api/centers/center-1/skills/skill-1/package":
			payload := []byte("# Skill One\n")
			sum := sha256.Sum256(payload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill_id":        "skill-1",
				"version":         "1.2.3",
				"format":          "skill.md",
				"sha256":          fmt.Sprintf("%x", sum[:]),
				"size":            len(payload),
				"content_base64":  base64.StdEncoding.EncodeToString(payload),
				"source_contract": "corelib/skillmarket.PackageDownload.v1",
			})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	imp := NewImporter(provider.Write, srv.URL, "center-1", "secret-abc")
	capability, err := imp.ImportFromCloud("skill-1", "tenant-a")
	if err != nil {
		t.Fatalf("ImportFromCloud() error: %v", err)
	}
	if capability.Status != "pending_review" || capability.Source != "iworkercloud:skill-1" || capability.PackageStatus != "package_cached" {
		t.Fatalf("capability = %+v", capability)
	}

	var tenantID, source, status, packageStatus, packageContent string
	if err := provider.Read.QueryRow(`SELECT tenant_id, source, status, package_status, package_content FROM capability_packages WHERE id=?`, capability.ID).Scan(&tenantID, &source, &status, &packageStatus, &packageContent); err != nil {
		t.Fatalf("query capability: %v", err)
	}
	if tenantID != "tenant-a" || source != "iworkercloud:skill-1" || status != "pending_review" || packageStatus != "package_cached" || packageContent == "" {
		t.Fatalf("tenant/source/status/package = %q/%q/%q/%q", tenantID, source, status, packageStatus)
	}

	again, err := imp.ImportFromCloud("skill-1", "tenant-a")
	if err != nil {
		t.Fatalf("ImportFromCloud() duplicate error: %v", err)
	}
	if again.ID != capability.ID || again.Status != "pending_review" || again.PackageStatus != "package_cached" {
		t.Fatalf("duplicate import returned %+v, want existing pending_review/package_cached", again)
	}
}

func TestImporterImportFromCloudRejectsTrailingSkillJSONWithoutSaving(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/center-1/skills/skill-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"skill-1","name":"Skill One","category":"ops"} {"id":"extra"}`))
		default:
			t.Fatalf("unexpected path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	imp := NewImporter(provider.Write, srv.URL, "center-1", "secret-abc")
	if _, err := imp.ImportFromCloud("skill-1", "tenant-a"); err == nil {
		t.Fatal("ImportFromCloud() error = nil, want trailing JSON rejection")
	}

	var count int
	if err := provider.Read.QueryRow(`SELECT COUNT(*) FROM capability_packages WHERE tenant_id=?`, "tenant-a").Scan(&count); err != nil {
		t.Fatalf("query capability count: %v", err)
	}
	if count != 0 {
		t.Fatalf("capability count = %d, want 0", count)
	}
}

func TestImporterImportFromCloudRequiresDownloadablePackage(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/centers/center-1/skills/skill-no-package":
			_ = json.NewEncoder(w).Encode(CloudSkill{
				ID:        "skill-no-package",
				Name:      "Skill Without Package",
				Category:  "ops",
				Version:   "1.0.0",
				RiskLevel: "low",
			})
		case "/api/centers/center-1/skills/skill-no-package/package":
			http.Error(w, "package not found", http.StatusNotFound)
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	imp := NewImporter(provider.Write, srv.URL, "center-1", "secret-abc")
	if _, err := imp.ImportFromCloud("skill-no-package", "tenant-a"); err == nil {
		t.Fatal("ImportFromCloud() error = nil, want package download failure")
	}

	var count int
	if err := provider.Read.QueryRow(`SELECT COUNT(*) FROM capability_packages WHERE tenant_id=?`, "tenant-a").Scan(&count); err != nil {
		t.Fatalf("query capability count: %v", err)
	}
	if count != 0 {
		t.Fatalf("capability count = %d, want 0", count)
	}
}

func TestImporterApproveRejectAreTenantScoped(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO capability_packages (id, tenant_id, name, source, status) VALUES
		('cap-1', 'tenant-a', 'Cap', 'iworkercloud:cap', 'pending_review'),
		('cap-1-other', 'tenant-b', 'Cap', 'iworkercloud:cap', 'pending_review')`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	imp := NewImporter(provider.Write, "http://cloud", "center", "secret")
	if err := imp.ApproveCapability("cap-1", "tenant-a"); err != nil {
		t.Fatalf("ApproveCapability() error: %v", err)
	}
	if err := imp.RejectCapability("cap-1-other", "tenant-a"); err == nil {
		t.Fatal("RejectCapability() should not affect another tenant")
	}

	var statusA, statusB string
	_ = provider.Read.QueryRow(`SELECT status FROM capability_packages WHERE tenant_id='tenant-a' AND id='cap-1'`).Scan(&statusA)
	_ = provider.Read.QueryRow(`SELECT status FROM capability_packages WHERE tenant_id='tenant-b' AND id='cap-1-other'`).Scan(&statusB)
	if statusA != "active" || statusB != "pending_review" {
		t.Fatalf("statuses = %q/%q", statusA, statusB)
	}
}
