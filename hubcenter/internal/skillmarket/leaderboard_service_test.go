package skillmarket

import (
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// ── Task 34.4: 排行榜功能测试 ───────────────────────────────────────────

func newTestLeaderboard(t *testing.T) (*LeaderboardService, *skill.SkillStore) {
	t.Helper()
	dir := t.TempDir()
	ss := skill.NewSkillStore(dir)
	return NewLeaderboardService(ss), ss
}

func publishTestSkill(t *testing.T, ss *skill.SkillStore, id, name string, avgRating float64, downloads int) {
	t.Helper()
	sk := skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{
			ID:          id,
			Name:        name,
			Description: "test skill " + name,
			Visible:     true,
			AvgRating:   avgRating,
			Downloads:   downloads,
			CreatedAt:   id, // 用 id 作为排序依据（字典序）
		},
	}
	if err := ss.Publish(sk); err != nil {
		t.Fatal(err)
	}
}

func TestLeaderboard_SortByRating(t *testing.T) {
	svc, ss := newTestLeaderboard(t)
	publishTestSkill(t, ss, "s1", "low-rated", 1.0, 100)
	publishTestSkill(t, ss, "s2", "high-rated", 4.5, 50)
	publishTestSkill(t, ss, "s3", "mid-rated", 3.0, 200)

	entries := svc.GetTop("rating", 10)
	if len(entries) != 3 {
		t.Fatalf("len=%d, want 3", len(entries))
	}
	if entries[0].Name != "high-rated" {
		t.Errorf("first=%s, want high-rated", entries[0].Name)
	}
}

func TestLeaderboard_SortByDownloads(t *testing.T) {
	svc, ss := newTestLeaderboard(t)
	publishTestSkill(t, ss, "s1", "few-downloads", 4.0, 10)
	publishTestSkill(t, ss, "s2", "many-downloads", 2.0, 500)
	publishTestSkill(t, ss, "s3", "mid-downloads", 3.0, 100)

	entries := svc.GetTop("downloads", 10)
	if len(entries) != 3 {
		t.Fatalf("len=%d, want 3", len(entries))
	}
	if entries[0].Name != "many-downloads" {
		t.Errorf("first=%s, want many-downloads", entries[0].Name)
	}
}

func TestLeaderboard_SortByNewest(t *testing.T) {
	svc, ss := newTestLeaderboard(t)
	publishTestSkill(t, ss, "a-old", "old-skill", 4.0, 100)
	publishTestSkill(t, ss, "c-new", "new-skill", 2.0, 50)
	publishTestSkill(t, ss, "b-mid", "mid-skill", 3.0, 75)

	entries := svc.GetTop("newest", 10)
	if len(entries) != 3 {
		t.Fatalf("len=%d, want 3", len(entries))
	}
	// CreatedAt 用 id 字典序，c-new > b-mid > a-old
	if entries[0].Name != "new-skill" {
		t.Errorf("first=%s, want new-skill", entries[0].Name)
	}
}

func TestLeaderboard_LimitBoundary(t *testing.T) {
	svc, ss := newTestLeaderboard(t)
	for i := 0; i < 5; i++ {
		publishTestSkill(t, ss, generateID(), "skill", float64(i), i*10)
	}

	// limit=0 → 默认 10
	entries := svc.GetTop("rating", 0)
	if len(entries) != 5 {
		t.Errorf("limit=0: len=%d, want 5", len(entries))
	}

	// limit=1
	entries = svc.GetTop("rating", 1)
	if len(entries) != 1 {
		t.Errorf("limit=1: len=%d, want 1", len(entries))
	}

	// limit=50
	entries = svc.GetTop("rating", 50)
	if len(entries) != 5 {
		t.Errorf("limit=50: len=%d, want 5", len(entries))
	}

	// limit=51 → capped to 50
	entries = svc.GetTop("rating", 51)
	if len(entries) != 5 {
		t.Errorf("limit=51: len=%d, want 5", len(entries))
	}
}

func TestLeaderboard_OnlyVisibleSkills(t *testing.T) {
	svc, ss := newTestLeaderboard(t)
	publishTestSkill(t, ss, "visible-1", "visible", 4.0, 100)
	publishTestSkill(t, ss, "hidden-1", "hidden", 5.0, 200)
	// 隐藏一个
	_ = ss.SetVisibility("hidden-1", false)

	entries := svc.GetTop("rating", 10)
	if len(entries) != 1 {
		t.Fatalf("len=%d, want 1", len(entries))
	}
	if entries[0].Name != "visible" {
		t.Errorf("first=%s, want visible", entries[0].Name)
	}
}

func TestLeaderboard_EmptyStore(t *testing.T) {
	svc, _ := newTestLeaderboard(t)
	entries := svc.GetTop("rating", 10)
	if entries != nil {
		t.Errorf("expected nil for empty store, got %v", entries)
	}
}
