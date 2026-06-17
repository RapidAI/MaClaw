package skillmarket

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	hubskill "github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// ── Task 31.5: 搜索功能测试 ─────────────────────────────────────────────

func setupSearchTest(t *testing.T) (*Store, *SearchService) {
	t.Helper()
	store := newTestStore(t)
	// SearchService 需要 skill.SkillStore, 但 FTS 索引可以直接通过 IndexSkill 测试
	svc := &SearchService{store: store}
	if err := svc.migrate(); err != nil {
		t.Fatal(err)
	}
	return store, svc
}

func TestSearch_FTS5Index(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	// 索引几个 Skill
	_ = svc.IndexSkill(ctx, "s1", "Python Formatter", "Format Python code", []string{"python", "format"}, 4.5, 100, 10, "published", "2025-01-01T00:00:00Z", "1.0.0", "alice")
	_ = svc.IndexSkill(ctx, "s2", "Go Linter", "Lint Go code", []string{"go", "lint"}, 3.0, 50, 0, "published", "2025-01-02T00:00:00Z", "2.1.0", "bob")
	_ = svc.IndexSkill(ctx, "s3", "Python Debugger", "Debug Python apps", []string{"python", "debug"}, 4.0, 200, 5, "trial", "2025-01-03T00:00:00Z", "", "")

	// 搜索 "python"
	results, err := svc.Search(ctx, "python", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'python', got %d", len(results))
	}
}

