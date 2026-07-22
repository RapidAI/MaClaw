package skill

import "testing"

func TestListAllPagedOrdersNewestSkillsFirst(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "older", Name: "Older", CreatedAt: "2026-07-10T01:00:00Z", UpdatedAt: "2026-07-10T01:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "newer", Name: "Newer", CreatedAt: "2026-07-11T01:00:00Z", UpdatedAt: "2026-07-11T01:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "middle", Name: "Middle", CreatedAt: "2026-07-10T12:00:00Z", UpdatedAt: "2026-07-10T12:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatalf("Publish(%s): %v", item.ID, err)
		}
	}

	got := store.ListAllPaged(1, 2)
	if got.Total != 3 || len(got.Skills) != 2 {
		t.Fatalf("ListAllPaged() = %#v, want first page with 2 of 3 skills", got)
	}
	if got.Skills[0].ID != "newer" || got.Skills[1].ID != "middle" {
		t.Fatalf("ListAllPaged() IDs = %q, %q; want newer, middle", got.Skills[0].ID, got.Skills[1].ID)
	}
}

func TestListAllPagedGroupsVersionsAndKeepsLatest(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "pdf-v2", SkillID: "paper.pdf-translator", Name: "PDF Translator", Version: "2.0.0", UpdatedAt: "2026-07-10T01:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "pdf-v10", SkillID: "paper.pdf-translator", Name: "PDF Translator", Version: "10.0.0", UpdatedAt: "2026-07-09T01:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "other", SkillID: "paper.other", Name: "Other", Version: "1.0.0", UpdatedAt: "2026-07-11T01:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	got := store.ListAllPaged(1, 40)
	if got.Total != 2 || len(got.Skills) != 2 {
		t.Fatalf("got %#v, want 2 grouped skills", got)
	}
	var pdf HubSkillMeta
	for _, item := range got.Skills {
		if item.SkillID == "paper.pdf-translator" {
			pdf = item
		}
	}
	if pdf.ID != "pdf-v10" || pdf.VersionCount != 2 || len(pdf.VersionHistory) != 2 {
		t.Fatalf("grouped PDF skill = %#v", pdf)
	}
	if pdf.VersionHistory[0].ID != "pdf-v10" {
		t.Fatalf("latest version should be first: %#v", pdf.VersionHistory)
	}
}

