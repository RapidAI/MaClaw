package cloudworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestAtomicIdempotencyConcurrentLoserReplaysAndRollsBack(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	encoder := func(value any) (int, []byte, error) {
		ws, _ := value.(*Workspace)
		raw, err := json.Marshal(map[string]any{"id": ws.ID, "name": ws.Name})
		return 201, raw, err
	}
	firstCtx, replay, err := st.PrepareAtomicIdempotency(ctx, "t1", "u1", "", "cwi-1", "create:key", "same-payload", now, encoder)
	if err != nil || replay != nil {
		t.Fatalf("first prepare replay=%+v err=%v", replay, err)
	}
	secondCtx, replay, err := st.PrepareAtomicIdempotency(ctx, "t1", "u1", "", "cwi-1", "create:key", "same-payload", now, encoder)
	if err != nil || replay != nil {
		t.Fatalf("second prepare replay=%+v err=%v", replay, err)
	}
	first, err := st.Create(firstCtx, CreateParams{TenantID: "t1", UserID: "u1", Name: "winner", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(secondCtx, CreateParams{TenantID: "t1", UserID: "u1", Name: "loser", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now); err == nil {
		t.Fatal("concurrent loser unexpectedly committed")
	} else if record, ok := IdempotencyReplay(err); !ok || record.StatusCode != 201 || !record.Completed {
		t.Fatalf("loser err=%v replay=%+v", err, record)
	}
	rows, err := st.ListOwned(ctx, "t1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != first.ID || rows[0].Name != "winner" {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestAtomicIdempotencyRejectsDifferentConcurrentPayload(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	encoder := func(value any) (int, []byte, error) { return 200, []byte(`{"ok":true}`), nil }
	firstCtx, _, err := st.PrepareAtomicIdempotency(ctx, "t1", "u1", "", "cwi-1", "key", "payload-a", now, encoder)
	if err != nil {
		t.Fatal(err)
	}
	secondCtx, _, err := st.PrepareAtomicIdempotency(ctx, "t1", "u1", "", "cwi-1", "key", "payload-b", now, encoder)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(firstCtx, CreateParams{TenantID: "t1", UserID: "u1", Name: "winner", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(secondCtx, CreateParams{TenantID: "t1", UserID: "u1", Name: "loser", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("different payload err=%v", err)
	}
}