func TestSearch_PrefixMatch(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Python Formatter", "Format Python code", []string{"python", "format"}, 4.5, 100, 10, "published", "2025-01-01T00:00:00Z", "", "")
	_ = svc.IndexSkill(ctx, "s2", "Go Linter", "Lint Go code", []string{"go", "lint"}, 3.0, 50, 0, "published", "2025-01-02T00:00:00Z", "", "")

	// 前缀搜索 "pyth" 应匹配 "Python"
	results, err := svc.Search(ctx, "pyth", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for prefix 'pyth', got %d", len(results))
	}

	// 前缀搜索 "form" 应匹配 "Formatter" / "Format"
	results, err = svc.Search(ctx, "form", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 1 {
		t.Errorf("expected >=1 result for prefix 'form', got %d", len(results))
	}
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"python", "python*"},
		{"pyth form", "pyth* form*"},
		{"hello-world", "hello* world*"},
		{"", ""},
		{"   ", ""},
		{"a+b", "a* b*"},
		{"中文搜索", "中文搜索*"},
		{"py 工具", "py* 工具*"},
	}
	for _, tt := range tests {
		got := sanitizeFTS5Query(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEscapeLIKE(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"100%", "100\\%"},
		{"a_b", "a\\_b"},
		{"a\\b", "a\\\\b"},
		{"%_\\", "\\%\\_\\\\"},
	}
	for _, tt := range tests {
		got := escapeLIKE(tt.input)
		if got != tt.want {
			t.Errorf("escapeLIKE(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSearch_TagsFilter(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Tool A", "desc", []string{"python", "format"}, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z", "", "")
	_ = svc.IndexSkill(ctx, "s2", "Tool B", "desc", []string{"go", "lint"}, 3.0, 50, 0, "published", "2025-01-01T00:00:00Z", "", "")

	// 仅 tags 过滤
	results, err := svc.Search(ctx, "", []string{"python"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for tag 'python', got %d", len(results))
	}
	if len(results) > 0 && results[0].ID != "s1" {
		t.Errorf("expected s1, got %s", results[0].ID)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	results, err := svc.Search(ctx, "nonexistent", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_SortByScore(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	// s1: 高评分高下载, s2: 低评分低下载
	_ = svc.IndexSkill(ctx, "s1", "Alpha Tool", "great tool", []string{"tool"}, 4.5, 500, 0, "published", "2025-01-01T00:00:00Z", "", "")
	_ = svc.IndexSkill(ctx, "s2", "Beta Tool", "okay tool", []string{"tool"}, 1.0, 5, 0, "published", "2025-01-01T00:00:00Z", "", "")

	results, err := svc.Search(ctx, "tool", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// s1 应排在前面（更高的综合得分）
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted by score: first=%.2f, second=%.2f", results[0].Score, results[1].Score)
	}
}

func TestSearch_ExcludesNonVisibleStatus(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Visible Tool", "desc", nil, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z", "", "")
	_ = svc.IndexSkill(ctx, "s2", "Hidden Tool", "desc", nil, 4.0, 100, 0, "withdrawn", "2025-01-01T00:00:00Z", "", "")
	_ = svc.IndexSkill(ctx, "s3", "Rejected Tool", "desc", nil, 4.0, 100, 0, "rejected", "2025-01-01T00:00:00Z", "", "")

	results, _ := svc.Search(ctx, "", nil, 10)
	if len(results) != 1 {
		t.Errorf("expected 1 visible result, got %d", len(results))
	}
}

func TestSearch_RemoveSkill(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Tool", "desc", nil, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z", "", "")
	_ = svc.RemoveSkill(ctx, "s1")

	results, _ := svc.Search(ctx, "", nil, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after removal, got %d", len(results))
	}
}

func TestSearch_ReturnsVersionAuthorCreatedAt(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "My Tool", "desc", nil, 4.0, 100, 0, "published", "2025-06-15T10:30:00Z", "1.2.3", "alice@example.com")

	results, err := svc.Search(ctx, "My Tool", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %q", r.Version)
	}
	if r.Author != "alice@example.com" {
		t.Errorf("expected author 'alice@example.com', got %q", r.Author)
	}
	if r.CreatedAt != "2025-06-15T10:30:00Z" {
		t.Errorf("expected created_at '2025-06-15T10:30:00Z', got %q", r.CreatedAt)
	}
}

func TestSearch_ReturnsMaclawAppProductMetadata(t *testing.T) {
	store := newTestStore(t)
	skillStore := hubskill.NewSkillStore(t.TempDir())
	svc := &SearchService{store: store, skillStore: skillStore}
	if err := svc.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := skillStore.Publish(hubskill.HubSkillFull{HubSkillMeta: hubskill.HubSkillMeta{
		ID:                           "app-skill",
		Name:                         "Invoice App",
		Description:                  "desc",
		Visible:                      true,
		ProductKind:                  "maclaw_app_skill",
		IsMaclawApp:                  true,
		MaclawAppID:                  "invoice-review",
		MaclawAppName:                "Invoice Review",
		MaclawAppDescription:         "Review invoices with a guided panel",
		MaclawAppCategory:            "finance",
		MaclawAppIcon:                "receipt",
		MaclawAppInputMode:           "file",
		MaclawAppOutputModes:         []string{"pdf", "docx"},
		MaclawAppDefinitionSHA256:    "abc123",
		MaclawAppTestEvidence:        &hubskill.MaclawAppTestEvidence{RunID: "run-ok-1", VerifiedAt: "2026-06-17T10:00:00Z", DefinitionFingerprint: "feedbeef", ArtifactPresent: true, ArtifactName: "invoice.pdf"},
		ArtifactContractRequired:     true,
		ArtifactContractOutputModes:  []string{"pdf", "docx"},
		ArtifactContractPresentation: "preview_or_file",
		Permissions:                  []string{"gui", "env:INVOICE_API_KEY", "tool:browser"},
		RequiredEnv:                  []string{"INVOICE_API_KEY"},
		RequiresGUI:                  true,
		SecurityLabels:               []string{"network", "file-output"},
	}, Files: map[string]string{
		"maclaw.app.json": base64.StdEncoding.EncodeToString([]byte(`{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","id":"invoice-review","layout":{"type":"form","localPath":"C:/secret/invoice.pdf"},"actions":[{"id":"run","type":"runSkill","secretToken":"hidden"}],"output":{"modes":["pdf"],"presentation":"preview_or_file"}}`)),
	}}); err != nil {
		t.Fatal(err)
	}
	_ = svc.IndexSkillWithProduct(ctx, "app-skill", "Invoice App", "desc", nil, 4.0, 100, 0, "published", "2026-01-01T00:00:00Z", "1.0.0", "alice", skillSearchIndexProductOptions{
		ProductKind: "maclaw_app_skill",
		IsMaclawApp: true,
	})

	results, err := svc.Search(ctx, "Invoice", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ProductKind != "maclaw_app_skill" || !results[0].IsMaclawApp {
		t.Fatalf("missing maclaw app product metadata: %#v", results[0])
	}
	if results[0].MaclawAppID != "invoice-review" || results[0].MaclawAppName != "Invoice Review" || results[0].MaclawAppDescription != "Review invoices with a guided panel" || results[0].MaclawAppCategory != "finance" || results[0].MaclawAppIcon != "receipt" {
		t.Fatalf("missing maclaw app preview metadata: %#v", results[0])
	}
	if results[0].MaclawAppInputMode != "file" || strings.Join(results[0].MaclawAppOutputModes, ",") != "pdf,docx" {
		t.Fatalf("missing maclaw app IO metadata: %#v", results[0])
	}
	if results[0].MaclawAppDefinitionSHA256 != "abc123" {
		t.Fatalf("missing maclaw app definition hash: %#v", results[0])
	}
	if results[0].MaclawAppTestEvidence == nil || results[0].MaclawAppTestEvidence.RunID != "run-ok-1" || results[0].MaclawAppTestEvidence.DefinitionFingerprint != "feedbeef" || !results[0].MaclawAppTestEvidence.ArtifactPresent || results[0].MaclawAppTestEvidence.ArtifactName != "invoice.pdf" {
		t.Fatalf("missing maclaw app test evidence: %#v", results[0])
	}
	if !results[0].ArtifactContractRequired || strings.Join(results[0].ArtifactContractOutputModes, ",") != "pdf,docx" || results[0].ArtifactContractPresentation != "preview_or_file" {
		t.Fatalf("missing artifact contract: %#v", results[0])
	}
	if strings.Join(results[0].Permissions, ",") != "gui,env:INVOICE_API_KEY,tool:browser" || strings.Join(results[0].RequiredEnv, ",") != "INVOICE_API_KEY" || !results[0].RequiresGUI || strings.Join(results[0].SecurityLabels, ",") != "network,file-output" {
		t.Fatalf("missing review governance fields: %#v", results[0])
	}
	preview := results[0].MaclawAppManifestPreview
	if preview["schema"] != "maclaw.app.v1" || preview["id"] != "invoice-review" {
		t.Fatalf("missing manifest preview identity: %#v", preview)
	}
	layout, _ := preview["layout"].(map[string]any)
	if layout == nil || layout["type"] != "form" {
		t.Fatalf("missing sanitized layout preview: %#v", preview)
	}
	if _, ok := layout["localPath"]; ok {
		t.Fatalf("manifest preview leaked path: %#v", preview)
	}
	actions, _ := preview["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("missing actions preview: %#v", preview)
	}
	action, _ := actions[0].(map[string]any)
	if _, ok := action["secretToken"]; ok {
		t.Fatalf("manifest preview leaked secret: %#v", preview)
	}
}

func TestReviewQueue_ReturnsPendingReviewAppMetadata(t *testing.T) {
	store := newTestStore(t)
	skillStore := hubskill.NewSkillStore(t.TempDir())
	svc := &SearchService{store: store, skillStore: skillStore}
	if err := svc.migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := skillStore.Publish(hubskill.HubSkillFull{HubSkillMeta: hubskill.HubSkillMeta{
		ID:                        "pending-app",
		Name:                      "Pending App",
		Visible:                   true,
		ProductKind:               "maclaw_app_skill",
		IsMaclawApp:               true,
		MaclawAppName:             "Pending Review App",
		MaclawAppDefinitionSHA256: "sha-pending",
		Permissions:               []string{"gui"},
		RequiredEnv:               []string{"APP_KEY"},
		RequiresGUI:               true,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.IndexSkillWithProduct(ctx, "pending-app", "Pending App", "desc", nil, 4.0, 1, 0, "pending_review", "2026-01-02T00:00:00Z", "1.0.0", "alice", skillSearchIndexProductOptions{ProductKind: "maclaw_app_skill", IsMaclawApp: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.IndexSkill(ctx, "published-skill", "Published", "desc", nil, 4.0, 1, 0, "published", "2026-01-01T00:00:00Z", "1.0.0", "bob"); err != nil {
		t.Fatal(err)
	}

	results, err := svc.ReviewQueue(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "pending-app" {
		t.Fatalf("expected pending app only, got %#v", results)
	}
	if !results[0].IsMaclawApp || results[0].MaclawAppName != "Pending Review App" || results[0].MaclawAppDefinitionSHA256 != "sha-pending" {
		t.Fatalf("missing pending app metadata: %#v", results[0])
	}
	if strings.Join(results[0].Permissions, ",") != "gui" || strings.Join(results[0].RequiredEnv, ",") != "APP_KEY" || !results[0].RequiresGUI {
		t.Fatalf("missing pending app review governance: %#v", results[0])
	}
}

func TestSearch_SpecialCharsOnlyQuery(t *testing.T) {
	_, svc := setupSearchTest(t)
	ctx := context.Background()

	_ = svc.IndexSkill(ctx, "s1", "Tool+++", "desc with +++", nil, 4.0, 100, 0, "published", "2025-01-01T00:00:00Z", "", "")

	// Query with only special chars falls back to LIKE (no FTS5 rank column).
	// This must not panic or return a scan error.
	results, err := svc.Search(ctx, "+++", nil, 10)
	if err != nil {
		t.Fatalf("search with special chars failed: %v", err)
	}
	// LIKE search for "+++" should match the description "desc with +++"
	if len(results) != 1 {
		t.Errorf("expected 1 result for special-chars query, got %d", len(results))
	}
}
