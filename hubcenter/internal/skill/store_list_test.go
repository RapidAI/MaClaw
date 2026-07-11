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
