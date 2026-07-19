package expert

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := NewStore(db)
	if err := st.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestUpsertCreateGetAndList(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	// 服务端生成 id
	ex, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		Name:         "代码助手",
		Description:  "帮助写代码",
		Icon:         "🤖",
		SystemPrompt: "你是资深工程师",
		Tools:        []string{"bash", "read"},
		Skills:       []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("fresh insert should be applied")
	}
	if ex.ID == "" {
		t.Fatal("id missing")
	}
	if ex.TenantID != "tenant-a" {
		t.Fatalf("tenant=%q", ex.TenantID)
	}
	if ex.CreatedAt == "" || ex.UpdatedAt == "" {
		t.Fatalf("timestamps missing: %+v", ex)
	}

	got, err := st.Get(ctx, "tenant-a", ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "代码助手" || got.SystemPrompt != "你是资深工程师" {
		t.Fatalf("got=%+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "bash" || got.Tools[1] != "read" {
		t.Fatalf("tools=%v", got.Tools)
	}
	if len(got.Skills) != 1 || got.Skills[0] != "go" {
		t.Fatalf("skills=%v", got.Skills)
	}

	// 空 tools/skills 应序列化为 [] 而非 null
	ex2, _, err := st.Upsert(ctx, "tenant-a", CreateInput{Name: "无工具专家", SystemPrompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if ex2.Tools == nil || ex2.Skills == nil {
		t.Fatal("empty tools/skills should be non-nil")
	}

	list, err := st.List(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len=%d", len(list))
	}
}

func TestUpsertWithClientIDAndLWW(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	ex, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID:           "fixed-id",
		Name:         "v1",
		SystemPrompt: "p",
		UpdatedAt:    old,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || ex.ID != "fixed-id" || ex.Name != "v1" {
		t.Fatalf("ex=%+v applied=%v", ex, applied)
	}
	created0 := ex.CreatedAt

	// 更新的写入（updated_at 更晚）应覆盖
	newer := time.Now().UTC().Format(time.RFC3339)
	ex, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID:           "fixed-id",
		Name:         "v2",
		SystemPrompt: "p",
		UpdatedAt:    newer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || ex.Name != "v2" || ex.UpdatedAt != newer {
		t.Fatalf("LWW newer failed: %+v applied=%v", ex, applied)
	}

	// 过期写入（updated_at 更早）不应覆盖，applied=false
	ex, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID:           "fixed-id",
		Name:         "v0-stale",
		SystemPrompt: "p",
		UpdatedAt:    old,
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("stale write should report applied=false")
	}
	if ex.Name != "v2" || ex.UpdatedAt != newer {
		t.Fatalf("LWW stale write overwrote: %+v", ex)
	}

	// created_at 保留首次创建值
	got, err := st.Get(ctx, "tenant-a", "fixed-id")
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedAt != created0 {
		t.Fatalf("created_at should be preserved: want %q, got %q", created0, got.CreatedAt)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	ex, _, err := st.Upsert(ctx, "tenant-a", CreateInput{
		Name:         "原始",
		SystemPrompt: "p",
		Tools:        []string{"a"},
		UpdatedAt:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}

	newName := "改名"
	newDesc := "新描述"
	upd, err := st.Update(ctx, "tenant-a", ex.ID, UpdateInput{Name: &newName, Description: &newDesc})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "改名" || upd.Description != "新描述" {
		t.Fatalf("upd=%+v", upd)
	}
	// 未提供的字段保持不变
	if len(upd.Tools) != 1 || upd.Tools[0] != "a" {
		t.Fatalf("tools changed unexpectedly: %v", upd.Tools)
	}
	if upd.UpdatedAt <= ex.UpdatedAt {
		t.Fatal("updated_at not bumped")
	}

	// 空 name 拒绝
	empty := "  "
	if _, err := st.Update(ctx, "tenant-a", ex.ID, UpdateInput{Name: &empty}); err == nil {
		t.Fatal("expected error for empty name")
	}
	// 空 system_prompt 拒绝
	if _, err := st.Update(ctx, "tenant-a", ex.ID, UpdateInput{SystemPrompt: &empty}); err == nil {
		t.Fatal("expected error for empty system_prompt")
	}

	if err := st.Delete(ctx, "tenant-a", ex.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, "tenant-a", ex.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
	if err := st.Delete(ctx, "tenant-a", ex.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows on second delete, got %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	ex, _, err := st.Upsert(ctx, "tenant-a", CreateInput{ID: "shared-id", Name: "A 的专家", SystemPrompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	// 同 id 不同 tenant 互不影响
	if _, _, err := st.Upsert(ctx, "tenant-b", CreateInput{ID: "shared-id", Name: "B 的专家", SystemPrompt: "p"}); err != nil {
		t.Fatal(err)
	}

	gotA, err := st.Get(ctx, "tenant-a", ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Name != "A 的专家" {
		t.Fatalf("tenant-a got %q", gotA.Name)
	}

	listB, err := st.List(ctx, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(listB) != 1 || listB[0].Name != "B 的专家" {
		t.Fatalf("tenant-b list=%+v", listB)
	}

	// B 不能 update / delete A 的资源
	name := "x"
	if _, err := st.Update(ctx, "tenant-c", ex.ID, UpdateInput{Name: &name}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-tenant update: %v", err)
	}
	if err := st.Delete(ctx, "tenant-c", ex.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-tenant delete: %v", err)
	}
}

func TestListOrderByUpdatedAtDesc(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"e1", "e2", "e3"} {
		if _, _, err := st.Upsert(ctx, "tenant-a", CreateInput{
			ID:           id,
			Name:         id,
			SystemPrompt: "p",
			UpdatedAt:    base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := st.List(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != "e3" || list[2].ID != "e1" {
		t.Fatalf("order wrong: %+v", list)
	}
}

func TestUpsertValidationErrors(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	base := CreateInput{Name: "n", SystemPrompt: "p"}

	// 非法 updated_at
	in := base
	in.UpdatedAt = "not-a-time"
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
		t.Fatal("expected error for invalid updated_at")
	}

	// 空 name / 超长 name
	in = base
	in.Name = "  "
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
		t.Fatal("expected error for empty name")
	}
	in = base
	in.Name = strings.Repeat("x", maxNameRunes+1)
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
		t.Fatal("expected error for too-long name")
	}

	// 空 / 超长 system_prompt
	in = base
	in.SystemPrompt = "   "
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
		t.Fatal("expected error for empty system_prompt")
	}
	in = base
	in.SystemPrompt = strings.Repeat("x", maxSystemPromptBytes+1)
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
		t.Fatal("expected error for too-long system_prompt")
	}

	// 非法 id 字符集
	for _, badID := range []string{"bad id", "a/b", "id!" + "x", strings.Repeat("a", 129)} {
		in = base
		in.ID = badID
		if _, _, err := st.Upsert(ctx, "tenant-a", in); err == nil {
			t.Fatalf("expected error for id %q", badID)
		}
	}
	// 合法 id 通过
	in = base
	in.ID = "ok_id-1.2"
	if _, _, err := st.Upsert(ctx, "tenant-a", in); err != nil {
		t.Fatalf("valid id rejected: %v", err)
	}

	// 校验错误应是 ValidationError（handler 据此映射 400）
	in = base
	in.Name = ""
	var verr *ValidationError
	if _, _, err := st.Upsert(ctx, "tenant-a", in); !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError, got %T %v", err, err)
	}
}

func TestTimestampOffsetNormalization(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("UTC+8", 8*3600)

	// 带 +08:00 offset 的时间应归一化为 UTC
	t1 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	s1 := t1.In(loc).Format(time.RFC3339)
	ex, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "v1", SystemPrompt: "p", UpdatedAt: s1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("fresh insert should be applied")
	}
	if !strings.HasSuffix(ex.UpdatedAt, "Z") {
		t.Fatalf("updated_at not normalized to UTC: %q", ex.UpdatedAt)
	}
	parsed, err := time.Parse(time.RFC3339Nano, ex.UpdatedAt)
	if err != nil || !parsed.Equal(t1) {
		t.Fatalf("normalized instant wrong: %q err=%v", ex.UpdatedAt, err)
	}

	// 同一时刻的 UTC 表达（相等）应满足 >= 条件而覆盖
	same := t1.Format(time.RFC3339)
	ex, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "v1-eq", SystemPrompt: "p", UpdatedAt: same,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || ex.Name != "v1-eq" {
		t.Fatalf("equal-instant write should apply: %+v applied=%v", ex, applied)
	}

	// offset 表达的更晚时刻应 LWW 胜出
	t2 := t1.Add(time.Hour)
	ex, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "v2", SystemPrompt: "p", UpdatedAt: t2.In(loc).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied || ex.Name != "v2" {
		t.Fatalf("offset newer write should win: %+v applied=%v", ex, applied)
	}

	// UTC 表达的更早时刻不得覆盖
	ex, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "v0", SystemPrompt: "p", UpdatedAt: t1.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied || ex.Name != "v2" {
		t.Fatalf("stale UTC write should lose: %+v applied=%v", ex, applied)
	}
}

func TestFutureTimestampClamped(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	before := time.Now().UTC()
	ex, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "n", SystemPrompt: "p", UpdatedAt: future,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("clamped write should still apply")
	}
	parsed, err := time.Parse(time.RFC3339Nano, ex.UpdatedAt)
	if err != nil {
		t.Fatalf("stored updated_at unparseable: %q", ex.UpdatedAt)
	}
	// 钳制到调用时刻附近，而不是客户端给的一小时后
	if parsed.After(time.Now().UTC().Add(time.Minute)) || parsed.Before(before.Add(-time.Minute)) {
		t.Fatalf("future timestamp not clamped: %q", ex.UpdatedAt)
	}
}

func TestTombstoneSemantics(t *testing.T) {
	st := openTestDB(t)
	ctx := context.Background()

	t1 := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, _, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "v1", SystemPrompt: "p", UpdatedAt: t1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx, "tenant-a", "e1"); err != nil {
		t.Fatal(err)
	}

	// 删除后同 updated_at 的重放写不得复活（applied=false）
	_, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "zombie", SystemPrompt: "p", UpdatedAt: t1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("replay at deleted timestamp should be rejected (applied=false)")
	}
	if _, err := st.Get(ctx, "tenant-a", "e1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("zombie resurrected: %v", err)
	}

	// 更早的重放写同样不得复活
	older := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, applied, err = st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "zombie", SystemPrompt: "p", UpdatedAt: older,
	}); err != nil || applied {
		t.Fatalf("older replay: applied=%v err=%v", applied, err)
	}

	// 比墓碑新的写入正常复活，并清掉墓碑（+1s 确保严格晚于 deleted_at 的纳秒值）
	newer := time.Now().UTC().Add(time.Second).Format(time.RFC3339)
	ex, applied, err := st.Upsert(ctx, "tenant-a", CreateInput{
		ID: "e1", Name: "reborn", SystemPrompt: "p", UpdatedAt: newer,
	})
	if err != nil || !applied || ex.Name != "reborn" {
		t.Fatalf("newer write after delete: %+v applied=%v err=%v", ex, applied, err)
	}
	if _, err := st.Get(ctx, "tenant-a", "e1"); err != nil {
		t.Fatalf("reborn expert missing: %v", err)
	}
	var tombCount int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM expert_tombstones WHERE tenant_id='tenant-a' AND id='e1'`).Scan(&tombCount); err != nil {
		t.Fatal(err)
	}
	if tombCount != 0 {
		t.Fatalf("tombstone should be cleared, count=%d", tombCount)
	}

	// 墓碑不跨租户：tenant-b 用同 id 创建不受 tenant-a 墓碑影响（先删一次制造墓碑场景）
	if err := st.Delete(ctx, "tenant-a", "e1"); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := st.Upsert(ctx, "tenant-b", CreateInput{
		ID: "e1", Name: "B 的", SystemPrompt: "p", UpdatedAt: older,
	}); err != nil || !applied {
		t.Fatalf("cross-tenant tombstone leaked: applied=%v err=%v", applied, err)
	}
}