func TestListAllPagedGroupsLegacyMaclawAppVersions(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "legacy-v2", Name: "PDF翻译工具", MaclawAppName: "PDF翻译工具", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "author@example.com", Version: "2", UpdatedAt: "2026-07-20T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "legacy-v3", Name: "PDF翻译工具", MaclawAppName: "PDF翻译工具", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "author@example.com", Version: "3", UpdatedAt: "2026-07-21T00:00:00Z"}},
		// A similarly named app from a different publisher must remain separate.
		{HubSkillMeta: HubSkillMeta{ID: "other-publisher", Name: "PDF翻译工具", MaclawAppName: "PDF翻译工具", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "other@example.com", Version: "1", UpdatedAt: "2026-07-21T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	got := store.ListAllPaged(1, 40)
	if got.Total != 2 {
		t.Fatalf("ListAllPaged() total = %d, want 2 groups", got.Total)
	}
	var legacy HubSkillMeta
	for _, item := range got.Skills {
		if item.ID == "legacy-v3" {
			legacy = item
		}
	}
	if legacy.VersionCount != 2 || len(legacy.VersionHistory) != 2 {
		t.Fatalf("legacy app grouping = %#v, want 2 revisions", legacy)
	}
}

func TestListAllPagedJoinsLegacyAndSkillIDVersionsByFingerprint(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		// Existing uploads did not preserve a stable skill_id. A newer upload
		// has one, but both originate from the same publisher and skill name.
		{HubSkillMeta: HubSkillMeta{ID: "legacy-v2", Name: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "author@example.com", Fingerprint: "author@example.com:PDF Translator", Version: "2", UpdatedAt: "2026-07-20T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "modern-v3", SkillID: "paper.pdf-translator", Name: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", UploaderEmail: "author@example.com", Fingerprint: "author@example.com:PDF Translator", Version: "3", UpdatedAt: "2026-07-21T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	got := store.ListAllPaged(1, 40)
	if got.Total != 1 || len(got.Skills) != 1 {
		t.Fatalf("ListAllPaged() = %#v, want one version group", got)
	}
	if got.Skills[0].ID != "modern-v3" || got.Skills[0].VersionCount != 2 {
		t.Fatalf("grouped skill = %#v, want latest item with both revisions", got.Skills[0])
	}
	if _, err := store.GetCurrentVisible("legacy-v2"); err == nil {
		t.Fatal("GetCurrentVisible() must reject a legacy revision in a mixed identity group")
	}
	if got := store.FindBySkillID("paper.pdf-translator"); got == nil || got.ID != "modern-v3" {
		t.Fatalf("FindBySkillID() = %#v, want current revision from mixed identity group", got)
	}
}

func TestVersionGroupingDoesNotBridgeDifferentSkillIDsThroughFingerprint(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "pdf-v2", SkillID: "paper.pdf-translator", Name: "PDF Translator", Fingerprint: "author@example.com:PDF Translator", Version: "2", UpdatedAt: "2026-07-20T00:00:00Z"}},
		// A publisher could rename a skill and retain the old display name. A
		// distinct explicit skill_id must remain a distinct catalog item.
		{HubSkillMeta: HubSkillMeta{ID: "pdf-rewrite-v1", SkillID: "paper.pdf-rewriter", Name: "PDF Translator", Fingerprint: "author@example.com:PDF Translator", Version: "1", UpdatedAt: "2026-07-21T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.ListAllPaged(1, 40); got.Total != 2 {
		t.Fatalf("ListAllPaged() total = %d, want two explicit skill IDs kept separate", got.Total)
	}
}

func TestVersionGroupingRetainsLegacyAppFallbackWhenFingerprintIsAmbiguous(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "legacy-v2", Name: "PDF Translator", MaclawAppName: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "author@example.com", Fingerprint: "author@example.com:PDF Translator", Version: "2", UpdatedAt: "2026-07-20T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "modern-v3", SkillID: "paper.pdf-translator", Name: "PDF Translator", MaclawAppName: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", UploaderEmail: "author@example.com", Fingerprint: "author@example.com:PDF Translator", Version: "3", UpdatedAt: "2026-07-21T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "renamed-v1", SkillID: "paper.pdf-rewriter", Name: "PDF Translator", MaclawAppName: "PDF Rewriter", IsMaclawApp: true, ProductKind: "maclaw_app_skill", UploaderEmail: "author@example.com", Fingerprint: "author@example.com:PDF Translator", Version: "1", UpdatedAt: "2026-07-22T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	got := store.ListAllPaged(1, 40)
	if got.Total != 2 {
		t.Fatalf("ListAllPaged() total = %d, want legacy and matching app version grouped separately from renamed skill", got.Total)
	}
	for _, item := range got.Skills {
		if item.ID == "modern-v3" && item.VersionCount != 2 {
			t.Fatalf("modern version group = %#v, want legacy revision retained", item)
		}
	}
}

func TestVersionGroupingDoesNotBridgeAmbiguousLegacyAppIdentity(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "legacy-v2", Name: "PDF Translator", MaclawAppName: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", Author: "author@example.com", Version: "2", UpdatedAt: "2026-07-20T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "translator-v3", SkillID: "paper.pdf-translator", Name: "PDF Translator", MaclawAppName: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", UploaderEmail: "author@example.com", Version: "3", UpdatedAt: "2026-07-21T00:00:00Z"}},
		{HubSkillMeta: HubSkillMeta{ID: "rewriter-v1", SkillID: "paper.pdf-rewriter", Name: "PDF Translator", MaclawAppName: "PDF Translator", IsMaclawApp: true, ProductKind: "maclaw_app_skill", UploaderEmail: "author@example.com", Version: "1", UpdatedAt: "2026-07-22T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.ListAllPaged(1, 40); got.Total != 3 {
		t.Fatalf("ListAllPaged() total = %d, want ambiguous legacy identity left ungrouped", got.Total)
	}
}

func TestSearchDoesNotFallBackToOlderVisibleVersion(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "old", SkillID: "paper.pdf", Name: "PDF", Version: "1.0.0", Visible: true}},
		{HubSkillMeta: HubSkillMeta{ID: "new", SkillID: "paper.pdf", Name: "PDF", Version: "2.0.0", Visible: false}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetVisibility("new", false); err != nil {
		t.Fatal(err)
	}
	if got := store.Search("PDF", nil, 1); got.Total != 0 {
		t.Fatalf("Search() = %#v, want hidden latest skill excluded", got)
	}
}

func TestSearchMatchesCurrentRevisionOnly(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "old", SkillID: "paper.pdf", Name: "Legacy PDF Tool", Version: "1.0.0"}},
		{HubSkillMeta: HubSkillMeta{ID: "new", SkillID: "paper.pdf", Name: "Document Translator", Version: "2.0.0"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.Search("legacy", nil, 1); got.Total != 0 {
		t.Fatalf("Search() = %#v, want no match from old revision metadata", got)
	}
	if got := store.Search("translator", nil, 1); got.Total != 1 || got.Skills[0].ID != "new" {
		t.Fatalf("Search() = %#v, want current revision", got)
	}
}

func TestFindBySkillIDAndTopByDownloadsUseLatestRevision(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "old", SkillID: "paper.pdf", Name: "PDF", Version: "1.0.0", Downloads: 99}},
		{HubSkillMeta: HubSkillMeta{ID: "new", SkillID: "paper.pdf", Name: "PDF", Version: "2.0.0", Downloads: 1}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.FindBySkillID("paper.pdf"); got == nil || got.ID != "new" {
		t.Fatalf("FindBySkillID() = %#v, want new", got)
	}
	if got := store.TopByDownloads(20); len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("TopByDownloads() = %#v, want latest revision only", got)
	}
}

func TestCompareVersionStableReleaseFollowsPrerelease(t *testing.T) {
	if got := compareVersion("1.0.0", "1.0.0-rc.1"); got <= 0 {
		t.Fatalf("compareVersion stable vs prerelease = %d, want > 0", got)
	}
}

func TestCompareVersionIgnoresBuildMetadata(t *testing.T) {
	if got := compareVersion("v1.0.0+build.7", "1.0.0+build.8"); got != 0 {
		t.Fatalf("compareVersion build metadata = %d, want 0", got)
	}
}

func TestLegacyVersionLabelsFallBackToUpdatedTime(t *testing.T) {
	older := HubSkillMeta{ID: "legacy", Version: "latest", UpdatedAt: "2026-07-10T00:00:00Z"}
	newer := HubSkillMeta{ID: "release", Version: "2.0.0", UpdatedAt: "2026-07-11T00:00:00Z"}
	if !skillVersionNewer(newer, older) {
		t.Fatal("a semver release must not lose to a legacy label when it is newer by timestamp")
	}
}

func TestGetVisibleDoesNotExposeHiddenRevision(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	if err := store.Publish(HubSkillFull{HubSkillMeta: HubSkillMeta{ID: "hidden", Name: "Hidden"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVisibility("hidden", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetVisible("hidden"); err == nil {
		t.Fatal("GetVisible() should reject a hidden revision")
	}
	if got, err := store.Get("hidden"); err != nil || got.ID != "hidden" {
		t.Fatalf("Get() = %#v, %v; want admin access to hidden revision", got, err)
	}
}

func TestRateRejectsHiddenRevision(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	if err := store.Publish(HubSkillFull{HubSkillMeta: HubSkillMeta{ID: "hidden", Name: "Hidden"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVisibility("hidden", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Rate("hidden", "machine-1", 5); err == nil {
		t.Fatal("Rate() should reject a hidden revision")
	}
}

func TestHistoricalVisibleRevisionCannotBeDownloadedOrRated(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	for _, item := range []HubSkillFull{
		{HubSkillMeta: HubSkillMeta{ID: "old", SkillID: "paper.pdf", Name: "PDF", Version: "1.0.0"}},
		{HubSkillMeta: HubSkillMeta{ID: "new", SkillID: "paper.pdf", Name: "PDF", Version: "2.0.0"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.GetVisible("old"); err != nil {
		t.Fatalf("GetVisible() should allow visible history details: %v", err)
	}
	if _, err := store.GetCurrentVisible("old"); err == nil {
		t.Fatal("GetCurrentVisible() should reject a historical revision")
	}
	if got, err := store.GetCurrentVisible("new"); err != nil || got.ID != "new" {
		t.Fatalf("GetCurrentVisible() = %#v, %v; want new", got, err)
	}
	if err := store.Rate("old", "machine-1", 5); err == nil {
		t.Fatal("Rate() should reject a historical revision")
	}
}
