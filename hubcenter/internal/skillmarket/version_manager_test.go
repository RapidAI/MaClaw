package skillmarket

import (
	"context"
	"testing"
	"time"
)

// ── Task 21.6: 版本管理属性测试 ─────────────────────────────────────────

func TestVersionManager_MonotonicVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	vm := NewVersionManager(store)

	fingerprint := "dev@test.com:my-skill"

	// 第一次提交：新建
	res, err := vm.ResolveSubmission(ctx, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsUpgrade || res.NextVersion != 1 {
		t.Errorf("first submission: isUpgrade=%v nextVersion=%d, want false/1", res.IsUpgrade, res.NextVersion)
	}

	// 模拟成功提交
	for i := 1; i <= 5; i++ {
		sub := &SkillSubmission{
			ID:          generateID(),
			Email:       "dev@test.com",
			Fingerprint: fingerprint,
			Status:      "success",
			SkillID:     generateID(),
			ZipPath:     "/tmp/test.zip",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := store.CreateSubmission(ctx, sub); err != nil {
			t.Fatal(err)
		}

		res, err := vm.ResolveSubmission(ctx, fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		expectedVersion := i + 1
		if !res.IsUpgrade {
			t.Errorf("submission %d: expected isUpgrade=true", i+1)
		}
		if res.NextVersion != expectedVersion {
			t.Errorf("submission %d: nextVersion=%d, want %d", i+1, res.NextVersion, expectedVersion)
		}
	}
}

func TestVersionManager_DedupConsistency(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	vm := NewVersionManager(store)

	// 两个不同 fingerprint 应独立计版本
	fp1 := "alice@test.com:skill-a"
	fp2 := "bob@test.com:skill-b"

	// fp1 提交 3 次
	for i := 0; i < 3; i++ {
		sub := &SkillSubmission{
			ID: generateID(), Email: "alice@test.com", Fingerprint: fp1,
			Status: "success", SkillID: generateID(), ZipPath: "/tmp/a.zip",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		_ = store.CreateSubmission(ctx, sub)
	}

	// fp2 提交 1 次
	sub := &SkillSubmission{
		ID: generateID(), Email: "bob@test.com", Fingerprint: fp2,
		Status: "success", SkillID: generateID(), ZipPath: "/tmp/b.zip",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.CreateSubmission(ctx, sub)

	res1, _ := vm.ResolveSubmission(ctx, fp1)
	res2, _ := vm.ResolveSubmission(ctx, fp2)

	if res1.NextVersion != 4 {
		t.Errorf("fp1 nextVersion=%d, want 4", res1.NextVersion)
	}
	if res2.NextVersion != 2 {
		t.Errorf("fp2 nextVersion=%d, want 2", res2.NextVersion)
	}
}

func TestVersionManager_NewFingerprint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	vm := NewVersionManager(store)

	res, err := vm.ResolveSubmission(ctx, "new@test.com:brand-new-skill")
	if err != nil {
		t.Fatal(err)
	}
	if res.IsUpgrade {
		t.Error("new fingerprint should not be upgrade")
	}
	if res.NextVersion != 1 {
		t.Errorf("nextVersion=%d, want 1", res.NextVersion)
	}
	if res.PrevSkillID != "" {
		t.Errorf("prevSkillID=%s, want empty", res.PrevSkillID)
	}
}
