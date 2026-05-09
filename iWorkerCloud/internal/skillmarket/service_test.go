package skillmarket

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

func TestCreateRejectsOversizedPackage(t *testing.T) {
	repo := &testSkillRepo{items: map[string]*store.Skill{}}
	svc := NewService(repo)
	content := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", maxSkillPackageBytes+1)))
	_, err := svc.Create(context.Background(), SkillInput{
		ID:                   "oversized-skill",
		Name:                 "Oversized Skill",
		PackageFormat:        "skill.md",
		PackageContentBase64: content,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Create error = %v, want package size error", err)
	}
	if len(repo.items) != 0 {
		t.Fatalf("unexpected stored skills: %+v", repo.items)
	}
}

func TestUpdateWithoutPackagePreservesExistingPackageAndProvenance(t *testing.T) {
	repo := &testSkillRepo{items: map[string]*store.Skill{}}
	svc := NewService(repo)
	content := base64.StdEncoding.EncodeToString([]byte("name: packaged skill\n"))
	created, err := svc.Create(context.Background(), SkillInput{
		ID:                   "packaged-skill",
		Name:                 "Packaged Skill",
		PackageFormat:        "skill.md",
		PackageContentBase64: content,
		SourceCenterID:       "center-a",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	repo.items[created.ID].AvgRating = 4.8
	repo.items[created.ID].DownloadCount = 9

	updated, err := svc.Update(context.Background(), created.ID, SkillInput{
		ID:          created.ID,
		Name:        "Renamed Skill",
		Description: "metadata-only update",
		Category:    "operations",
		Version:     "1.0.1",
		Tags:        []string{"ops"},
		RiskLevel:   "medium",
		Status:      "disabled",
		Author:      "iWorkerCloud",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "Renamed Skill" || updated.Status != "disabled" {
		t.Fatalf("metadata not updated: %+v", updated)
	}
	if updated.PackageContent != created.PackageContent || updated.PackageSHA256 != created.PackageSHA256 || updated.PackageSize != created.PackageSize {
		t.Fatalf("package fields were not preserved: got content=%q sha=%q size=%d", updated.PackageContent, updated.PackageSHA256, updated.PackageSize)
	}
	if updated.PackageFormat != "skill.md" {
		t.Fatalf("PackageFormat = %q, want skill.md", updated.PackageFormat)
	}
	if updated.SourceCenterID != "center-a" {
		t.Fatalf("SourceCenterID = %q, want center-a", updated.SourceCenterID)
	}
	if updated.AvgRating != 4.8 || updated.DownloadCount != 9 {
		t.Fatalf("market counters were not preserved: rating=%v downloads=%d", updated.AvgRating, updated.DownloadCount)
	}
}

func TestSearchActiveOnlyReturnsPackagedSkillsForCenters(t *testing.T) {
	repo := &testSkillRepo{items: map[string]*store.Skill{}}
	svc := NewService(repo)
	content := base64.StdEncoding.EncodeToString([]byte("name: packaged skill\n"))
	if _, err := svc.Create(context.Background(), SkillInput{
		ID:                   "packaged-skill",
		Name:                 "Packaged Skill",
		Status:               "active",
		PackageFormat:        "skill.md",
		PackageContentBase64: content,
	}); err != nil {
		t.Fatalf("Create packaged skill: %v", err)
	}
	if _, err := svc.Create(context.Background(), SkillInput{
		ID:     "metadata-only-skill",
		Name:   "Metadata Only Skill",
		Status: "active",
	}); err != nil {
		t.Fatalf("Create metadata-only skill: %v", err)
	}
	if _, err := svc.Create(context.Background(), SkillInput{
		ID:                   "draft-packaged-skill",
		Name:                 "Draft Packaged Skill",
		Status:               "draft",
		PackageFormat:        "skill.md",
		PackageContentBase64: content,
	}); err != nil {
		t.Fatalf("Create draft packaged skill: %v", err)
	}

	results, err := svc.SearchActive(context.Background(), "")
	if err != nil {
		t.Fatalf("SearchActive() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "packaged-skill" {
		t.Fatalf("SearchActive() = %+v, want only packaged active skill", results)
	}
}

func TestGetInstallableRejectsActiveSkillWithoutPackage(t *testing.T) {
	repo := &testSkillRepo{items: map[string]*store.Skill{}}
	svc := NewService(repo)
	if _, err := svc.Create(context.Background(), SkillInput{
		ID:     "metadata-only-skill",
		Name:   "Metadata Only Skill",
		Status: "active",
	}); err != nil {
		t.Fatalf("Create metadata-only skill: %v", err)
	}

	if _, err := svc.GetInstallable(context.Background(), "metadata-only-skill"); err == nil || !strings.Contains(err.Error(), "package") {
		t.Fatalf("GetInstallable() error = %v, want package unavailable", err)
	}
}

type testSkillRepo struct {
	items map[string]*store.Skill
}

func (r *testSkillRepo) Create(_ context.Context, s *store.Skill) error {
	copy := *s
	r.items[s.ID] = &copy
	return nil
}

func (r *testSkillRepo) Update(_ context.Context, s *store.Skill) error {
	copy := *s
	r.items[s.ID] = &copy
	return nil
}

func (r *testSkillRepo) GetByID(_ context.Context, id string) (*store.Skill, error) {
	if item, ok := r.items[id]; ok {
		copy := *item
		return &copy, nil
	}
	return nil, testNotFoundError{}
}

func (r *testSkillRepo) List(context.Context) ([]*store.Skill, error) {
	out := make([]*store.Skill, 0, len(r.items))
	for _, item := range r.items {
		copy := *item
		out = append(out, &copy)
	}
	return out, nil
}

func (r *testSkillRepo) SearchActive(context.Context, string) ([]*store.Skill, error) {
	return r.List(context.Background())
}

func (r *testSkillRepo) Delete(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func (r *testSkillRepo) Count(context.Context) (int, error) {
	return len(r.items), nil
}

type testNotFoundError struct{}

func (testNotFoundError) Error() string { return "not found" }
